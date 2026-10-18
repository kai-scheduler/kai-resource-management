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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

var deletionPollInterval = time.Second

type CleanupOptions struct {
	Namespace string
	// ConfigName is the KRMConfig to delete; skipped when empty.
	ConfigName                     string
	ReleaseAdoptedSchedulingShards bool
	// NodePoolFinalizer is stripped from every NodePool; skipped when empty.
	NodePoolFinalizer string
}

// Cleanup deletes the operator-managed Deployments in the namespace, then runs the
// optional steps, each of which is a no-op on a rerun. Objects that are already gone are
// not errors. Like `kubectl delete`, it returns only once each object has actually
// disappeared, so a finalizer cannot leave one behind for the next install.
func Cleanup(ctx context.Context, k8sClient client.Client, opts CleanupOptions) error {
	logger := logf.FromContext(ctx)

	// The co-install steps undo what the controllers write, so none may still be running:
	// a foreground delete returns only once a Deployment's pods are gone, not while a
	// controller is still reconciling in its grace period.
	var deleteOpts []client.DeleteOption
	if opts.ReleaseAdoptedSchedulingShards || opts.NodePoolFinalizer != "" {
		deleteOpts = append(deleteOpts, client.PropagationPolicy(metav1.DeletePropagationForeground))
	}
	if err := deleteOperatorManagedDeployments(ctx, k8sClient, opts.Namespace, deleteOpts...); err != nil {
		return err
	}

	if opts.ReleaseAdoptedSchedulingShards {
		if err := releaseAdoptedSchedulingShards(ctx, k8sClient); err != nil {
			return err
		}
	}

	// After the shards are released: a NodePool deleted without its finalizer takes
	// whatever it still owns with it.
	if opts.NodePoolFinalizer != "" {
		if err := stripNodePoolFinalizers(ctx, k8sClient, opts.NodePoolFinalizer); err != nil {
			return err
		}
	}

	if opts.ConfigName == "" {
		return nil
	}

	config := &kaires.KRMConfig{ObjectMeta: metav1.ObjectMeta{Name: opts.ConfigName}}
	if err := deleteAndWait(ctx, k8sClient, config); err != nil {
		// Helm does not order a release's CRD removal against this hook, so the kind can
		// already be unmapped by the time it runs. Nothing is left to delete either way.
		if meta.IsNoMatchError(err) {
			logger.Info("KRMConfig CRD already gone, nothing to delete", "name", opts.ConfigName)
			return nil
		}
		return fmt.Errorf("failed to delete KRMConfig %s: %w", opts.ConfigName, err)
	}
	logger.Info("Deleted KRMConfig", "name", opts.ConfigName)
	return nil
}

func deleteOperatorManagedDeployments(
	ctx context.Context, k8sClient client.Client, namespace string, opts ...client.DeleteOption,
) error {
	deployments := &appsv1.DeploymentList{}
	if err := k8sClient.List(ctx, deployments, client.InNamespace(namespace),
		client.MatchingLabels{common.OperatorManagedByLabelKey: common.OperatorManagedByLabelValue}); err != nil {
		return fmt.Errorf("failed to list operator-managed deployments in %s: %w", namespace, err)
	}
	for i := range deployments.Items {
		deployment := &deployments.Items[i]
		if err := deleteAndWait(ctx, k8sClient, deployment, opts...); err != nil {
			return fmt.Errorf("failed to delete deployment %s/%s: %w", namespace, deployment.Name, err)
		}
		logf.FromContext(ctx).Info("Deleted deployment", "namespace", namespace, "name", deployment.Name)
	}
	return nil
}

func deleteAndWait(ctx context.Context, k8sClient client.Client, object client.Object, opts ...client.DeleteOption) error {
	key := client.ObjectKeyFromObject(object)
	if err := k8sClient.Get(ctx, key, object); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	uid := object.GetUID()

	if err := k8sClient.Delete(ctx, object, opts...); err != nil {
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

// stripNodePoolFinalizers covers every NodePool, not only the release's: Helm deletes
// neither the CRD nor user-created NodePools, and with no controller left to finalize
// them, deleting one would hang.
func stripNodePoolFinalizers(ctx context.Context, k8sClient client.Client, finalizer string) error {
	logger := logf.FromContext(ctx)

	nodePools := &kaires.NodePoolList{}
	if err := k8sClient.List(ctx, nodePools); err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("NodePool CRD not found, no finalizers to strip")
			return nil
		}
		return fmt.Errorf("failed to list node pools: %w", err)
	}

	for i := range nodePools.Items {
		nodePool := &nodePools.Items[i]
		if !controllerutil.ContainsFinalizer(nodePool, finalizer) {
			continue
		}
		if err := patchIfChanged(ctx, k8sClient, nodePool, func() bool {
			return controllerutil.RemoveFinalizer(nodePool, finalizer)
		}); err != nil {
			return fmt.Errorf("failed to remove finalizer %s from node pool %s: %w", finalizer, nodePool.Name, err)
		}
		logger.Info("Removed finalizer", "kind", "NodePool", "name", nodePool.Name, "finalizer", finalizer)
	}
	return nil
}

// patchIfChanged writes nothing when mutate reports no change, which is what makes a rerun
// a no-op. An object deleted since it was listed needs no change either.
func patchIfChanged(ctx context.Context, k8sClient client.Client, object client.Object, mutate func() bool) error {
	original := object.DeepCopyObject().(client.Object)
	if !mutate() {
		return nil
	}
	return client.IgnoreNotFound(k8sClient.Patch(ctx, object, client.MergeFrom(original)))
}
