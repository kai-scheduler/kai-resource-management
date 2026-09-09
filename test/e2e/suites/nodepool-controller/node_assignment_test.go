// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package nodepool_controller covers the nodepool-controller: which nodes a node pool
// claims as their labels change, and what holds a node pool back from being deleted.
package nodepool_controller

import (
	kaiconstants "github.com/kai-scheduler/api/constants"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/nodes"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("A node pool", Ordered, Label("nodepool-controller"), func() {
	var (
		nodePool *kaires.NodePool
		nodeName string
	)

	BeforeAll(func() {
		nodePool = resources.GeneratedNodePool("npc-nodes", nodePoolLabelKey)
		Expect(testClient.Create(ctx, nodePool)).To(Succeed())

		// The controller derives a phase from the nodes only once the scheduler behind the
		// pool's shard is up, so waiting for Empty here keeps the claim below from racing
		// a scheduler that is starting.
		wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolEmpty)

		DeferCleanup(func() {
			// The node has to leave the nodepool before the nodepool can finish deleting, and
			// a spec that failed part way may have left it in.
			if nodeName != "" {
				Expect(nodes.RemoveLabel(ctx, testClient, nodeName, nodePoolLabelKey)).To(Succeed())
			}

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, nodePool)
		})
	})

	It("claims a node that gains the label it selects on", func() {
		var err error
		nodeName, err = nodes.LabelWorker(ctx, testClient, nodePool.Spec.LabelKey, nodePool.Spec.LabelValue)
		Expect(err).ToNot(HaveOccurred())

		claimed := wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolReady)

		// Validate exactly 1 node is claimed, and that it is the one we labeled.
		Expect(claimed.Status.Nodes).To(ConsistOf(kaires.NodeInNodePool{
			Name:   nodeName,
			Status: kaires.NodeReady,
		}))
	})

	It("stamps that node with the pool it now belongs to", func() {
		Eventually(func(g Gomega) {
			node := &corev1.Node{}
			g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
			g.Expect(node.Labels).To(HaveKeyWithValue(kaiconstants.DefaultNodePoolLabelKey, nodePool.Name))
		}).Should(Succeed())
	})

	It("releases the node when that label goes", func() {
		Expect(nodes.RemoveLabel(ctx, testClient, nodeName, nodePoolLabelKey)).To(Succeed())

		released := wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolEmpty)
		Expect(released.Status.Nodes).To(BeEmpty())

		// The default pool is the absence of the key, not its name as a value.
		Eventually(func(g Gomega) {
			node := &corev1.Node{}
			g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
			g.Expect(node.Labels).ToNot(HaveKey(kaiconstants.DefaultNodePoolLabelKey))
		}).Should(Succeed())
	})
})
