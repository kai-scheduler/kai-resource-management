// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	kaischedulerv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1/schedulingshardargs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

const (
	shardArgName  = "verbosity"
	shardArgValue = "4"
	gpuStrategy   = "spread"
)

var _ = Describe("A node pool carrying scheduling shard config", Ordered, Label("nodepool-controller"), func() {
	var nodePool *kaires.NodePool

	BeforeAll(func() {
		nodePool = resources.GeneratedNodePool("npc-shard", nodePoolLabelKey,
			resources.WithSchedulingShardConfig(&kaires.SchedulingShardConfig{
				Args:              map[string]string{shardArgName: shardArgValue},
				PlacementStrategy: &kaischedulerv1.PlacementStrategy{GPU: ptr.To(gpuStrategy)},
			}))
		Expect(testClient.Create(ctx, nodePool)).To(Succeed())

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, nodePool)
		})
	})

	It("passes that config through to the shard it derives", func() {
		shard := wait.ForSchedulingShardReady(ctx, testClient, nodePool.Name)

		Expect(shard.Spec.Args).To(HaveKeyWithValue(shardArgName, shardArgValue))
		Expect(shard.Spec.PlacementStrategy).ToNot(BeNil())
		Expect(shard.Spec.PlacementStrategy.GPU).To(HaveValue(Equal(gpuStrategy)))
	})

	It("keeps the args the controller adds for itself", func() {
		// Merged into the controller's args, not substituted for them.
		shard := wait.ForSchedulingShardReady(ctx, testClient, nodePool.Name)

		Expect(shard.Spec.Args).To(SatisfyAll(
			HaveKey(schedulingshardargs.CPUWorkerNodeLabelKey),
			HaveKey(schedulingshardargs.GPUWorkerNodeLabelKey),
		))
	})
})
