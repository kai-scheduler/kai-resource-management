// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	kaischedulerv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// One unmanaged shard is shared by every spec: each new shard makes the KAI operator deploy a
// scheduler, and its generated partitionLabelValue is carried by no node, so nothing schedules.
var _ = Describe("The partitionLabelValue webhooks", Ordered, Label("nodepool-controller"), func() {
	var (
		unmanagedShard *kaischedulerv1.SchedulingShard
		nodePool       *kaires.NodePool
		nodePoolShard  *kaischedulerv1.SchedulingShard
	)

	// A wrongly admitted object still has to be cleaned up.
	createExpectingRefusal := func(obj client.Object) error {
		err := testClient.Create(ctx, obj)
		if err == nil {
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, obj))).To(Succeed())
				wait.ForDeleted(ctx, testClient, obj)
			})
		}
		return err
	}

	currentUnmanagedShard := func() *kaischedulerv1.SchedulingShard {
		shard := &kaischedulerv1.SchedulingShard{}
		Expect(testClient.Get(ctx, client.ObjectKeyFromObject(unmanagedShard), shard)).To(Succeed())
		return shard
	}

	BeforeAll(func() {
		unmanagedShard = resources.SchedulingShard(utils.GenerateName("npc-unmanaged"),
			utils.GenerateName("npc-legacy"), resources.Unmanaged())
		Expect(testClient.Create(ctx, unmanagedShard)).To(Succeed(),
			"the webhook should admit an unmanaged shard with no node pool")
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, unmanagedShard))).To(Succeed())
			wait.ForDeleted(ctx, testClient, unmanagedShard)
		})

		nodePool = resources.GeneratedNodePool("npc-owner", nodePoolLabelKey)
		Expect(testClient.Create(ctx, nodePool)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, nodePool)
		})
		// The controller creates this shard through the same webhook, so getting here is
		// also the admission of a shard whose node pool exists.
		nodePoolShard = wait.ForSchedulingShardReady(ctx, testClient, nodePool.Name)
	})

	Context("on SchedulingShard create", func() {
		It("refuses a shard with no node pool and no unmanaged label, naming the remedy", func() {
			partitionLabelValue := utils.GenerateName("npc-orphan")
			orphan := resources.SchedulingShard(partitionLabelValue, partitionLabelValue)

			err := createExpectingRefusal(orphan)

			Expect(err).To(HaveOccurred(), "an orphan shard was accepted")
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring(orphan.Name), ContainSubstring(unmanaged_shards.IgnoreShardLabelKey)))
		})

		It("refuses a second shard with a partitionLabelValue another shard already has", func() {
			second := resources.SchedulingShard(utils.GenerateName("npc-second"),
				unmanagedShard.Spec.PartitionLabelValue, resources.Unmanaged())

			err := createExpectingRefusal(second)

			Expect(err).To(HaveOccurred(), "two shards were accepted on one partitionLabelValue")
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring(unmanagedShard.Name), ContainSubstring(unmanagedShard.Spec.PartitionLabelValue)))
		})

		It("refuses an unmanaged shard on a node pool's partitionLabelValue", func() {
			competing := resources.SchedulingShard(utils.GenerateName("npc-competing"),
				nodePoolShard.Spec.PartitionLabelValue, resources.Unmanaged())

			err := createExpectingRefusal(competing)

			Expect(err).To(HaveOccurred(), "a second scheduler was accepted on a node pool's nodes")
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring(nodePoolShard.Name), ContainSubstring(nodePoolShard.Spec.PartitionLabelValue)))
		})
	})

	Context("on SchedulingShard update", func() {
		// Re-read each time: a rejected update leaves the local copy dirty.
		It("refuses to change partitionLabelValue", func() {
			shard := currentUnmanagedShard()
			shard.Spec.PartitionLabelValue = utils.GenerateName("npc-moved")

			Expect(testClient.Update(ctx, shard)).
				To(MatchError(ContainSubstring("cannot change partitionLabelValue")))
		})

		It("refuses to remove the unmanaged label", func() {
			shard := currentUnmanagedShard()
			delete(shard.Labels, unmanaged_shards.IgnoreShardLabelKey)

			Expect(testClient.Update(ctx, shard)).
				To(MatchError(ContainSubstring(unmanaged_shards.IgnoreShardLabelKey)))
		})

		It("admits an update that keeps both", func() {
			shard := currentUnmanagedShard()
			shard.Spec.Args = map[string]string{"verbosity": "4"}

			Expect(testClient.Update(ctx, shard)).To(Succeed())
		})
	})

	Context("on NodePool create", func() {
		It("refuses a node pool over an unmanaged shard's partitionLabelValue", func() {
			nodePool := resources.NodePool(unmanagedShard.Spec.PartitionLabelValue,
				nodePoolLabelKey, unmanagedShard.Spec.PartitionLabelValue)

			err := createExpectingRefusal(nodePool)

			Expect(err).To(HaveOccurred(), "a node pool was accepted over an unmanaged shard")
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring(unmanagedShard.Name), ContainSubstring(unmanaged_shards.IgnoreShardLabelKey)))
		})

		It("refuses a node pool whose shard name another partitionLabelValue's shard holds", func() {
			nodePool := resources.NodePool(unmanagedShard.Name, nodePoolLabelKey, unmanagedShard.Name)

			err := createExpectingRefusal(nodePool)

			Expect(err).To(HaveOccurred(), "a node pool was accepted though its shard name is taken")
			Expect(err.Error()).To(ContainSubstring(unmanagedShard.Spec.PartitionLabelValue))
		})
	})
})
