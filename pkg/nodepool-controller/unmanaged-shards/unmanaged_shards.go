// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package unmanaged_shards

import (
	"context"
	"fmt"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IgnoreShardLabelKey marks a SchedulingShard whose partition stays with a pre-existing
// scheduler: KRM neither reconciles the shard nor touches the partition's nodes.
const IgnoreShardLabelKey = "kai/ignore-shard-for-krm"

// IsUnmanaged ignores the label on a shard a NodePool already owns, since releasing an
// adopted shard is an uninstall, and on the default partition, which always needs a NodePool.
func IsUnmanaged(shard *kaiv1.SchedulingShard) bool {
	return shard.Labels[IgnoreShardLabelKey] == "true" &&
		shard.Spec.PartitionLabelValue != "" &&
		OwningNodePoolName(shard) == ""
}

// ListPartitionLabelValues returns the partition label values of every unmanaged shard.
func ListPartitionLabelValues(ctx context.Context, reader client.Reader) (map[string]bool, error) {
	shards := &kaiv1.SchedulingShardList{}
	if err := reader.List(ctx, shards, client.HasLabels{IgnoreShardLabelKey}); err != nil {
		return nil, fmt.Errorf("listing scheduling shards labelled %s: %w", IgnoreShardLabelKey, err)
	}

	partitionLabelValues := map[string]bool{}
	for i := range shards.Items {
		if IsUnmanaged(&shards.Items[i]) {
			partitionLabelValues[shards.Items[i].Spec.PartitionLabelValue] = true
		}
	}
	return partitionLabelValues, nil
}

// OwningNodePoolName returns the NodePool controlling the shard, or "" when none does.
func OwningNodePoolName(shard *kaiv1.SchedulingShard) string {
	owner := metav1.GetControllerOf(shard)
	if owner == nil || owner.Kind != "NodePool" {
		return ""
	}
	ownerGroupVersion, err := schema.ParseGroupVersion(owner.APIVersion)
	if err != nil || ownerGroupVersion.Group != v1alpha1.GroupVersion.Group {
		return ""
	}
	return owner.Name
}
