// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestProjectController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Project controller suite")
}

var _ = Describe("ProjectController", func() {
	Context("construction", func() {
		It("retains the scheme it is given", func() {
			scheme := runtime.NewScheme()
			Expect(New(scheme).Scheme()).To(BeIdenticalTo(scheme))
		})
	})
})
