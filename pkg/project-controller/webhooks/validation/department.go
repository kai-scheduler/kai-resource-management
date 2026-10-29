// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"
	"strings"

	multierror "github.com/hashicorp/go-multierror"

	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/hierarchy"
)

// validateDepartment runs all create/update validations for a Department:
//   - every node pool referenced by a queue must exist in the cluster (empty
//     queues are allowed),
//   - a new or changed parent must exist, must not be being deleted, and must
//     leave the department tree free of loops and at most hierarchy.MaxDepth
//     levels deep.
//
// oldDepartment is nil on create. All failures are aggregated so the caller
// sees every problem at once.
func (v *Validator) validateDepartment(ctx context.Context, department, oldDepartment *kaiv1alpha1.Department) error {
	var errs error

	if err := v.validateQueueNodePoolsExist(ctx, department.Spec.Queues); err != nil {
		errs = multierror.Append(errs, err)
	}
	// Only a new parent can break the tree. Re-checking an unchanged one would block every later
	// write to a department that is already too deep, the finalizer removal included.
	if oldDepartment == nil || oldDepartment.Spec.Parent != department.Spec.Parent {
		if err := v.validateDepartmentParent(ctx, department); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return errs
}

func (v *Validator) validateDepartmentParent(ctx context.Context, department *kaiv1alpha1.Department) error {
	parentName := department.Spec.Parent
	if parentName == "" {
		return nil
	}

	parent, err := v.getParentDepartment(ctx, parentName)
	if err != nil {
		return err
	}
	if !parent.DeletionTimestamp.IsZero() {
		return fmt.Errorf("parent department %q is being deleted", parentName)
	}

	departmentList := &kaiv1alpha1.DepartmentList{}
	if err := v.client.List(ctx, departmentList); err != nil {
		return newInternalError(fmt.Errorf("failed to list departments: %w", err))
	}
	tree := hierarchy.NewTree(departmentList.Items)
	tree[parent.Name] = parent.Spec.Parent
	tree[department.Name] = parentName

	ancestors, inLoop := tree.Ancestors(department.Name, hierarchy.MaxDepth)
	if inLoop {
		loop := append(append([]string{department.Name}, ancestors...), department.Name)
		return fmt.Errorf("department %q cannot have parent %q: that would make a loop: %s",
			department.Name, parentName, strings.Join(loop, " → "))
	}
	// Levels above the department, plus the levels of its own subtree.
	if len(ancestors)+tree.Height(department.Name, hierarchy.MaxDepth) > hierarchy.MaxDepth {
		return fmt.Errorf("department %q cannot have parent %q: departments can nest at most %d levels deep, "+
			"and this would nest them deeper", department.Name, parentName, hierarchy.MaxDepth)
	}
	return nil
}
