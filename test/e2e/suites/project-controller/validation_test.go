// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
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
		project.Spec.Queues = []kaires.QueueConfig{}

		Expect(testClient.Create(ctx, project)).To(MatchError(ContainSubstring("has no queue defined")))
	})

	It("refuses a parent department that does not exist", func() {
		project := resources.Project(utils.GenerateName("pc-noparent"),
			[]string{testcontext.DefaultNodePoolName},
			resources.WithParentDepartment("pc-not-a-department"))

		Expect(testClient.Create(ctx, project)).
			To(MatchError(ContainSubstring("parent department")))
	})

	Context("on update", Ordered, func() {
		var project *kaires.Project

		BeforeAll(func() {
			project = resources.Project(utils.GenerateName("pc-update"),
				[]string{testcontext.DefaultNodePoolName})
			Expect(testClient.Create(ctx, project)).To(Succeed())
			wait.ForProjectReady(ctx, testClient, project.Name)

			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
				wait.ForDeleted(ctx, testClient, project)
			})
		})

		// Re-read each time: a stale copy fails as a conflict, not by the webhook.
		update := func(change func(*kaires.Project)) error {
			latest := &kaires.Project{}
			Expect(testClient.Get(ctx,
				types.NamespacedName{Name: project.Name}, latest)).To(Succeed())
			change(latest)

			return testClient.Update(ctx, latest)
		}

		It("refuses a queue moved onto a node pool that does not exist", func() {
			Expect(update(func(latest *kaires.Project) {
				latest.Spec.Queues[0].Nodepool = "pc-not-a-node-pool"
			})).To(MatchError(ContainSubstring("does not exist")))
		})

		It("refuses a second queue added on a node pool already taken", func() {
			Expect(update(func(latest *kaires.Project) {
				latest.Spec.Queues = append(latest.Spec.Queues, kaires.QueueConfig{
					Name:     "second",
					Nodepool: testcontext.DefaultNodePoolName,
				})
			})).To(MatchError(ContainSubstring("already referenced by another queue")))
		})

		It("refuses a parent department added that does not exist", func() {
			Expect(update(func(latest *kaires.Project) {
				latest.Spec.Parent = "pc-not-a-department"
			})).To(MatchError(ContainSubstring("parent department")))
		})
	})
})
