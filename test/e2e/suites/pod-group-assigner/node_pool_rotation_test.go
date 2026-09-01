// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_assigner

import (
	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	kaiconstants "github.com/kai-scheduler/api/constants"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("A pod group whose first node pool is unavailable", Ordered, Label("pod-group-assigner"), func() {
	var (
		unavailablePool *kaires.NodePool
		availablePool   *kaires.NodePool
		project         *kaires.Project
		pod             *corev1.Pod
		podGroup        *kaiv2alpha2.PodGroup
	)

	BeforeAll(func() {
		unavailablePool = resources.GeneratedNodePool("pga-unavailable", nodePoolLabelKey)
		availablePool = resources.GeneratedNodePool("pga-available", nodePoolLabelKey)
		for _, nodePool := range []*kaires.NodePool{unavailablePool, availablePool} {
			Expect(testClient.Create(ctx, nodePool)).To(Succeed())
			wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolEmpty)
		}

		// Order matters: the unavailable pool is the one the assigner would take first.
		project = resources.Project(utils.GenerateName("pga-skip-proj"),
			[]string{unavailablePool.Name, availablePool.Name}, resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespace := wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		// The project's queue parks it in Deleting, which is neither Ready nor Empty.
		Expect(testClient.Delete(ctx, unavailablePool)).To(Succeed())
		wait.ForNodePoolPhase(ctx, testClient, unavailablePool.Name, kaires.NodePoolDeleting)

		pod = resources.Pod(utils.GenerateName("pga-skip-pod"), namespace)
		Expect(testClient.Create(ctx, pod)).To(Succeed())
		podGroup = wait.ForPodGroup(ctx, testClient, namespace, pod.Name)

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, pod)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			for _, nodePool := range []*kaires.NodePool{unavailablePool, availablePool} {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
				wait.ForDeleted(ctx, testClient, nodePool)
			}
		})
	})

	It("is assigned the next pool in the list, with that pool's queue", func() {
		Eventually(func(g Gomega) {
			assigned := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), assigned)).To(Succeed())

			g.Expect(assigned.Labels).To(HaveKeyWithValue(
				kaiconstants.DefaultNodePoolLabelKey, availablePool.Name))
			g.Expect(assigned.Spec.Queue).
				To(Equal(resources.QueueName(project.Name, availablePool.Name)))
		}).Should(Succeed())
	})

	It("is marked unschedulable, that pool being the last one left to try", func() {
		Eventually(func(g Gomega) {
			lastResort := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), lastResort)).To(Succeed())

			g.Expect(lastResort.Spec.MarkUnschedulable).To(HaveValue(BeTrue()))
		}).Should(Succeed())
	})
})
