// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"errors"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

const (
	revertNamespace = "kai-resource-management"
	krmFinalizer    = "nodepool.kai.scheduler/finalize"
)

func nodePoolOwnerReference(name string) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: kaires.GroupVersion.String(), Kind: "NodePool", Name: name, UID: types.UID("uid-" + name),
		Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true),
	}
}

func shardWithAdoption(name, adoption string) *kaiv1.SchedulingShard {
	return &kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			unmanaged_shards.AdoptionLabelKey: adoption,
			"app":                             "kai-scheduler",
		},
		Annotations:     map[string]string{"kai.resources/managed-shard-args": "a,b"},
		OwnerReferences: []metav1.OwnerReference{nodePoolOwnerReference(name)},
	}}
}

func nodePoolWithFinalizers(name string, finalizers ...string) *kaires.NodePool {
	return &kaires.NodePool{ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: finalizers}}
}

var _ = Describe("RevertForUninstall", func() {
	var (
		ctx  context.Context
		opts RevertOptions
	)

	newScheme := func() *runtime.Scheme {
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(kaires.AddToScheme(scheme))
		utilruntime.Must(kaiv1.AddToScheme(scheme))
		return scheme
	}

	newClient := func(funcs interceptor.Funcs, objects ...client.Object) client.Client {
		return fake.NewClientBuilder().WithScheme(newScheme()).
			WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
	}

	getShard := func(k8sClient client.Client, name string) *kaiv1.SchedulingShard {
		shard := &kaiv1.SchedulingShard{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, shard)).To(Succeed())
		return shard
	}

	BeforeEach(func() {
		ctx = context.Background()
		opts = RevertOptions{
			Namespace:                       revertNamespace,
			OperatorDeployment:              "krm-operator",
			ValidatingWebhookConfigurations: []string{"kai-nodepool-validation"},
			MutatingWebhookConfigurations:   []string{"kai-pod-group-mutation"},
			FinalizerDomain:                 "kai.scheduler",
		}
	})

	Context("an adopted shard", func() {
		It("loses its NodePool OwnerReference and KRM metadata and stays in place", func() {
			k8sClient := newClient(interceptor.Funcs{}, shardWithAdoption("team-a", unmanaged_shards.AdoptionAdopted))

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			shard := getShard(k8sClient, "team-a")
			Expect(shard.OwnerReferences).To(BeEmpty())
			Expect(shard.Labels).NotTo(HaveKey(unmanaged_shards.AdoptionLabelKey))
			Expect(shard.Annotations).NotTo(HaveKey("kai.resources/managed-shard-args"))
		})

		It("keeps the app label and owners that are not NodePools", func() {
			shard := shardWithAdoption("team-a", unmanaged_shards.AdoptionAdopted)
			foreignOwner := metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "admin", UID: "uid-admin"}
			shard.OwnerReferences = append(shard.OwnerReferences, foreignOwner)
			k8sClient := newClient(interceptor.Funcs{}, shard)

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			released := getShard(k8sClient, "team-a")
			Expect(released.OwnerReferences).To(ConsistOf(foreignOwner))
			Expect(released.Labels).To(HaveKeyWithValue("app", "kai-scheduler"))
		})
	})

	Context("a shard KRM did not adopt", func() {
		It("leaves it to go with its NodePool", func() {
			shard := shardWithAdoption("team-c", "")
			delete(shard.Labels, unmanaged_shards.AdoptionLabelKey)
			k8sClient := newClient(interceptor.Funcs{}, shard)

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			Expect(getShard(k8sClient, "team-c").OwnerReferences).To(ConsistOf(nodePoolOwnerReference("team-c")))
		})
	})

	Context("NodePool finalizers", func() {
		It("strips KRM's finalizer from every NodePool and keeps foreign ones", func() {
			k8sClient := newClient(interceptor.Funcs{},
				nodePoolWithFinalizers("default", krmFinalizer),
				nodePoolWithFinalizers("user-created", krmFinalizer, "example.com/keep"))

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			nodePool := &kaires.NodePool{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, nodePool)).To(Succeed())
			Expect(nodePool.Finalizers).To(BeEmpty())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "user-created"}, nodePool)).To(Succeed())
			Expect(nodePool.Finalizers).To(ConsistOf("example.com/keep"))
		})

		It("strips the finalizer composed from a custom domain", func() {
			opts.FinalizerDomain = "example.com"
			k8sClient := newClient(interceptor.Funcs{},
				nodePoolWithFinalizers("default", "nodepool.example.com/finalize", krmFinalizer))

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			nodePool := &kaires.NodePool{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, nodePool)).To(Succeed())
			Expect(nodePool.Finalizers).To(ConsistOf(krmFinalizer))
		})
	})

	Context("webhooks and controllers", func() {
		It("deletes the named webhook configurations", func() {
			k8sClient := newClient(interceptor.Funcs{},
				&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "kai-nodepool-validation"}},
				&admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "kai-pod-group-mutation"}},
				&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "somebody-elses"}})

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "kai-nodepool-validation"},
				&admissionregistrationv1.ValidatingWebhookConfiguration{}))).To(BeTrue())
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: "kai-pod-group-mutation"},
				&admissionregistrationv1.MutatingWebhookConfiguration{}))).To(BeTrue())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "somebody-elses"},
				&admissionregistrationv1.ValidatingWebhookConfiguration{})).To(Succeed())
		})

		It("deletes the operator and its controllers in the foreground, in the release namespace only", func() {
			var foregroundDeletes []string
			k8sClient := newClient(interceptor.Funcs{
				Delete: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
					deleteOpts ...client.DeleteOption) error {
					options := &client.DeleteOptions{}
					options.ApplyOptions(deleteOpts)
					if ptr.Deref(options.PropagationPolicy, "") == metav1.DeletePropagationForeground {
						foregroundDeletes = append(foregroundDeletes, object.GetName())
					}
					return wrapped.Delete(ctx, object, deleteOpts...)
				},
			},
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "krm-operator", Namespace: revertNamespace}},
				managedDeployment("nodepool-controller", revertNamespace),
				managedDeployment("nodepool-controller", "other"))

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			Expect(foregroundDeletes).To(Equal([]string{"krm-operator", "nodepool-controller"}))
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: "other", Name: "nodepool-controller"},
				&appsv1.Deployment{})).To(Succeed())
		})

		It("skips the operator when none is named", func() {
			opts.OperatorDeployment = ""
			k8sClient := newClient(interceptor.Funcs{},
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "krm-operator", Namespace: revertNamespace}})

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: revertNamespace, Name: "krm-operator"},
				&appsv1.Deployment{})).To(Succeed())
		})
	})

	Context("ordering", func() {
		It("releases no shard while the controllers could still be running", func() {
			k8sClient := newClient(interceptor.Funcs{
				Delete: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
					deleteOpts ...client.DeleteOption) error {
					if _, isDeployment := object.(*appsv1.Deployment); isDeployment {
						return errors.New("delete refused")
					}
					return wrapped.Delete(ctx, object, deleteOpts...)
				},
			},
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "krm-operator", Namespace: revertNamespace}},
				shardWithAdoption("team-a", unmanaged_shards.AdoptionAdopted))

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(MatchError(ContainSubstring("delete refused")))

			Expect(getShard(k8sClient, "team-a").OwnerReferences).To(ConsistOf(nodePoolOwnerReference("team-a")))
		})

		It("releases the shards before the finalizers that would let GC delete them", func() {
			var patched []string
			k8sClient := newClient(interceptor.Funcs{
				Patch: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
					patch client.Patch, patchOpts ...client.PatchOption) error {
					patched = append(patched, object.GetName())
					return wrapped.Patch(ctx, object, patch, patchOpts...)
				},
			},
				nodePoolWithFinalizers("team-a", krmFinalizer),
				shardWithAdoption("shard-a", unmanaged_shards.AdoptionAdopted))

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())

			Expect(patched).To(Equal([]string{"shard-a", "team-a"}))
		})
	})

	Context("a rerun", func() {
		It("writes nothing once the revert has completed", func() {
			patches := 0
			k8sClient := newClient(interceptor.Funcs{
				Patch: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
					patch client.Patch, patchOpts ...client.PatchOption) error {
					patches++
					return wrapped.Patch(ctx, object, patch, patchOpts...)
				},
			},
				shardWithAdoption("team-a", unmanaged_shards.AdoptionAdopted),
				nodePoolWithFinalizers("default", krmFinalizer))

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())
			Expect(patches).To(Equal(2))

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())
			Expect(patches).To(Equal(2))
		})

		It("succeeds when the webhook configurations and deployments are already gone", func() {
			Expect(RevertForUninstall(ctx, newClient(interceptor.Funcs{}), opts)).To(Succeed())
		})

		It("succeeds when the SchedulingShard and NodePool CRDs are gone", func() {
			k8sClient := newClient(interceptor.Funcs{
				List: func(ctx context.Context, wrapped client.WithWatch, list client.ObjectList,
					listOpts ...client.ListOption) error {
					switch list.(type) {
					case *kaiv1.SchedulingShardList, *kaires.NodePoolList:
						return &meta.NoKindMatchError{}
					}
					return wrapped.List(ctx, list, listOpts...)
				},
			})

			Expect(RevertForUninstall(ctx, k8sClient, opts)).To(Succeed())
		})
	})
})
