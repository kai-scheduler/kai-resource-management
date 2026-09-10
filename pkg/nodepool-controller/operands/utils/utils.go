// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/operands"
)

func ResourcesForNodePool(ctx context.Context, k8sReader client.Reader, nodePool *v1alpha1.NodePool,
	params *common.NodePoolControllerParams, resourceFunctions []operands.ResourceFunction,
	operandName string) ([]operands.ResourceOld, error) {
	operandObjects := []client.Object{}
	for _, resourceFunction := range resourceFunctions {
		resource, err := resourceFunction(ctx, k8sReader, nodePool, params, operandName)
		if err != nil {
			return nil, err
		}

		operandObjects = append(operandObjects, resource)
	}

	operandResources := []operands.ResourceOld{}
	for _, resource := range operandObjects {
		if resource != nil {
			operandResources = append(operandResources, operands.ResourceForOperand(resource, operandName))
		}
	}

	return operandResources, nil
}

func Status(ctx context.Context, k8sReader client.Reader, nodePool *v1alpha1.NodePool,
	params *common.NodePoolControllerParams, resourceStatusFunctions []operands.ResourceStatusFunction,
	operandName string) (operands.Status, error) {
	resourcesStatuses := []operands.Status{}
	for _, resourceStatusFunction := range resourceStatusFunctions {
		status, err := resourceStatusFunction(ctx, k8sReader, nodePool, params, operandName)
		if err != nil {
			return operands.NotReadyStatus(), err
		}

		resourcesStatuses = append(resourcesStatuses, status)
	}

	aggregatedStatus := operands.ReadyStatus()
	for _, status := range resourcesStatuses {
		if !status.Ready {
			aggregatedStatus.Ready = false
			aggregatedStatus.Reasons = append(aggregatedStatus.Reasons, status.Reasons...)
		}
	}

	return aggregatedStatus, nil
}
