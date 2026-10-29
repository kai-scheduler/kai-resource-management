// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package hierarchy_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/hierarchy"
)

func TestHierarchy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Department Hierarchy Suite")
}

// chain returns d1 → d2 → … → d<n>, d1 being top-level.
func chain(n int) hierarchy.Tree {
	tree := hierarchy.Tree{"d1": ""}
	for i := 2; i <= n; i++ {
		tree[fmt.Sprintf("d%d", i)] = fmt.Sprintf("d%d", i-1)
	}
	return tree
}

var _ = Describe("Tree", func() {
	It("is built from each department's parent", func() {
		tree := hierarchy.NewTree([]kaiv1alpha1.Department{
			{ObjectMeta: metav1.ObjectMeta{Name: "research"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "ml"}, Spec: kaiv1alpha1.DepartmentSpec{Parent: "research"}},
		})
		Expect(tree).To(Equal(hierarchy.Tree{"research": "", "ml": "research"}))
	})

	Describe("Ancestors", func() {
		It("lists them nearest first, up to the top-level department", func() {
			ancestors, inLoop := chain(4).Ancestors("d4", hierarchy.MaxDepth)
			Expect(ancestors).To(Equal([]string{"d3", "d2", "d1"}))
			Expect(inLoop).To(BeFalse())
		})

		It("has none for a top-level department", func() {
			ancestors, inLoop := chain(1).Ancestors("d1", hierarchy.MaxDepth)
			Expect(ancestors).To(BeEmpty())
			Expect(inLoop).To(BeFalse())
		})

		It("stops at the limit", func() {
			ancestors, _ := chain(10).Ancestors("d10", hierarchy.MaxDepth)
			Expect(ancestors).To(HaveLen(hierarchy.MaxDepth))
		})

		It("stops at a parent that does not exist", func() {
			ancestors, inLoop := hierarchy.Tree{"ml": "research", "research": "gone"}.Ancestors("ml", hierarchy.MaxDepth)
			Expect(ancestors).To(Equal([]string{"research"}))
			Expect(inLoop).To(BeFalse())
		})

		It("reports a loop through the department", func() {
			ancestors, inLoop := hierarchy.Tree{"a": "b", "b": "c", "c": "a"}.Ancestors("a", hierarchy.MaxDepth)
			Expect(ancestors).To(Equal([]string{"b", "c"}))
			Expect(inLoop).To(BeTrue())
		})

		It("reports a department that is its own parent", func() {
			_, inLoop := hierarchy.Tree{"a": "a"}.Ancestors("a", hierarchy.MaxDepth)
			Expect(inLoop).To(BeTrue())
		})
	})

	Describe("Height", func() {
		It("is 1 for a department with no children", func() {
			Expect(chain(3).Height("d3", hierarchy.MaxDepth)).To(Equal(1))
		})

		It("counts the deepest branch", func() {
			tree := hierarchy.Tree{"root": "", "a": "root", "b": "root", "b1": "b", "b2": "b1"}
			Expect(tree.Height("root", hierarchy.MaxDepth)).To(Equal(4))
		})

		It("stops at the limit", func() {
			Expect(chain(10).Height("d1", hierarchy.MaxDepth)).To(Equal(hierarchy.MaxDepth))
		})

		It("ends when the department is in a loop", func() {
			tree := hierarchy.Tree{"a": "c", "b": "a", "c": "b"}
			Expect(tree.Height("a", 100)).To(Equal(3))
		})
	})

	Describe("Children", func() {
		It("lists the departments directly under one, sorted", func() {
			tree := hierarchy.Tree{"root": "", "b": "root", "a": "root", "a1": "a"}
			Expect(tree.Children("root")).To(Equal([]string{"a", "b"}))
			Expect(tree.Children("a1")).To(BeEmpty())
		})
	})
})
