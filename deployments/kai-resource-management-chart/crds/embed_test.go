// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package crds

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCRDs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Embedded CRDs suite")
}

var _ = Describe("LoadEmbeddedCRDs", func() {
	It("loads every manifest in the directory as a named CRD", func() {
		objects, err := LoadEmbeddedCRDs()
		Expect(err).NotTo(HaveOccurred())
		Expect(objects).NotTo(BeEmpty())

		for _, object := range objects {
			Expect(object.GetKind()).To(Equal("CustomResourceDefinition"))
			Expect(object.GetName()).NotTo(BeEmpty())
		}
	})
})
