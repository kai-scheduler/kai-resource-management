// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"context"
	"fmt"

	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/common"
)

// validateQueue keeps queue trees that KRM does not own apart from the ones it does: KRM
// models exactly two levels (department queue, then project queue), so a queue it does not
// own, placed under one of its queues, builds a tree it can neither model nor safely reconcile.
// oldQueue is nil on create.
func (v *Validator) validateQueue(ctx context.Context, queue, oldQueue *kaiv2.Queue) error {
	parentName := queue.Spec.ParentQueue

	// Only a change of parent can move a queue under a KRM queue. Re-checking an unchanged
	// parent would block every later write to a queue that predates this webhook, including
	// the finalizer removal and garbage-collector orphaning that let it be deleted.
	if oldQueue != nil && oldQueue.Spec.ParentQueue == parentName {
		return nil
	}
	if parentName == "" {
		return nil
	}
	if _, owned := common.ProjectOrDepartmentOwner(queue); owned {
		return nil
	}

	parent := &kaiv2.Queue{}
	err := v.client.Get(ctx, types.NamespacedName{Name: parentName}, parent)
	if err != nil && apierrors.IsNotFound(err) {
		// With no parent there is no KRM tree to protect. Whether a parent must exist is
		// for KAI's own queue webhook to decide.
		return nil
	}
	if err != nil {
		return newInternalError(fmt.Errorf("failed to get parent queue %q: %w", parentName, err))
	}

	owner, owned := common.ProjectOrDepartmentOwner(parent)
	if !owned {
		return nil
	}
	return fmt.Errorf("queue %q cannot use %q as its parent queue: %q is managed by KAI Resource Management "+
		"(%s %q), which supports only its own two-level hierarchy. Choose a parent queue that "+
		"KAI Resource Management does not manage, or create a Project instead of this queue",
		queue.Name, parentName, parentName, owner.Kind, owner.Name)
}
