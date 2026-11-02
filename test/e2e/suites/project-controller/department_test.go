// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	"time"

	schedv2 "github.com/kai-scheduler/api/scheduling/v2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// blockedGracePeriod is how long a department is watched for a deletion that should not
// happen while a project still names it.
const blockedGracePeriod = 7 * time.Second

var _ = Describe("Deleting a department", Ordered, Label("project-controller"), func() {
	var (
		department *kaires.Department
		project    *kaires.Project
	)

	BeforeAll(func() {
		department = resources.Department(utils.GenerateName("pc-dept"),
			[]string{testcontext.DefaultNodePoolName})
		Expect(testClient.Create(ctx, department)).To(Succeed())

		project, _ = newProject(resources.WithParentDepartment(department.Name))

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, department))).To(Succeed())
			wait.ForDeleted(ctx, testClient, department)
		})
	})

	It("is held while a project still names it as parent", func() {
		Expect(testClient.Delete(ctx, department)).To(Succeed())

		blocked := wait.ForDepartmentCondition(ctx, testClient, department.Name,
			kaires.DepartmentDeletionBlocked, corev1.ConditionTrue)
		Expect(blocked.Message).To(ContainSubstring(project.Name))

		Consistently(func() error {
			return testClient.Get(ctx, types.NamespacedName{Name: department.Name}, &kaires.Department{})
		}).WithContext(ctx).WithTimeout(blockedGracePeriod).WithPolling(constant.Interval).
			Should(Succeed(), "the department went while a project still named it")
	})

	It("goes once that project does", func() {
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
		wait.ForDeleted(ctx, testClient, project)

		wait.ForDeleted(ctx, testClient, department)
	})
})

var _ = Describe("Nested departments", Ordered, Label("project-controller"), func() {
	var top, middle, other, leaf *kaires.Department

	// newDepartment creates a department and registers its deletion, so cleanup removes children
	// before the parents whose deletion they would block.
	newDepartment := func(prefix string, options ...resources.DepartmentOption) *kaires.Department {
		department := resources.Department(utils.GenerateName(prefix),
			[]string{testcontext.DefaultNodePoolName}, options...)
		Expect(testClient.Create(ctx, department)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, department))).To(Succeed())
			wait.ForDeleted(ctx, testClient, department)
		})
		return department
	}

	queueOf := func(owner string) string {
		return resources.QueueName(owner, testcontext.DefaultNodePoolName)
	}

	expectParentQueue := func(owner, parentOwner string) {
		Eventually(func(g Gomega) {
			queue := &schedv2.Queue{}
			g.Expect(testClient.Get(ctx, types.NamespacedName{Name: queueOf(owner)}, queue)).To(Succeed())
			g.Expect(queue.Spec.ParentQueue).To(Equal(queueOf(parentOwner)))
		}).Should(Succeed())
	}

	BeforeAll(func() {
		top = newDepartment("pc-top")
		other = newDepartment("pc-other")
		middle = newDepartment("pc-middle", resources.WithParent(top.Name))
		leaf = newDepartment("pc-leaf", resources.WithParent(middle.Name))
	})

	It("places each queue under its parent's queue, down to a project", func() {
		project, _ := newProject(resources.WithParentDepartment(leaf.Name))

		expectParentQueue(middle.Name, top.Name)
		expectParentQueue(leaf.Name, middle.Name)
		expectParentQueue(project.Name, leaf.Name)
	})

	It("moves a department's queue in place, keeping the departments under it", func() {
		before := wait.ForQueue(ctx, testClient, queueOf(middle.Name))

		Eventually(func(g Gomega) {
			latest := &kaires.Department{}
			g.Expect(testClient.Get(ctx, types.NamespacedName{Name: middle.Name}, latest)).To(Succeed())
			latest.Spec.Parent = other.Name
			g.Expect(testClient.Update(ctx, latest)).To(Succeed())
		}).Should(Succeed())

		expectParentQueue(middle.Name, other.Name)
		Expect(wait.ForQueue(ctx, testClient, queueOf(middle.Name)).UID).To(Equal(before.UID))
		expectParentQueue(leaf.Name, middle.Name)
	})
})
