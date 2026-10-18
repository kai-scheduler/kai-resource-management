// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"
	"strings"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

var krmMetadataPrefix = kaires.GroupVersion.Group + "/"

// releaseAdoptedSchedulingShards hands shards that predate KRM back to their scheduler, so
// deleting a NodePool after uninstall cannot garbage-collect them. It must run once the
// controllers are gone: a reconcile in flight would write the ownership back.
func releaseAdoptedSchedulingShards(ctx context.Context, k8sClient client.Client) error {
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
		if err := patchIfChanged(ctx, k8sClient, shard, func() bool { return releaseSchedulingShard(shard) }); err != nil {
			return fmt.Errorf("failed to release scheduling shard %s: %w", shard.Name, err)
		}
		logger.Info("Released adopted scheduling shard", "kind", "SchedulingShard", "name", shard.Name)
	}
	return nil
}

func releaseSchedulingShard(shard *kaiv1.SchedulingShard) bool {
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
