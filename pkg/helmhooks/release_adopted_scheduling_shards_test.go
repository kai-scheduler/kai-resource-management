// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

func nodePoolOwnerReference(name string) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: kaires.GroupVersion.String(), Kind: "NodePool", Name: name, UID: types.UID("uid-" + name),
		Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true),
	}
}

func adoptedShard(name string) *kaiv1.SchedulingShard {
	return &kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			unmanaged_shards.AdoptionLabelKey: unmanaged_shards.AdoptionLabelValue,
			"app":                             "kai-scheduler",
		},
		Annotations:     map[string]string{"kai.resources/managed-shard-args": "a,b"},
		OwnerReferences: []metav1.OwnerReference{nodePoolOwnerReference(name)},
	}}
}

var _ = Describe("Cleanup releasing adopted scheduling shards", func() {
	var (
		ctx  context.Context
		opts CleanupOptions
	)

	getShard := func(k8sClient client.Client, name string) *kaiv1.SchedulingShard {
		shard := &kaiv1.SchedulingShard{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, shard)).To(Succeed())
		return shard
	}

	BeforeEach(func() {
		ctx = context.Background()
		opts = CleanupOptions{Namespace: cleanupNamespace, ReleaseAdoptedSchedulingShards: true}
	})

	It("drops the NodePool OwnerReference and KRM metadata and leaves the shard in place", func() {
		k8sClient := newCleanupClient(interceptor.Funcs{}, adoptedShard("team-a"))

		Expect(Cleanup(ctx, k8sClient, opts)).To(Succeed())

		shard := getShard(k8sClient, "team-a")
		Expect(shard.OwnerReferences).To(BeEmpty())
		Expect(shard.Labels).NotTo(HaveKey(unmanaged_shards.AdoptionLabelKey))
		Expect(shard.Annotations).NotTo(HaveKey("kai.resources/managed-shard-args"))
	})

	It("keeps the app label and owners that are not NodePools", func() {
		shard := adoptedShard("team-a")
		foreignOwner := metav1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "admin", UID: "uid-admin"}
		shard.OwnerReferences = append(shard.OwnerReferences, foreignOwner)
		k8sClient := newCleanupClient(interceptor.Funcs{}, shard)

		Expect(Cleanup(ctx, k8sClient, opts)).To(Succeed())

		released := getShard(k8sClient, "team-a")
		Expect(released.OwnerReferences).To(ConsistOf(foreignOwner))
		Expect(released.Labels).To(HaveKeyWithValue("app", "kai-scheduler"))
	})

	It("leaves a shard KRM did not adopt to go with its NodePool", func() {
		shard := adoptedShard("team-b")
		delete(shard.Labels, unmanaged_shards.AdoptionLabelKey)
		k8sClient := newCleanupClient(interceptor.Funcs{}, shard)

		Expect(Cleanup(ctx, k8sClient, opts)).To(Succeed())

		Expect(getShard(k8sClient, "team-b").OwnerReferences).To(ConsistOf(nodePoolOwnerReference("team-b")))
	})

	It("leaves adopted shards alone unless asked to release them", func() {
		opts.ReleaseAdoptedSchedulingShards = false
		k8sClient := newCleanupClient(interceptor.Funcs{}, adoptedShard("team-a"))

		Expect(Cleanup(ctx, k8sClient, opts)).To(Succeed())

		Expect(getShard(k8sClient, "team-a").OwnerReferences).To(ConsistOf(nodePoolOwnerReference("team-a")))
	})

	It("writes nothing on a rerun", func() {
		patches := 0
		k8sClient := newCleanupClient(interceptor.Funcs{
			Patch: func(ctx context.Context, wrapped client.WithWatch, object client.Object,
				patch client.Patch, patchOpts ...client.PatchOption) error {
				patches++
				return wrapped.Patch(ctx, object, patch, patchOpts...)
			},
		}, adoptedShard("team-a"))

		Expect(Cleanup(ctx, k8sClient, opts)).To(Succeed())
		Expect(patches).To(Equal(1))

		Expect(Cleanup(ctx, k8sClient, opts)).To(Succeed())
		Expect(patches).To(Equal(1))
	})

	It("succeeds when the SchedulingShard CRD is gone", func() {
		k8sClient := newCleanupClient(interceptor.Funcs{
			List: func(ctx context.Context, wrapped client.WithWatch, list client.ObjectList,
				listOpts ...client.ListOption) error {
				if _, isShardList := list.(*kaiv1.SchedulingShardList); isShardList {
					return &meta.NoKindMatchError{}
				}
				return wrapped.List(ctx, list, listOpts...)
			},
		})

		Expect(Cleanup(ctx, k8sClient, opts)).To(Succeed())
	})
})
