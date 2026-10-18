// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"
	"strings"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

var krmMetadataPrefix = kaires.GroupVersion.Group + "/"

type RevertOptions struct {
	Namespace string
	// OperatorDeployment is skipped when empty, for a release that does not run the operator.
	OperatorDeployment              string
	ValidatingWebhookConfigurations []string
	MutatingWebhookConfigurations   []string
	FinalizerDomain                 string
}

// RevertForUninstall hands adopted SchedulingShards back to the scheduler that owned them
// before KRM, so deleting a NodePool later cannot garbage-collect them. Each step makes
// the next one safe, so a failure stops the run rather than skipping ahead:
//
//  1. webhook configurations go first, since a Fail policy with its service stopped would
//     reject the writes below;
//  2. the controllers are stopped, so none re-adds what the next steps remove;
//  3. adopted shards lose their NodePool OwnerReferences and KRM metadata, and stay put;
//  4. NodePool finalizers are stripped, since no controller is left to process them.
//
// Every step skips what is already done, so a rerun after a partial failure resumes.
func RevertForUninstall(ctx context.Context, k8sClient client.Client, opts RevertOptions) error {
	if err := deleteWebhookConfigurations(ctx, k8sClient, opts); err != nil {
		return err
	}
	if err := stopControllers(ctx, k8sClient, opts); err != nil {
		return err
	}
	if err := releaseAdoptedShards(ctx, k8sClient); err != nil {
		return err
	}
	return stripNodePoolFinalizers(ctx, k8sClient, config.FinalizerNameForDomain(opts.FinalizerDomain))
}

func deleteWebhookConfigurations(ctx context.Context, k8sClient client.Client, opts RevertOptions) error {
	for _, name := range opts.ValidatingWebhookConfigurations {
		if err := deleteWebhookConfiguration(ctx, k8sClient, "ValidatingWebhookConfiguration",
			&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil {
			return err
		}
	}
	for _, name := range opts.MutatingWebhookConfigurations {
		if err := deleteWebhookConfiguration(ctx, k8sClient, "MutatingWebhookConfiguration",
			&admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil {
			return err
		}
	}
	return nil
}

func deleteWebhookConfiguration(ctx context.Context, k8sClient client.Client, kind string, object client.Object) error {
	if err := k8sClient.Delete(ctx, object); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete %s %s: %w", kind, object.GetName(), err)
	}
	logf.FromContext(ctx).Info("Deleted webhook configuration", "kind", kind, "name", object.GetName())
	return nil
}

func stopControllers(ctx context.Context, k8sClient client.Client, opts RevertOptions) error {
	// Foreground deletion keeps the Deployment until its pods are gone, so waiting for it
	// also waits out each controller's termination grace period.
	foreground := client.PropagationPolicy(metav1.DeletePropagationForeground)

	// The operator goes first, or it would recreate the controllers deleted below.
	if opts.OperatorDeployment != "" {
		operator := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name: opts.OperatorDeployment, Namespace: opts.Namespace}}
		if err := deleteAndWait(ctx, k8sClient, operator, foreground); err != nil {
			return fmt.Errorf("failed to delete deployment %s/%s: %w", opts.Namespace, opts.OperatorDeployment, err)
		}
		logf.FromContext(ctx).Info("Deleted deployment", "namespace", opts.Namespace, "name", opts.OperatorDeployment)
	}
	return deleteOperatorManagedDeployments(ctx, k8sClient, opts.Namespace, foreground)
}

func releaseAdoptedShards(ctx context.Context, k8sClient client.Client) error {
	logger := logf.FromContext(ctx)

	shards := &kaiv1.SchedulingShardList{}
	if err := k8sClient.List(ctx, shards, client.MatchingLabels{
		unmanaged_shards.AdoptionLabelKey: unmanaged_shards.AdoptionAdopted}); err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("SchedulingShard CRD not found, no shards to release")
			return nil
		}
		return fmt.Errorf("failed to list adopted scheduling shards: %w", err)
	}

	for i := range shards.Items {
		shard := &shards.Items[i]
		if err := patchIfChanged(ctx, k8sClient, shard, func() bool { return releaseShard(shard) }); err != nil {
			return fmt.Errorf("failed to release scheduling shard %s: %w", shard.Name, err)
		}
		logger.Info("Released adopted scheduling shard", "kind", "SchedulingShard", "name", shard.Name)
	}
	return nil
}

func releaseShard(shard *kaiv1.SchedulingShard) bool {
	isChanged := false

	var owners []metav1.OwnerReference
	for _, owner := range shard.OwnerReferences {
		if unmanaged_shards.IsNodePoolReference(owner) {
			isChanged = true
			continue
		}
		owners = append(owners, owner)
	}
	shard.OwnerReferences = owners

	for _, metadata := range []map[string]string{shard.Labels, shard.Annotations} {
		for key := range metadata {
			if strings.HasPrefix(key, krmMetadataPrefix) {
				delete(metadata, key)
				isChanged = true
			}
		}
	}
	return isChanged
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
