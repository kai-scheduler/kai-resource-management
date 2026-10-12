// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package deletion_test

import (
	"context"

	kaiv2 "github.com/kai-scheduler/api/scheduling/v2"
	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/config"
	. "github.com/kai-scheduler/kai-resource-management/pkg/project-controller/handlers/deletion"
	. "github.com/kai-scheduler/kai-resource-management/pkg/project-controller/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Queue Deletion Handler", func() {

	var (
		handler   QueueDeletionHandler
		client    client.Client
		project   kaiv1alpha1.Project
		namespace v1.Namespace
		queue     kaiv2.Queue
	)

	BeforeEach(func() {
		project = *TestProject.DeepCopy()
		namespace = *TestNamespace.DeepCopy()
		queue = *TestQueue.DeepCopy()

		project.Finalizers = append(project.Finalizers, config.FinalizerName())
		client = fake.NewClientBuilder().WithScheme(scheme).Build()
		handler = NewQueueDeletionHandler(client)
	})

	It("Finalizes a Project", func() {

		// Given
		Expect(client.Create(context.TODO(), &project)).To(Succeed())
		Expect(client.Create(context.TODO(), &namespace)).To(Succeed())
		Expect(client.Create(context.TODO(), &queue)).To(Succeed())

		// Sanity
		Expect(handler.GetQueue(queue.Name)).ToNot(BeZero())
		Expect(handler.GetNamespace(TestNamespace.Name)).ToNot(BeZero())

		// When
		conditions, err := handler.OnDelete(&project)
		Expect(err).To(BeNil())

		// Then
		By("ProjectConditions are correct", func() {
			Expect(conditions).To(HaveLen(1))
			Expect(conditions[0]).ToNot(BeNil())
			Expect(conditions[0].Type).To(Equal(kaiv1alpha1.QueuesReady))
			Expect(conditions[0].Status).To(Equal(v1.ConditionTrue))
			Expect(conditions[0].Reason).To(Equal(""))
			Expect(conditions[0].Message).To(Equal(""))
		})

		By("Deleting the owned Queue", func() {
			for _, queueSpec := range project.Spec.Queues {
				actualQueue, err := handler.GetQueue(queueSpec.Name)
				Expect(err).To(HaveOccurred())
				Expect(actualQueue).To(BeZero())
			}
		})
	})

	// Every Queue here carries the project's label, which is all the deletion lists by.
	DescribeTable("Queues carrying the project's label",
		func(externalQueuesAllowed bool) {
			if externalQueuesAllowed {
				withExternalQueues := *config.Get()
				withExternalQueues.AllowExternalQueues = true
				DeferCleanup(config.SetForTest(&withExternalQueues))
			}
			unowned := queue.DeepCopy()
			unowned.Name = "platform-queue"
			unowned.OwnerReferences = nil
			ownedByAnother := queue.DeepCopy()
			ownedByAnother.Name = "unrelated-proj-queue"
			ownedByAnother.OwnerReferences = UnrelatedProjectOwnerRef
			Expect(client.Create(context.TODO(), &project)).To(Succeed())
			Expect(client.Create(context.TODO(), &queue)).To(Succeed())
			Expect(client.Create(context.TODO(), unowned)).To(Succeed())
			Expect(client.Create(context.TODO(), ownedByAnother)).To(Succeed())

			_, err := handler.OnDelete(&project)
			Expect(err).To(BeNil())

			_, err = handler.GetQueue(queue.Name)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the owned Queue is always deleted")
			for _, notOwned := range []*kaiv2.Queue{unowned, ownedByAnother} {
				_, err = handler.GetQueue(notOwned.Name)
				if !externalQueuesAllowed {
					Expect(apierrors.IsNotFound(err)).To(BeTrue(), notOwned.Name)
				} else {
					Expect(err).ToNot(HaveOccurred(), notOwned.Name)
				}
			}
		},
		Entry("are all deleted when external queues are not allowed", false),
		Entry("are deleted only if the project owns them when external queues are allowed", true),
	)
})
