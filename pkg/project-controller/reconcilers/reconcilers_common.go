// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package reconcilers

import (
	"context"

	"github.com/go-logr/logr"
	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/common"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type IntraEventSender struct {
	projectEvents chan event.GenericEvent
}

// Sends events for each project in the list to the ProjectReconciler via the dedicated channel
func (eventSender IntraEventSender) triggerEventsForAllProjects(
	projectsList kaiv1alpha1.ProjectList,
) (ctrl.Result, error) {
	for _, project := range projectsList.Items {
		go func(event event.GenericEvent) {
			eventSender.projectEvents <- event
		}(event.GenericEvent{
			Object: project.DeepCopy(),
		})
	}
	return ctrl.Result{}, nil
}

// mapQueueToDepartmentChildren returns a request for each object in children whose parent is the
// department owning queue.
func mapQueueToDepartmentChildren(ctx context.Context, reader client.Reader, log logr.Logger,
	queue client.Object, children client.ObjectList, parentIndexField string) []reconcile.Request {
	owner, ok := common.ProjectOrDepartmentOwner(queue)
	if !ok || owner.Kind != common.DepartmentKind {
		return nil
	}

	if err := reader.List(ctx, children, client.MatchingFields{parentIndexField: owner.Name}); err != nil {
		log.Error(err, "Failed listing a department's children for a queue event",
			common.LogQueueTag, queue.GetName(), common.LogDepartmentTag, owner.Name)
		return nil
	}
	items, err := meta.ExtractList(children)
	if err != nil {
		log.Error(err, "Failed reading a department's children for a queue event",
			common.LogQueueTag, queue.GetName(), common.LogDepartmentTag, owner.Name)
		return nil
	}

	requests := make([]reconcile.Request, 0, len(items))
	for _, item := range items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: item.(client.Object).GetName()}})
	}
	return requests
}
