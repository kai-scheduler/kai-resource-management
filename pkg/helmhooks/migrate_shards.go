// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	kaiconstants "github.com/kai-scheduler/api/constants"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1/schedulingshardargs"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/operands/kai-scheduler/resources"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

const (
	AdoptionLabelKey   = "kai.resources/adoption"
	AdoptionLabelValue = "adopted"
)

// MigrateShardsOptions are the controller settings a migrated shard must agree with.
type MigrateShardsOptions struct {
	// PartitionLabelKey is the node label KAI reads partitions from and the controller writes as
	// <key>=<NodePool name>; not a NodePool's own spec.labelKey.
	PartitionLabelKey   string
	DefaultNodePoolName string
	// SchedulingShardArgs are the cluster-wide args the controller writes into every shard.
	SchedulingShardArgs map[string]string
	// EnsureDefaultNodePool makes the hook the one creator of the default NodePool, for
	// installs whose chart does not render it beside an existing KAI Scheduler. Helm picks
	// what to create before any hook runs, so the two cannot both create it.
	EnsureDefaultNodePool bool
	// DefaultNodePoolSpec builds the default NodePool when no default shard exists to
	// build it from.
	DefaultNodePoolSpec kaires.NodePoolSpec
}

type shardMigration struct {
	shard            *kaiv1.SchedulingShard
	nodePoolName     string
	shardConfig      *kaires.SchedulingShardConfig
	existingNodePool *kaires.NodePool
}

// MigrateShards hands every pre-existing SchedulingShard to a NodePool built from its settings.
func MigrateShards(ctx context.Context, k8sClient client.Client, opts MigrateShardsOptions) error {
	logger := logf.FromContext(ctx)

	shards := &kaiv1.SchedulingShardList{}
	if err := k8sClient.List(ctx, shards); err != nil {
		if meta.IsNoMatchError(err) {
			logger.Info("SchedulingShard CRD is not installed; nothing to migrate")
			return nil
		}
		return fmt.Errorf("failed to list SchedulingShards: %w", err)
	}

	pending := shardsToMigrate(shards.Items)
	if len(pending) == 0 {
		logger.Info("no SchedulingShard needs migrating")
	}

	migrations, err := planMigrations(ctx, k8sClient, shards.Items, pending, opts)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if err := migrate(ctx, k8sClient, migration, opts); err != nil {
			return err
		}
		logger.Info("migrated SchedulingShard into a NodePool",
			"schedulingShard", migration.shard.Name, "nodePool", migration.nodePoolName)
	}

	if opts.EnsureDefaultNodePool {
		return ensureDefaultNodePool(ctx, k8sClient, opts)
	}
	return nil
}

// ensureDefaultNodePool creates the default NodePool from the chart's spec when nothing
// created it, such as a default shard. An existing one is left as it is.
func ensureDefaultNodePool(ctx context.Context, k8sClient client.Client, opts MigrateShardsOptions) error {
	// Looked up first rather than created and left to fail: on an upgrade the NodePool
	// webhook is running and refuses a duplicate for its own reasons before the API server
	// would answer that it already exists.
	existingNodePool, err := getNodePool(ctx, k8sClient, opts.DefaultNodePoolName)
	if err != nil || existingNodePool != nil {
		return err
	}

	nodePool := &kaires.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: opts.DefaultNodePoolName},
		Spec:       *opts.DefaultNodePoolSpec.DeepCopy(),
	}
	if err := k8sClient.Create(ctx, nodePool); err != nil {
		return fmt.Errorf("failed to create the default NodePool %s: %w", opts.DefaultNodePoolName, err)
	}
	logf.FromContext(ctx).Info("created the default NodePool", "nodePool", opts.DefaultNodePoolName)
	return nil
}

// shardsToMigrate skips non-default shards an admin excluded from KRM, and shards that
// already have a controller: a NodePool, or one another installer hands over itself, such
// as an older NodePool kind whose own upgrade hook re-parents its shards after this one.
func shardsToMigrate(shards []kaiv1.SchedulingShard) []*kaiv1.SchedulingShard {
	var pending []*kaiv1.SchedulingShard
	for i := range shards {
		shard := &shards[i]
		if metav1.GetControllerOf(shard) != nil || unmanaged_shards.IsUnmanaged(shard) {
			continue
		}
		pending = append(pending, shard)
	}
	slices.SortFunc(pending, func(a, b *kaiv1.SchedulingShard) int { return strings.Compare(a.Name, b.Name) })
	return pending
}

func planMigrations(ctx context.Context, k8sClient client.Client, all []kaiv1.SchedulingShard,
	pending []*kaiv1.SchedulingShard, opts MigrateShardsOptions,
) ([]shardMigration, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	errs := []error{checkPartitionLabelKey(ctx, k8sClient, opts.PartitionLabelKey)}

	shardNamesPerPartitionValue := map[string][]string{}
	for _, shard := range all {
		partitionLabelValue := shard.Spec.PartitionLabelValue
		shardNamesPerPartitionValue[partitionLabelValue] = append(
			shardNamesPerPartitionValue[partitionLabelValue], shard.Name)
	}

	var migrations []shardMigration
	for _, shard := range pending {
		partitionLabelValue := shard.Spec.PartitionLabelValue
		nodePoolName := partitionLabelValue
		if partitionLabelValue == "" {
			nodePoolName = opts.DefaultNodePoolName
		}

		if partitionLabelValue == "" && !opts.EnsureDefaultNodePool {
			errs = append(errs, fmt.Errorf("SchedulingShard %s serves the default partition, but "+
				"defaultNodePool.enabled=false leaves it no NodePool; enable defaultNodePool", shard.Name))
			continue
		}
		if shardNames := shardNamesPerPartitionValue[partitionLabelValue]; len(shardNames) > 1 {
			errs = append(errs, fmt.Errorf("SchedulingShards %v all serve partition %q; "+
				"a partition can belong to only one NodePool, so delete or relabel all but one",
				shardNames, partitionLabelValue))
			continue
		}
		if err := checkNodePoolName(shard, nodePoolName, opts.DefaultNodePoolName); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := checkShardArgs(shard, opts.SchedulingShardArgs); err != nil {
			errs = append(errs, err)
			continue
		}
		shardConfig, err := resources.SchedulingShardConfigFromShard(shard)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		existingNodePool, err := getNodePool(ctx, k8sClient, nodePoolName)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if existingNodePool != nil {
			if err := checkNodePoolSelectsPartition(existingNodePool, shard, opts.PartitionLabelKey); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		migrations = append(migrations, shardMigration{
			shard: shard, nodePoolName: nodePoolName, shardConfig: shardConfig, existingNodePool: existingNodePool,
		})
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("cannot migrate the existing SchedulingShards into NodePools, nothing was changed: %w", err)
	}
	return migrations, nil
}

// checkPartitionLabelKey fails when KAI reads partitions from another label than the controller writes.
func checkPartitionLabelKey(ctx context.Context, reader client.Reader, partitionLabelKey string) error {
	kaiConfig := &kaiv1.Config{}
	err := reader.Get(ctx, types.NamespacedName{Name: kaiconstants.DefaultKAIConfigSingeltonInstanceName}, kaiConfig)
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read KAI Scheduler Config %q: %w", kaiconstants.DefaultKAIConfigSingeltonInstanceName, err)
	}

	kaiLabelKey := kaiconstants.DefaultNodePoolLabelKey
	if kaiConfig.Spec.Global != nil && ptr.Deref(kaiConfig.Spec.Global.NodePoolLabelKey, "") != "" {
		kaiLabelKey = *kaiConfig.Spec.Global.NodePoolLabelKey
	}
	if kaiLabelKey != partitionLabelKey {
		return fmt.Errorf("KAI Scheduler reads node pools from label %q but KRM would use %q; "+
			"set the chart value kai-scheduler.global.nodePoolLabelKey=%s", kaiLabelKey, partitionLabelKey, kaiLabelKey)
	}
	return nil
}

func checkNodePoolName(shard *kaiv1.SchedulingShard, nodePoolName, defaultNodePoolName string) error {
	if shard.Spec.PartitionLabelValue == "" {
		return nil
	}
	if nodePoolName == defaultNodePoolName {
		return fmt.Errorf("SchedulingShard %s serves partition %q, which is the default NodePool's name; "+
			"move its nodes to a partition with another name", shard.Name, nodePoolName)
	}
	if problems := validation.IsDNS1123Subdomain(nodePoolName); len(problems) > 0 {
		return fmt.Errorf("SchedulingShard %s serves partition %q, which cannot be a NodePool name (%s); "+
			"move its nodes to a partition with a lowercase name", shard.Name, nodePoolName, strings.Join(problems, "; "))
	}
	return nil
}

// checkShardArgs fails on an arg the controller will overwrite with a different value.
func checkShardArgs(shard *kaiv1.SchedulingShard, clusterArgs map[string]string) error {
	controllerArgs := map[string]string{
		schedulingshardargs.CPUWorkerNodeLabelKey: kaiconstants.DefaultCPUWorkerNodeLabelKey,
		schedulingshardargs.GPUWorkerNodeLabelKey: kaiconstants.DefaultGPUWorkerNodeLabelKey,
		schedulingshardargs.MIGWorkerNodeLabelKey: kaiconstants.DefaultMIGWorkerNodeLabelKey,
	}
	for key, value := range clusterArgs {
		controllerArgs[key] = value
	}

	var conflicts []string
	for key, value := range shard.Spec.Args {
		if want, ok := controllerArgs[key]; ok && want != value {
			conflicts = append(conflicts, fmt.Sprintf("%s=%q (KRM sets %q)", key, value, want))
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	slices.Sort(conflicts)
	return fmt.Errorf("SchedulingShard %s sets args KRM overwrites with other values: %s; "+
		"set them in nodePoolController.args.schedulingShardArgs instead", shard.Name, strings.Join(conflicts, ", "))
}

// getNodePool returns nil when the NodePool does not exist.
func getNodePool(ctx context.Context, reader client.Reader, name string) (*kaires.NodePool, error) {
	nodePool := &kaires.NodePool{}
	err := reader.Get(ctx, types.NamespacedName{Name: name}, nodePool)
	if err != nil && apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read NodePool %s: %w", name, err)
	}
	return nodePool, nil
}

// checkNodePoolSelectsPartition refuses a NodePool that selects other nodes than the shard serves:
// the controller relabels them into its partition, moving nodes between schedulers.
func checkNodePoolSelectsPartition(nodePool *kaires.NodePool, shard *kaiv1.SchedulingShard,
	partitionLabelKey string,
) error {
	partitionLabelValue := shard.Spec.PartitionLabelValue
	if partitionLabelValue == "" ||
		(nodePool.Spec.LabelKey == partitionLabelKey && nodePool.Spec.LabelValue == partitionLabelValue) {
		return nil
	}
	return fmt.Errorf("NodePool %s already exists and selects nodes labelled %s=%s, but SchedulingShard %s "+
		"serves the nodes labelled %s=%s; taking it over would move nodes between schedulers, so either select "+
		"%s=%s in the NodePool, rename the NodePool, or move the shard and its nodes to another partitionLabelValue",
		nodePool.Name, nodePool.Spec.LabelKey, nodePool.Spec.LabelValue, shard.Name,
		partitionLabelKey, partitionLabelValue, partitionLabelKey, partitionLabelValue)
}

func migrate(ctx context.Context, k8sClient client.Client, migration shardMigration, opts MigrateShardsOptions) error {
	nodePool, err := nodePoolForShard(ctx, k8sClient, migration, opts)
	if err != nil {
		return err
	}

	shard := migration.shard
	original := shard.DeepCopy()
	if err := controllerutil.SetControllerReference(nodePool, shard, k8sClient.Scheme()); err != nil {
		return fmt.Errorf("failed to make NodePool %s the owner of SchedulingShard %s: %w", nodePool.Name, shard.Name, err)
	}
	if shard.Labels == nil {
		shard.Labels = map[string]string{}
	}
	shard.Labels[AdoptionLabelKey] = AdoptionLabelValue
	// The default partition always needs a NodePool, so it can never stay excluded.
	if shard.Spec.PartitionLabelValue == "" {
		delete(shard.Labels, unmanaged_shards.IgnoreShardLabelKey)
	}
	if err := k8sClient.Patch(ctx, shard, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to hand SchedulingShard %s to NodePool %s: %w", shard.Name, nodePool.Name, err)
	}
	return nil
}

// nodePoolForShard creates the shard's NodePool, or updates an existing one to the shard's settings.
func nodePoolForShard(ctx context.Context, k8sClient client.Client, migration shardMigration,
	opts MigrateShardsOptions,
) (*kaires.NodePool, error) {
	if nodePool := migration.existingNodePool; nodePool != nil {
		if equality.Semantic.DeepEqual(nodePool.Spec.SchedulingShardConfig, migration.shardConfig) {
			return nodePool, nil
		}
		original := nodePool.DeepCopy()
		nodePool.Spec.SchedulingShardConfig = migration.shardConfig
		if err := k8sClient.Patch(ctx, nodePool, client.MergeFrom(original)); err != nil {
			return nil, fmt.Errorf("failed to update NodePool %s to SchedulingShard %s's settings: %w",
				nodePool.Name, migration.shard.Name, err)
		}
		return nodePool, nil
	}

	nodePool := &kaires.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: migration.nodePoolName},
		Spec:       kaires.NodePoolSpec{SchedulingShardConfig: migration.shardConfig},
	}
	if partitionLabelValue := migration.shard.Spec.PartitionLabelValue; partitionLabelValue != "" {
		nodePool.Spec.LabelKey = opts.PartitionLabelKey
		nodePool.Spec.LabelValue = partitionLabelValue
	}
	if err := k8sClient.Create(ctx, nodePool); err != nil {
		return nil, fmt.Errorf("failed to create NodePool %s for SchedulingShard %s: %w",
			nodePool.Name, migration.shard.Name, err)
	}
	return nodePool, nil
}
