// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
)

var _ = Describe("The project webhook", Label("project-controller"), func() {
	It("refuses a queue naming a node pool that does not exist", func() {
		project := resources.Project(utils.GenerateName("pc-nopool"), []string{"pc-not-a-node-pool"})

		Expect(testClient.Create(ctx, project)).To(MatchError(ContainSubstring("does not exist")))
	})

	It("refuses a queue with no node pool at all", func() {
		project := resources.Project(utils.GenerateName("pc-emptypool"), nil)
		project.Spec.Queues = []kaires.QueueConfig{{Name: "queue-without-a-pool"}}

		Expect(testClient.Create(ctx, project)).To(MatchError(ContainSubstring("must not be empty")))
	})

	It("refuses two queues on the same node pool", func() {
		project := resources.Project(utils.GenerateName("pc-dupqueue"), nil)
		project.Spec.Queues = []kaires.QueueConfig{
			{Name: "first", Nodepool: testcontext.DefaultNodePoolName},
			{Name: "second", Nodepool: testcontext.DefaultNodePoolName},
		}

		Expect(testClient.Create(ctx, project)).
			To(MatchError(ContainSubstring("already referenced by another queue")))
	})

	It("refuses a default node pool the spec has no queue for", func() {
		project := resources.Project(utils.GenerateName("pc-nodefaultqueue"),
			[]string{testcontext.DefaultNodePoolName})
		project.Spec.Queues = nil

		Expect(testClient.Create(ctx, project)).To(MatchError(ContainSubstring("has no queue defined")))
	})

	It("refuses a parent department that does not exist", func() {
		project := resources.Project(utils.GenerateName("pc-noparent"),
			[]string{testcontext.DefaultNodePoolName},
			resources.WithParentDepartment("pc-not-a-department"))

		Expect(testClient.Create(ctx, project)).
			To(MatchError(ContainSubstring("parent department")))
	})
})
