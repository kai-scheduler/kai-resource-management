// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package hierarchy reads the department tree that Department.spec.parent forms.
package hierarchy

import kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"

// MaxDepth is how many levels departments may nest; a top-level department is level 1.
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

// Ancestors lists the departments above name, nearest first, up to limit of them.
// inLoop is true when walking up leads back to name.
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

// Height counts the levels from name down to its deepest descendant, name included.
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
