// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroupassigner

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestPodGroupAssigner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pod group assigner suite")
}

var _ = Describe("PodGroupAssigner", func() {
	Context("construction", func() {
		It("retains the scheme it is given", func() {
			scheme := runtime.NewScheme()
			Expect(New(scheme).Scheme()).To(BeIdenticalTo(scheme))
		})
	})
})
