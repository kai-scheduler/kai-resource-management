// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	schedv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/handlers"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("The queue a project derives", Ordered, Label("project-controller"), func() {
	var (
		configured *kaires.Project
		bare       *kaires.Project
	)

	quota := &kaires.QueueResourcesConfig{
		GPU:    kaires.SystemResource{Deserved: 1, OverQuotaWeight: 2, Limit: 3},
		CPU:    kaires.SystemResource{Deserved: 1000, OverQuotaWeight: 4, Limit: 2000},
		Memory: kaires.SystemResource{Deserved: 100, OverQuotaWeight: 5, Limit: 200},
	}

	BeforeAll(func() {
		configured = resources.Project(utils.GenerateName("pc-queueconf"),
			[]string{testcontext.DefaultNodePoolName},
			resources.WithQueueResources(quota, ptr.To(int32(42))))
		bare = resources.Project(utils.GenerateName("pc-queuebare"),
			[]string{testcontext.DefaultNodePoolName})

		for _, project := range []*kaires.Project{configured, bare} {
			Expect(testClient.Create(ctx, project)).To(Succeed())
			wait.ForProjectReady(ctx, testClient, project.Name)
		}

		DeferCleanup(func() {
			for _, project := range []*kaires.Project{configured, bare} {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
				wait.ForDeleted(ctx, testClient, project)
			}
		})
	})

	It("carries the resource config the project asked for", func() {
		queue := wait.ForQueue(ctx, testClient,
			resources.QueueName(configured.Name, testcontext.DefaultNodePoolName))

		Expect(queue.Spec.Resources).To(HaveValue(Equal(schedv2.QueueResources{
			GPU:    schedv2.QueueResource{Quota: 1, OverQuotaWeight: 2, Limit: 3},
			CPU:    schedv2.QueueResource{Quota: 1000, OverQuotaWeight: 4, Limit: 2000},
			Memory: schedv2.QueueResource{Quota: 100, OverQuotaWeight: 5, Limit: 200},
		})))
	})

	It("carries the priority the project asked for", func() {
		queue := wait.ForQueue(ctx, testClient,
			resources.QueueName(configured.Name, testcontext.DefaultNodePoolName))

		Expect(queue.Spec.Priority).To(HaveValue(Equal(42)))
	})

	It("falls back to the default priority when the project names none", func() {
		queue := wait.ForQueue(ctx, testClient,
			resources.QueueName(bare.Name, testcontext.DefaultNodePoolName))

		Expect(queue.Spec.Priority).To(HaveValue(Equal(handlers.DefaultQueuePriority)))
	})

	It("is displayed under its own name", func() {
		queueName := resources.QueueName(bare.Name, testcontext.DefaultNodePoolName)

		queue := wait.ForQueue(ctx, testClient, queueName)

		Expect(queue.Spec.DisplayName).To(Equal(queueName))
	})
})
