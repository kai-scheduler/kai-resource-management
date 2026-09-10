// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// crdReader answers about the queues CRD and nothing else. Passing no versions
// stands in for a cluster without KAI Scheduler installed.
func crdReader(servedVersions ...string) client.Reader {
	scheme := runtime.NewScheme()
	Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())

	var objects []client.Object
	if len(servedVersions) > 0 {
		queues := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: "queues.scheduling.run.ai"},
		}
		for _, version := range servedVersions {
			queues.Spec.Versions = append(queues.Spec.Versions,
				apiextensionsv1.CustomResourceDefinitionVersion{Name: version, Served: true})
		}
		objects = append(objects, queues)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

var _ = Describe("ProjectController.HasMissingDependencies", func() {
	ctx := context.Background()

	It("reports nothing when the queues CRD is installed", func() {
		message, err := (&ProjectController{}).HasMissingDependencies(ctx, crdReader("v2"), newKRMConfig())

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	It("names the queues CRD when it is absent", func() {
		message, err := (&ProjectController{}).HasMissingDependencies(ctx, crdReader(), newKRMConfig())

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal("CRD queues.scheduling.run.ai/v2"))
	})

	// A KAI Scheduler downgraded past the version project-controller writes
	// Queues through is as unusable as one that is not installed.
	It("names the queues CRD when it no longer serves v2", func() {
		message, err := (&ProjectController{}).HasMissingDependencies(ctx, crdReader("v1"), newKRMConfig())

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal("CRD queues.scheduling.run.ai/v2"))
	})

	// Nothing is deployed, so nothing is depended on. Reporting otherwise would
	// hold Ready false over a service the configuration deliberately left out.
	It("reports nothing when the service is disabled", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Service.Enabled = ptr.To(false)

		message, err := (&ProjectController{}).HasMissingDependencies(ctx, crdReader(), krmConfig)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})
})
