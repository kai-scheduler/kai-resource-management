// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"errors"
	"fmt"
	"time"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

const cleanupNamespace = "kai-resource-management"

func managedDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels:    map[string]string{common.OperatorManagedByLabelKey: common.OperatorManagedByLabelValue},
	}}
}

func newCleanupClient(funcs interceptor.Funcs, objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kaires.AddToScheme(scheme))
	utilruntime.Must(kaiv1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
}

const nodePoolFinalizer = "nodepool.kai.scheduler/finalize"

func nodePoolWithFinalizers(name string, finalizers ...string) *kaires.NodePool {
	return &kaires.NodePool{ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: finalizers}}
}

var _ = Describe("Cleanup in co-install mode", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("strips the finalizer from every NodePool and keeps foreign ones", func() {
		k8sClient := newCleanupClient(interceptor.Funcs{},
			nodePoolWithFinalizers("default", nodePoolFinalizer),
			nodePoolWithFinalizers("user-created", nodePoolFinalizer, "example.com/keep"))

		Expect(Cleanup(ctx, k8sClient,
			CleanupOptions{Namespace: cleanupNamespace, NodePoolFinalizer: nodePoolFinalizer})).To(Succeed())

		nodePool := &kaires.NodePool{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, nodePool)).To(Succeed())
		Expect(nodePool.Finalizers).To(BeEmpty())
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "user-created"}, nodePool)).To(Succeed())
		Expect(nodePool.Finalizers).To(ConsistOf("example.com/keep"))
	})

	It("leaves NodePool finalizers alone when none is named", func() {
		k8sClient := newCleanupClient(interceptor.Funcs{}, nodePoolWithFinalizers("default", nodePoolFinalizer))

		Expect(Cleanup(ctx, k8sClient, CleanupOptions{Namespace: cleanupNamespace})).To(Succeed())

		nodePool := &kaires.NodePool{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, nodePool)).To(Succeed())
		Expect(nodePool.Finalizers).To(ConsistOf(nodePoolFinalizer))
	})

	It("succeeds when the NodePool CRD is gone", func() {
		k8sClient := newCleanupClient(interceptor.Funcs{
			List: func(ctx context.Context, wrapped client.WithWatch, list client.ObjectList,
				listOpts ...client.ListOption) error {
				if _, isNodePoolList := list.(*kaires.NodePoolList); isNodePoolList {
					return &meta.NoKindMatchError{}
				}
				return wrapped.List(ctx, list, listOpts...)
			},
		})

		Expect(Cleanup(ctx, k8sClient,
			CleanupOptions{Namespace: cleanupNamespace, NodePoolFinalizer: nodePoolFinalizer})).To(Succeed())
	})

	// A controller still running could write back what the shard release removes, and a
	// NodePool released from its finalizer first could take its shard with it.
	It("deletes the controllers in the foreground, then releases shards, then strips finalizers", func() {
		var calls []string
		k8sClient := newCleanupClient(interceptor.Funcs{
			Delete: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
				deleteOpts ...client.DeleteOption) error {
				options := &client.DeleteOptions{}
				options.ApplyOptions(deleteOpts)
				calls = append(calls, fmt.Sprintf("delete %s %s",
					object.GetName(), ptr.Deref(options.PropagationPolicy, "")))
				return wrapped.Delete(ctx, object, deleteOpts...)
			},
			Patch: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
				patch client.Patch, patchOpts ...client.PatchOption) error {
				calls = append(calls, "patch "+object.GetName())
				return wrapped.Patch(ctx, object, patch, patchOpts...)
			},
		},
			managedDeployment("nodepool-controller", cleanupNamespace),
			adoptedShard("shard-a"),
			nodePoolWithFinalizers("pool-a", nodePoolFinalizer))

		Expect(Cleanup(ctx, k8sClient, CleanupOptions{
			Namespace:                      cleanupNamespace,
			ReleaseAdoptedSchedulingShards: true,
			NodePoolFinalizer:              nodePoolFinalizer,
		})).To(Succeed())

		Expect(calls).To(Equal([]string{
			"delete nodepool-controller Foreground",
			"patch shard-a",
			"patch pool-a",
		}))
	})

	It("deletes the controllers with the default propagation outside co-install mode", func() {
		var policies []string
		k8sClient := newCleanupClient(interceptor.Funcs{
			Delete: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
				deleteOpts ...client.DeleteOption) error {
				options := &client.DeleteOptions{}
				options.ApplyOptions(deleteOpts)
				policies = append(policies, string(ptr.Deref(options.PropagationPolicy, "")))
				return wrapped.Delete(ctx, object, deleteOpts...)
			},
		}, managedDeployment("nodepool-controller", cleanupNamespace))

		Expect(Cleanup(ctx, k8sClient, CleanupOptions{Namespace: cleanupNamespace})).To(Succeed())

		Expect(policies).To(Equal([]string{""}))
	})

	It("releases no shard when the controllers could not be deleted", func() {
		k8sClient := newCleanupClient(interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return errors.New("delete refused")
			},
		},
			managedDeployment("nodepool-controller", cleanupNamespace),
			adoptedShard("shard-a"))

		Expect(Cleanup(ctx, k8sClient, CleanupOptions{
			Namespace: cleanupNamespace, ReleaseAdoptedSchedulingShards: true,
		})).To(MatchError(ContainSubstring("delete refused")))

		shard := &kaiv1.SchedulingShard{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "shard-a"}, shard)).To(Succeed())
		Expect(shard.OwnerReferences).To(ConsistOf(nodePoolOwnerReference("shard-a")))
	})
})

var _ = Describe("Cleanup", func() {
	var ctx context.Context

	newClient := func(objects ...client.Object) client.Client {
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(kaires.AddToScheme(scheme))
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	}

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("deletes the operator-managed deployments in the namespace", func() {
		fakeClient := newClient(managedDeployment("nodepool-controller", cleanupNamespace))

		Expect(Cleanup(ctx, fakeClient, CleanupOptions{Namespace: cleanupNamespace})).To(Succeed())

		err := fakeClient.Get(ctx,
			client.ObjectKey{Namespace: cleanupNamespace, Name: "nodepool-controller"}, &appsv1.Deployment{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("leaves a deployment the operator does not manage", func() {
		foreign := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name: "somebody-elses", Namespace: cleanupNamespace}}
		fakeClient := newClient(foreign)

		Expect(Cleanup(ctx, fakeClient, CleanupOptions{Namespace: cleanupNamespace})).To(Succeed())

		Expect(fakeClient.Get(ctx,
			client.ObjectKey{Namespace: cleanupNamespace, Name: "somebody-elses"}, &appsv1.Deployment{})).To(Succeed())
	})

	It("leaves a managed deployment in another namespace", func() {
		fakeClient := newClient(managedDeployment("nodepool-controller", "other"))

		Expect(Cleanup(ctx, fakeClient, CleanupOptions{Namespace: cleanupNamespace})).To(Succeed())

		Expect(fakeClient.Get(ctx,
			client.ObjectKey{Namespace: "other", Name: "nodepool-controller"}, &appsv1.Deployment{})).To(Succeed())
	})

	It("deletes the named KRMConfig", func() {
		config := &kaires.KRMConfig{ObjectMeta: metav1.ObjectMeta{Name: "krm-config"}}
		fakeClient := newClient(config)

		Expect(Cleanup(ctx, fakeClient, CleanupOptions{Namespace: cleanupNamespace, ConfigName: "krm-config"})).To(Succeed())

		err := fakeClient.Get(ctx, client.ObjectKey{Name: "krm-config"}, &kaires.KRMConfig{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("leaves the KRMConfig alone when no name is given", func() {
		config := &kaires.KRMConfig{ObjectMeta: metav1.ObjectMeta{Name: "krm-config"}}
		fakeClient := newClient(config)

		Expect(Cleanup(ctx, fakeClient, CleanupOptions{Namespace: cleanupNamespace})).To(Succeed())

		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "krm-config"}, &kaires.KRMConfig{})).To(Succeed())
	})

	It("succeeds when the KRMConfig is already gone", func() {
		Expect(Cleanup(ctx, newClient(), CleanupOptions{Namespace: cleanupNamespace, ConfigName: "krm-config"})).To(Succeed())
	})

	// The KRMConfig is built from a name alone, so its UID has to be read before the
	// delete: without it the poll cannot tell a recreation from the object it removed,
	// and the hook would block until the Job's deadline.
	It("stops waiting when the object is recreated under a new UID", func() {
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(kaires.AddToScheme(scheme))

		deleted := false
		recreating := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(&kaires.KRMConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "krm-config", UID: "original"}}).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
					opts ...client.DeleteOption) error {
					deleted = true
					return wrapped.Delete(ctx, object, opts...)
				},
				Get: func(ctx context.Context, wrapped client.WithWatch, key client.ObjectKey,
					object client.Object, opts ...client.GetOption) error {
					if !deleted {
						return wrapped.Get(ctx, key, object, opts...)
					}
					config, isConfig := object.(*kaires.KRMConfig)
					if !isConfig {
						return wrapped.Get(ctx, key, object, opts...)
					}
					*config = kaires.KRMConfig{
						ObjectMeta: metav1.ObjectMeta{Name: key.Name, UID: "recreated"}}
					return nil
				},
			}).Build()

		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		Expect(Cleanup(bounded, recreating, CleanupOptions{Namespace: cleanupNamespace, ConfigName: "krm-config"})).To(Succeed())
	})

	It("succeeds when the KRMConfig CRD itself is already gone", func() {
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(kaires.AddToScheme(scheme))
		unmapped := fake.NewClientBuilder().WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return &meta.NoKindMatchError{}
				},
			}).Build()

		Expect(Cleanup(ctx, unmapped, CleanupOptions{Namespace: cleanupNamespace, ConfigName: "krm-config"})).To(Succeed())
	})
})
