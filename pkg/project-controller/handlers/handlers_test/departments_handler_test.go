// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handlers_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	kaiv2 "github.com/kai-scheduler/api/scheduling/v2"
	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/handlers"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/test"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Department Handler Tests", func() {

	var (
		handler           handlers.DepartmentHandler
		k8sClient         client.Client
		dep1, dep2        kaiv1alpha1.Department
		queue1d, queuenp1 kaiv2.Queue
	)

	BeforeEach(func() {
		dep1 = *test.KaiTestDepartment1.DeepCopy()
		dep2 = *test.KaiTestDepartment2.DeepCopy()
		queue1d = *test.KaiTestQueueDep1NpD.DeepCopy()
		queuenp1 = *test.KaiTestQueueDep1Np1.DeepCopy()

		k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		handler = handlers.NewDepartmentHandler(k8sClient, k8sClient, events.NewFakeRecorder(10))
	})

	Context("With external queues allowed, Queues carrying the department's label", func() {
		var (
			departmentLabels map[string]string
			dep2Owner        []metav1.OwnerReference
		)

		BeforeEach(func() {
			allowExternalQueues()
			departmentLabels = map[string]string{config.Get().QueueDepartmentNameLabelKey: dep2.Name}
			dep2Owner = []metav1.OwnerReference{{
				APIVersion:         kaiv1alpha1.GroupVersion.Identifier(),
				Kind:               common.DepartmentKind,
				Name:               dep2.Name,
				UID:                dep2.UID,
				Controller:         &common.TrueRef,
				BlockOwnerDeletion: &common.TrueRef,
			}}
		})

		It("Updates only the stale Queue the department owns", func() {
			withThirdPool := dep2.DeepCopy()
			withThirdPool.Spec.Queues = append(withThirdPool.Spec.Queues, kaiv1alpha1.QueueConfig{
				Name:     "dep2-third-pool",
				Nodepool: "third-pool",
				Resources: &kaiv1alpha1.QueueResourcesConfig{
					GPU: kaiv1alpha1.SystemResource{Deserved: 3},
				},
			})
			owned := labelledQueue(withThirdPool.Spec.Queues[0], departmentLabels, dep2Owner)
			unowned := labelledQueue(withThirdPool.Spec.Queues[1], departmentLabels, nil)
			ownedByAnother := labelledQueue(withThirdPool.Spec.Queues[2], departmentLabels, test.KaiDep1OwnerRef)
			for _, existing := range []*kaiv2.Queue{owned, unowned, ownedByAnother} {
				Expect(k8sClient.Create(context.TODO(), existing.DeepCopy())).To(Succeed())
			}

			Expect(handler.Handle(context.Background(), withThirdPool)).Error().NotTo(HaveOccurred())

			updated, err := getQueue(k8sClient, owned.Name)
			Expect(err).ToNot(HaveOccurred())
			assertQueueResourcesIdenticalToProjectSpecResources(&updated, withThirdPool.Spec.Queues[0])

			for _, untouched := range []struct {
				queue *kaiv2.Queue
				spec  kaiv1alpha1.QueueConfig
			}{
				{unowned, withThirdPool.Spec.Queues[1]},
				{ownedByAnother, withThirdPool.Spec.Queues[2]},
			} {
				actual, err := getQueue(k8sClient, untouched.queue.Name)
				Expect(err).ToNot(HaveOccurred())
				expectQueueUntouched(actual, untouched.queue)

				own, err := getQueueOwnedBy(k8sClient, dep2.UID, untouched.spec.Nodepool)
				Expect(err).ToNot(HaveOccurred(), "the department gets a queue of its own instead")
				Expect(own.Name).To(HavePrefix(untouched.queue.Name))
				Expect(own.Name).ToNot(Equal(untouched.queue.Name))
				assertQueueResourcesIdenticalToProjectSpecResources(&own, untouched.spec)
			}
		})

		It("Deletes only the Queue the department owns when its node pool leaves the spec", func() {
			retired := func(name string) kaiv1alpha1.QueueConfig {
				return kaiv1alpha1.QueueConfig{Name: name, Nodepool: "retired-pool"}
			}
			owned := labelledQueue(retired("dep2-retired"), departmentLabels, dep2Owner)
			unowned := labelledQueue(retired("platform-retired"), departmentLabels, nil)
			ownedByAnother := labelledQueue(retired("dep1-retired"), departmentLabels, test.KaiDep1OwnerRef)
			for _, existing := range []*kaiv2.Queue{owned, unowned, ownedByAnother} {
				Expect(k8sClient.Create(context.TODO(), existing.DeepCopy())).To(Succeed())
			}

			Expect(handler.Handle(context.Background(), &dep2)).Error().NotTo(HaveOccurred())

			_, err := getQueue(k8sClient, owned.Name)
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the owned Queue is deleted")
			for _, untouched := range []*kaiv2.Queue{unowned, ownedByAnother} {
				actual, err := getQueue(k8sClient, untouched.Name)
				Expect(err).ToNot(HaveOccurred())
				expectQueueUntouched(actual, untouched)
			}
		})
	})

	Describe("Department Handler Tests - validate correct creation of queues", func() {
		It("Creating new queues matching the Department queues spec", func() {

			// Given
			beforeCreation, beforeCreationErr := getQueue(k8sClient,
				handlers.GetQueueName(dep2.Spec.Queues[0], dep2.Name))
			Expect(beforeCreationErr).To(HaveOccurred())
			Expect(beforeCreation).To(BeZero())
			beforeCreation, beforeCreationErr = getQueue(k8sClient,
				handlers.GetQueueName(dep2.Spec.Queues[1], dep2.Name))
			Expect(beforeCreationErr).To(HaveOccurred())
			Expect(beforeCreation).To(BeZero())

			// When
			_, err := handler.Handle(context.Background(), &dep2)
			Expect(err).Should(Succeed())

			// Then
			assertQueuesIdenticalToDepartmentSpec(k8sClient, &dep2, test.TestDepartment2Id)
		})

		It("1 queue spec is identical, 1 is not - not modifying/modifying accordingly", func() {
			Expect(k8sClient.Create(context.TODO(), &dep1)).To(Succeed())
			Expect(k8sClient.Create(context.TODO(), &queue1d)).To(Succeed())
			Expect(k8sClient.Create(context.TODO(), &queuenp1)).To(Succeed())

			// The fake client strips TypeMeta on Create; restore it so the handler
			// builds the queue owner reference with the department's GVK, as it would
			// with a real client.
			populateGVK(&dep1)

			// When
			_, err := handler.Handle(context.Background(), &dep1)
			Expect(err).Should(Succeed())

			// Then
			assertQueuesIdenticalToDepartmentSpec(k8sClient, &dep1, test.TestDepartment1Id)

			// original queue for dep1-np1 is equal to actual - was already equal before
			actual, err := getQueue(k8sClient,
				handlers.GetQueueName(dep1.Spec.Queues[1], dep1.Name))
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).ToNot(BeZero())
			expectQueuesEqualV2(actual, queuenp1)

			// original queue for dep1-default-np - not equal to actual - the handler reconciled it
			actual, err = getQueue(k8sClient,
				handlers.GetQueueName(dep1.Spec.Queues[0], dep1.Name))
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).ToNot(BeZero())
			Expect(queue1d.Spec.Resources).ToNot(Equal(actual.Spec.Resources))
			Expect(queue1d.Labels).ToNot(Equal(actual.Labels))
		})

		It("Not modifying the queues when the department name changes (only owner ref name)", func() {
			_, err := handler.Handle(context.Background(), &dep2)
			Expect(err).Should(Succeed())

			assertQueuesIdenticalToDepartmentSpec(k8sClient, &dep2, test.TestDepartment2Id)

			// "rename" department - meaning create the new one and delete the old one
			renamedDep := test.KaiTestDepartment2.DeepCopy()
			renamedDep.Name = "different-name"

			// validate spec as expected for new department name after Handle
			_, err = handler.Handle(context.Background(), renamedDep)
			Expect(err).Should(Succeed())
			assertQueuesIdenticalToDepartmentSpec(k8sClient, renamedDep, test.TestDepartment2Id)

			actual, err := getQueue(k8sClient,
				handlers.GetQueueName(renamedDep.Spec.Queues[1], renamedDep.Name))
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).ToNot(BeZero())
			Expect(actual.OwnerReferences).ToNot(BeZero())
			Expect(len(actual.OwnerReferences)).To(Equal(1))
			Expect(actual.OwnerReferences[0].Name).To(Equal(renamedDep.Name))
		})

		It("Queue with suggested name already exists for other resource - generating different name", func() {
			queueWithNameOfSuggestedQueueNameForDep2 := test.KaiTestQueueDep1NpD.DeepCopy()
			queueWithNameOfSuggestedQueueNameForDep2.Name = handlers.GetQueueName(
				dep2.Spec.Queues[0], dep2.Name)
			err := k8sClient.Create(context.Background(), queueWithNameOfSuggestedQueueNameForDep2)
			Expect(err).Should(Succeed())

			queueWithNameOfSuggestedQueueNameForDep2 = test.KaiTestQueueDep1NpD.DeepCopy()
			queueWithNameOfSuggestedQueueNameForDep2.Name = handlers.GetQueueName(
				dep2.Spec.Queues[1], dep2.Name)
			err = k8sClient.Create(context.Background(), queueWithNameOfSuggestedQueueNameForDep2)
			Expect(err).Should(Succeed())

			// When
			_, err = handler.Handle(context.Background(), &dep2)
			Expect(err).Should(Succeed())

			// Then
			assertQueuesIdenticalToDepartmentSpecInner(k8sClient, &dep2,
				test.TestDepartment2Id, false)

			queue, err := getQueueForDepartmentByLabel(k8sClient, test.TestDepartment2Name, test.DefaultNodePoolName)
			Expect(err).Should(Succeed())
			createdDepartmentQueueNameDefaultNodepool := queue.Name
			queue, err = getQueueForDepartmentByLabel(k8sClient, test.TestDepartment2Name, test.SomeNodePoolName)
			Expect(err).Should(Succeed())
			createdDepartmentQueueNameSomeNodepool := queue.Name

			_, err = handler.Handle(context.Background(), &dep2)
			Expect(err).Should(Succeed())
			_, err = handler.Handle(context.Background(), &dep2)
			Expect(err).Should(Succeed())

			assertQueuesIdenticalToDepartmentSpecInner(k8sClient, &dep2,
				test.TestDepartment2Id, false)

			queue, err = getQueueForDepartmentByLabel(k8sClient, test.TestDepartment2Name, test.DefaultNodePoolName)
			Expect(err).Should(Succeed())
			Expect(queue.Name).To(Equal(createdDepartmentQueueNameDefaultNodepool))
			queue, err = getQueueForDepartmentByLabel(k8sClient, test.TestDepartment2Name, test.SomeNodePoolName)
			Expect(err).Should(Succeed())
			Expect(queue.Name).To(Equal(createdDepartmentQueueNameSomeNodepool))
		})

		It("Validate department queue too long name gets trimmed with random suffix", func() {
			// Given - clear the queue names so the "<departmentName>-<nodepool>" fallback applies;
			// combined with the long nodepool names this yields an over-long queue name that must
			// be trimmed and given a random suffix.
			dep3 := test.KaiTestDepartment3.DeepCopy()
			dep3.Spec.Queues[0].Name = ""
			dep3.Spec.Queues[1].Name = ""
			dep3.Spec.Queues[0].Nodepool = "the-longest-possible-nodepool-name-that-is-exactly-63-charslong"
			dep3.Spec.Queues[1].Nodepool = "the-longest-possible-nodepool-name-that-is-exactly-63-charslon2"

			// When
			_, err := handler.Handle(context.Background(), dep3)
			Expect(err).Should(Succeed())

			// Then
			assertQueuesIdenticalToDepartmentSpecInner(k8sClient, dep3, test.TestDepartment3Id, false)

			queue0, err := getQueueForDepartmentByLabel(k8sClient, test.TestDepartment3Name, dep3.Spec.Queues[0].Nodepool)
			Expect(err).Should(Succeed())
			Expect(queue0.Name).To(HavePrefix(test.TestDepartment3Name + "-the-longest-possible-nodepool-name-that"))
			queue0Name := queue0.Name
			queue1, err := getQueueForDepartmentByLabel(k8sClient, test.TestDepartment3Name, dep3.Spec.Queues[1].Nodepool)
			Expect(err).Should(Succeed())
			Expect(queue1.Name).To(HavePrefix(test.TestDepartment3Name + "-the-longest-possible-nodepool-name-that"))
			queue1Name := queue1.Name

			// Handle again to ensure no changes
			_, err = handler.Handle(context.Background(), dep3)
			Expect(err).Should(Succeed())

			assertQueuesIdenticalToDepartmentSpecInner(k8sClient, dep3, test.TestDepartment3Id, false)

			queue0, err = getQueueForDepartmentByLabel(k8sClient, test.TestDepartment3Name, dep3.Spec.Queues[0].Nodepool)
			Expect(err).Should(Succeed())
			Expect(queue0.Name).To(Equal(queue0Name))
			queue1, err = getQueueForDepartmentByLabel(k8sClient, test.TestDepartment3Name, dep3.Spec.Queues[1].Nodepool)
			Expect(err).Should(Succeed())
			Expect(queue1.Name).To(Equal(queue1Name))
		})
	})

	Describe("Department Handler Tests - validate correct deletion of queues", func() {
		It("Delete queue if nodepool deleted - removed from department spec", func() {
			// When
			_, err := handler.Handle(context.Background(), &dep2)
			Expect(err).Should(Succeed())

			// Then
			assertQueuesIdenticalToDepartmentSpec(k8sClient, &dep2, test.TestDepartment2Id)
			npQueueName := handlers.GetQueueName(dep2.Spec.Queues[1], dep2.Name)
			actual, err := getQueue(k8sClient, npQueueName)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).ToNot(BeZero())

			// "delete" a nodepool - meaning delete it from the department spec
			modifiedDep := test.KaiTestDepartment2.DeepCopy()
			modifiedDep.Spec.Queues = []kaiv1alpha1.QueueConfig{modifiedDep.Spec.Queues[0]}

			// validate spec as expected after Handle
			_, err = handler.Handle(context.Background(), modifiedDep)
			Expect(err).Should(Succeed())

			assertQueuesIdenticalToDepartmentSpec(k8sClient, modifiedDep, test.TestDepartment2Id)
			actual, err = getQueue(k8sClient, npQueueName)
			Expect(err).To(HaveOccurred())
			Expect(actual).To(BeZero())
		})
	})

	Context("Nested departments", func() {
		var recorder *events.FakeRecorder

		BeforeEach(func() {
			recorder = events.NewFakeRecorder(10)
			handler = handlers.NewDepartmentHandler(k8sClient, k8sClient, recorder)
		})

		// newDepartment creates a department with one unnamed queue per node pool, so its queues are
		// named "<name>" in the default node pool and "<name>-<nodepool>" elsewhere.
		newDepartment := func(name, parent string, nodepools ...string) *kaiv1alpha1.Department {
			department := &kaiv1alpha1.Department{
				ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name + "-uid")},
				Spec:       kaiv1alpha1.DepartmentSpec{Parent: parent},
			}
			for _, nodepool := range nodepools {
				department.Spec.Queues = append(department.Spec.Queues, kaiv1alpha1.QueueConfig{Nodepool: nodepool})
			}
			Expect(k8sClient.Create(context.TODO(), department)).To(Succeed())
			return department
		}

		reparent := func(department *kaiv1alpha1.Department, parent string) {
			department.Spec.Parent = parent
			Expect(k8sClient.Update(context.TODO(), department)).To(Succeed())
		}

		handle := func(department *kaiv1alpha1.Department) (heldAsRoot bool) {
			heldAsRoot, err := handler.Handle(context.Background(), department)
			Expect(err).NotTo(HaveOccurred())
			return heldAsRoot
		}

		parentQueueOf := func(queueName string) string {
			queue, err := getQueue(k8sClient, queueName)
			Expect(err).NotTo(HaveOccurred())
			return queue.Spec.ParentQueue
		}

		otherPool := test.SomeNodePoolName
		inOtherPool := func(name string) string { return name + "-" + otherPool }

		It("places each queue under the parent department's queue in the same node pool", func() {
			handle(newDepartment("research", "", test.DefaultNodePoolName, otherPool))

			Expect(handle(newDepartment("ml", "research", test.DefaultNodePoolName, otherPool))).To(BeFalse())

			Expect(parentQueueOf("ml")).To(Equal("research"))
			Expect(parentQueueOf(inOtherPool("ml"))).To(Equal(inOtherPool("research")))
		})

		It("makes a queue a root queue where the parent department has no queue in its node pool", func() {
			handle(newDepartment("research", "", test.DefaultNodePoolName))

			handle(newDepartment("ml", "research", test.DefaultNodePoolName, otherPool))

			Expect(parentQueueOf("ml")).To(Equal("research"))
			Expect(parentQueueOf(inOtherPool("ml"))).To(BeEmpty())
		})

		It("makes the queues root queues when the parent department does not exist", func() {
			Expect(handle(newDepartment("ml", "missing", test.DefaultNodePoolName))).To(BeFalse())

			Expect(parentQueueOf("ml")).To(BeEmpty())
		})

		It("moves a department's queues in place to the new parent department", func() {
			handle(newDepartment("research", "", test.DefaultNodePoolName))
			handle(newDepartment("platform", "", test.DefaultNodePoolName))
			ml := newDepartment("ml", "research", test.DefaultNodePoolName)
			handle(ml)
			before, err := getQueue(k8sClient, "ml")
			Expect(err).NotTo(HaveOccurred())

			reparent(ml, "platform")
			handle(ml)

			after, err := getQueue(k8sClient, "ml")
			Expect(err).NotTo(HaveOccurred())
			Expect(after.UID).To(Equal(before.UID))
			Expect(after.Spec.ParentQueue).To(Equal("platform"))
		})

		It("keeps a queue a root queue rather than close a loop of departments", func() {
			a := newDepartment("a", "b", test.DefaultNodePoolName)
			b := newDepartment("b", "a", test.DefaultNodePoolName)
			handle(a)
			handle(b)
			Expect(parentQueueOf("b")).To(Equal("a"))

			Expect(handle(a)).To(BeTrue())

			Expect(parentQueueOf("a")).To(BeEmpty())
			Expect(recorder.Events).To(Receive(ContainSubstring(handlers.ParentQueueLoopReason)))
		})

		It("places a held queue once the queue blocking it moves away", func() {
			// a sits under b, and the two swap places. b is reconciled first, while a's queue is
			// still under b's.
			b := newDepartment("b", "", test.DefaultNodePoolName)
			a := newDepartment("a", "b", test.DefaultNodePoolName)
			handle(b)
			handle(a)
			Expect(parentQueueOf("a")).To(Equal("b"))

			reparent(b, "a")
			reparent(a, "")
			Expect(handle(b)).To(BeTrue())
			Expect(parentQueueOf("b")).To(BeEmpty())

			handle(a)
			Expect(handle(b)).To(BeFalse())
			Expect(parentQueueOf("a")).To(BeEmpty())
			Expect(parentQueueOf("b")).To(Equal("a"))
		})
	})

	It("sets the queue's min runtimes from the department", func() {
		dep1.Spec.Queues[0].MinRuntime = &kaiv1alpha1.QueueMinRuntime{
			PreemptMinRuntime: ptr.To("1h30m"),
			ReclaimMinRuntime: ptr.To("1w"),
		}

		_, err := handler.Handle(context.Background(), &dep1)
		Expect(err).Should(Succeed())

		queue, err := getQueue(k8sClient, dep1.Spec.Queues[0].Name)
		Expect(err).Should(Succeed())
		Expect(queue.Spec.PreemptMinRuntime).To(Equal(&metav1.Duration{Duration: 90 * time.Minute}))
		Expect(queue.Spec.ReclaimMinRuntime).To(Equal(&metav1.Duration{Duration: 7 * 24 * time.Hour}))
	})

	It("Fails to Handle queues", func() {
		badScheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(badScheme)).Should(Succeed())
		Expect(kaiv1alpha1.AddToScheme(badScheme)).Should(Succeed())
		// not adding kaiv2 to scheme - so queue handler should fail
		badK8sClient := fake.NewClientBuilder().WithScheme(badScheme).Build()
		badHandler := handlers.NewDepartmentHandler(badK8sClient, badK8sClient, events.NewFakeRecorder(10))

		Expect(badK8sClient.Create(context.TODO(), &dep1)).To(Succeed())

		_, err := badHandler.Handle(context.TODO(), &dep1)
		Expect(err).To(HaveOccurred())
	})
})

func assertQueuesIdenticalToDepartmentSpec(k8sClient client.Client,
	department *kaiv1alpha1.Department, departmentId string) {
	assertQueuesIdenticalToDepartmentSpecInner(k8sClient, department, departmentId, true)
}

func assertQueuesIdenticalToDepartmentSpecInner(k8sClient client.Client,
	department *kaiv1alpha1.Department, departmentId string, queueNameEqualToSuggested bool) {
	for _, depQueueSpec := range department.Spec.Queues {
		var queue kaiv2.Queue
		var err error

		if queueNameEqualToSuggested {
			queue, err = getQueue(k8sClient, handlers.GetQueueName(depQueueSpec, department.Name))
			Expect(err).ToNot(HaveOccurred())
			Expect(queue).ToNot(BeZero())

			Expect(queue.Name).To(Equal(handlers.GetQueueName(depQueueSpec, department.Name)))
		} else {
			queue, err = getQueueForDepartmentByLabel(k8sClient, department.Name, depQueueSpec.Nodepool)
			Expect(err).ToNot(HaveOccurred())
			Expect(queue).ToNot(BeZero())

			Expect(queue.Name).ToNot(Equal(handlers.GetQueueName(depQueueSpec, department.Name)))

			prefixToExpect := handlers.GetQueueName(depQueueSpec, department.Name)
			if len(prefixToExpect) > 58 {
				prefixToExpect = prefixToExpect[:58]
			}
			Expect(queue.Name).To(HavePrefix(prefixToExpect))
		}

		Expect(queue.Spec.ParentQueue).To(Equal(""))
		if depQueueSpec.Priority == nil {
			Expect(queue.Spec.Priority).To(Equal(ptr.To(handlers.DefaultQueuePriority)))
		} else {
			Expect(queue.Spec.Priority).To(Equal(ptr.To(int(*depQueueSpec.Priority))))
		}

		Expect(queue.Labels[test.QueueDepartmentNameLabel]).To(Equal(department.Name))
		if depQueueSpec.Nodepool != test.DefaultNodePoolName {
			Expect(queue.Labels[test.NodePoolLabelKey]).To(Equal(depQueueSpec.Nodepool))
		} else {
			Expect(queue.Labels).ToNot(HaveKey(test.NodePoolLabelKey))
		}

		Expect(queue.OwnerReferences).ToNot(BeZero())
		Expect(len(queue.OwnerReferences)).To(Equal(1))
		Expect(queue.OwnerReferences[0].APIVersion).To(Equal(department.APIVersion))
		Expect(queue.OwnerReferences[0].Kind).To(Equal(department.Kind))
		Expect(queue.OwnerReferences[0].Name).To(Equal(department.Name))
		Expect(queue.OwnerReferences[0].UID).To(Equal(department.UID))
		Expect(queue.OwnerReferences[0].Controller).To(Equal(&common.TrueRef))

		assertQueueResourcesIdenticalToDepartmentSpecResources(&queue, depQueueSpec)
	}
}

func assertQueueResourcesIdenticalToDepartmentSpecResources(
	queue *kaiv2.Queue, depQueueSpec kaiv1alpha1.QueueConfig) {
	Expect(queue.Spec.Resources.GPU.Limit).To(Equal(depQueueSpec.Resources.GPU.Limit))
	Expect(queue.Spec.Resources.GPU.OverQuotaWeight).To(Equal(depQueueSpec.Resources.GPU.OverQuotaWeight))
	Expect(queue.Spec.Resources.GPU.Quota).To(Equal(depQueueSpec.Resources.GPU.Deserved))

	Expect(queue.Spec.Resources.CPU.Limit).To(Equal(depQueueSpec.Resources.CPU.Limit))
	Expect(queue.Spec.Resources.CPU.OverQuotaWeight).To(Equal(depQueueSpec.Resources.CPU.OverQuotaWeight))
	Expect(queue.Spec.Resources.CPU.Quota).To(Equal(depQueueSpec.Resources.CPU.Deserved))

	Expect(queue.Spec.Resources.Memory.Limit).To(Equal(depQueueSpec.Resources.Memory.Limit))
	Expect(queue.Spec.Resources.Memory.OverQuotaWeight).To(Equal(depQueueSpec.Resources.Memory.OverQuotaWeight))
	Expect(queue.Spec.Resources.Memory.Quota).To(Equal(depQueueSpec.Resources.Memory.Deserved))
}

func expectQueuesEqualV2(left, right kaiv2.Queue) {
	var transformMap = func(m map[string]string) map[string]string {
		if m == nil {
			return map[string]string{}
		}
		return m
	}
	left.Labels = transformMap(left.Labels)
	right.Labels = transformMap(right.Labels)

	Expect(left.Name).To(Equal(right.Name))
	Expect(left.Labels).To(Equal(right.Labels))
	Expect(left.APIVersion).To(Equal(right.APIVersion))
	Expect(left.OwnerReferences).To(Equal(right.OwnerReferences))
	Expect(left.Spec).To(Equal(right.Spec))
}

func getQueue(k8sClient client.Client, queueName string) (kaiv2.Queue, error) {
	queue := kaiv2.Queue{}
	err := k8sClient.Get(context.Background(),
		client.ObjectKey{Name: queueName},
		&queue,
	)
	return queue, err
}

func getQueueForDepartmentByLabel(k8sClient client.Client, departmentName, nodepoolName string) (kaiv2.Queue, error) {
	queues := &kaiv2.QueueList{}
	err := k8sClient.List(context.Background(), queues,
		client.MatchingLabels(map[string]string{test.QueueDepartmentNameLabel: departmentName}))
	if err != nil {
		return kaiv2.Queue{}, err
	}

	for _, queue := range queues.Items {
		// The department-name label is shared with project queues; only consider queues owned
		// by a Department.
		if !isOwnedByDepartment(queue.OwnerReferences) {
			continue
		}

		nodePoolLabelValue, found := queue.Labels[test.NodePoolLabelKey]

		if nodepoolName == test.DefaultNodePoolName && !found {
			return queue, nil
		}

		if nodepoolName != test.DefaultNodePoolName && found && nodePoolLabelValue == nodepoolName {
			return queue, nil
		}
	}

	return kaiv2.Queue{}, fmt.Errorf("couldn't find queue for department %s and nodepool %s",
		departmentName, nodepoolName)
}

// isOwnedByDepartment reports whether the queue is owned by a Department (vs a Project), matching
// how the handlers distinguish department queues from project queues that share the department
// name label.
func isOwnedByDepartment(ownerRefs []metav1.OwnerReference) bool {
	for _, ref := range ownerRefs {
		if ref.Kind == common.DepartmentKind {
			return true
		}
	}
	return false
}
