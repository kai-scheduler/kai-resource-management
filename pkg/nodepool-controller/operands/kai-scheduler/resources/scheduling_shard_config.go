// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"fmt"
	"maps"
	"strconv"
	"time"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/xhit/go-str2duration/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// kaiDefaultUsageQueries are the queries KAI Scheduler's Prometheus usage client falls back
// to. SchedulingShardForNodePool fills in per-node-pool queries for keys a node pool leaves
// unset, so a shard that relied on these needs them written out to keep reading the same data.
var kaiDefaultUsageQueries = map[string]string{
	"gpuAllocationMetric":    "kai_queue_allocated_gpus",
	"cpuAllocationMetric":    "kai_queue_allocated_cpu_cores",
	"memoryAllocationMetric": "kai_queue_allocated_memory_bytes",
	"gpuCapacityMetric":      `sum(kube_node_status_capacity{resource="nvidia_com_gpu"})`,
	"cpuCapacityMetric":      `sum(kube_node_status_capacity{resource="cpu"})`,
	"memoryCapacityMetric":   `sum(kube_node_status_capacity{resource="memory"})`,
}

// SchedulingShardConfigFromShard is the reverse of SchedulingShardForNodePool: the node pool
// configuration that SchedulingShardForNodePool turns back into the shard's scheduling settings.
func SchedulingShardConfigFromShard(shard *kaiv1.SchedulingShard) (*v1alpha1.SchedulingShardConfig, error) {
	spec := shard.Spec.DeepCopy()
	timeBasedFairShare, err := timeBasedFairShareFromShard(shard.Name, spec)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.SchedulingShardConfig{
		Args:                spec.Args,
		PlacementStrategy:   spec.PlacementStrategy,
		QueueDepthPerAction: spec.QueueDepthPerAction,
		MinRuntime:          spec.MinRuntime,
		Plugins:             spec.Plugins,
		Actions:             spec.Actions,
		TimeBasedFairShare:  timeBasedFairShare,
	}, nil
}

// timeBasedFairShareFromShard maps a shard's usage DB back to the node pool field it is
// compiled from. A kValue without a usage DB is dropped: with no usage to weigh, KAI's share
// weight w+k*(w-0) scales every queue alike and normalising cancels k out. The client type
// and the connection-string env var are not carried over: prometheus is the only production
// client, and nothing on a shard can set the scheduler's env.
func timeBasedFairShareFromShard(shardName string, spec *kaiv1.SchedulingShardSpec) (*v1alpha1.TimeBasedFairShare, error) {
	usageDB := spec.UsageDBConfig
	if usageDB == nil {
		return nil, nil
	}

	fairShare := &v1alpha1.TimeBasedFairShare{Enabled: ptr.To(true)}
	if spec.KValue != nil {
		// KAI treats a negative kValue as 0, which the node pool's minimum also requires.
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
	window := &v1alpha1.FairShareWindow{}
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
	if *window != (v1alpha1.FairShareWindow{}) {
		fairShare.Window = window
	}

	sampling := &v1alpha1.FairShareSampling{
		FetchInterval:   goDuration(params.FetchInterval),
		StalenessPeriod: goDuration(params.StalenessPeriod),
		WaitTimeout:     goDuration(params.WaitTimeout),
	}
	if *sampling != (v1alpha1.FairShareSampling{}) {
		fairShare.Sampling = sampling
	}
	return fairShare, nil
}

func withKAIDefaultUsageQueries(extraParams map[string]string) map[string]string {
	params := maps.Clone(kaiDefaultUsageQueries)
	maps.Copy(params, extraParams)
	return params
}

// nodePoolDuration formats a duration in the units the node pool's half-life and window size
// accept: days, hours and minutes.
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
