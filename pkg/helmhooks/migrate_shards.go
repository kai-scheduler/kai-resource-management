// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	kaiconstants "github.com/kai-scheduler/api/constants"
	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1/schedulingshardargs"
	"github.com/xhit/go-str2duration/v2"
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
	// AdoptionLabelKey marks a SchedulingShard that existed before KRM and was taken over
	// by a NodePool, as opposed to one the controller created.
	AdoptionLabelKey   = "kai.resources/adoption"
	AdoptionLabelValue = "adopted"
)

// kaiDefaultUsageQueries are the queries KAI Scheduler's Prometheus usage client falls back
// to. The controller writes per-node-pool queries for keys a node pool leaves unset, so a
// migrated shard that relied on these gets them written out to keep reading the same data.
var kaiDefaultUsageQueries = map[string]string{
	"gpuAllocationMetric":    "kai_queue_allocated_gpus",
	"cpuAllocationMetric":    "kai_queue_allocated_cpu_cores",
	"memoryAllocationMetric": "kai_queue_allocated_memory_bytes",
	"gpuCapacityMetric":      `sum(kube_node_status_capacity{resource="nvidia_com_gpu"})`,
	"cpuCapacityMetric":      `sum(kube_node_status_capacity{resource="cpu"})`,
	"memoryCapacityMetric":   `sum(kube_node_status_capacity{resource="memory"})`,
}

// MigrateShardsOptions are the controller settings a migrated shard must agree with.
type MigrateShardsOptions struct {
	// NodePoolLabelKey is the controller's --nodepool-label-key.
	NodePoolLabelKey    string
	DefaultNodePoolName string
	// SchedulingShardArgs are the cluster-wide args the controller writes into every shard.
	SchedulingShardArgs map[string]string
	// ReleaseName and ReleaseNamespace, when set, mark a default NodePool the hook creates
	// as belonging to the release that renders it, so Helm adopts it instead of failing.
	ReleaseName      string
	ReleaseNamespace string
}

type shardMigration struct {
	shard        *kaiv1.SchedulingShard
	nodePoolName string
	config       *kaires.SchedulingShardConfig
	existing     *kaires.NodePool
}

// MigrateShards gives every pre-existing SchedulingShard a NodePool that owns it, carrying
// the shard's settings over so the scheduler behaves the same once the controller takes
// over. It checks every shard before changing any, and changes nothing if one cannot be
// migrated exactly. Shards a NodePool already owns are skipped, so a rerun is a no-op.
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
		return nil
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
	errs := []error{checkNodePoolLabelKey(ctx, k8sClient, opts.NodePoolLabelKey)}

	shardsPerPartition := map[string][]string{}
	for _, shard := range all {
		partition := shard.Spec.PartitionLabelValue
		shardsPerPartition[partition] = append(shardsPerPartition[partition], shard.Name)
	}

	var migrations []shardMigration
	for _, shard := range pending {
		partition := shard.Spec.PartitionLabelValue
		nodePoolName := partition
		if partition == "" {
			nodePoolName = opts.DefaultNodePoolName
		}

		if names := shardsPerPartition[partition]; len(names) > 1 {
			errs = append(errs, fmt.Errorf("SchedulingShards %v all serve partition %q; "+
				"a partition can belong to only one NodePool, so delete or relabel all but one", names, partition))
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
		config, err := schedulingShardConfig(shard)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		existing, err := existingNodePool(ctx, k8sClient, shard, nodePoolName, opts.NodePoolLabelKey, config)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		migrations = append(migrations, shardMigration{
			shard: shard, nodePoolName: nodePoolName, config: config, existing: existing,
		})
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("cannot migrate the existing SchedulingShards into NodePools, nothing was changed: %w", err)
	}
	return migrations, nil
}

// checkNodePoolLabelKey makes sure a migrated NodePool selects the same nodes its shard
// did: KAI reads a node's partition from its own label key, and the controller labels
// nodes with the key it was given.
func checkNodePoolLabelKey(ctx context.Context, reader client.Reader, nodePoolLabelKey string) error {
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
	if kaiLabelKey != nodePoolLabelKey {
		return fmt.Errorf("KAI Scheduler reads node pools from label %q but KRM would use %q; "+
			"set the chart value kai-scheduler.global.nodePoolLabelKey=%s", kaiLabelKey, nodePoolLabelKey, kaiLabelKey)
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

// existingNodePool returns the NodePool the shard will belong to when it already exists.
// That only happens legitimately when an earlier run stopped after creating it, so it
// must select the shard's nodes and hold exactly the shard's settings: taking the shard
// over otherwise would overwrite its settings, and deleting the NodePool would drain it.
func existingNodePool(ctx context.Context, reader client.Reader, shard *kaiv1.SchedulingShard,
	nodePoolName, nodePoolLabelKey string, config *kaires.SchedulingShardConfig,
) (*kaires.NodePool, error) {
	nodePool := &kaires.NodePool{}
	err := reader.Get(ctx, types.NamespacedName{Name: nodePoolName}, nodePool)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read NodePool %s: %w", nodePoolName, err)
	}

	partition := shard.Spec.PartitionLabelValue
	if partition != "" && (nodePool.Spec.LabelKey != nodePoolLabelKey || nodePool.Spec.LabelValue != partition) {
		return nil, fmt.Errorf("NodePool %s already exists and selects nodes labelled %s=%s, "+
			"not the %s=%s nodes of SchedulingShard %s; rename one of them",
			nodePoolName, nodePool.Spec.LabelKey, nodePool.Spec.LabelValue, nodePoolLabelKey, partition, shard.Name)
	}
	if !sameSchedulingShardConfig(nodePool.Spec.SchedulingShardConfig, config) {
		return nil, fmt.Errorf("NodePool %s already exists with different settings than SchedulingShard %s; "+
			"delete the NodePool or align its schedulingShardConfig with the shard, then retry",
			nodePoolName, shard.Name)
	}
	return nodePool, nil
}

func sameSchedulingShardConfig(a, b *kaires.SchedulingShardConfig) bool {
	empty := &kaires.SchedulingShardConfig{}
	if a == nil {
		a = empty
	}
	if b == nil {
		b = empty
	}
	return equality.Semantic.DeepEqual(a, b)
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
	if shard.Annotations == nil {
		shard.Annotations = map[string]string{}
	}
	// Tracked with nothing managed, so the controller keeps the args already on the shard
	// as the admin's instead of dropping every arg its NodePool does not list.
	if _, tracked := shard.Annotations[resources.ManagedShardArgsAnnotation]; !tracked {
		shard.Annotations[resources.ManagedShardArgsAnnotation] = ""
	}
	if err := k8sClient.Patch(ctx, shard, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("failed to hand SchedulingShard %s to NodePool %s: %w", shard.Name, nodePool.Name, err)
	}
	return nil
}

func nodePoolForShard(ctx context.Context, k8sClient client.Client, migration shardMigration,
	opts MigrateShardsOptions,
) (*kaires.NodePool, error) {
	if migration.existing != nil {
		return migration.existing, nil
	}

	nodePool := &kaires.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: migration.nodePoolName},
		Spec:       kaires.NodePoolSpec{SchedulingShardConfig: migration.config},
	}
	if partition := migration.shard.Spec.PartitionLabelValue; partition != "" {
		nodePool.Spec.LabelKey = opts.NodePoolLabelKey
		nodePool.Spec.LabelValue = partition
	} else if opts.ReleaseName != "" {
		markAsHelmRelease(nodePool, opts.ReleaseName, opts.ReleaseNamespace)
	}
	if err := k8sClient.Create(ctx, nodePool); err != nil {
		return nil, fmt.Errorf("failed to create NodePool %s for SchedulingShard %s: %w",
			nodePool.Name, migration.shard.Name, err)
	}
	return nodePool, nil
}

// markAsHelmRelease sets what Helm checks before adopting an object it did not create.
// Helm then patches it only where the chart's manifest differs from itself, which is
// nowhere, so the settings copied from the shard survive the adoption.
func markAsHelmRelease(nodePool *kaires.NodePool, releaseName, releaseNamespace string) {
	nodePool.Labels = map[string]string{"app.kubernetes.io/managed-by": "Helm"}
	nodePool.Annotations = map[string]string{
		"meta.helm.sh/release-name":      releaseName,
		"meta.helm.sh/release-namespace": releaseNamespace,
		"helm.sh/resource-policy":        "keep",
	}
}

// schedulingShardConfig is the NodePool configuration the controller turns back into the
// shard as it is. Args stay on the shard, where the controller keeps them as the admin's.
func schedulingShardConfig(shard *kaiv1.SchedulingShard) (*kaires.SchedulingShardConfig, error) {
	spec := shard.Spec.DeepCopy()
	timeBasedFairShare, err := timeBasedFairShare(shard.Name, spec)
	if err != nil {
		return nil, err
	}
	config := &kaires.SchedulingShardConfig{
		PlacementStrategy:   spec.PlacementStrategy,
		QueueDepthPerAction: spec.QueueDepthPerAction,
		MinRuntime:          spec.MinRuntime,
		Plugins:             spec.Plugins,
		Actions:             spec.Actions,
		TimeBasedFairShare:  timeBasedFairShare,
	}
	if equality.Semantic.DeepEqual(config, &kaires.SchedulingShardConfig{}) {
		return nil, nil
	}
	return config, nil
}

// timeBasedFairShare maps a shard's usage DB back to the NodePool field the controller
// compiles it from. A kValue without a usage DB is dropped: with no usage to weigh,
// KAI's share weight w+k*(w-0) scales every queue alike and normalising cancels k out.
// The client type and the connection-string env var are not carried over: prometheus is
// the only production client, and nothing on a shard can set the scheduler's env.
func timeBasedFairShare(shardName string, spec *kaiv1.SchedulingShardSpec) (*kaires.TimeBasedFairShare, error) {
	usageDB := spec.UsageDBConfig
	if usageDB == nil {
		return nil, nil
	}

	fairShare := &kaires.TimeBasedFairShare{Enabled: ptr.To(true)}
	if spec.KValue != nil {
		// KAI treats a negative kValue as 0, which the NodePool's minimum also requires.
		fairShare.HistoricalUsageWeight = ptr.To(float32(max(*spec.KValue, 0)))
	}
	if usageDB.ConnectionString != "" {
		fairShare.ConnectionString = ptr.To(usageDB.ConnectionString)
	}

	params := usageDB.UsageParams
	if params == nil {
		fairShare.ExtraParams = withKAIDefaultUsageQueries(nil)
		return fairShare, nil
	}
	fairShare.ExtraParams = withKAIDefaultUsageQueries(params.ExtraParams)

	if params.HalfLifePeriod != nil {
		fairShare.HalfLifePeriod = ptr.To(nodePoolDuration(params.HalfLifePeriod.Duration))
	}
	window := &kaires.FairShareWindow{}
	if params.WindowSize != nil {
		size, err := str2duration.ParseDuration(string(*params.WindowSize))
		if err != nil {
			return nil, fmt.Errorf("SchedulingShard %s has usage window size %q, which cannot be read as a duration: %w",
				shardName, *params.WindowSize, err)
		}
		window.Size = ptr.To(nodePoolDuration(size))
	}
	if params.WindowType != nil {
		window.Type = ptr.To(string(*params.WindowType))
	}
	window.TumblingStartTime = params.TumblingWindowStartTime
	if params.CronString != "" {
		window.CronString = ptr.To(params.CronString)
	}
	if *window != (kaires.FairShareWindow{}) {
		fairShare.Window = window
	}

	sampling := &kaires.FairShareSampling{
		FetchInterval:   goDuration(params.FetchInterval),
		StalenessPeriod: goDuration(params.StalenessPeriod),
		WaitTimeout:     goDuration(params.WaitTimeout),
	}
	if *sampling != (kaires.FairShareSampling{}) {
		fairShare.Sampling = sampling
	}
	return fairShare, nil
}

func withKAIDefaultUsageQueries(extraParams map[string]string) map[string]string {
	params := make(map[string]string, len(extraParams)+len(kaiDefaultUsageQueries))
	for key, query := range kaiDefaultUsageQueries {
		params[key] = query
	}
	for key, value := range extraParams {
		params[key] = value
	}
	return params
}

// nodePoolDuration formats a duration in the units the NodePool's half-life and window
// size accept: days, hours and minutes.
func nodePoolDuration(duration time.Duration) string {
	switch {
	case duration <= 0:
		return "0"
	case duration%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", duration/(24*time.Hour))
	case duration%time.Hour == 0:
		return fmt.Sprintf("%dh", duration/time.Hour)
	default:
		return strconv.FormatFloat(duration.Minutes(), 'f', -1, 64) + "m"
	}
}

func goDuration(duration *metav1.Duration) *string {
	if duration == nil {
		return nil
	}
	return ptr.To(duration.Duration.String())
}
