// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package knowntypes

import (
	"context"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

// CollectableOwnerKey is the field index name. Each indexed object stores the key
// of its controller, so "everything owned by this KRMConfig" is one List per kind.
const CollectableOwnerKey = ".metadata.controller"

// Collectable is one kind an operand is allowed to own.
//
// The operator prunes: anything it created that is no longer desired gets deleted.
// That is only safe if it can list what it currently owns, which needs a field
// index and a watch per kind. A Collectable bundles the four wirings that make one
// kind ownable.
type Collectable struct {
	// Collect lists the objects of this kind owned by owner, keyed by GetKey.
	Collect func(ctx context.Context, runtimeClient client.Client, owner client.Object) (map[string]client.Object, error)

	// InitWithManager registers the field index. Once per kind per manager only —
	// IndexField panics on a duplicate.
	InitWithManager func(ctx context.Context, mgr manager.Manager) error

	// InitWithBuilder adds the watch, so editing an owned object re-reconciles its owner.
	InitWithBuilder func(b *builder.Builder) *builder.Builder

	// InitWithFakeClientBuilder registers the same index on a fake client, so tests
	// exercise the real lookup path.
	InitWithFakeClientBuilder func(fb *fake.ClientBuilder)
}

var (
	// KRMConfigOwned is every kind a KRMConfig operand may own; the reconciler
	// collects current state from all of them. Populated by init below.
	KRMConfigOwned []*Collectable

	// Initiated records which collectables already had their index registered, so
	// a second reconciler sharing this registry skips them instead of panicking.
	Initiated []*Collectable
)

// Only the kinds the KRM services actually create. Adding one here is what makes
// the operator able to own, watch and prune it.
func init() {
	register[appsv1.Deployment, appsv1.DeploymentList](
		appsv1.SchemeGroupVersion.WithKind("Deployment"),
		func(l *appsv1.DeploymentList) []appsv1.Deployment { return l.Items })
	register[corev1.ServiceAccount, corev1.ServiceAccountList](
		corev1.SchemeGroupVersion.WithKind("ServiceAccount"),
		func(l *corev1.ServiceAccountList) []corev1.ServiceAccount { return l.Items })
	register[corev1.Service, corev1.ServiceList](
		corev1.SchemeGroupVersion.WithKind("Service"),
		func(l *corev1.ServiceList) []corev1.Service { return l.Items })
	register[corev1.ConfigMap, corev1.ConfigMapList](
		corev1.SchemeGroupVersion.WithKind("ConfigMap"),
		func(l *corev1.ConfigMapList) []corev1.ConfigMap { return l.Items })

	// Admission webhooks and their TLS secrets stay with the Helm chart, which
	// mints the certificates; the operator does not own them.

	// These CRDs come from components KRM does not install, so their watches must
	// not be started when absent.
	registerOptional[monitoringv1.ServiceMonitor, monitoringv1.ServiceMonitorList](
		monitoringv1.SchemeGroupVersion.WithKind(monitoringv1.ServiceMonitorsKind),
		func(l *monitoringv1.ServiceMonitorList) []monitoringv1.ServiceMonitor { return l.Items })
	registerOptional[vpav1.VerticalPodAutoscaler, vpav1.VerticalPodAutoscalerList](
		vpav1.SchemeGroupVersion.WithKind("VerticalPodAutoscaler"),
		func(l *vpav1.VerticalPodAutoscalerList) []vpav1.VerticalPodAutoscaler { return l.Items })
}

// pointerObject says "PT is *T, and *T implements client.Object". The methods are
// on the pointer, but a typed List gives back values, so register needs both the
// value type T (to range over Items) and its pointer PT (to use as an Object).
type pointerObject[T any] interface {
	client.Object
	*T
}

// pointerObjectList is the same trick for the List type.
type pointerObjectList[L any] interface {
	client.ObjectList
	*L
}

// register makes one kind ownable and adds it to KRMConfigOwned. Replaces the
// per-kind indexer/register/collect boilerplate with a single instantiation:
//
//	register[corev1.Service, corev1.ServiceList](
//	    corev1.SchemeGroupVersion.WithKind("Service"),
//	    func(l *corev1.ServiceList) []corev1.Service { return l.Items })
//
// itemsOf is passed in because Go has no way to reach .Items on an arbitrary list.
func register[
	T any,
	L any,
	PT pointerObject[T],
	PL pointerObjectList[L],
](
	gvk schema.GroupVersionKind,
	itemsOf func(PL) []T,
) *Collectable {
	// Runs on every object of this kind in the cache and returns the index values
	// to store for it. Returning nil leaves the object out of the index entirely.
	indexer := func(obj client.Object) []string {
		owner := metav1.GetControllerOf(obj)
		if !ownedByKAIResourcesKind(owner) {
			return nil
		}
		return []string{ownerKey(owner)}
	}

	collectable := &Collectable{
		Collect: func(
			ctx context.Context, runtimeClient client.Client, owner client.Object,
		) (map[string]client.Object, error) {
			l := PL(new(L))
			if err := runtimeClient.List(
				ctx, l, client.MatchingFields{CollectableOwnerKey: ReconcilerKey(owner)}); err != nil {
				return nil, err
			}

			items := itemsOf(l)
			collected := make(map[string]client.Object, len(items))
			for index := range items {
				obj := PT(&items[index])
				// A typed object read back from the API server has no TypeMeta,
				// and both the diff key and the equality check include the GVK.
				obj.GetObjectKind().SetGroupVersionKind(gvk)
				collected[GetKey(gvk, obj.GetNamespace(), obj.GetName())] = obj
			}
			return collected, nil
		},
		InitWithManager: func(ctx context.Context, mgr manager.Manager) error {
			return mgr.GetFieldIndexer().IndexField(
				ctx, PT(new(T)), CollectableOwnerKey, indexer)
		},
		InitWithBuilder: func(b *builder.Builder) *builder.Builder {
			return b.Owns(PT(new(T)))
		},
		InitWithFakeClientBuilder: func(fb *fake.ClientBuilder) {
			fb.WithIndex(PT(new(T)), CollectableOwnerKey, indexer)
		},
	}

	KRMConfigOwned = append(KRMConfigOwned, collectable)
	return collectable
}

// registerOptional is register for a kind whose CRD may not be installed. It wraps
// the three functions so that, when the CRD is absent, the index and watch are
// skipped and Collect reports nothing — instead of the manager failing to start.
func registerOptional[
	T any,
	L any,
	PT pointerObject[T],
	PL pointerObjectList[L],
](
	gvk schema.GroupVersionKind,
	itemsOf func(PL) []T,
) *Collectable {
	collectable := register[T, L, PT, PL](gvk, itemsOf)

	// Resolved once at manager start; a CRD installed later is picked up on the
	// next restart, which is also when its watch could first be established.
	var available bool
	collect := collectable.Collect
	initWithManager := collectable.InitWithManager
	initWithBuilder := collectable.InitWithBuilder

	collectable.Collect = func(
		ctx context.Context, runtimeClient client.Client, owner client.Object,
	) (map[string]client.Object, error) {
		if !available {
			return map[string]client.Object{}, nil
		}
		return collect(ctx, runtimeClient, owner)
	}
	collectable.InitWithManager = func(ctx context.Context, mgr manager.Manager) error {
		// Asked of the RESTMapper so the operator needs no permission on CRDs.
		_, err := mgr.GetRESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
		switch {
		case meta.IsNoMatchError(err):
			available = false
			return nil
		case err != nil:
			return err
		}
		available = true
		return initWithManager(ctx, mgr)
	}
	collectable.InitWithBuilder = func(b *builder.Builder) *builder.Builder {
		if !available {
			return b
		}
		return initWithBuilder(b)
	}
	return collectable
}

// GetKey matches a desired object to a current one. The GVK is part of the key, so
// a Service and a ConfigMap of the same name are never confused.
func GetKey(gvk schema.GroupVersionKind, namespace, name string) string {
	return gvk.String() + "/" + types.NamespacedName{Namespace: namespace, Name: name}.String()
}

// ReconcilerKey is the value to look up in the index; ownerKey is the value stored
// there. They must produce the same string for the same object.
func ReconcilerKey(reconciledObject client.Object) string {
	return reconciledObject.GetObjectKind().GroupVersionKind().Kind + "/" + reconciledObject.GetName()
}

func ownerKey(owner *metav1.OwnerReference) string {
	return owner.Kind + "/" + owner.Name
}

// ownedByKAIResourcesKind admits any owner in this API group, not only a KRMConfig:
// nodepool-controller owns objects through a NodePool, which shares the group. What
// keeps the two apart is Collect, which looks up ReconcilerKey — kind and name — so
// an object owned by NodePool/x is never collected for KRMConfig/y, and never pruned
// on its behalf.
func ownedByKAIResourcesKind(owner *metav1.OwnerReference) bool {
	return owner != nil && owner.APIVersion == krmv1alpha1.GroupVersion.String()
}
