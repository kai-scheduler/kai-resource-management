// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	kaipgconstants "github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	kaiconstants "github.com/kai-scheduler/api/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// admittedPod returns the pod as the API server stored it, after the mutating webhook.
func admittedPod(namespace, name string) *corev1.Pod {
	admitted := &corev1.Pod{}
	ExpectWithOffset(1, testClient.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: name}, admitted)).To(Succeed())

	return admitted
}

var _ = Describe("A project that does not enforce the KAI scheduler", Ordered, Label("project-controller"), func() {
	var namespace string

	BeforeAll(func() {
		_, namespace = newProject(resources.WithEnforceScheduler(false))
	})

	It("leaves a pod that asks for nothing on the default scheduler", func() {
		pod := resources.Pod(utils.GenerateName("pc-plain"), namespace)
		Expect(testClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, pod)
		})

		admitted := admittedPod(namespace, pod.Name)

		Expect(admitted.Spec.SchedulerName).ToNot(Equal(kaiconstants.DefaultSchedulerName))
		// The mutator returns before any of its writes, so the project label is absent too.
		Expect(admitted.Labels).ToNot(HaveKey(kaipgconstants.ProjectLabelKey))
	})

	It("still mutates a pod that names the KAI scheduler itself", func() {
		pod := resources.Pod(utils.GenerateName("pc-optin"), namespace,
			resources.WithSchedulerName(kaiconstants.DefaultSchedulerName))
		Expect(testClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, pod)
		})

		admitted := admittedPod(namespace, pod.Name)

		Expect(admitted.Spec.SchedulerName).To(Equal(kaiconstants.DefaultSchedulerName))
		Expect(admitted.Labels).To(HaveKey(kaipgconstants.ProjectLabelKey))
	})
})
