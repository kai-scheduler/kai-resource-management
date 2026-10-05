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

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/operands/kai-scheduler/resources"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

// +kubebuilder:webhook:path=/validate-kai-resources-v1alpha1-nodepool,mutating=false,failurePolicy=fail,sideEffects=None,groups=kai.resources,resources=nodepools,verbs=create,versions=v1alpha1,name=nodepool-validation.kai.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/validate-kai-resources-v1alpha1-nodepool,mutating=false,failurePolicy=fail,sideEffects=None,groups=kai.resources,resources=nodepools,verbs=delete,versions=v1alpha1,name=nodepool-deletion.kai.io,admissionReviewVersions=v1

// nodePoolValidator is an admission webhook that keeps NodePool label selectors
// consistent across the cluster. A NodePool selects its nodes by a
// labelKey/labelValue pair, and the validator enforces two rules on create
// (the pair is immutable after creation, enforced by the CRD, so updates need
// no revalidation):
//
//   - Every NodePool must set a non-empty labelKey and labelValue, so it
//     targets a well-defined set of nodes. The "default" NodePool is the sole
//     exception: it must leave both empty and selects every node not claimed by
//     another NodePool.
//   - No two NodePools may share the same labelKey/labelValue pair, so a node is
//     never claimed by more than one NodePool.
//
// On create it also refuses a NodePool whose partitionLabelValue another scheduler already
// serves, since reconciling it would put a second scheduler on the same nodes. See
// validatePartitionLabelValue.
//
// On delete it also refuses to let the default NodePool go.
type nodePoolValidator struct {
	client client.Client
}

// SetupNodePoolWebhookWithManager registers the NodePool validating webhook with
// the manager's webhook server.
func SetupNodePoolWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &v1alpha1.NodePool{}).
		WithValidator(&nodePoolValidator{client: mgr.GetClient()}).
		Complete()
}

func (v *nodePoolValidator) ValidateCreate(ctx context.Context, nodePool *v1alpha1.NodePool) (admission.Warnings, error) {
	if err := v.validateLabels(ctx, nodePool); err != nil {
		return nil, err
	}
	return nil, v.validatePartitionLabelValue(ctx, nodePool)
}

// ValidateUpdate is a no-op because the labelKey/labelValue pair is immutable after creation.
func (v *nodePoolValidator) ValidateUpdate(_ context.Context, _, _ *v1alpha1.NodePool) (admission.Warnings, error) {
	return nil, nil
}

// ValidateDelete keeps the default nodepool alive: nothing recreates it, and removing it
// would leave every node that no other nodepool selects without a nodepool.
func (v *nodePoolValidator) ValidateDelete(_ context.Context, nodePool *v1alpha1.NodePool) (admission.Warnings, error) {
	if nodePool.Name == config.Get().DefaultNodepoolName {
		return nil, fmt.Errorf("the %q nodepool cannot be deleted; it is the catch-all for nodes no other nodepool claims",
			config.Get().DefaultNodepoolName)
	}
	return nil, nil
}

// validateLabels enforces, on create, the default-only empty-pair rule and the
// no-duplicate-pair rule.
func (v *nodePoolValidator) validateLabels(ctx context.Context, nodePool *v1alpha1.NodePool) error {
	key, value := nodePool.Spec.LabelKey, nodePool.Spec.LabelValue

	if nodePool.Name == config.Get().DefaultNodepoolName {
		// The default nodepool selects every otherwise-unclaimed node and must
		// not carry a label selector. Its empty pair has nothing to collide on.
		if key != "" || value != "" {
			return fmt.Errorf("the %q nodepool must not set labelKey or labelValue", config.Get().DefaultNodepoolName)
		}
		return nil
	}

	// Every other nodepool must target a well-defined set of nodes.
	if key == "" || value == "" {
		return fmt.Errorf(
			"nodepool %q must set a non-empty labelKey and labelValue; only the %q nodepool may leave them empty",
			nodePool.Name, config.Get().DefaultNodepoolName)
	}

	var nodePools v1alpha1.NodePoolList
	if err := v.client.List(ctx, &nodePools); err != nil {
		log.Error().Err(err).Str("name", nodePool.Name).
			Msg("failed to list nodepools for duplicate-label validation")
		return fmt.Errorf("failed to validate nodepool %q: could not list existing nodepools", nodePool.Name)
	}

	for i := range nodePools.Items {
		other := &nodePools.Items[i]
		if other.Name == nodePool.Name {
			continue
		}
		if other.Spec.LabelKey == key && other.Spec.LabelValue == value {
			return fmt.Errorf("nodepool %q labelKey %q / labelValue %q duplicates existing nodepool %q",
				nodePool.Name, key, value, other.Name)
		}
	}

	return nil
}

// validatePartitionLabelValue admits a NodePool only when no shard has its partitionLabelValue,
// or exactly one does that nothing owns and nothing excludes, which the migration hook can
// take over. Any other shard there already has a scheduler serving it for someone else.
func (v *nodePoolValidator) validatePartitionLabelValue(ctx context.Context, nodePool *v1alpha1.NodePool) error {
	partitionLabelValue := resources.PartitionLabelValueForNodePool(nodePool.Name)

	shards, err := listShardsWithPartitionLabelValue(ctx, v.client, partitionLabelValue)
	if err != nil {
		log.Error().Err(err).Str("name", nodePool.Name).
			Msg("failed to list scheduling shards for partitionLabelValue validation")
		return fmt.Errorf("failed to validate nodepool %q: could not list scheduling shards", nodePool.Name)
	}

	for i := range shards {
		shard := &shards[i]
		if owner := owningNodePoolName(shard); owner != "" {
			return fmt.Errorf("nodepool %q cannot use partitionLabelValue %q: scheduling shard %q already has it, "+
				"for nodepool %q", nodePool.Name, partitionLabelValue, shard.Name, owner)
		}
		if unmanaged_shards.IsUnmanaged(shard) {
			return fmt.Errorf("nodepool %q cannot use partitionLabelValue %q: scheduling shard %q has it and is "+
				"labelled %s, leaving those nodes to another scheduler", nodePool.Name, partitionLabelValue, shard.Name,
				unmanaged_shards.IgnoreShardLabelKey)
		}
	}
	if len(shards) > 1 {
		return fmt.Errorf("nodepool %q cannot use partitionLabelValue %q: more than one scheduling shard %v already has it",
			nodePool.Name, partitionLabelValue, sortedShardNames(shards))
	}
	if len(shards) == 1 {
		return nil
	}

	// With no shard to take over, the controller creates one named after the nodepool,
	// and cannot if a shard with another partitionLabelValue already holds that name.
	existing := &kaiv1.SchedulingShard{}
	err = v.client.Get(ctx, types.NamespacedName{Name: nodePool.Name}, existing)
	if err != nil && errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		log.Error().Err(err).Str("name", nodePool.Name).Msg("failed to get the scheduling shard named after a nodepool")
		return fmt.Errorf("failed to validate nodepool %q: could not get scheduling shard %q", nodePool.Name, nodePool.Name)
	}
	return fmt.Errorf("nodepool %q cannot use partitionLabelValue %q: its scheduling shard would be named %q, "+
		"which already has partitionLabelValue %q", nodePool.Name, partitionLabelValue, existing.Name,
		existing.Spec.PartitionLabelValue)
}
