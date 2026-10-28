// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	kaiv2 "github.com/kai-scheduler/api/scheduling/v2"
	. "github.com/onsi/ginkgo/v2" // nolint:staticcheck // dot import for test framework is intentional
	. "github.com/onsi/gomega"    // nolint:staticcheck // dot import for test framework is intentional

	admissionv1 "k8s.io/api/admission/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/webhooks/validation"
)

func TestValidationWebhook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Validation Webhook Tests")
}

const (
	existingNodePoolA = "np-a"
	existingNodePoolB = "np-b"
	missingNodePool   = "np-missing"
	existingParent    = "dep-1"
	missingParent     = "dep-missing"

	departmentQueue = "dep-1-queue"
	projectQueue    = "proj-1-queue"
	adminQueue      = "admin-queue"
	missingQueue    = "queue-missing"
)

var (
	departmentOwnerRef = metav1.OwnerReference{
		APIVersion: kaiv1alpha1.GroupVersion.Identifier(), Kind: "Department", Name: existingParent, UID: "dep-uid"}
	projectOwnerRef = metav1.OwnerReference{
		APIVersion: kaiv1alpha1.GroupVersion.Identifier(), Kind: "Project", Name: "proj-1", UID: "proj-uid"}
)

var _ = Describe("Validation webhook Handle", func() {
	var (
		ctx       context.Context
		validator *validation.Validator
	)

	BeforeEach(func() {
		ctx = context.Background()

		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(kaiv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(kaiv2.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			&kaiv1alpha1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: existingNodePoolA}},
			&kaiv1alpha1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: existingNodePoolB}},
			&kaiv1alpha1.Department{ObjectMeta: metav1.ObjectMeta{Name: existingParent}},
			&kaiv2.Queue{ObjectMeta: metav1.ObjectMeta{
				Name: departmentQueue, OwnerReferences: []metav1.OwnerReference{departmentOwnerRef}}},
			&kaiv2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: projectQueue, OwnerReferences: []metav1.OwnerReference{projectOwnerRef}},
				Spec:       kaiv2.QueueSpec{ParentQueue: departmentQueue},
			},
			&kaiv2.Queue{ObjectMeta: metav1.ObjectMeta{Name: adminQueue}},
		).Build()

		validator = validation.NewValidator(fakeClient, fakeClient)
	})

	queue := func(name, nodepool string) kaiv1alpha1.QueueConfig {
		return kaiv1alpha1.QueueConfig{Name: name, Nodepool: nodepool}
	}

	handleProject := func(spec kaiv1alpha1.ProjectSpec) admission.Response {
		project := &kaiv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: "proj-1"},
			Spec:       spec,
		}
		raw, err := json.Marshal(project)
		Expect(err).NotTo(HaveOccurred())
		return validator.Handle(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   kaiv1alpha1.GroupVersion.Group,
				Version: kaiv1alpha1.GroupVersion.Version,
				Kind:    "Project",
			},
			Object: runtime.RawExtension{Raw: raw},
		}})
	}

	handleDepartment := func(spec kaiv1alpha1.DepartmentSpec) admission.Response {
		department := &kaiv1alpha1.Department{
			ObjectMeta: metav1.ObjectMeta{Name: "dep-x"},
			Spec:       spec,
		}
		raw, err := json.Marshal(department)
		Expect(err).NotTo(HaveOccurred())
		return validator.Handle(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   kaiv1alpha1.GroupVersion.Group,
				Version: kaiv1alpha1.GroupVersion.Version,
				Kind:    "Department",
			},
			Object: runtime.RawExtension{Raw: raw},
		}})
	}

	expectDenied := func(resp admission.Response, substr string) {
		Expect(resp.Allowed).To(BeFalse())
		Expect(resp.Result).NotTo(BeNil())
		Expect(resp.Result.Message).To(ContainSubstring(substr))
	}

	Context("Project", func() {
		It("allows an empty parent with no queues and no default node pools", func() {
			resp := handleProject(kaiv1alpha1.ProjectSpec{})
			Expect(resp.Allowed).To(BeTrue())
		})

		It("allows an existing parent and queues referencing existing node pools", func() {
			resp := handleProject(kaiv1alpha1.ProjectSpec{
				Parent: existingParent,
				Queues: []kaiv1alpha1.QueueConfig{queue("q-a", existingNodePoolA)},
			})
			Expect(resp.Allowed).To(BeTrue())
		})

		It("denies a non-empty parent that does not exist", func() {
			resp := handleProject(kaiv1alpha1.ProjectSpec{Parent: missingParent})
			expectDenied(resp, missingParent)
		})

		It("denies a queue that references a non-existing node pool", func() {
			resp := handleProject(kaiv1alpha1.ProjectSpec{
				Queues: []kaiv1alpha1.QueueConfig{queue("q-missing", missingNodePool)},
			})
			expectDenied(resp, missingNodePool)
		})

		It("denies a queue with an empty node pool", func() {
			resp := handleProject(kaiv1alpha1.ProjectSpec{
				Queues: []kaiv1alpha1.QueueConfig{queue("q-empty", "")},
			})
			expectDenied(resp, "node pool must not be empty")
		})

		It("denies the same node pool being referenced by more than one queue", func() {
			resp := handleProject(kaiv1alpha1.ProjectSpec{
				Queues: []kaiv1alpha1.QueueConfig{
					queue("q-a", existingNodePoolA),
					queue("q-a-dup", existingNodePoolA),
				},
			})
			expectDenied(resp, "is already referenced by another queue")
		})

		It("denies a default node pool that has no queue in the spec", func() {
			resp := handleProject(kaiv1alpha1.ProjectSpec{
				Queues:           []kaiv1alpha1.QueueConfig{queue("q-a", existingNodePoolA)},
				DefaultNodePools: []string{existingNodePoolB},
			})
			expectDenied(resp, existingNodePoolB)
		})

		It("allows a default node pool that is backed by a queue", func() {
			resp := handleProject(kaiv1alpha1.ProjectSpec{
				Queues:           []kaiv1alpha1.QueueConfig{queue("q-a", existingNodePoolA)},
				DefaultNodePools: []string{existingNodePoolA},
			})
			Expect(resp.Allowed).To(BeTrue())
		})

		It("errors when the project object cannot be unmarshaled", func() {
			resp := validator.Handle(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   kaiv1alpha1.GroupVersion.Group,
					Version: kaiv1alpha1.GroupVersion.Version,
					Kind:    "Project",
				},
				Object: runtime.RawExtension{Raw: []byte("not-json")},
			}})
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Message).To(ContainSubstring("failed to unmarshal"))
		})
	})

	Context("Department", func() {
		It("allows empty queues", func() {
			resp := handleDepartment(kaiv1alpha1.DepartmentSpec{})
			Expect(resp.Allowed).To(BeTrue())
		})

		It("allows queues referencing existing node pools", func() {
			resp := handleDepartment(kaiv1alpha1.DepartmentSpec{
				Queues: []kaiv1alpha1.QueueConfig{queue("q-a", existingNodePoolA)},
			})
			Expect(resp.Allowed).To(BeTrue())
		})

		It("denies a queue that references a non-existing node pool", func() {
			resp := handleDepartment(kaiv1alpha1.DepartmentSpec{
				Queues: []kaiv1alpha1.QueueConfig{queue("q-missing", missingNodePool)},
			})
			expectDenied(resp, missingNodePool)
		})

		It("denies a queue with an empty node pool", func() {
			resp := handleDepartment(kaiv1alpha1.DepartmentSpec{
				Queues: []kaiv1alpha1.QueueConfig{queue("q-empty", "")},
			})
			expectDenied(resp, "node pool must not be empty")
		})

		It("denies the same node pool being referenced by more than one queue", func() {
			resp := handleDepartment(kaiv1alpha1.DepartmentSpec{
				Queues: []kaiv1alpha1.QueueConfig{
					queue("q-a", existingNodePoolA),
					queue("q-a-dup", existingNodePoolA),
				},
			})
			expectDenied(resp, "is already referenced by another queue")
		})

		It("errors when the department object cannot be unmarshaled", func() {
			resp := validator.Handle(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   kaiv1alpha1.GroupVersion.Group,
					Version: kaiv1alpha1.GroupVersion.Version,
					Kind:    "Department",
				},
				Object: runtime.RawExtension{Raw: []byte("not-json")},
			}})
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Message).To(ContainSubstring("failed to unmarshal"))
		})

		Context("parent", func() {
			newScheme := func() *runtime.Scheme {
				scheme := runtime.NewScheme()
				Expect(kaiv1alpha1.AddToScheme(scheme)).To(Succeed())
				return scheme
			}

			department := func(name, parent string) *kaiv1alpha1.Department {
				return &kaiv1alpha1.Department{
					ObjectMeta: metav1.ObjectMeta{Name: name},
					Spec:       kaiv1alpha1.DepartmentSpec{Parent: parent},
				}
			}

			// chain returns <prefix>1 → <prefix>2 → … → <prefix><n>, <prefix>1 being top-level.
			chain := func(prefix string, n int) []client.Object {
				departments := []client.Object{department(prefix+"1", "")}
				for i := 2; i <= n; i++ {
					departments = append(departments, department(fmt.Sprintf("%s%d", prefix, i), fmt.Sprintf("%s%d", prefix, i-1)))
				}
				return departments
			}

			validatorWith := func(departments ...client.Object) *validation.Validator {
				c := fake.NewClientBuilder().WithScheme(newScheme()).WithObjects(departments...).Build()
				return validation.NewValidator(c, c)
			}

			// request sends a create of the department, or an update when oldParent is set.
			request := func(v *validation.Validator, name, parent string, oldParent *string) admission.Response {
				raw, err := json.Marshal(department(name, parent))
				Expect(err).NotTo(HaveOccurred())
				req := admissionv1.AdmissionRequest{
					Operation: admissionv1.Create,
					Kind: metav1.GroupVersionKind{
						Group:   kaiv1alpha1.GroupVersion.Group,
						Version: kaiv1alpha1.GroupVersion.Version,
						Kind:    "Department",
					},
					Object: runtime.RawExtension{Raw: raw},
				}
				if oldParent != nil {
					oldRaw, err := json.Marshal(department(name, *oldParent))
					Expect(err).NotTo(HaveOccurred())
					req.Operation = admissionv1.Update
					req.OldObject = runtime.RawExtension{Raw: oldRaw}
				}
				return v.Handle(ctx, admission.Request{AdmissionRequest: req})
			}

			It("allows an existing parent", func() {
				Expect(handleDepartment(kaiv1alpha1.DepartmentSpec{Parent: existingParent}).Allowed).To(BeTrue())
			})

			It("denies a parent that does not exist", func() {
				expectDenied(handleDepartment(kaiv1alpha1.DepartmentSpec{Parent: missingParent}),
					`parent department "`+missingParent+`" does not exist`)
			})

			It("re-reads a parent the cache has not seen yet", func() {
				emptyCache := fake.NewClientBuilder().WithScheme(newScheme()).Build()
				apiServer := fake.NewClientBuilder().WithScheme(newScheme()).
					WithObjects(department("research", "")).Build()

				resp := request(validation.NewValidator(emptyCache, apiServer), "ml", "research", nil)

				Expect(resp.Allowed).To(BeTrue(), resp.Result.Message)
			})

			It("denies a parent that is being deleted", func() {
				deleting := department("research", "")
				deleting.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				deleting.Finalizers = []string{"test"}

				expectDenied(request(validatorWith(deleting), "ml", "research", nil),
					`parent department "research" is being deleted`)
			})

			It("denies a move that would make a loop", func() {
				v := validatorWith(chain("d", 3)...)

				expectDenied(request(v, "d1", "d3", ptr.To("")),
					`department "d1" cannot have parent "d3": that would make a loop: d1 → d3 → d2 → d1`)
			})

			It("allows departments to nest 7 levels deep and denies an 8th", func() {
				v := validatorWith(chain("d", 7)...)

				Expect(request(v, "sibling", "d6", nil).Allowed).To(BeTrue())
				expectDenied(request(v, "too-deep", "d7", nil), "departments can nest at most 7 levels deep")
			})

			It("counts the moved department's own sub-departments", func() {
				v := validatorWith(append(chain("d", 5), chain("s", 3)...)...)

				Expect(request(v, "s1", "d4", ptr.To("")).Allowed).To(BeTrue())
				expectDenied(request(v, "s1", "d5", ptr.To("")), "departments can nest at most 7 levels deep")
			})

			It("allows any other change to a department that is already too deep", func() {
				v := validatorWith(chain("d", 9)...)

				Expect(request(v, "d9", "d8", ptr.To("d8")).Allowed).To(BeTrue())
			})

			It("reports a missing node pool and a missing parent together", func() {
				resp := handleDepartment(kaiv1alpha1.DepartmentSpec{
					Parent: missingParent,
					Queues: []kaiv1alpha1.QueueConfig{queue("q-missing", missingNodePool)},
				})

				expectDenied(resp, missingNodePool)
				expectDenied(resp, missingParent)
			})

			It("returns an internal server error when listing departments fails", func() {
				failingClient := fake.NewClientBuilder().WithScheme(newScheme()).
					WithObjects(department("research", "")).
					WithInterceptorFuncs(interceptor.Funcs{
						List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
							if _, ok := list.(*kaiv1alpha1.DepartmentList); ok {
								return apierrors.NewServiceUnavailable("api server is down")
							}
							return c.List(ctx, list, opts...)
						},
					}).Build()

				resp := request(validation.NewValidator(failingClient, failingClient), "ml", "research", nil)

				Expect(resp.Allowed).To(BeFalse())
				Expect(resp.Result.Code).To(Equal(int32(http.StatusInternalServerError)))
			})
		})
	})

	Context("Queue", func() {
		newQueue := func(name, parent string, ownerRefs ...metav1.OwnerReference) *kaiv2.Queue {
			return &kaiv2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: name, OwnerReferences: ownerRefs},
				Spec:       kaiv2.QueueSpec{ParentQueue: parent},
			}
		}

		queueRequest := func(operation admissionv1.Operation, queue, oldQueue *kaiv2.Queue) admission.Request {
			raw, err := json.Marshal(queue)
			Expect(err).NotTo(HaveOccurred())
			request := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: operation,
				Kind: metav1.GroupVersionKind{
					Group:   kaiv2.GroupVersion.Group,
					Version: kaiv2.GroupVersion.Version,
					Kind:    "Queue",
				},
				Object: runtime.RawExtension{Raw: raw},
			}}
			if oldQueue != nil {
				oldRaw, err := json.Marshal(oldQueue)
				Expect(err).NotTo(HaveOccurred())
				request.OldObject = runtime.RawExtension{Raw: oldRaw}
			}
			return request
		}

		handleCreate := func(queue *kaiv2.Queue) admission.Response {
			return validator.Handle(ctx, queueRequest(admissionv1.Create, queue, nil))
		}

		handleUpdate := func(oldQueue, queue *kaiv2.Queue) admission.Response {
			return validator.Handle(ctx, queueRequest(admissionv1.Update, queue, oldQueue))
		}

		It("allows a root queue", func() {
			Expect(handleCreate(newQueue("team-a", "")).Allowed).To(BeTrue())
		})

		It("allows an admin queue under an admin queue", func() {
			Expect(handleCreate(newQueue("team-a", adminQueue)).Allowed).To(BeTrue())
		})

		It("allows a project queue under its department queue", func() {
			Expect(handleCreate(newQueue("proj-2-queue", departmentQueue, projectOwnerRef)).Allowed).To(BeTrue())
		})

		It("allows a queue whose parent does not exist", func() {
			Expect(handleCreate(newQueue("team-a", missingQueue)).Allowed).To(BeTrue())
		})

		It("denies an admin queue under a department queue, naming the parent and its owner", func() {
			resp := handleCreate(newQueue("team-a", departmentQueue))
			expectDenied(resp, `queue "team-a" cannot use "dep-1-queue" as its parent queue`)
			Expect(resp.Result.Message).To(ContainSubstring(`Department "dep-1"`))
		})

		It("denies an admin queue under a project queue", func() {
			resp := handleCreate(newQueue("team-a", projectQueue))
			expectDenied(resp, `Project "proj-1"`)
		})

		It("denies a queue owned by a Project of another API group", func() {
			foreignOwner := metav1.OwnerReference{APIVersion: "example.com/v1", Kind: "Project", Name: "proj-1", UID: "x"}
			resp := handleCreate(newQueue("team-a", departmentQueue, foreignOwner))
			expectDenied(resp, "cannot use")
		})

		It("denies moving an existing queue under a KRM queue", func() {
			resp := handleUpdate(newQueue("team-a", adminQueue), newQueue("team-a", departmentQueue))
			expectDenied(resp, "cannot use")
		})

		It("allows any update that leaves an already grafted parent unchanged", func() {
			// The tree may predate the webhook; the queue must stay editable and deletable.
			grafted := newQueue("team-a", departmentQueue)
			updated := grafted.DeepCopy()
			updated.Finalizers = []string{"example.com/finalizer"}
			Expect(handleUpdate(grafted, updated).Allowed).To(BeTrue())
		})

		It("allows a project queue to lose its owner when its parent is unchanged", func() {
			// The garbage collector does exactly this when a Project is deleted with orphaning.
			owned := newQueue(projectQueue, departmentQueue, projectOwnerRef)
			Expect(handleUpdate(owned, newQueue(projectQueue, departmentQueue)).Allowed).To(BeTrue())
		})

		It("errors when the queue object cannot be unmarshaled", func() {
			request := queueRequest(admissionv1.Create, newQueue("team-a", ""), nil)
			request.Object = runtime.RawExtension{Raw: []byte("not-json")}
			resp := validator.Handle(ctx, request)
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Message).To(ContainSubstring("failed to unmarshal queue"))
		})

		It("errors when the old queue object cannot be unmarshaled", func() {
			request := queueRequest(admissionv1.Update, newQueue("team-a", ""), newQueue("team-a", ""))
			request.OldObject = runtime.RawExtension{Raw: []byte("not-json")}
			resp := validator.Handle(ctx, request)
			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result.Message).To(ContainSubstring("failed to unmarshal old queue"))
		})
	})

	It("denies an unsupported resource kind", func() {
		resp := validator.Handle(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
			Kind: metav1.GroupVersionKind{
				Group:   kaiv1alpha1.GroupVersion.Group,
				Version: kaiv1alpha1.GroupVersion.Version,
				Kind:    "Unsupported",
			},
			Object: runtime.RawExtension{Raw: []byte("{}")},
		}})
		Expect(resp.Allowed).To(BeFalse())
		Expect(resp.Result).NotTo(BeNil())
	})

	Context("backend failures", func() {
		It("returns an internal server error when listing node pools fails", func() {
			scheme := runtime.NewScheme()
			Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
			Expect(kaiv1alpha1.AddToScheme(scheme)).To(Succeed())

			failingClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return apierrors.NewServiceUnavailable("api server is down")
				},
			}).Build()
			failingValidator := validation.NewValidator(failingClient, failingClient)

			project := &kaiv1alpha1.Project{
				ObjectMeta: metav1.ObjectMeta{Name: "proj-1"},
				Spec:       kaiv1alpha1.ProjectSpec{Queues: []kaiv1alpha1.QueueConfig{queue("q-a", existingNodePoolA)}},
			}
			raw, err := json.Marshal(project)
			Expect(err).NotTo(HaveOccurred())

			resp := failingValidator.Handle(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Kind: metav1.GroupVersionKind{
					Group:   kaiv1alpha1.GroupVersion.Group,
					Version: kaiv1alpha1.GroupVersion.Version,
					Kind:    "Project",
				},
				Object: runtime.RawExtension{Raw: raw},
			}})

			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result).NotTo(BeNil())
			Expect(resp.Result.Code).To(Equal(int32(http.StatusInternalServerError)))
		})

		It("returns an internal server error when getting the parent queue fails", func() {
			scheme := runtime.NewScheme()
			Expect(kaiv2.AddToScheme(scheme)).To(Succeed())

			failingClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return apierrors.NewServiceUnavailable("api server is down")
				},
			}).Build()
			failingValidator := validation.NewValidator(failingClient, failingClient)

			queue := &kaiv2.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "team-a"},
				Spec:       kaiv2.QueueSpec{ParentQueue: departmentQueue},
			}
			raw, err := json.Marshal(queue)
			Expect(err).NotTo(HaveOccurred())

			resp := failingValidator.Handle(ctx, admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Operation: admissionv1.Create,
				Kind: metav1.GroupVersionKind{
					Group:   kaiv2.GroupVersion.Group,
					Version: kaiv2.GroupVersion.Version,
					Kind:    "Queue",
				},
				Object: runtime.RawExtension{Raw: raw},
			}})

			Expect(resp.Allowed).To(BeFalse())
			Expect(resp.Result).NotTo(BeNil())
			Expect(resp.Result.Code).To(Equal(int32(http.StatusInternalServerError)))
		})
	})
})
