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

var _ = Describe("A pod group asking for two node pools", Ordered, Label("pod-group-assigner"), func() {
	var (
		firstPool  *kaires.NodePool
		secondPool *kaires.NodePool
		project    *kaires.Project
		namespace  string
		pod        *corev1.Pod
		podGroup   *kaiv2alpha2.PodGroup
	)

	BeforeAll(func() {
		firstPool = resources.GeneratedNodePool("pga-first", nodePoolLabelKey)
		secondPool = resources.GeneratedNodePool("pga-second", nodePoolLabelKey)
		for _, nodePool := range []*kaires.NodePool{firstPool, secondPool} {
			Expect(testClient.Create(ctx, nodePool)).To(Succeed())
			wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolEmpty)
		}

		// Order matters: the assigner takes the first of these until told otherwise.
		project = resources.Project(utils.GenerateName("pga-rotate-proj"),
			[]string{firstPool.Name, secondPool.Name}, resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		pod = resources.Pod(utils.GenerateName("pga-rotate-pod"), namespace)
		Expect(testClient.Create(ctx, pod)).To(Succeed())
		podGroup = wait.ForPodGroup(ctx, testClient, namespace, pod.Name)

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, pod)

			// The project's queues are what hold the deleted pool in Deleting.
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			for _, nodePool := range []*kaires.NodePool{firstPool, secondPool} {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
				wait.ForDeleted(ctx, testClient, nodePool)
			}
		})
	})

	It("is assigned the first pool the project lists, and not marked unschedulable", func() {
		Eventually(func(g Gomega) {
			assigned := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), assigned)).To(Succeed())

			g.Expect(assigned.Labels).To(HaveKeyWithValue(
				kaiconstants.DefaultNodePoolLabelKey, firstPool.Name))
			g.Expect(assigned.Spec.Queue).To(Equal(resources.QueueName(project.Name, firstPool.Name)))

			// More pools are left to try, so giving up would be premature.
			g.Expect(assigned.Spec.MarkUnschedulable).To(HaveValue(BeFalse()))
		}).Should(Succeed())
	})

	It("moves to the second pool once the first stops being available", func() {
		// Deleting it is enough: the project still has a queue for it, so it settles in
		// Deleting rather than going, and Deleting is neither Ready nor Empty.
		Expect(testClient.Delete(ctx, firstPool)).To(Succeed())
		wait.ForNodePoolPhase(ctx, testClient, firstPool.Name, kaires.NodePoolDeleting)

		Eventually(func(g Gomega) {
			reassigned := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), reassigned)).To(Succeed())

			g.Expect(reassigned.Labels).To(HaveKeyWithValue(
				kaiconstants.DefaultNodePoolLabelKey, secondPool.Name))
			g.Expect(reassigned.Spec.Queue).To(Equal(resources.QueueName(project.Name, secondPool.Name)))
		}).Should(Succeed())
	})

	It("is marked unschedulable now that it is on the last pool", func() {
		Eventually(func(g Gomega) {
			lastResort := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), lastResort)).To(Succeed())

			g.Expect(lastResort.Spec.MarkUnschedulable).To(HaveValue(BeTrue()))
		}).Should(Succeed())
	})
})
