// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"

	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsExternalNamespace reports whether namespace belongs to no project, so its PodGroups are left to
// whatever scheduled the cluster before KRM. Unless external queues are allowed every namespace is
// taken to be KRM's and nothing is read; a namespace that is gone counts as unlabelled.
func IsExternalNamespace(ctx context.Context, k8sClient client.Client, namespace string) (bool, error) {
	if !config.Config().AllowExternalQueues {
		return false, nil
	}

	namespaceObj := &corev1.Namespace{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, namespaceObj); err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to get namespace <%s>: %w", namespace, err)
	}

	_, found := namespaceObj.Labels[config.Config().NamespaceProjectLabelKey]
	return !found, nil
}

func GetProjectNameOfNamespace(ctx context.Context, k8sClient client.Client, namespace string) (string, error) {
	namespaceObj := &corev1.Namespace{}
	namespaceKey := types.NamespacedName{Name: namespace}

	if err := k8sClient.Get(ctx, namespaceKey, namespaceObj); err != nil {
		return "", fmt.Errorf("failed to get namespace <%s>, error: %s", namespace, err.Error())
	}

	projectName, found := namespaceObj.Labels[config.Config().NamespaceProjectLabelKey]
	if !found {
		return "", fmt.Errorf("can't find project label for namespace <%s>", namespace)
	}

	return projectName, nil
}
