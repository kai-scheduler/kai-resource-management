// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"context"
	"encoding/json"

	jsonpatch "github.com/evanphx/json-patch/v5"
	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/config"
)

const (
	testNamespaceProjectLabelKey = "test/project"
	projectNamespace             = "project-ns"
	externalNamespace            = "external-ns"
)

var _ = Describe("Handle with external queues allowed", func() {
	BeforeEach(func() {
		DeferCleanup(config.SetForTest(externalQueuesConfig(true)))
	})

	It("mutates a pod group in a project namespace", func() {
		podGroup := newPodGroup(projectNamespace)
		mutator := newPodGroupMutatorForTest(namespace(projectNamespace, "research"), project("research"))

		response := mutator.Handle(context.Background(), requestFor(podGroup))

		Expect(response.Allowed).To(BeTrue())
		mutated := patchedPodGroup(podGroup, response)
		Expect(mutated.Spec.SchedulingBackoff).To(HaveValue(Equal(int32(1))))
		Expect(mutated.Spec.MarkUnschedulable).To(HaveValue(BeFalse()))
		Expect(mutated.Labels).To(HaveKeyWithValue(testNodePoolLabelKey, testUnexistingNodePool))
	})

	It("admits a pod group in a namespace with no project label unchanged", func() {
		podGroup := newPodGroup(externalNamespace)
		mutator := newPodGroupMutatorForTest(namespace(externalNamespace, ""))

		response := mutator.Handle(context.Background(), requestFor(podGroup))

		Expect(response.Allowed).To(BeTrue())
		Expect(response.Patches).To(BeEmpty())
	})

	It("admits a pod group whose namespace names a project that is gone unchanged", func() {
		podGroup := newPodGroup(projectNamespace)
		mutator := newPodGroupMutatorForTest(namespace(projectNamespace, "research"))

		response := mutator.Handle(context.Background(), requestFor(podGroup))

		Expect(response.Allowed).To(BeTrue())
		Expect(response.Patches).To(BeEmpty())
	})

	It("admits a pod group whose namespace cannot be found unchanged", func() {
		podGroup := newPodGroup(externalNamespace)
		mutator := newPodGroupMutatorForTest()

		response := mutator.Handle(context.Background(), requestFor(podGroup))

		Expect(response.Allowed).To(BeTrue())
		Expect(response.Patches).To(BeEmpty())
	})
})

// Releasability: without the flag the namespace is never read, so a cluster that only runs KRM
// behaves as before.
var _ = Describe("Handle with external queues not allowed", func() {
	BeforeEach(func() {
		DeferCleanup(config.SetForTest(externalQueuesConfig(false)))
	})

	It("still mutates a pod group in a namespace with no project label", func() {
		podGroup := newPodGroup(externalNamespace)
		mutator := newPodGroupMutatorForTest(namespace(externalNamespace, ""))

		response := mutator.Handle(context.Background(), requestFor(podGroup))

		Expect(response.Allowed).To(BeTrue())
		Expect(patchedPodGroup(podGroup, response).Spec.SchedulingBackoff).To(HaveValue(Equal(int32(1))))
	})
})

func externalQueuesConfig(allowed bool) config.PodGroupAssignerConfig {
	return config.PodGroupAssignerConfig{
		NodePoolLabelKey:           testNodePoolLabelKey,
		NamespaceProjectLabelKey:   testNamespaceProjectLabelKey,
		UnexistingNodepoolSentinel: testUnexistingNodePool,
		DefaultNodepoolName:        testDefaultNodePoolName,
		AllowExternalQueues:        allowed,
	}
}

func newPodGroupMutatorForTest(initObjs ...client.Object) *PodGroupMutator {
	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	return NewPodGroupMutator(fake.NewClientBuilder().WithScheme(scheme).WithObjects(initObjs...).Build())
}

func project(name string) *v1alpha1.Project {
	return &v1alpha1.Project{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

// namespace builds a namespace fixture; an empty project leaves the project label off.
func namespace(name, project string) *corev1.Namespace {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if project != "" {
		ns.Labels = map[string]string{testNamespaceProjectLabelKey: project}
	}

	return ns
}

func newPodGroup(namespace string) *kaiv2alpha2.PodGroup {
	return &kaiv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: namespace},
		Spec:       kaiv2alpha2.PodGroupSpec{Queue: "some-queue"},
	}
}

func requestFor(podGroup *kaiv2alpha2.PodGroup) admission.Request {
	raw, err := json.Marshal(podGroup)
	Expect(err).NotTo(HaveOccurred())

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: podGroup.Namespace,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

// patchedPodGroup applies the response's JSON patch to the original pod group, so specs assert on
// the result rather than on patch internals.
func patchedPodGroup(original *kaiv2alpha2.PodGroup, response admission.Response) *kaiv2alpha2.PodGroup {
	originalRaw, err := json.Marshal(original)
	Expect(err).NotTo(HaveOccurred())

	patchRaw, err := json.Marshal(response.Patches)
	Expect(err).NotTo(HaveOccurred())

	patch, err := jsonpatch.DecodePatch(patchRaw)
	Expect(err).NotTo(HaveOccurred())

	patchedRaw, err := patch.Apply(originalRaw)
	Expect(err).NotTo(HaveOccurred())

	mutated := &kaiv2alpha2.PodGroup{}
	Expect(json.Unmarshal(patchedRaw, mutated)).To(Succeed())

	return mutated
}
