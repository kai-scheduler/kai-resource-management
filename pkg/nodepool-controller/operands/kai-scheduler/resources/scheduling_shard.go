// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	usagedbapi "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/usagedb/api"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1/schedulingshardargs"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/rs/zerolog/log"
	"github.com/xhit/go-str2duration/v2"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/operands"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

const (
	defaultTimeBasedFairShareHistoricalUsageWeight = float32(1.0)
	defaultTimeBasedFairShareDecayHalfLife         = 0 * time.Second
	defaultTimeBasedFairShareWindowDuration        = 7 * 24 * time.Hour
	defaultTimeBasedFairShareWindowType            = usagedbapi.SlidingWindow

	managedShardArgsAnnotation = "kai.resources/managed-shard-args"
)

func SchedulerBaseOperandName() string { return config.Get().SchedulerName }

func SchedulingShardForNodePool(
	ctx context.Context, k8sReader client.Reader, nodePool *v1alpha1.NodePool,
	params *common.NodePoolControllerParams, _ string,
) (client.Object, error) {
	shard, err := ResolveShardForNodePool(ctx, k8sReader, nodePool)
	if err != nil {
		return nil, err
	}
	if shard == nil {
		if shard, err = GetOrInitShardNamedAfterNodePool(ctx, k8sReader, nodePool); err != nil {
			return nil, err
		}
	}

	if shard.Labels == nil {
		shard.Labels = map[string]string{}
	}
	shard.Labels["app"] = SchedulerBaseOperandName()
	cfg := nodePool.Spec.SchedulingShardConfig
	shard.Spec.Args = mergeShardArgs(shard, buildShardArgs(cfg, params))
	shard.Spec.PlacementStrategy = getPlacementStrategy(cfg)
	shard.Spec.PartitionLabelValue = PartitionLabelValueForNodePool(nodePool.Name)
	shard.Spec.QueueDepthPerAction = getQueueDepthPerAction(cfg)
	shard.Spec.MinRuntime = getMinRuntime(cfg)
	shard.Spec.KValue = getKValue(nodePool)
	shard.Spec.UsageDBConfig = getTimeBasedFairShareFields(nodePool)
	shard.Spec.Plugins = getPlugins(cfg)
	shard.Spec.Actions = getActions(cfg)
	return shard, nil
}

func mergeShardArgs(shard *kaiv1.SchedulingShard, desired map[string]string) map[string]string {
	args := map[string]string{}
	if managed, isTracked := shard.Annotations[managedShardArgsAnnotation]; isTracked {
		previouslyManaged := sets.New(strings.Split(managed, ",")...)
		for key, value := range shard.Spec.Args {
			if !previouslyManaged.Has(key) {
				args[key] = value
			}
		}
	}
	maps.Copy(args, desired)

	if shard.Annotations == nil {
		shard.Annotations = map[string]string{}
	}
	shard.Annotations[managedShardArgsAnnotation] = strings.Join(slices.Sorted(maps.Keys(desired)), ",")
	return args
}

func ResolveShardForNodePool(
	ctx context.Context, k8sReader client.Reader, nodePool *v1alpha1.NodePool,
) (*kaiv1.SchedulingShard, error) {
	partitionLabelValue := PartitionLabelValueForNodePool(nodePool.Name)
	shards, err := ListShardsWithPartitionLabelValue(ctx, k8sReader, partitionLabelValue)
	if err != nil {
		return nil, fmt.Errorf("node pool %s: %w", nodePool.Name, err)
	}

	switch len(shards) {
	case 0:
		return nil, nil
	case 1:
		shard := &shards[0]
		if unmanaged_shards.IsUnmanaged(shard) {
			return nil, fmt.Errorf("partitionLabelValue %q of node pool %s is served by scheduling shard %s, labelled %s",
				partitionLabelValue, nodePool.Name, shard.Name, unmanaged_shards.IgnoreShardLabelKey)
		}
		return shard, nil
	default:
		names := make([]string, 0, len(shards))
		for _, shard := range shards {
			names = append(names, shard.Name)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("partitionLabelValue %q of node pool %s is served by more than one scheduling shard %v",
			partitionLabelValue, nodePool.Name, names)
	}
}

func ListShardsWithPartitionLabelValue(
	ctx context.Context, k8sReader client.Reader, partitionLabelValue string,
) ([]kaiv1.SchedulingShard, error) {
	shards := &kaiv1.SchedulingShardList{}
	if err := k8sReader.List(ctx, shards,
		client.MatchingFields{common.SchedulingShardPartitionField: partitionLabelValue}); err != nil {
		return nil, fmt.Errorf("listing scheduling shards with partitionLabelValue %q: %w", partitionLabelValue, err)
	}
	return shards.Items, nil
}

// GetOrInitShardNamedAfterNodePool returns the shard named after the NodePool, or an unsaved one
// when none exists yet. It fails when another owner's shard already holds that name.
func GetOrInitShardNamedAfterNodePool(
	ctx context.Context, k8sReader client.Reader, nodePool *v1alpha1.NodePool,
) (*kaiv1.SchedulingShard, error) {
	shard := &kaiv1.SchedulingShard{}
	err := k8sReader.Get(ctx, types.NamespacedName{Name: nodePool.Name}, shard)
	if err != nil && errors.IsNotFound(err) {
		return &kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{Name: nodePool.Name}}, nil
	}
	if err != nil {
		return nil, err
	}
	if !metav1.IsControlledBy(shard, nodePool) {
		return nil, fmt.Errorf(
			"scheduling shard %s serves partitionLabelValue %q and is not owned by node pool %s, "+
				"which needs its name for partitionLabelValue %q",
			shard.Name, shard.Spec.PartitionLabelValue, nodePool.Name, PartitionLabelValueForNodePool(nodePool.Name))
	}
	return shard, nil
}

func shardNameForNodePool(ctx context.Context, k8sReader client.Reader, nodePool *v1alpha1.NodePool) (string, error) {
	shard, err := ResolveShardForNodePool(ctx, k8sReader, nodePool)
	if err != nil {
		return "", err
	}
	if shard == nil {
		return nodePool.Name, nil
	}
	return shard.Name, nil
}

func buildShardArgs(cfg *v1alpha1.SchedulingShardConfig, params *common.NodePoolControllerParams) map[string]string {
	args := map[string]string{}
	if cfg != nil {
		maps.Copy(args, cfg.Args)
	}
	maps.Copy(args, params.SchedulingShardArgs)
	args[schedulingshardargs.CPUWorkerNodeLabelKey] = config.Get().CPUWorkerNodeLabelKey
	args[schedulingshardargs.GPUWorkerNodeLabelKey] = config.Get().GPUWorkerNodeLabelKey
	args[schedulingshardargs.MIGWorkerNodeLabelKey] = config.Get().MIGWorkerNodeLabelKey
	return args
}

func getPlacementStrategy(cfg *v1alpha1.SchedulingShardConfig) *kaiv1.PlacementStrategy {
	if cfg == nil {
		return nil
	}
	return cfg.PlacementStrategy
}

func getMinRuntime(cfg *v1alpha1.SchedulingShardConfig) *kaiv1.MinRuntime {
	if cfg == nil {
		return nil
	}
	return cfg.MinRuntime
}

func getPlugins(cfg *v1alpha1.SchedulingShardConfig) map[string]kaiv1.PluginConfig {
	if cfg == nil {
		return nil
	}
	return cfg.Plugins
}

func getActions(cfg *v1alpha1.SchedulingShardConfig) map[string]kaiv1.ActionConfig {
	if cfg == nil {
		return nil
	}
	return cfg.Actions
}

func getQueueDepthPerAction(cfg *v1alpha1.SchedulingShardConfig) map[string]int {
	if cfg == nil {
		return nil
	}
	return cfg.QueueDepthPerAction
}

func SchedulingShardStatus(
	ctx context.Context, k8sReader client.Reader, nodePool *v1alpha1.NodePool,
	_ *common.NodePoolControllerParams, _ string,
) (operands.Status, error) {
	shard, err := ResolveShardForNodePool(ctx, k8sReader, nodePool)
	if err != nil {
		return operands.NotReadyStatus("scheduling shard is not deployed"), err
	}
	if shard == nil {
		shard = &kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{Name: nodePool.Name}}
	}

	var deployed, available bool
	message := ""
	for _, condition := range shard.Status.Conditions {
		switch condition.Type {
		case string(kaiv1.ConditionTypeDeployed):
			deployed = condition.Status == metav1.ConditionTrue
			if !deployed {
				message = condition.Message
			}
		case string(kaiv1.ConditionTypeAvailable):
			available = condition.Status == metav1.ConditionTrue
			if !available && message == "" {
				message = condition.Message
			}
		}
	}
	if deployed && available {
		return operands.ReadyStatus(), nil
	}
	if message == "" {
		message = "no status message available"
	}
	return operands.NotReadyStatus(fmt.Sprintf("scheduler [%s] is not running yet: %s", shard.Name, message)), nil
}

func PartitionLabelValueForNodePool(nodePoolName string) string {
	if nodePoolName == config.Get().DefaultNodepoolName {
		return ""
	}
	return nodePoolName
}

func NodePoolNameForPartitionLabelValue(partitionLabelValue string) string {
	if partitionLabelValue == "" {
		return config.Get().DefaultNodepoolName
	}
	return partitionLabelValue
}

func timeBasedFairShare(nodePool *v1alpha1.NodePool) *v1alpha1.TimeBasedFairShare {
	cfg := nodePool.Spec.SchedulingShardConfig
	if cfg == nil || cfg.TimeBasedFairShare == nil || !ptr.Deref(cfg.TimeBasedFairShare.Enabled, false) {
		return nil
	}
	return cfg.TimeBasedFairShare
}

func getKValue(nodePool *v1alpha1.NodePool) *float64 {
	tbfs := timeBasedFairShare(nodePool)
	if tbfs == nil {
		return nil
	}
	value := float64(ptr.Deref(tbfs.HistoricalUsageWeight, defaultTimeBasedFairShareHistoricalUsageWeight))
	return &value
}

func getTimeBasedFairShareFields(nodePool *v1alpha1.NodePool) *usagedbapi.UsageDBConfig {
	tbfs := timeBasedFairShare(nodePool)
	if tbfs == nil {
		return nil
	}

	var windowSize, windowType *string
	var tumblingStart *metav1.Time
	var cronString string
	if window := tbfs.Window; window != nil {
		windowSize = window.Size
		windowType = window.Type
		tumblingStart = window.TumblingStartTime
		cronString = ptr.Deref(window.CronString, "")
	}

	usageParams := &usagedbapi.UsageParams{
		HalfLifePeriod: parseDurationParam(tbfs.HalfLifePeriod, defaultTimeBasedFairShareDecayHalfLife, "HalfLifePeriod", nodePool.Name),
		WindowSize:     parseMonitoringDurationParam(windowSize, defaultTimeBasedFairShareWindowDuration, "Window.Size", nodePool.Name),
		WindowType:     ptr.To(defaultTimeBasedFairShareWindowType),
		CronString:     cronString,
	}
	if windowType != nil {
		if wt := usagedbapi.WindowType(*windowType); wt.IsValid() {
			usageParams.WindowType = ptr.To(wt)
		}
	}
	if tumblingStart != nil {
		usageParams.TumblingWindowStartTime = ptr.To(*tumblingStart)
	}
	if sampling := tbfs.Sampling; sampling != nil {
		usageParams.FetchInterval = parseOptionalDurationParam(sampling.FetchInterval, "Sampling.FetchInterval", nodePool.Name)
		usageParams.StalenessPeriod = parseOptionalDurationParam(sampling.StalenessPeriod, "Sampling.StalenessPeriod", nodePool.Name)
		usageParams.WaitTimeout = parseOptionalDurationParam(sampling.WaitTimeout, "Sampling.WaitTimeout", nodePool.Name)
	}

	result := &usagedbapi.UsageDBConfig{ClientType: "prometheus", UsageParams: usageParams}
	overrideCapacityMetricsIfNeeded(result.UsageParams, nodePool.Name)
	return result
}

func parseOptionalDurationParam(value *string, field, nodePool string) *metav1.Duration {
	if value == nil {
		return nil
	}
	if duration, err := str2duration.ParseDuration(*value); err == nil {
		return &metav1.Duration{Duration: duration}
	}
	log.Warn().Msgf("failed to parse TimeBasedFairShare.%s '%s' for NodePool '%s'", field, *value, nodePool)
	return nil
}

func parseDurationParam(value *string, fallback time.Duration, field, nodePool string) *metav1.Duration {
	if parsed := parseOptionalDurationParam(value, field, nodePool); parsed != nil {
		return parsed
	}
	return &metav1.Duration{Duration: fallback}
}

func parseMonitoringDurationParam(value *string, fallback time.Duration, field, nodePool string) *monitoringv1.Duration {
	if value != nil {
		if _, err := str2duration.ParseDuration(*value); err == nil {
			return ptr.To(monitoringv1.Duration(*value))
		}
		log.Warn().Msgf("failed to parse TimeBasedFairShare.%s '%s' for NodePool '%s'", field, *value, nodePool)
	}
	return ptr.To(monitoringv1.Duration(fallback.String()))
}

func overrideCapacityMetricsIfNeeded(params *usagedbapi.UsageParams, nodePoolName string) {
	if params.ExtraParams == nil {
		params.ExtraParams = map[string]string{}
	}
	prefix := config.Get().MetricsNamespace
	setDefaultExtraParam(params, "gpuAllocationMetric", fmt.Sprintf("%s_queue_allocated_gpus", prefix))
	setDefaultExtraParam(params, "cpuAllocationMetric", fmt.Sprintf("%s_queue_allocated_cpu_cores", prefix))
	setDefaultExtraParam(params, "memoryAllocationMetric", fmt.Sprintf("%s_queue_allocated_memory_bytes", prefix))
	setDefaultExtraParam(params, "gpuCapacityMetric", fmt.Sprintf("resources_per_nodepool{resource=\"nvidia_com_gpu\", nodepool=\"%s\"}", nodePoolName))
	setDefaultExtraParam(params, "cpuCapacityMetric", fmt.Sprintf("resources_per_nodepool{resource=\"cpu\", nodepool=\"%s\"}", nodePoolName))
	setDefaultExtraParam(params, "memoryCapacityMetric", fmt.Sprintf("resources_per_nodepool{resource=\"memory\", nodepool=\"%s\"}", nodePoolName))
}

func setDefaultExtraParam(params *usagedbapi.UsageParams, key, value string) {
	if params.ExtraParams[key] == "" {
		params.ExtraParams[key] = value
	}
}
