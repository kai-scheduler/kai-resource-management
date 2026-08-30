// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package pod_group_assigner covers the pod-group-assigner: which node pool a workload
// ends up in, and what the assigner writes onto its pod group.
package pod_group_assigner

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
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("A pod whose affinity matches one node pool's nodes", Ordered, Label("pod-group-assigner"), func() {
	var (
		selectedPool *kaires.NodePool
		otherPool    *kaires.NodePool
		selectedNode string
		otherNode    string
		project      *kaires.Project
		namespace    string
		pod          *corev1.Pod
	)

	BeforeAll(func() {
		workers, err := nodes.Workers(ctx, testClient)
		Expect(err).ToNot(HaveOccurred())
		if len(workers) < 2 {
			Skip("two node pools need a node each; this cluster has fewer than two workers")
		}
		selectedNode, otherNode = workers[0], workers[1]

		selectedPool = resources.GeneratedNodePool("pga-selected", nodePoolLabelKey)
		otherPool = resources.GeneratedNodePool("pga-other", nodePoolLabelKey)
		for _, nodePool := range []*kaires.NodePool{selectedPool, otherPool} {
			Expect(testClient.Create(ctx, nodePool)).To(Succeed())
		}

		Expect(nodes.SetLabel(ctx, testClient, selectedNode,
			selectedPool.Spec.LabelKey, selectedPool.Spec.LabelValue)).To(Succeed())
		Expect(nodes.SetLabel(ctx, testClient, otherNode,
			otherPool.Spec.LabelKey, otherPool.Spec.LabelValue)).To(Succeed())

		wait.ForNodePoolPhase(ctx, testClient, selectedPool.Name, kaires.NodePoolReady)
		wait.ForNodePoolPhase(ctx, testClient, otherPool.Name, kaires.NodePoolReady)

		// A queue for each pool, so the assigner is free to pick either and the affinity
		// is what decides rather than the project's spec.
		project = resources.Project(utils.GenerateName("pga-proj"),
			[]string{selectedPool.Name, otherPool.Name},
			resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		pod = resources.Pod(utils.GenerateName("pga-pod"), namespace,
			resources.WithNodeAffinity(resources.NodeSelectorForNodePool(selectedPool)))
		Expect(testClient.Create(ctx, pod)).To(Succeed())

		DeferCleanup(func() {
			// Pod first, then the node labels, then the pools: a pool cannot finish
			// deleting while it owns a node, and a node cannot leave one while a pod
			// assigned to it is still running.
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, pod)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			Expect(nodes.RemoveLabel(ctx, testClient, selectedNode, nodePoolLabelKey)).To(Succeed())
			Expect(nodes.RemoveLabel(ctx, testClient, otherNode, nodePoolLabelKey)).To(Succeed())

			for _, nodePool := range []*kaires.NodePool{selectedPool, otherPool} {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
				wait.ForDeleted(ctx, testClient, nodePool)
			}
		})
	})

	It("runs on the node belonging to the pool its affinity selects", func() {
		running := wait.ForPodRunning(ctx, testClient, namespace, pod.Name)

		Expect(running.Spec.NodeName).To(Equal(selectedNode))
	})

	It("leaves each node stamped with its own pool", func() {
		Eventually(func(g Gomega) {
			for nodeName, nodePool := range map[string]*kaires.NodePool{
				selectedNode: selectedPool,
				otherNode:    otherPool,
			} {
				node := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
				g.Expect(node.Labels).
					To(HaveKeyWithValue(kaiconstants.DefaultNodePoolLabelKey, nodePool.Name))
			}
		}).Should(Succeed())
	})

	It("shows that node, and only that node, in the selected pool's status", func() {
		claimed := wait.ForNodePoolPhase(ctx, testClient, selectedPool.Name, kaires.NodePoolReady)

		Expect(claimed.Status.Nodes).To(ConsistOf(kaires.NodeInNodePool{
			Name:   selectedNode,
			Status: kaires.NodeReady,
		}))
	})
})
