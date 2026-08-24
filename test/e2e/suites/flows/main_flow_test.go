// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package flows covers the main flow through the system and its variations.
package flows

import (
	"fmt"

	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	kaiconstants "github.com/kai-scheduler/api/constants"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("The main flow", Ordered, Label("flows"), func() {
	var (
		department *kaires.Department
		project    *kaires.Project
		namespace  string
	)

	BeforeAll(func() {
		department = resources.Department(utils.GenerateName("flows-dept"), []string{testNodePool.Name})
		Expect(testClient.Create(ctx, department)).To(Succeed())

		project = resources.Project(utils.GenerateName("flows-proj"), []string{testNodePool.Name},
			resources.WithParentDepartment(department.Name),
			resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())

		namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		// The project's queues are parented to the department's, so it goes first.
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			Eventually(func() error {
				return testClient.Get(ctx, types.NamespacedName{Name: project.Name}, &kaires.Project{})
			}).Should(MatchError(ContainSubstring("not found")))

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, department))).To(Succeed())
		})
	})

	Context("the node pool", func() {
		It("claims the node it selects", func() {
			nodePool := wait.ForNodePoolPhase(ctx, testClient, testNodePool.Name, kaires.NodePoolReady)

			Expect(nodePool.Status.Nodes).To(ContainElement(HaveField("Name", testNodeName)))
		})

		It("gets a scheduling shard of its own", func() {
			shard := wait.ForSchedulingShardReady(ctx, testClient, testNodePool.Name)

			Expect(shard.Spec.PartitionLabelValue).To(Equal(testNodePool.Name),
				"the shard should partition on the node pool it belongs to")
			Expect(shard.OwnerReferences).To(ContainElement(HaveField("Name", testNodePool.Name)))
		})

		It("gets a service monitor owned by the node pool", func() {
			name := fmt.Sprintf("%s-%s", kaiconstants.DefaultSchedulerName, testNodePool.Name)

			monitor := wait.ForServiceMonitor(ctx, testClient, name)

			Expect(monitor.OwnerReferences).To(ContainElement(HaveField("Name", testNodePool.Name)))
		})
	})

	Context("the department", func() {
		It("gets a queue for each node pool in its spec", func() {
			queue := wait.ForQueue(ctx, testClient,
				resources.QueueName(department.Name, testNodePool.Name))

			Expect(queue.Spec.ParentQueue).To(BeEmpty(), "a department queue is a root")
		})
	})

	Context("the project under that department", func() {
		It("is given a namespace", func() {
			wait.ForNamespace(ctx, testClient, namespace)
		})

		It("gets a queue parented to the department's queue for the same node pool", func() {
			queue := wait.ForQueue(ctx, testClient,
				resources.QueueName(project.Name, testNodePool.Name))

			Expect(queue.Spec.ParentQueue).
				To(Equal(resources.QueueName(department.Name, testNodePool.Name)))
		})
	})

	Context("a pod submitted to the project", Ordered, func() {
		var pod *corev1.Pod

		BeforeAll(func() {
			pod = resources.Pod(utils.GenerateName("flows-pod"), namespace)
			Expect(testClient.Create(ctx, pod)).To(Succeed())

			// Before the node can leave the node pool at AfterSuite.
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
				Eventually(func() error {
					return testClient.Get(ctx,
						types.NamespacedName{Namespace: namespace, Name: pod.Name}, &corev1.Pod{})
				}).Should(MatchError(ContainSubstring("not found")))
			})
		})

		It("is mutated onto the KAI scheduler and labelled with its project", func() {
			admitted := &corev1.Pod{}
			Expect(testClient.Get(ctx,
				types.NamespacedName{Namespace: namespace, Name: pod.Name}, admitted)).To(Succeed())

			Expect(admitted.Spec.SchedulerName).To(Equal(kaiconstants.DefaultSchedulerName))
			Expect(admitted.Labels).To(HaveKeyWithValue("project", project.Name))
		})

		It("gets a pod group assigned to the node pool and the project's queue", func() {
			Eventually(func(g Gomega) {
				list := &kaiv2alpha2.PodGroupList{}
				g.Expect(testClient.List(ctx, list, client.InNamespace(namespace))).To(Succeed())
				g.Expect(list.Items).To(HaveLen(1), "expected one pod group in %q", namespace)

				podGroup := list.Items[0]
				g.Expect(podGroup.Spec.Queue).
					To(Equal(resources.QueueName(project.Name, testNodePool.Name)))
				g.Expect(podGroup.Labels).
					To(HaveKeyWithValue(kaiconstants.DefaultNodePoolLabelKey, testNodePool.Name))
			}).Should(Succeed())
		})

		It("runs on the node the node pool claimed", func() {
			running := wait.ForPodRunning(ctx, testClient, namespace, pod.Name)

			Expect(running.Spec.NodeName).To(Equal(testNodeName))
		})
	})
})
