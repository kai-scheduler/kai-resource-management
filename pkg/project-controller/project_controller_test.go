// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProjectController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Project controller suite")
}

// TEMPORARY: placeholder suite. It exists only so every package is covered by
// the test targets the CI workflows run. Replace it with real specs.
var _ = Describe("ProjectController", func() {
	Context("placeholder", func() {
		It("has no behavior to assert yet", func() {
			Expect(0).To(Equal(0))
		})
	})
})
