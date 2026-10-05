// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package tests

import (
	"fmt"

	kaiv2 "github.com/kai-scheduler/api/scheduling/v2"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	pgaconfig "github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/podgroup/assigner"
	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/podgroup/assignment_params"
)

const (
	externalQueuesProject        = "team-ext"
	externalQueuesUnownedProject = "team-ext-unowned"
)

var _ = Describe("Pod Group Assigner with external queues allowed", Ordered, func() {
	var (
		projectNamespace        = fmt.Sprintf("runai-%s", externalQueuesProject)
		unownedProjectNamespace = fmt.Sprintf("runai-%s", externalQueuesUnownedProject)
		namespaces              = []string{RunaiNamespace, projectNamespace, unownedProjectNamespace}

		// Named to list ahead of the project's own queue, so taking the first match would pick it.
		adminQueue        = getQueueObj("admin-"+externalQueuesProject, externalQueuesProject, testDefaultNodePoolName)
		projectQueue      = ownedByProject(getQueueObj(externalQueuesProject, externalQueuesProject, testDefaultNodePoolName))
		unownedAdminQueue = getQueueObj("admin-"+externalQueuesUnownedProject, externalQueuesUnownedProject, testDefaultNodePoolName)

		// A labelled namespace is a project's only while its Project exists.
		projects = []*v1alpha1.Project{
			{ObjectMeta: metav1.ObjectMeta{Name: externalQueuesProject}},
			{ObjectMeta: metav1.ObjectMeta{Name: externalQueuesUnownedProject}},
		}
	)

	BeforeAll(func() {
		withExternalQueues := *pgaconfig.Config()
		withExternalQueues.AllowExternalQueues = true
		DeferCleanup(pgaconfig.SetForTest(withExternalQueues))

		controllerSetup()

		createTestNodePool("BeforeAll", &defaultNodePool, k8sClient)
		for _, namespace := range namespaces {
			createNamespaceIfNeeded(namespace, k8sClient)
		}
		for _, queue := range []*kaiv2.Queue{adminQueue, projectQueue, unownedAdminQueue} {
			expectCreateResource(k8sClient, queue)
		}
		for _, project := range projects {
			expectCreateResource(k8sClient, project)
		}

		DeferCleanup(func() {
			for _, project := range projects {
				deleteAndPollUntilDeleted(k8sClient, project)
			}
			for _, queue := range []*kaiv2.Queue{adminQueue, projectQueue, unownedAdminQueue} {
				deleteQueue(queue.Name, k8sClient)
			}
			for _, namespace := range namespaces {
				deleteNamespace(namespace, k8sClient)
			}
			deleteNodePool("AfterAll", defaultNodePool.Name, k8sClient)
		})
	})

	AfterEach(afterEachCleanup)

	It("assigns a project's pod group to the queue its Project owns", func() {
		pg := types.NamespacedName{Namespace: projectNamespace, Name: generatePodGroupName()}
		createdPodGroups = append(createdPodGroups, pg)
		createPodGroupAndPodsFromSpecWithProjName("owned queue", pg, []string{}, 1, externalQueuesProject, k8sClient)

		eventuallyExpectNodePoolAssignmentWithQueueName(pg, projectQueue.Name, &assigner.FullNodePoolAssignmentParams{
			NodePoolAssignmentParams: assignment_params.NodePoolAssignmentParams{
				NodePoolName:      testDefaultNodePoolName,
				MarkUnschedulable: true,
				SchedulingBackoff: common.NoSchedulingBackoff,
			},
		}, k8sClient)
	})

	It("leaves a pod group outside every project alone once it is marked unschedulable", func() {
		pg := types.NamespacedName{Namespace: RunaiNamespace, Name: generatePodGroupName()}
		createdPodGroups = append(createdPodGroups, pg)

		// A PodGroup the mutator never saw, with a backoff its own scheduler honours.
		podGroup := getPodGroupObj(pg, []string{}, "", "")
		delete(podGroup.Labels, testNodePoolLabelKey)
		createPodGroup("external pod group", podGroup, k8sClient)
		SchedulerMock.UnschedulableOnNodePoolNoValidate(pg, &assigner.FullNodePoolAssignmentParams{
			NodePoolAssignmentParams: assignment_params.NodePoolAssignmentParams{NodePoolName: testDefaultNodePoolName},
		}, k8sClient)

		_, err := reconciler.Reconcile(apiCtx, ctrl.Request{NamespacedName: pg})

		Expect(err).NotTo(HaveOccurred())
		pgFromClient := getPodGroupFromClient(pg, k8sClient)
		Expect(pgFromClient.Spec.Queue).To(Equal(podGroup.Spec.Queue))
		Expect(pgFromClient.Labels).NotTo(HaveKey(testNodePoolLabelKey))
		Expect(pgFromClient.Labels).NotTo(HaveKey(RunaiQueueLabel))
	})

	It("does not assign a project's pod group to a queue only labelled for the project", func() {
		pg := types.NamespacedName{Namespace: unownedProjectNamespace, Name: generatePodGroupName()}
		createdPodGroups = append(createdPodGroups, pg)
		createPodGroupAndPodsFromSpecWithProjName("unowned queue", pg, []string{}, 1, externalQueuesUnownedProject, k8sClient)

		_, err := reconciler.Reconcile(apiCtx, ctrl.Request{NamespacedName: pg})

		Expect(err).To(HaveOccurred())
		pgFromClient := getPodGroupFromClient(pg, k8sClient)
		Expect(pgFromClient.Spec.Queue).NotTo(Equal(unownedAdminQueue.Name))
		Expect(pgFromClient.Labels).To(HaveKeyWithValue(testNodePoolLabelKey, testUnexistingNodePool))
	})
})

func ownedByProject(queue *kaiv2.Queue) *kaiv2.Queue {
	queue.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "kai.resources/v1alpha1",
		Kind:       "Project",
		Name:       queue.Labels[RunaiProjectLabel],
		UID:        types.UID(queue.Labels[RunaiProjectLabel]),
	}}

	return queue
}
