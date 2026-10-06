// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"fmt"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/operands/kai-scheduler/resources"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

// SchedulingShardWebhookPath is chosen by hand rather than generated from the GVK: KAI
// Scheduler ships no SchedulingShard webhook, and the path says this one is KRM's.
const SchedulingShardWebhookPath = "/validate-krm-v1-schedulingshard"

// schedulingShardValidator keeps every partitionLabelValue on exactly one shard that something
// reconciles: either a NodePool, or the pre-existing scheduler the shard is labelled
// unmanaged for. Two shards with one partitionLabelValue are two schedulers competing for the
// same nodes, and a shard with neither is an orphan, since only the install-time migration
// hook ever takes a shard over.
type schedulingShardValidator struct {
	client client.Client
}

func SetupSchedulingShardWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &kaiv1.SchedulingShard{}).
		WithValidator(&schedulingShardValidator{client: mgr.GetClient()}).
		WithValidatorCustomPath(SchedulingShardWebhookPath).
		Complete()
}

func (v *schedulingShardValidator) ValidateCreate(ctx context.Context, shard *kaiv1.SchedulingShard) (admission.Warnings, error) {
	partitionLabelValue := shard.Spec.PartitionLabelValue

	others, err := resources.ListShardsWithPartitionLabelValue(ctx, v.client, partitionLabelValue)
	if err != nil {
		log.Error().Err(err).Str("name", shard.Name).
			Msg("failed to list scheduling shards for partitionLabelValue validation")
		return nil, fmt.Errorf("failed to validate scheduling shard %q: could not list existing scheduling shards", shard.Name)
	}
	for i := range others {
		if others[i].Name != shard.Name {
			return nil, fmt.Errorf("scheduling shard %q cannot use partitionLabelValue %q: scheduling shard %q already has it",
				shard.Name, partitionLabelValue, others[i].Name)
		}
	}

	if unmanaged_shards.IsUnmanaged(shard) {
		return nil, nil
	}
	return nil, v.requireNodePool(ctx, shard)
}

// ValidateUpdate keeps a shard's partitionLabelValue and unmanaged label as they were created:
// changing either would turn an admitted shard into one ValidateCreate rejects. The label is
// free on the default partitionLabelValue, where it has no effect and the migration hook strips it.
func (v *schedulingShardValidator) ValidateUpdate(
	_ context.Context, oldShard, newShard *kaiv1.SchedulingShard,
) (admission.Warnings, error) {
	oldPartitionLabelValue, newPartitionLabelValue := oldShard.Spec.PartitionLabelValue, newShard.Spec.PartitionLabelValue
	if oldPartitionLabelValue != newPartitionLabelValue {
		return nil, fmt.Errorf("scheduling shard %q cannot change partitionLabelValue from %q to %q; create a new shard instead",
			newShard.Name, oldPartitionLabelValue, newPartitionLabelValue)
	}

	oldLabel, oldHasLabel := oldShard.Labels[unmanaged_shards.IgnoreShardLabelKey]
	newLabel, newHasLabel := newShard.Labels[unmanaged_shards.IgnoreShardLabelKey]
	if newPartitionLabelValue != "" && (oldHasLabel != newHasLabel || oldLabel != newLabel) {
		return nil, fmt.Errorf("scheduling shard %q with partitionLabelValue %q cannot add, change or remove its %s label "+
			"after creation", newShard.Name, newPartitionLabelValue, unmanaged_shards.IgnoreShardLabelKey)
	}
	return nil, nil
}

func (v *schedulingShardValidator) ValidateDelete(_ context.Context, _ *kaiv1.SchedulingShard) (admission.Warnings, error) {
	return nil, nil
}

func (v *schedulingShardValidator) requireNodePool(ctx context.Context, shard *kaiv1.SchedulingShard) error {
	nodePoolName := resources.NodePoolNameForPartitionLabelValue(shard.Spec.PartitionLabelValue)
	err := v.client.Get(ctx, types.NamespacedName{Name: nodePoolName}, &v1alpha1.NodePool{})
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		log.Error().Err(err).Str("name", shard.Name).Str("nodepool", nodePoolName).
			Msg("failed to get the nodepool for a scheduling shard's partitionLabelValue")
		return fmt.Errorf("failed to validate scheduling shard %q: could not get nodepool %q", shard.Name, nodePoolName)
	}
	// The label has no effect on the default partitionLabelValue, so suggesting it would not help.
	labelHint := ""
	if shard.Spec.PartitionLabelValue != "" {
		labelHint = fmt.Sprintf(", or label the shard %s=true to leave its nodes to another scheduler",
			unmanaged_shards.IgnoreShardLabelKey)
	}
	return fmt.Errorf("scheduling shard %q cannot use partitionLabelValue %q: no nodepool %q exists for it; "+
		"create that nodepool%s", shard.Name, shard.Spec.PartitionLabelValue, nodePoolName, labelHint)
}
