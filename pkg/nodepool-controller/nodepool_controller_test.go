// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodepoolcontroller

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestNodePoolController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodePool controller suite")
}

var _ = Describe("NodePoolController", func() {
	Context("construction", func() {
		It("retains the scheme it is given", func() {
			scheme := runtime.NewScheme()
			Expect(New(scheme).Scheme()).To(BeIdenticalTo(scheme))
		})

		It("reports its name", func() {
			Expect(New(runtime.NewScheme()).Name()).To(Equal(ControllerName))
		})
	})
})
