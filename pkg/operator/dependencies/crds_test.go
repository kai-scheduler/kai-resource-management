// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestDependencies(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dependencies suite")
}

var (
	queues     = CRDRequirement{Name: "queues.scheduling.run.ai", Version: "v2"}
	podGroups  = CRDRequirement{Name: "podgroups.scheduling.run.ai", Version: "v2alpha2"}
	topologies = CRDRequirement{Name: "topologies.kai.scheduler", Version: "v1alpha1"}
)

// crd builds a CustomResourceDefinition serving exactly the given versions.
func crd(name string, servedVersions ...string) *apiextensionsv1.CustomResourceDefinition {
	definition := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	for _, version := range servedVersions {
		definition.Spec.Versions = append(definition.Spec.Versions,
			apiextensionsv1.CustomResourceDefinitionVersion{Name: version, Served: true})
	}
	return definition
}

func newReader(objects ...client.Object) client.Reader {
	scheme := runtime.NewScheme()
	Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

var _ = Describe("MissingCRDs", func() {
	ctx := context.Background()

	DescribeTable("reports what the cluster does not satisfy",
		func(installed []client.Object, required []CRDRequirement, expected string) {
			message, err := MissingCRDs(ctx, newReader(installed...), required...)

			Expect(err).ToNot(HaveOccurred())
			Expect(message).To(Equal(expected))
		},
		Entry("nothing required", nil, nil, ""),
		Entry("every requirement met",
			[]client.Object{crd(queues.Name, "v2"), crd(podGroups.Name, "v2alpha2")},
			[]CRDRequirement{queues, podGroups},
			""),
		Entry("one CRD absent",
			[]client.Object{crd(queues.Name, "v2")},
			[]CRDRequirement{queues, podGroups},
			"CRD podgroups.scheduling.run.ai/v2alpha2"),
		Entry("several absent are listed together",
			nil,
			[]CRDRequirement{queues, podGroups, topologies},
			"CRDs queues.scheduling.run.ai/v2, podgroups.scheduling.run.ai/v2alpha2, "+
				"topologies.kai.scheduler/v1alpha1"),
		// The upgrade case the ticket is about: KAI is installed, but the API
		// version the service talks to it through is gone.
		Entry("CRD present but serving a different version",
			[]client.Object{crd(queues.Name, "v1")},
			[]CRDRequirement{queues},
			"CRD queues.scheduling.run.ai/v2"),
		Entry("CRD present with no versions at all",
			[]client.Object{crd(queues.Name)},
			[]CRDRequirement{queues},
			"CRD queues.scheduling.run.ai/v2"),
	)

	It("treats a version that is served alongside others as present", func() {
		reader := newReader(crd(queues.Name, "v1", "v2"))

		message, err := MissingCRDs(ctx, reader, queues)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	It("treats a version that exists but is no longer served as missing", func() {
		definition := crd(queues.Name, "v2")
		definition.Spec.Versions[0].Served = false
		reader := newReader(definition)

		message, err := MissingCRDs(ctx, reader, queues)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal("CRD queues.scheduling.run.ai/v2"))
	})

	// The apiextensions group itself being unreachable reads as "not installed"
	// rather than as a failed check, so a cluster without it still gets a usable
	// condition message.
	It("treats a no-match error as a missing CRD", func() {
		scheme := runtime.NewScheme()
		Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())
		reader := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(
				_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object,
				_ ...client.GetOption,
			) error {
				return &meta.NoKindMatchError{
					GroupKind: schema.GroupKind{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"},
				}
			},
		}).Build()

		message, err := MissingCRDs(ctx, reader, queues)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal("CRD queues.scheduling.run.ai/v2"))
	})

	// A cluster that could not be asked is not a cluster that answered "absent":
	// reporting it as an unmet dependency would be a guess.
	It("returns an error rather than a message when the read fails", func() {
		scheme := runtime.NewScheme()
		Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())
		reader := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(
				_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object,
				_ ...client.GetOption,
			) error {
				return errors.New("apiserver unavailable")
			},
		}).Build()

		message, err := MissingCRDs(ctx, reader, queues)

		Expect(err).To(MatchError(ContainSubstring("apiserver unavailable")))
		Expect(err).To(MatchError(ContainSubstring("queues.scheduling.run.ai")))
		Expect(message).To(BeEmpty())
	})
})
