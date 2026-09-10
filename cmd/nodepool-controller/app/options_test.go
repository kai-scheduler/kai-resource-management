// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func TestApp(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodePool Controller App Suite")
}

var _ = Describe("ParseUninstallDetectionRef", func() {
	Context("when the flag is empty", func() {
		It("disables the check", func() {
			ref, err := ParseUninstallDetectionRef("")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).To(BeNil())
		})
	})

	Context("when a namespace segment is given", func() {
		It("builds a namespaced reference", func() {
			ref, err := ParseUninstallDetectionRef("example.com/v1/Cluster/kai-system/my-cluster")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref.GVK).To(Equal(schema.GroupVersionKind{
				Group: "example.com", Version: "v1", Kind: "Cluster",
			}))
			Expect(ref.Key).To(Equal(types.NamespacedName{Name: "my-cluster", Namespace: "kai-system"}))
		})
	})

	Context("when the namespace segment is empty", func() {
		It("builds a cluster-scoped reference", func() {
			ref, err := ParseUninstallDetectionRef("example.com/v1/Cluster//my-cluster")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref.Key).To(Equal(types.NamespacedName{Name: "my-cluster"}))
		})
	})

	DescribeTable("rejecting malformed input",
		func(input string) {
			ref, err := ParseUninstallDetectionRef(input)
			Expect(err).To(MatchError(ContainSubstring("group/version/Kind/namespace/name")))
			Expect(ref).To(BeNil())
		},
		Entry("too few segments", "example.com/v1/Cluster/my-cluster"),
		Entry("too many segments", "example.com/v1/Cluster/ns/my-cluster/extra"),
		Entry("empty group", "/v1/Cluster//my-cluster"),
		Entry("empty version", "example.com//Cluster//my-cluster"),
		Entry("empty kind", "example.com/v1///my-cluster"),
		Entry("empty name", "example.com/v1/Cluster/kai-system/"),
	)
})
