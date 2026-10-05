// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package pod_group_assigner

import (
	"time"

	kaiconstants "github.com/kai-scheduler/api/constants"
	kaiv2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/nodes"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// staysAssignedFor is how long a placed pod group must keep its pool. The reset it guards
// against followed the scheduler clearing the conditions within a reconcile.
const staysAssignedFor = 10 * time.Second

// The host does not resolve, so the pod is bound and then never starts.
func withUnpullableImage() resources.PodOption {
	return func(pod *corev1.Pod) { pod.Spec.Containers[0].Image = "e2e.invalid/never-pulls:0" }
}

var _ = Describe("A pod group placed on its second node pool", Ordered, Label("pod-group-assigner"), func() {
	var (
		emptyPool  *kaires.NodePool
		placedPool *kaires.NodePool
		placedNode string
		project    *kaires.Project
		namespace  string
		runningPod *corev1.Pod
		stuckPod   *corev1.Pod
	)

	BeforeAll(func() {
		workers, err := nodes.Workers(ctx, testClient)
		Expect(err).ToNot(HaveOccurred())
		if len(workers) == 0 {
			Skip("the placed node pool needs a node; this cluster has no workers")
		}
		placedNode = workers[0]

		emptyPool = resources.GeneratedNodePool("pga-empty", nodePoolLabelKey)
		placedPool = resources.GeneratedNodePool("pga-placed", nodePoolLabelKey)
		for _, nodePool := range []*kaires.NodePool{emptyPool, placedPool} {
			Expect(testClient.Create(ctx, nodePool)).To(Succeed())
		}

		Expect(nodes.SetLabel(ctx, testClient, placedNode,
			placedPool.Spec.LabelKey, placedPool.Spec.LabelValue)).To(Succeed())

		wait.ForNodePoolPhase(ctx, testClient, emptyPool.Name, kaires.NodePoolEmpty)
		wait.ForNodePoolPhase(ctx, testClient, placedPool.Name, kaires.NodePoolReady)

		// The empty pool first: it is where a reset sends the pod group.
		project = resources.Project(utils.GenerateName("pga-placed-proj"),
			[]string{emptyPool.Name, placedPool.Name}, resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		runningPod = resources.Pod(utils.GenerateName("pga-running-pod"), namespace)
		stuckPod = resources.Pod(utils.GenerateName("pga-stuck-pod"), namespace, withUnpullableImage())
		for _, pod := range []*corev1.Pod{runningPod, stuckPod} {
			Expect(testClient.Create(ctx, pod)).To(Succeed())
		}

		DeferCleanup(func() {
			for _, pod := range []*corev1.Pod{runningPod, stuckPod} {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
				wait.ForDeleted(ctx, testClient, pod)
			}

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			Expect(nodes.RemoveLabel(ctx, testClient, placedNode, nodePoolLabelKey)).To(Succeed())

			for _, nodePool := range []*kaires.NodePool{emptyPool, placedPool} {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
				wait.ForDeleted(ctx, testClient, nodePool)
			}
		})
	})

	It("is assigned the second pool, the first having no nodes", func() {
		running := wait.ForPodRunning(ctx, testClient, namespace, runningPod.Name)
		Expect(running.Spec.NodeName).To(Equal(placedNode))

		podGroup := wait.ForPodGroup(ctx, testClient, namespace, runningPod.Name)
		Expect(podGroup.Labels).To(HaveKeyWithValue(kaiconstants.DefaultNodePoolLabelKey, placedPool.Name))
	})

	It("keeps that pool once the scheduler clears its conditions", func() {
		expectPoolKeptAfterConditionsCleared(wait.ForPodGroup(ctx, testClient, namespace, runningPod.Name), placedPool)
	})

	// Deterministic: the pod stays Pending, so the pod group never stops being up for assignment.
	It("keeps its pool while a bound pod cannot start", func() {
		bound := wait.ForPodBound(ctx, testClient, namespace, stuckPod.Name)
		Expect(bound.Spec.NodeName).To(Equal(placedNode))

		expectPoolKeptAfterConditionsCleared(wait.ForPodGroup(ctx, testClient, namespace, stuckPod.Name), placedPool)
	})
})

func expectPoolKeptAfterConditionsCleared(podGroup *kaiv2alpha2.PodGroup, nodePool *kaires.NodePool) {
	GinkgoHelper()

	current := &kaiv2alpha2.PodGroup{}
	Eventually(func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), current)).To(Succeed())
		g.Expect(current.Status.SchedulingConditions).To(BeEmpty())
	}).Should(Succeed())

	Consistently(func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), current)).To(Succeed())
		g.Expect(current.Labels).To(HaveKeyWithValue(kaiconstants.DefaultNodePoolLabelKey, nodePool.Name))
	}).WithTimeout(staysAssignedFor).Should(Succeed())
}
