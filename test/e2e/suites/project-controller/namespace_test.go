// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/handlers"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("A project pointed at a namespace of its own", Ordered, Label("project-controller"), func() {
	var (
		namespace *corev1.Namespace
		project   *kaires.Project
	)

	BeforeAll(func() {
		// Created first: a missing namespace leaves the project NamespaceNotFound.
		namespace = resources.Namespace(utils.GenerateName("pc-external"))
		Expect(testClient.Create(ctx, namespace)).To(Succeed())

		project = resources.Project(utils.GenerateName("pc-external-proj"),
			[]string{testcontext.DefaultNodePoolName},
			resources.WithNamespace(namespace.Name))
		Expect(testClient.Create(ctx, project)).To(Succeed())

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, namespace))).To(Succeed())
		})
	})

	It("adopts it rather than generating one", func() {
		ready := wait.ForProjectReady(ctx, testClient, project.Name)

		Expect(ready.Status.Namespace).To(Equal(namespace.Name))
	})

	It("labels it with the project that now owns it", func() {
		Eventually(func(g Gomega) {
			adopted := &corev1.Namespace{}
			g.Expect(testClient.Get(ctx,
				types.NamespacedName{Name: namespace.Name}, adopted)).To(Succeed())
			g.Expect(adopted.Labels).To(HaveKeyWithValue(
				kaires.NamespaceProjectLabelKey, project.Name))
		}).Should(Succeed())
	})

	It("refuses to take a namespace another project already holds", func() {
		second := resources.Project(utils.GenerateName("pc-external-second"),
			[]string{testcontext.DefaultNodePoolName},
			resources.WithNamespace(namespace.Name))
		Expect(testClient.Create(ctx, second)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, second))).To(Succeed())
			wait.ForDeleted(ctx, testClient, second)
		})

		condition := wait.ForProjectCondition(ctx, testClient, second.Name,
			kaires.NamespaceReady, corev1.ConditionFalse)

		Expect(condition.Reason).To(Equal(string(handlers.NamespaceLabelMissing)))
	})
})

var _ = Describe("A project whose generated namespace is deleted", Ordered, Label("project-controller"), func() {
	var (
		project       *kaires.Project
		namespaceName string
	)

	BeforeAll(func() {
		project = resources.Project(utils.GenerateName("pc-nsrecreate"),
			[]string{testcontext.DefaultNodePoolName})
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespaceName = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)
		})
	})

	It("has it created again", func() {
		namespace := &corev1.Namespace{}
		Expect(testClient.Get(ctx,
			types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
		Expect(testClient.Delete(ctx, namespace)).To(Succeed())

		// A namespace mid-termination still reads back, so Active is what to wait on.
		Eventually(func(g Gomega) {
			recreated := &corev1.Namespace{}
			g.Expect(testClient.Get(ctx,
				types.NamespacedName{Name: namespaceName}, recreated)).To(Succeed())
			g.Expect(recreated.Status.Phase).To(Equal(corev1.NamespaceActive))
			g.Expect(recreated.DeletionTimestamp.IsZero()).To(BeTrue())
		}).Should(Succeed())
	})
})
