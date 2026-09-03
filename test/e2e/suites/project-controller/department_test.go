// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	"time"

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
