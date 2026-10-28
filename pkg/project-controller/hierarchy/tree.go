// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package hierarchy reads the department tree that Department.spec.parent forms.
package hierarchy

import (
	"sort"

	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
)

// MaxDepth is how many levels departments may nest, a top-level department being level 1. It also
// bounds every walk the admission webhook makes over the tree.
const MaxDepth = 7

// Tree maps each department name to its parent's name, "" for a top-level department.
type Tree map[string]string

func NewTree(departments []kaiv1alpha1.Department) Tree {
	tree := make(Tree, len(departments))
	for i := range departments {
		tree[departments[i].Name] = departments[i].Spec.Parent
	}
	return tree
}

// Ancestors returns up to limit departments above name, nearest first. The walk ends at a
// top-level department, at a parent that does not exist, or at a repeat; inLoop reports that it
// came back to name. A walk that repeats without reaching name is under a loop, not in it.
func (t Tree) Ancestors(name string, limit int) (ancestors []string, inLoop bool) {
	seen := map[string]bool{}
	for parent := t[name]; parent != "" && len(ancestors) < limit; parent = t[parent] {
		if parent == name {
			return ancestors, true
		}
		if _, exists := t[parent]; !exists || seen[parent] {
			break
		}
		seen[parent] = true
		ancestors = append(ancestors, parent)
	}
	return ancestors, false
}

// Height returns how many levels the subtree under name spans, name included, counting at most
// limit levels.
func (t Tree) Height(name string, limit int) int {
	children := map[string][]string{}
	for child, parent := range t {
		children[parent] = append(children[parent], child)
	}

	seen := map[string]bool{name: true}
	height := 0
	for level := []string{name}; len(level) > 0 && height < limit; height++ {
		var next []string
		for _, department := range level {
			for _, child := range children[department] {
				if !seen[child] {
					seen[child] = true
					next = append(next, child)
				}
			}
		}
		level = next
	}
	return height
}

// Children returns the departments whose parent is name, sorted.
func (t Tree) Children(name string) []string {
	var children []string
	for child, parent := range t {
		if parent == name {
			children = append(children, child)
		}
	}
	sort.Strings(children)
	return children
}
