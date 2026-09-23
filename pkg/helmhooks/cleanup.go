// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"
	"time"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

var deletionPollInterval = time.Second

// Cleanup deletes the operator-managed Deployments in namespace and, when configName is
// not empty, the KRMConfig of that name. Objects that are already gone are not errors.
// Like `kubectl delete`, it returns only once each object has actually disappeared, so a
// finalizer cannot leave one behind for the next install.
func Cleanup(ctx context.Context, k8sClient client.Client, namespace, configName string) error {
	logger := logf.FromContext(ctx)

	deployments := &appsv1.DeploymentList{}
	if err := k8sClient.List(ctx, deployments, client.InNamespace(namespace),
		client.MatchingLabels{common.OperatorManagedByLabelKey: common.OperatorManagedByLabelValue}); err != nil {
		return fmt.Errorf("failed to list operator-managed deployments in %s: %w", namespace, err)
	}
	for i := range deployments.Items {
		deployment := &deployments.Items[i]
		if err := deleteAndWait(ctx, k8sClient, deployment); err != nil {
			return fmt.Errorf("failed to delete deployment %s/%s: %w", namespace, deployment.Name, err)
		}
		logger.Info("Deleted deployment", "namespace", namespace, "name", deployment.Name)
	}

	if configName == "" {
		return nil
	}

	config := &kaires.KRMConfig{ObjectMeta: metav1.ObjectMeta{Name: configName}}
	if err := deleteAndWait(ctx, k8sClient, config); err != nil {
		// Helm does not order a release's CRD removal against this hook, so the kind can
		// already be unmapped by the time it runs. Nothing is left to delete either way.
		if meta.IsNoMatchError(err) {
			logger.Info("KRMConfig CRD already gone, nothing to delete", "name", configName)
			return nil
		}
		return fmt.Errorf("failed to delete KRMConfig %s: %w", configName, err)
	}
	logger.Info("Deleted KRMConfig", "name", configName)
	return nil
}

func deleteAndWait(ctx context.Context, k8sClient client.Client, object client.Object) error {
	key := client.ObjectKeyFromObject(object)
	if err := k8sClient.Get(ctx, key, object); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	uid := object.GetUID()

	if err := k8sClient.Delete(ctx, object); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return wait.PollUntilContextCancel(ctx, deletionPollInterval, true, func(ctx context.Context) (bool, error) {
		current := object.DeepCopyObject().(client.Object)
		err := k8sClient.Get(ctx, key, current)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		// A different UID under the same name means the object we deleted is gone and
		// something has already recreated it.
		return current.GetUID() != uid, nil
	})
}
