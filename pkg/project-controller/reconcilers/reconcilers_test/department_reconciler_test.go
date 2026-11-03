// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package reconcilers_test

import (
	"context"
	"time"

	kaiv2 "github.com/kai-scheduler/api/scheduling/v2"
	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/config"
	. "github.com/kai-scheduler/kai-resource-management/pkg/project-controller/reconcilers"
	. "github.com/kai-scheduler/kai-resource-management/pkg/project-controller/test"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Department Reconciler Tests", func() {

	var (
		reconciler *DepartmentReconciler
		k8sClient  client.Client
		department kaiv1alpha1.Department
		project    kaiv1alpha1.Project

		departmentReconcileRequest ctrl.Request
	)

	BeforeEach(func() {
		k8sClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&kaiv1alpha1.Department{}).
			WithIndex(&kaiv1alpha1.Project{}, ProjectDepartmentIndexField, IndexProjectByDepartment).
			WithIndex(&kaiv1alpha1.Department{}, DepartmentParentIndexField, IndexDepartmentByParent).
			Build()
		reconciler = NewDepartmentReconciler(k8sClient, k8sClient, events.NewFakeRecorder(10))

		department = *KaiTestDepartment1.DeepCopy()
		project = *TestProject.DeepCopy()
		// Sanity: the test project references the test department by name.
		Expect(project.Spec.Parent).To(Equal(department.Name))

		departmentReconcileRequest = ctrl.Request{
			NamespacedName: types.NamespacedName{Name: department.Name},
		}
	})

	It("Adds the controller to the department finalizers list", func() {
		// Given
		Expect(k8sClient.Create(context.TODO(), &department)).To(Succeed())

		// When
		result, err := reconciler.Reconcile(context.TODO(), departmentReconcileRequest)

		// Then
		Expect(result).To(BeZero())
		Expect(err).ToNot(HaveOccurred())

		actual := kaiv1alpha1.Department{}
		Expect(k8sClient.Get(context.TODO(), departmentReconcileRequest.NamespacedName, &actual)).To(Succeed())
		Expect(actual.Finalizers).To(ContainElement(config.DepartmentFinalizerName()))
	})

	When("A department is being deleted", func() {
		It("Blocks deletion while a project still references the department", func() {
			// Given - a department being deleted, with our finalizer set, and a project that references it
			deletedDepartment := department.DeepCopy()
			deletedDepartment.DeletionTimestamp = &now
			deletedDepartment.Finalizers = []string{config.DepartmentFinalizerName()}

			k8sClientWithDeleted := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&kaiv1alpha1.Department{}).
				WithIndex(&kaiv1alpha1.Project{}, ProjectDepartmentIndexField, IndexProjectByDepartment).
				WithIndex(&kaiv1alpha1.Department{}, DepartmentParentIndexField, IndexDepartmentByParent).
				WithObjects(deletedDepartment, &project).
				Build()
			reconcilerWithDeleted := NewDepartmentReconciler(k8sClientWithDeleted, k8sClientWithDeleted, events.NewFakeRecorder(10))

			// When
			result, err := reconcilerWithDeleted.Reconcile(context.TODO(), departmentReconcileRequest)

			// Then - mirrors the Project reconciler: an error is returned so the request
			// is requeued with backoff, and our finalizer is kept in place.
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeZero())

			By("Keeping the finalizer in place so the department is not removed + Setting a deletion-blocked condition on the department status", func() {
				actual := kaiv1alpha1.Department{}
				Expect(k8sClientWithDeleted.Get(context.TODO(),
					departmentReconcileRequest.NamespacedName, &actual)).To(Succeed())
				Expect(actual.Finalizers).To(ContainElement(config.DepartmentFinalizerName()))

				var blockedCondition *kaiv1alpha1.DepartmentCondition
				for i := range actual.Status.Conditions {
					if actual.Status.Conditions[i].Type == kaiv1alpha1.DepartmentDeletionBlocked {
						blockedCondition = &actual.Status.Conditions[i]
						break
					}
				}
				Expect(blockedCondition).ToNot(BeNil())
				Expect(blockedCondition.Status).To(Equal(corev1.ConditionTrue))
				Expect(blockedCondition.Reason).To(Equal("ProjectsStillAssigned"))
				Expect(blockedCondition.Message).ToNot(BeEmpty())
			})
		})

		It("Removes the finalizer when no project references the department", func() {
			// Given - a department being deleted, with our finalizer plus another finalizer so the
			// fake client keeps the object around after we remove ours, and no project referencing it.
			deletedDepartment := department.DeepCopy()
			deletedDepartment.DeletionTimestamp = &now
			deletedDepartment.Finalizers = []string{config.DepartmentFinalizerName(), "other-finalizer"}

			k8sClientWithDeleted := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&kaiv1alpha1.Department{}).
				WithIndex(&kaiv1alpha1.Project{}, ProjectDepartmentIndexField, IndexProjectByDepartment).
				WithIndex(&kaiv1alpha1.Department{}, DepartmentParentIndexField, IndexDepartmentByParent).
				WithObjects(deletedDepartment).
				Build()
			reconcilerWithDeleted := NewDepartmentReconciler(k8sClientWithDeleted, k8sClientWithDeleted, events.NewFakeRecorder(10))

			// When
			result, err := reconcilerWithDeleted.Reconcile(context.TODO(), departmentReconcileRequest)

			// Then
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())

			By("Removing our finalizer from the department", func() {
				actual := kaiv1alpha1.Department{}
				Expect(k8sClientWithDeleted.Get(context.TODO(),
					departmentReconcileRequest.NamespacedName, &actual)).To(Succeed())
				Expect(actual.Finalizers).ToNot(ContainElement(config.DepartmentFinalizerName()))
				Expect(actual.Finalizers).To(ContainElement("other-finalizer"))

				Expect(actual.Status.Conditions).To(BeEmpty())
			})
		})

		It("Deletes the department once our finalizer is the last one removed", func() {
			// Given - a department being deleted whose only finalizer is ours, and no
			// project referencing it. Removing our finalizer should let the fake client
			// actually delete the object.
			deletedDepartment := department.DeepCopy()
			deletedDepartment.DeletionTimestamp = &now
			deletedDepartment.Finalizers = []string{config.DepartmentFinalizerName()}

			k8sClientWithDeleted := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&kaiv1alpha1.Department{}).
				WithIndex(&kaiv1alpha1.Project{}, ProjectDepartmentIndexField, IndexProjectByDepartment).
				WithIndex(&kaiv1alpha1.Department{}, DepartmentParentIndexField, IndexDepartmentByParent).
				WithObjects(deletedDepartment).
				Build()
			reconcilerWithDeleted := NewDepartmentReconciler(k8sClientWithDeleted, k8sClientWithDeleted, events.NewFakeRecorder(10))

			// When
			result, err := reconcilerWithDeleted.Reconcile(context.TODO(), departmentReconcileRequest)

			// Then
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())

			By("Removing the department from the cluster", func() {
				actual := kaiv1alpha1.Department{}
				getErr := k8sClientWithDeleted.Get(context.TODO(),
					departmentReconcileRequest.NamespacedName, &actual)
				Expect(apierrors.IsNotFound(getErr)).To(BeTrue())
			})
		})

		It("Does not block deletion for projects that reference a different department", func() {
			// Given - a project assigned to a different department
			otherProject := project.DeepCopy()
			otherProject.Spec.Parent = "some-other-department"

			deletedDepartment := department.DeepCopy()
			deletedDepartment.DeletionTimestamp = &now
			deletedDepartment.Finalizers = []string{config.DepartmentFinalizerName(), "other-finalizer"}

			k8sClientWithDeleted := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&kaiv1alpha1.Department{}).
				WithIndex(&kaiv1alpha1.Project{}, ProjectDepartmentIndexField, IndexProjectByDepartment).
				WithIndex(&kaiv1alpha1.Department{}, DepartmentParentIndexField, IndexDepartmentByParent).
				WithObjects(deletedDepartment, otherProject).
				Build()
			reconcilerWithDeleted := NewDepartmentReconciler(k8sClientWithDeleted, k8sClientWithDeleted, events.NewFakeRecorder(10))

			// When
			result, err := reconcilerWithDeleted.Reconcile(context.TODO(), departmentReconcileRequest)

			// Then
			Expect(result).To(BeZero())
			Expect(err).ToNot(HaveOccurred())

			actual := kaiv1alpha1.Department{}
			Expect(k8sClientWithDeleted.Get(context.TODO(),
				departmentReconcileRequest.NamespacedName, &actual)).To(Succeed())
			Expect(actual.Finalizers).ToNot(ContainElement(config.DepartmentFinalizerName()))
		})

		Context("with child departments", func() {
			// reconcileDeletion reconciles the department being deleted next to the given other
			// departments, and returns it as it stands afterwards.
			reconcileDeletion := func(others ...*kaiv1alpha1.Department) (
				kaiv1alpha1.Department, client.Client, error) {
				deletedDepartment := department.DeepCopy()
				deletedDepartment.DeletionTimestamp = &now
				deletedDepartment.Finalizers = []string{config.DepartmentFinalizerName(), "other-finalizer"}

				objects := []client.Object{deletedDepartment}
				for _, other := range others {
					objects = append(objects, other)
				}
				c := fake.NewClientBuilder().
					WithScheme(scheme).
					WithStatusSubresource(&kaiv1alpha1.Department{}).
					WithIndex(&kaiv1alpha1.Project{}, ProjectDepartmentIndexField, IndexProjectByDepartment).
					WithIndex(&kaiv1alpha1.Department{}, DepartmentParentIndexField, IndexDepartmentByParent).
					WithObjects(objects...).
					Build()

				_, err := NewDepartmentReconciler(c, c, events.NewFakeRecorder(10)).
					Reconcile(context.TODO(), departmentReconcileRequest)
				actual := kaiv1alpha1.Department{}
				Expect(c.Get(context.TODO(), departmentReconcileRequest.NamespacedName, &actual)).To(Succeed())
				return actual, c, err
			}

			childOf := func(name, parent string) *kaiv1alpha1.Department {
				return &kaiv1alpha1.Department{
					ObjectMeta: metav1.ObjectMeta{Name: name},
					Spec:       kaiv1alpha1.DepartmentSpec{Parent: parent},
				}
			}

			It("Blocks deletion while a child department still references the department", func() {
				actual, _, err := reconcileDeletion(childOf("child", department.Name))

				Expect(err).To(MatchError(ContainSubstring("child department(s) still reference it")))
				Expect(actual.Finalizers).To(ContainElement(config.DepartmentFinalizerName()))
				Expect(actual.Status.Conditions).To(ContainElement(SatisfyAll(
					HaveField("Type", kaiv1alpha1.DepartmentDeletionBlocked),
					HaveField("Status", corev1.ConditionTrue),
					HaveField("Reason", "ChildDepartmentsStillAssigned"),
					HaveField("Message", ContainSubstring(`"child"`)),
				)))
			})

			It("Removes the finalizer once the child department moves away", func() {
				child := childOf("child", department.Name)
				_, c, err := reconcileDeletion(child)
				Expect(err).To(HaveOccurred())

				Expect(c.Get(context.TODO(), client.ObjectKeyFromObject(child), child)).To(Succeed())
				child.Spec.Parent = ""
				Expect(c.Update(context.TODO(), child)).To(Succeed())
				_, err = NewDepartmentReconciler(c, c, events.NewFakeRecorder(10)).
					Reconcile(context.TODO(), departmentReconcileRequest)

				Expect(err).ToNot(HaveOccurred())
				actual := kaiv1alpha1.Department{}
				Expect(c.Get(context.TODO(), departmentReconcileRequest.NamespacedName, &actual)).To(Succeed())
				Expect(actual.Finalizers).ToNot(ContainElement(config.DepartmentFinalizerName()))
			})
		})
	})

	It("Retries a department whose queue is kept a root queue to avoid a loop", func() {
		department := func(name, parent string) *kaiv1alpha1.Department {
			return &kaiv1alpha1.Department{
				ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name + "-uid")},
				Spec: kaiv1alpha1.DepartmentSpec{
					Parent: parent,
					Queues: []kaiv1alpha1.QueueConfig{{Nodepool: DefaultNodePoolName}},
				},
			}
		}
		Expect(k8sClient.Create(context.TODO(), department("a", "b"))).To(Succeed())
		Expect(k8sClient.Create(context.TODO(), department("b", "a"))).To(Succeed())
		request := func(name string) ctrl.Request {
			return ctrl.Request{NamespacedName: types.NamespacedName{Name: name}}
		}

		for _, name := range []string{"a", "b"} {
			result, err := reconciler.Reconcile(context.TODO(), request(name))
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeZero())
		}
		result, err := reconciler.Reconcile(context.TODO(), request("a"))

		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Minute))
	})

	Describe("Watching queues for child departments", func() {
		departmentQueue := func(owner metav1.OwnerReference) *kaiv2.Queue {
			return &kaiv2.Queue{ObjectMeta: metav1.ObjectMeta{
				Name: owner.Name, OwnerReferences: []metav1.OwnerReference{owner}}}
		}
		ownedBy := func(kind, name string) metav1.OwnerReference {
			return metav1.OwnerReference{APIVersion: kaiv1alpha1.GroupVersion.Identifier(), Kind: kind, Name: name}
		}

		It("Maps a department's queue to the departments under that department", func() {
			child := func(name, parent string) *kaiv1alpha1.Department {
				return &kaiv1alpha1.Department{
					ObjectMeta: metav1.ObjectMeta{Name: name},
					Spec:       kaiv1alpha1.DepartmentSpec{Parent: parent},
				}
			}
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithIndex(&kaiv1alpha1.Department{}, DepartmentParentIndexField, IndexDepartmentByParent).
				WithObjects(child("ml", "research"), child("vision", "research"), child("infra", "platform")).
				Build()
			mapper := NewDepartmentReconciler(c, c, events.NewFakeRecorder(10))

			Expect(mapper.MapQueueToChildDepartments(context.TODO(),
				departmentQueue(ownedBy("Department", "research")))).To(ConsistOf(
				ctrl.Request{NamespacedName: types.NamespacedName{Name: "ml"}},
				ctrl.Request{NamespacedName: types.NamespacedName{Name: "vision"}},
			))
			Expect(mapper.MapQueueToChildDepartments(context.TODO(),
				departmentQueue(ownedBy("Project", "research")))).To(BeEmpty())
			Expect(mapper.MapQueueToChildDepartments(context.TODO(), &kaiv2.Queue{})).To(BeEmpty())
		})

		It("Passes a queue that appears, goes away or gets a new parent, and drops status updates", func() {
			before := departmentQueue(ownedBy("Department", "research"))
			statusOnly := before.DeepCopy()
			statusOnly.Status.Allocated = corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}
			moved := before.DeepCopy()
			moved.Spec.ParentQueue = "platform"

			Expect(QueuePlacementChangedPredicate.Create(event.CreateEvent{Object: before})).To(BeTrue())
			Expect(QueuePlacementChangedPredicate.Delete(event.DeleteEvent{Object: before})).To(BeTrue())
			Expect(QueuePlacementChangedPredicate.Update(
				event.UpdateEvent{ObjectOld: before, ObjectNew: moved})).To(BeTrue())
			Expect(QueuePlacementChangedPredicate.Update(
				event.UpdateEvent{ObjectOld: before, ObjectNew: statusOnly})).To(BeFalse())
		})
	})
})
