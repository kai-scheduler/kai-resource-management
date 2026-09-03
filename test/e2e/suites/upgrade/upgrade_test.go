// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package upgrade

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

// Ordered: the specs share BeforeAll's world and the upgrade the first one runs.
var _ = Describe("An installation upgraded to a new chart", Ordered, Label("upgrade"), func() {
	var (
		chart      string
		nodePool   *kaires.NodePool
		department *kaires.Department
		project    *kaires.Project
		namespace  string
		nodeName   string
		pod        *corev1.Pod

		nodePoolUID types.UID
		projectUID  types.UID
		queueUID    types.UID
	)

	// Nothing comes from the chart: it re-applies its own objects rather than migrating them.
	BeforeAll(func() {
		chart = chartPath()

		By("creating a node pool on the installed version")
		nodePool = resources.GeneratedNodePool("upgrade", nodePoolLabelKey)
		Expect(testClient.Create(ctx, nodePool)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, nodePool)
		})

		var err error
		nodeName, err = nodes.LabelWorker(ctx, testClient, nodePool.Spec.LabelKey, nodePool.Spec.LabelValue)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			Expect(nodes.RemoveLabel(ctx, testClient, nodeName, nodePoolLabelKey)).To(Succeed())
		})

		By("creating a department and a project under it")
		department = resources.Department(utils.GenerateName("upgrade-dept"), []string{nodePool.Name})
		Expect(testClient.Create(ctx, department)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, department))).To(Succeed())
			wait.ForDeleted(ctx, testClient, department)
		})

		project = resources.Project(utils.GenerateName("upgrade-proj"), []string{nodePool.Name},
			resources.WithParentDepartment(department.Name),
			resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)
		})

		By("waiting for the installed version to reconcile all of it")
		nodePoolUID = wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolReady).UID
		wait.ForSchedulingShardReady(ctx, testClient, nodePool.Name)

		ready := wait.ForProjectReady(ctx, testClient, project.Name)
		projectUID = ready.UID
		namespace = ready.Status.Namespace
		queueUID = wait.ForQueue(ctx, testClient, resources.QueueName(project.Name, nodePool.Name)).UID

		By("running a pod on the installed version")
		pod = resources.Pod(utils.GenerateName("upgrade-pod"), namespace)
		Expect(testClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, pod)
		})

		// The installed version's CRDs silently prune fields the API module knows.
		wait.ForPodGroup(ctx, testClient, namespace, pod.Name)
		wait.ForPodRunning(ctx, testClient, namespace, pod.Name)
	})

	It("reports every operand deployed for the new spec", func() {
		installed := wait.ForKRMConfig(ctx, testClient)

		By("upgrading the release to " + chart)
		upgradeRelease(ctx, chart)

		// The operator rolls operands after helm returns; wait out the old conditions.
		upgraded := wait.ForKRMConfigGenerationAfter(ctx, testClient, installed.Generation)

		// Owner references hang off this config, so a hook that recreated it would
		// cascade-delete everything it owns while still looking healthy.
		Expect(upgraded.UID).To(Equal(installed.UID),
			"the krm config was recreated by the upgrade rather than applied in place")

		wait.ForKRMConfigDeployed(ctx, testClient)
		wait.ForSchedulingShardReady(ctx, testClient, nodePool.Name)
	})

	It("keeps the workload that was running before it", func() {
		// Not Eventually: a retry would paper over the pod having stopped.
		running := &corev1.Pod{}
		Expect(testClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: pod.Name}, running)).
			To(Succeed())

		Expect(running.Status.Phase).To(Equal(corev1.PodRunning))
		Expect(running.Spec.NodeName).To(Equal(nodeName),
			"the pod was rescheduled, so it did not survive the upgrade in place")
	})

	It("keeps the objects that existed before it", func() {
		By("the node pool still owning its node")
		upgradedPool := wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolReady)
		Expect(upgradedPool.UID).To(Equal(nodePoolUID), "the node pool was recreated by the upgrade")
		Expect(upgradedPool.Status.Nodes).To(ContainElement(HaveField("Name", nodeName)))

		By("the project keeping its namespace")
		upgradedProject := wait.ForProjectReady(ctx, testClient, project.Name)
		Expect(upgradedProject.UID).To(Equal(projectUID), "the project was recreated by the upgrade")
		Expect(upgradedProject.Status.Namespace).To(Equal(namespace))

		By("the project's queue keeping its place in the hierarchy")
		queue := wait.ForQueue(ctx, testClient, resources.QueueName(project.Name, nodePool.Name))
		Expect(queue.UID).To(Equal(queueUID), "the queue was recreated by the upgrade")
		Expect(queue.Spec.ParentQueue).
			To(Equal(resources.QueueName(department.Name, nodePool.Name)))
	})

	It("schedules a new workload in a project that predates it", func() {
		// The webhooks are the real subject: they fail closed on a stale caBundle.
		newPod := resources.Pod(utils.GenerateName("upgrade-pod"), namespace)
		Expect(testClient.Create(ctx, newPod)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, newPod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, newPod)
		})

		podGroup := wait.ForPodGroup(ctx, testClient, namespace, newPod.Name)
		Expect(podGroup.Spec.Queue).To(Equal(resources.QueueName(project.Name, nodePool.Name)))
		Expect(podGroup.Labels).To(HaveKeyWithValue(kaiconstants.DefaultNodePoolLabelKey, nodePool.Name))

		wait.ForPodRunning(ctx, testClient, namespace, newPod.Name)
	})

	It("admits a new project under a department that predates it", func() {
		// New CRDs parented onto the previous version's objects: exercises the CRD hook.
		newProject := resources.Project(utils.GenerateName("upgrade-proj"), []string{nodePool.Name},
			resources.WithParentDepartment(department.Name),
			resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, newProject)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, newProject))).To(Succeed())
			wait.ForDeleted(ctx, testClient, newProject)
		})

		newNamespace := wait.ForProjectReady(ctx, testClient, newProject.Name).Status.Namespace

		queue := wait.ForQueue(ctx, testClient, resources.QueueName(newProject.Name, nodePool.Name))
		Expect(queue.Spec.ParentQueue).
			To(Equal(resources.QueueName(department.Name, nodePool.Name)))

		newPod := resources.Pod(utils.GenerateName("upgrade-pod"), newNamespace)
		Expect(testClient.Create(ctx, newPod)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, newPod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, newPod)
		})

		wait.ForPodRunning(ctx, testClient, newNamespace, newPod.Name)
	})

	// Last: it takes the namespace the specs above put pods in.
	It("deletes an object that predates it", func() {
		// The finalizer domain is a chart value; a moved one strands the finalizer.
		Expect(testClient.Delete(ctx, project)).To(Succeed())
		wait.ForDeleted(ctx, testClient, project)
	})
})
