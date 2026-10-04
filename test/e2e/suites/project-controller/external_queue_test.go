// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaipgconstants "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// newExternalQueue creates a queue no project or department owns.
func newExternalQueue(name string, labels map[string]string) *kaiv2.Queue {
	queue := resources.Queue(name, labels)
	Expect(testClient.Create(ctx, queue)).To(Succeed())

	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, queue))).To(Succeed())
	})

	return queue
}

// expectUntouched compares against what Create returned, so server-side defaults are
// already in created and only a controller's write would differ.
func expectUntouched(created *kaiv2.Queue) {
	latest := &kaiv2.Queue{}
	Expect(testClient.Get(ctx, client.ObjectKeyFromObject(created), latest)).To(Succeed())

	Expect(latest.OwnerReferences).To(BeEmpty())
	Expect(latest.Labels).To(Equal(created.Labels))
	Expect(latest.Spec).To(Equal(created.Spec))
}

// Both depend on hack/e2e-values-unreleased.yaml starting project-controller with
// --allow-external-queues. Without it, the first queue is adopted and the second deleted.
var _ = Describe("A queue KRM did not create", Label("project-controller"), func() {
	It("keeps the name a project derives, and the project's queue takes a suffix", func() {
		projectName := utils.GenerateName("pc-extq")
		derivedName := resources.QueueName(projectName, testcontext.DefaultNodePoolName)
		external := newExternalQueue(derivedName, nil)

		project := resources.Project(projectName, []string{testcontext.DefaultNodePoolName})
		Expect(testClient.Create(ctx, project)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)
		})
		wait.ForProjectReady(ctx, testClient, project.Name)

		var owned kaiv2.Queue
		Eventually(func(g Gomega) []kaiv2.Queue {
			queues := &kaiv2.QueueList{}
			g.Expect(testClient.List(ctx, queues)).To(Succeed())

			return queues.Items
		}).Should(ContainElement(
			HaveField("OwnerReferences", ContainElement(HaveField("UID", project.UID))), &owned))

		Expect(owned.Name).To(HavePrefix(derivedName + "-"))
		expectUntouched(external)
	})

	It("outlives the project whose label it carries", func() {
		project, _ := newProject()
		// The install leaves --project-label-key unset, so this is the key project-controller
		// selects a project's queues by.
		external := newExternalQueue(utils.GenerateName("pc-extq-labelled"),
			map[string]string{kaipgconstants.ProjectLabelKey: project.Name})

		Expect(testClient.Delete(ctx, project)).To(Succeed())
		wait.ForDeleted(ctx, testClient, project)

		expectUntouched(external)
	})
})
