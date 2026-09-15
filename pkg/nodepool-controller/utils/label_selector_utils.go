// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"k8s.io/apimachinery/pkg/labels"

	nodepoolutils "github.com/kai-scheduler/kai-resource-management/pkg/common/node-pool-utils/utils"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
)

func GetNodePoolNameFromLabels(labels map[string]string) string {
	return nodepoolutils.GetNodePoolNameFromLabels(labels, config.Get().NodePoolNameLabel, config.Get().DefaultNodepoolName)
}

func GetNodePoolLabelSelector(nodePoolName string) (requirement *labels.Requirement, err error) {
	return nodepoolutils.GetNodePoolLabelSelector(nodePoolName, config.Get().NodePoolNameLabel, config.Get().DefaultNodepoolName)
}

func RequirementNodePoolNameMatching(nodePoolName string) (requirement *labels.Requirement, err error) {
	return nodepoolutils.RequirementNodePoolNameMatching(nodePoolName, config.Get().NodePoolNameLabel)
}

func GetNotNodePoolLabelSelector(nodePoolName string) (requirement *labels.Requirement, err error) {
	return nodepoolutils.GetNotNodePoolLabelSelector(nodePoolName, config.Get().NodePoolNameLabel, config.Get().DefaultNodepoolName)
}

func RequirementNodePoolLabelMatching(labelKey, labelValue string) (requirement *labels.Requirement, err error) {
	return nodepoolutils.RequirementNodePoolLabelMatching(labelKey, labelValue)
}

func RequirementNodePoolLabelNotMatching(labelKey, labelValue string) (requirement *labels.Requirement, err error) {
	return nodepoolutils.RequirementNodePoolLabelNotMatching(labelKey, labelValue)
}
