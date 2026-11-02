// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"fmt"

	multierror "github.com/hashicorp/go-multierror"
	schedv2 "github.com/kai-scheduler/api/scheduling/v2"
	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ParentQueueLoopReason is the Event reason for a department queue kept a root queue to avoid a loop.
const ParentQueueLoopReason = "ParentQueueLoop"

// DepartmentHandler reconciles a Department's queues, one per node pool, each under its parent
// department's queue for the same node pool.
type DepartmentHandler struct {
	common.WithLoggerAndCli
	apiReader client.Reader
	recorder  events.EventRecorder
}

func NewDepartmentHandler(client client.Client, apiReader client.Reader, recorder events.EventRecorder) DepartmentHandler {
	return DepartmentHandler{
		WithLoggerAndCli: common.WithLoggerAndCli{
			Client: client,
			Log:    ctrl.Log.WithName("handlers").WithName(common.LogDepartmentTag),
		},
		apiReader: apiReader,
		recorder:  recorder,
	}
}

// Handle reconciles the department's queues. heldAsRoot reports that a queue was kept a root
// queue to avoid a queue loop, so the caller should retry until it can be placed.
func (h *DepartmentHandler) Handle(ctx context.Context, department *kaiv1alpha1.Department) (heldAsRoot bool, err error) {
	reconciledQueues := map[string]bool{}
	for _, departmentQueue := range department.Spec.Queues {
		parentQueueName, innerErr := departmentQueueName(ctx, h.Client,
			h.Log.WithValues(common.LogDepartmentTag, department.Name), department.Spec.Parent, departmentQueue.Nodepool)
		if innerErr != nil {
			err = multierror.Append(err, innerErr)
			continue
		}

		queueObject := h.buildQueueFromSpec(departmentQueue, department, parentQueueName)
		held, innerErr := h.reconcileQueueObject(ctx, queueObject, department, departmentQueue.Nodepool)
		if innerErr != nil {
			h.Log.Error(innerErr, "Failed reconciling queue object for department",
				common.LogQueueTag, queueObject.Name, common.LogDepartmentTag, department.Name)
			err = multierror.Append(err, innerErr)
			continue
		}

		heldAsRoot = heldAsRoot || held
		reconciledQueues[queueObject.Name] = true
	}

	if err != nil {
		h.Log.Info("Error occurred during reconcile of queues - skipping deletion of unnecessary queues",
			common.LogDepartmentTag, department.Name)
		return heldAsRoot, err
	}

	err = deleteUnnecessaryQueues(ctx, h.Client, h.Log, reconciledQueues, department.UID,
		common.LogDepartmentTag, department.Name,
		config.Get().QueueDepartmentNameLabelKey, department.Name, common.DepartmentKind)
	if err != nil {
		h.Log.Error(err, "Failed deleting unnecessary queues for department",
			common.LogDepartmentTag, department.Name)
		return heldAsRoot, err
	}

	h.Log.Info("Successfully reconciled queues for department", common.LogDepartmentTag, department.Name)
	return heldAsRoot, nil
}

func (h *DepartmentHandler) reconcileQueueObject(ctx context.Context,
	queueObject *schedv2.Queue, department *kaiv1alpha1.Department, nodepoolName string) (heldAsRoot bool, err error) {
	departmentName := department.Name
	existingQueue, err := h.getExistingQueueOfDepartment(ctx, department, nodepoolName)
	if err != nil {
		if errors.IsNotFound(err) {
			queueObject.Name = h.generateQueueNameOfDepartment(ctx, departmentName, queueObject.Name, department.UID)
			if heldAsRoot, err = h.holdAsRootOnLoop(ctx, department, queueObject, ""); err != nil {
				return false, err
			}
			return heldAsRoot, createQueue(ctx, h.Client, h.Log,
				queueObject, common.LogDepartmentTag, departmentName)
		}
		h.Log.Error(err, "Error encountered while trying to get queue for department",
			common.LogQueueTag, queueObject.Name, common.LogDepartmentTag, departmentName)
		return false, err
	}

	queueObject.Name = existingQueue.Name
	if heldAsRoot, err = h.holdAsRootOnLoop(ctx, department, queueObject, existingQueue.Spec.ParentQueue); err != nil {
		return false, err
	}
	return heldAsRoot, h.handleExisting(ctx, queueObject, existingQueue, departmentName, department.UID)
}

// holdAsRootOnLoop keeps queue a root queue when its new parent queue already sits under it.
// KAI Scheduler has no loop check, so a queue loop stops scheduling.
func (h *DepartmentHandler) holdAsRootOnLoop(ctx context.Context, department *kaiv1alpha1.Department,
	queue *schedv2.Queue, currentParentQueue string) (bool, error) {
	parentQueue := queue.Spec.ParentQueue
	// Only a new parent can close a loop.
	if parentQueue == "" || parentQueue == currentParentQueue {
		return false, nil
	}

	loop, err := wouldCloseQueueLoop(ctx, h.apiReader, queue.Name, parentQueue)
	if err != nil || !loop {
		return false, err
	}

	h.Log.Info("Keeping queue a root queue: its parent department's queue sits under it",
		common.LogQueueTag, queue.Name, "parentQueue", parentQueue, common.LogDepartmentTag, department.Name)
	h.recorder.Eventf(department, nil, corev1.EventTypeWarning, ParentQueueLoopReason, "PlaceQueue",
		"queue %q stays a root queue: placing it under queue %q would make a queue loop", queue.Name, parentQueue)
	queue.Spec.ParentQueue = ""
	return true, nil
}

func (h *DepartmentHandler) handleExisting(ctx context.Context,
	expectedQueueObject, existingQueue *schedv2.Queue, departmentName string, departmentUID types.UID) error {
	if areQueuesEqual(expectedQueueObject, existingQueue) {
		h.Log.Info("Existing Queue identical to expected in department spec, skipping",
			common.LogQueueTag, existingQueue.Name, common.LogDepartmentTag, departmentName)
		return nil
	}

	return updateExistingQueue(ctx, h.Client, h.Log,
		expectedQueueObject, existingQueue, departmentUID, common.LogDepartmentTag, departmentName)
}

// buildQueueFromSpec - the returned queue spec will have the suggested queue name.
// this queue name might already be taken by other resources, the caller needs to validate it.
func (h *DepartmentHandler) buildQueueFromSpec(queueDepartmentSpec kaiv1alpha1.QueueConfig,
	department *kaiv1alpha1.Department, parentQueueName string) *schedv2.Queue {
	queue := &schedv2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: GetQueueName(queueDepartmentSpec, department.Name),
			Labels: map[string]string{
				config.Get().QueueDepartmentNameLabelKey: department.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				// Set from constants (not department.TypeMeta, which is often empty at runtime)
				// so the owner Kind reliably identifies this as a department-owned queue.
				APIVersion:         kaiv1alpha1.GroupVersion.Identifier(),
				Kind:               common.DepartmentKind,
				Name:               department.Name,
				UID:                department.UID,
				Controller:         &common.TrueRef,
				BlockOwnerDeletion: &common.TrueRef,
			}},
		},
	}
	populateQueueSpec(queueDepartmentSpec.Resources, department.Name, parentQueueName, queueDepartmentSpec.Priority, &queue.Spec)
	addNodePoolLabelIfNeeded(queue.Labels, config.Get().NodePoolLabelKey, queueDepartmentSpec.Nodepool)
	return queue
}

// getExistingQueueOfDepartment finds the department's own queue for the nodepool by OwnerReference
// (Department UID). Using the owner - not the department-name label - makes this rename-safe
// (the UID is stable) and immune to project queues that share the department-name label.
func (h *DepartmentHandler) getExistingQueueOfDepartment(ctx context.Context,
	department *kaiv1alpha1.Department, nodepoolName string) (*schedv2.Queue, error) {
	queues, err := listQueuesForNodepool(ctx, h.Client, nodepoolName)
	if err != nil {
		h.Log.Error(err, "Failed listing queues for department", common.LogDepartmentTag, department.Name,
			"nodepool", nodepoolName)
		return nil, err
	}
	for i := range queues {
		if isOwnedByUID(queues[i].OwnerReferences, department.UID) {
			return &queues[i], nil
		}
	}
	return nil, errors.NewNotFound(schedv2.Resource("queue"),
		fmt.Sprintf("%s/%s", department.Name, nodepoolName))
}

func (h *DepartmentHandler) generateQueueNameOfDepartment(ctx context.Context,
	departmentName, suggestedQueueName string, departmentUID types.UID) string {
	return generateQueueNameForResource(ctx, h.Client, "",
		departmentName, suggestedQueueName, departmentUID)
}
