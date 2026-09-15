// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nodepoolcontroller

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

// The CRDs nodepool-controller reads, as the KAI Scheduler chart installs them.
var requiredCRDs = map[string]string{
	"schedulingshards.kai.scheduler": "v1",
	"topologies.kai.scheduler":       "v1alpha1",
	"podgroups.scheduling.run.ai":    "v2alpha2",
}

// crdReader answers about the named CRDs and nothing else, which is how a
// cluster without KAI Scheduler installed answers.
func crdReader(names ...string) client.Reader {
	scheme := runtime.NewScheme()
	Expect(apiextensionsv1.AddToScheme(scheme)).To(Succeed())

	var objects []client.Object
	for _, name := range names {
		objects = append(objects, &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
					{Name: requiredCRDs[name], Served: true},
				},
			},
		})
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

var _ = Describe("NodePoolController.HasMissingDependencies", func() {
	ctx := context.Background()

	It("reports nothing when every CRD it reads is installed", func() {
		reader := crdReader("schedulingshards.kai.scheduler",
			"topologies.kai.scheduler", "podgroups.scheduling.run.ai")

		message, err := (&NodePoolController{}).HasMissingDependencies(ctx, reader, newKRMConfig())

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	It("names every CRD the cluster is missing", func() {
		message, err := (&NodePoolController{}).HasMissingDependencies(ctx, crdReader(), newKRMConfig())

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal("CRDs schedulingshards.kai.scheduler/v1, " +
			"topologies.kai.scheduler/v1alpha1, podgroups.scheduling.run.ai/v2alpha2"))
	})

	It("names only the CRD the cluster is missing", func() {
		reader := crdReader("schedulingshards.kai.scheduler", "podgroups.scheduling.run.ai")

		message, err := (&NodePoolController{}).HasMissingDependencies(ctx, reader, newKRMConfig())

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal("CRD topologies.kai.scheduler/v1alpha1"))
	})

	// Nothing is deployed, so nothing is depended on. Reporting otherwise would
	// hold Ready false over a service the configuration deliberately left out.
	It("reports nothing when the service is disabled", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.NodePoolController.Service.Enabled = ptr.To(false)

		message, err := (&NodePoolController{}).HasMissingDependencies(ctx, crdReader(), krmConfig)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})
})
