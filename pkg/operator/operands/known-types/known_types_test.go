// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package knowntypes

import (
	"context"
	"testing"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKnownTypes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Known types suite")
}

const testNamespace = "kai-resource-management"

func krmConfigOwner() *krmv1alpha1.KRMConfig {
	owner := &krmv1alpha1.KRMConfig{
		ObjectMeta: metav1.ObjectMeta{Name: krmv1alpha1.KRMConfigSingletonName},
	}
	owner.SetGroupVersionKind(krmv1alpha1.GroupVersion.WithKind(krmv1alpha1.KRMConfigKind))
	return owner
}

func ownedBy(apiVersion, kind, name string) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Controller: ptr.To(true),
	}}
}

func newClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(krmv1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())
	Expect(vpav1.AddToScheme(scheme)).To(Succeed())

	clientBuilder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...)
	for _, collectable := range KRMConfigOwned {
		collectable.InitWithFakeClientBuilder(clientBuilder)
	}
	return clientBuilder.Build()
}

// collectAll merges what every registered kind reports, so a test asserting "only
// this object is collected" covers the whole registry rather than one kind.
func collectAll(ctx context.Context, runtimeClient client.Client, owner client.Object) map[string]client.Object {
	collected := map[string]client.Object{}
	for _, collectable := range KRMConfigOwned {
		found, err := collectable.Collect(ctx, runtimeClient, owner)
		Expect(err).ToNot(HaveOccurred())
		for key, object := range found {
			collected[key] = object
		}
	}
	return collected
}

var _ = Describe("GetKey", func() {
	It("distinguishes two kinds that share a name", func() {
		configMapKey := GetKey(corev1.SchemeGroupVersion.WithKind("ConfigMap"), testNamespace, "settings")
		serviceKey := GetKey(corev1.SchemeGroupVersion.WithKind("Service"), testNamespace, "settings")

		Expect(configMapKey).ToNot(Equal(serviceKey))
	})

	It("distinguishes the same name in two namespaces", func() {
		gvk := corev1.SchemeGroupVersion.WithKind("ConfigMap")

		Expect(GetKey(gvk, "one", "settings")).ToNot(Equal(GetKey(gvk, "two", "settings")))
	})
})

var _ = Describe("Collect", func() {
	ctx := context.Background()

	It("finds an object the KRMConfig controls", func() {
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "nodepool-controller",
				Namespace:       testNamespace,
				OwnerReferences: ownedBy(krmv1alpha1.GroupVersion.String(), krmv1alpha1.KRMConfigKind, krmv1alpha1.KRMConfigSingletonName),
			},
		}

		collected := collectAll(ctx, newClient(deployment), krmConfigOwner())

		Expect(collected).To(HaveLen(1))
		Expect(collected).To(HaveKey(GetKey(
			appsv1.SchemeGroupVersion.WithKind("Deployment"), testNamespace, "nodepool-controller")))
	})

	// A typed object read back from the API server carries no TypeMeta, and both
	// the diff key and the equality check include the GVK.
	It("stamps the GroupVersionKind on what it collects", func() {
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "settings",
				Namespace:       testNamespace,
				OwnerReferences: ownedBy(krmv1alpha1.GroupVersion.String(), krmv1alpha1.KRMConfigKind, krmv1alpha1.KRMConfigSingletonName),
			},
		}

		collected := collectAll(ctx, newClient(configMap), krmConfigOwner())

		Expect(collected).To(HaveLen(1))
		for _, object := range collected {
			Expect(object.GetObjectKind().GroupVersionKind().Kind).To(Equal("ConfigMap"))
		}
	})

	// Pruning deletes what is collected but not desired, so collecting a foreign
	// object would mean deleting it.
	It("ignores an object controlled by something else", func() {
		foreign := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "someone-elses",
				Namespace:       testNamespace,
				OwnerReferences: ownedBy("apps/v1", "Deployment", "another-owner"),
			},
		}

		Expect(collectAll(ctx, newClient(foreign), krmConfigOwner())).To(BeEmpty())
	})

	It("ignores an object with no controller at all", func() {
		orphan := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "orphan", Namespace: testNamespace},
		}

		Expect(collectAll(ctx, newClient(orphan), krmConfigOwner())).To(BeEmpty())
	})

	// nodepool-controller owns SchedulingShards and ServiceMonitors through a
	// NodePool, which is in the same API group as KRMConfig and so passes the index
	// predicate. Only the kind in ReconcilerKey keeps the two apart, and pruning
	// deletes whatever Collect returns — so a regression here has the operator
	// deleting the other controller's objects.
	It("ignores an object owned by a NodePool, which shares the API group", func() {
		nodePoolOwned := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "shard-settings",
				Namespace:       testNamespace,
				OwnerReferences: ownedBy(krmv1alpha1.GroupVersion.String(), "NodePool", "default"),
			},
		}

		Expect(collectAll(ctx, newClient(nodePoolOwned), krmConfigOwner())).To(BeEmpty())
	})

	It("ignores an object owned by a different KRMConfig", func() {
		otherConfig := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "other",
				Namespace:       testNamespace,
				OwnerReferences: ownedBy(krmv1alpha1.GroupVersion.String(), krmv1alpha1.KRMConfigKind, "a-different-config"),
			},
		}

		Expect(collectAll(ctx, newClient(otherConfig), krmConfigOwner())).To(BeEmpty())
	})

	// registerOptional resolves availability when the manager starts. These tests
	// never start one, so the optional kinds stay unavailable and must report
	// nothing rather than failing.
	It("reports nothing for a kind whose CRD was never resolved", func() {
		serviceMonitor := &monitoringv1.ServiceMonitor{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "nodepool-controller",
				Namespace:       testNamespace,
				OwnerReferences: ownedBy(krmv1alpha1.GroupVersion.String(), krmv1alpha1.KRMConfigKind, krmv1alpha1.KRMConfigSingletonName),
			},
		}

		Expect(collectAll(ctx, newClient(serviceMonitor), krmConfigOwner())).To(BeEmpty())
	})
})

var _ = Describe("VPAFieldInherit", func() {
	It("keeps the status the recommender wrote", func() {
		current := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{ResourceVersion: "7"},
			Status: vpav1.VerticalPodAutoscalerStatus{
				Conditions: []vpav1.VerticalPodAutoscalerCondition{{Type: vpav1.RecommendationProvided}},
			},
		}
		desired := &vpav1.VerticalPodAutoscaler{}

		VPAFieldInherit(current, desired)

		Expect(desired.Status.Conditions).To(HaveLen(1))
		Expect(desired.ResourceVersion).To(Equal("7"))
	})

	It("keeps annotations the operator does not set", func() {
		current := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"added-by": "an-admin"}},
		}
		desired := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"ours": "yes"}},
		}

		VPAFieldInherit(current, desired)

		Expect(desired.Annotations).To(HaveKeyWithValue("added-by", "an-admin"))
		Expect(desired.Annotations).To(HaveKeyWithValue("ours", "yes"))
	})

	It("lets the operator's own value win", func() {
		current := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"shared": "theirs"}},
		}
		desired := &vpav1.VerticalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"shared": "ours"}},
		}

		VPAFieldInherit(current, desired)

		Expect(desired.Annotations).To(HaveKeyWithValue("shared", "ours"))
	})
})
