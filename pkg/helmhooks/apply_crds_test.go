// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-resource-management/deployments/kai-resource-management-chart/crds"
)

var _ = Describe("ApplyCRDs", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
	})

	It("creates every embedded CRD", func() {
		embedded, err := crds.LoadEmbeddedCRDs()
		Expect(err).NotTo(HaveOccurred())
		Expect(embedded).NotTo(BeEmpty())

		Expect(ApplyCRDs(ctx, fakeClient)).To(Succeed())

		for _, expected := range embedded {
			applied := &apiextensionsv1.CustomResourceDefinition{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Name: expected.GetName()}, applied)).To(Succeed(),
				"CRD %s should have been applied", expected.GetName())
		}
	})

	It("is idempotent", func() {
		Expect(ApplyCRDs(ctx, fakeClient)).To(Succeed())
		Expect(ApplyCRDs(ctx, fakeClient)).To(Succeed())
	})
})
