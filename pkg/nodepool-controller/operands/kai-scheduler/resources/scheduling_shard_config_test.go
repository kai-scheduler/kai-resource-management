// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"time"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	usagedbapi "github.com/kai-scheduler/api/usagedb"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
)

var _ = Describe("SchedulingShardConfigFromShard", func() {
	shardWithSpec := func(spec kaiv1.SchedulingShardSpec) *kaiv1.SchedulingShard {
		return &kaiv1.SchedulingShard{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}, Spec: spec}
	}

	It("copies every setting a node pool models", func() {
		spec := kaiv1.SchedulingShardSpec{
			Args:                map[string]string{"verbosity": "4"},
			PlacementStrategy:   &kaiv1.PlacementStrategy{GPU: ptr.To("spread")},
			QueueDepthPerAction: map[string]int{"allocate": 50},
			MinRuntime:          &kaiv1.MinRuntime{PreemptMinRuntime: ptr.To("5m")},
			Plugins:             map[string]kaiv1.PluginConfig{"gpuspread": {Enabled: ptr.To(true)}},
			Actions:             map[string]kaiv1.ActionConfig{"reclaim": {Enabled: ptr.To(false)}},
		}

		config, err := SchedulingShardConfigFromShard(shardWithSpec(spec))

		Expect(err).NotTo(HaveOccurred())
		Expect(config).To(Equal(&v1alpha1.SchedulingShardConfig{
			Args:                spec.Args,
			PlacementStrategy:   spec.PlacementStrategy,
			QueueDepthPerAction: spec.QueueDepthPerAction,
			MinRuntime:          spec.MinRuntime,
			Plugins:             spec.Plugins,
			Actions:             spec.Actions,
		}))
	})

	It("maps the usage DB back to time-based fair share", func() {
		start := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		config, err := SchedulingShardConfigFromShard(shardWithSpec(kaiv1.SchedulingShardSpec{
			KValue: ptr.To(0.5),
			UsageDBConfig: &usagedbapi.UsageDBConfig{
				ClientType:       "prometheus",
				ConnectionString: "http://team-a-prometheus:9090",
				UsageParams: &usagedbapi.UsageParams{
					HalfLifePeriod:          &metav1.Duration{Duration: 90 * time.Minute},
					WindowSize:              ptr.To(monitoringv1.Duration("1w")),
					WindowType:              ptr.To(usagedbapi.TumblingWindow),
					TumblingWindowStartTime: &start,
					FetchInterval:           &metav1.Duration{Duration: 30 * time.Second},
					ExtraParams:             map[string]string{"usageQueryTimeout": "30s"},
				},
			},
		}))

		Expect(err).NotTo(HaveOccurred())
		fairShare := config.TimeBasedFairShare
		Expect(fairShare.Enabled).To(Equal(ptr.To(true)))
		Expect(fairShare.HistoricalUsageWeight).To(Equal(ptr.To(float32(0.5))))
		Expect(fairShare.ConnectionString).To(Equal(ptr.To("http://team-a-prometheus:9090")))
		Expect(fairShare.HalfLifePeriod).To(Equal(ptr.To("90m")))
		Expect(fairShare.Window.Size).To(Equal(ptr.To("7d")))
		Expect(fairShare.Window.Type).To(Equal(ptr.To("tumbling")))
		Expect(fairShare.Window.TumblingStartTime.Time).To(BeTemporally("==", start.Time))
		Expect(fairShare.Sampling.FetchInterval).To(Equal(ptr.To("30s")))
		Expect(fairShare.ExtraParams).To(HaveKeyWithValue("usageQueryTimeout", "30s"))
	})

	It("writes KAI's default usage queries for keys the shard left unset", func() {
		config, err := SchedulingShardConfigFromShard(shardWithSpec(kaiv1.SchedulingShardSpec{
			UsageDBConfig: &usagedbapi.UsageDBConfig{
				ClientType: "prometheus",
				UsageParams: &usagedbapi.UsageParams{
					ExtraParams: map[string]string{"gpuCapacityMetric": "my_gpu_capacity"},
				},
			},
		}))

		Expect(err).NotTo(HaveOccurred())
		expected := withKAIDefaultUsageQueries(nil)
		expected["gpuCapacityMetric"] = "my_gpu_capacity"
		Expect(config.TimeBasedFairShare.ExtraParams).To(Equal(expected))
	})

	It("drops a kValue that has no usage DB to weigh", func() {
		config, err := SchedulingShardConfigFromShard(shardWithSpec(kaiv1.SchedulingShardSpec{KValue: ptr.To(0.8)}))

		Expect(err).NotTo(HaveOccurred())
		Expect(config.TimeBasedFairShare).To(BeNil())
	})

	It("fails on a window size it cannot read", func() {
		_, err := SchedulingShardConfigFromShard(shardWithSpec(kaiv1.SchedulingShardSpec{
			UsageDBConfig: &usagedbapi.UsageDBConfig{
				ClientType:  "prometheus",
				UsageParams: &usagedbapi.UsageParams{WindowSize: ptr.To(monitoringv1.Duration("soon"))},
			},
		}))

		Expect(err).To(MatchError(ContainSubstring(`"soon"`)))
	})

	// The two directions must stay inverse: a shard migrated into a node pool has to come
	// back from SchedulingShardForNodePool scheduling the same way.
	It("is turned back into the shard's scheduling settings by SchedulingShardForNodePool", func() {
		start := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		shard := shardWithSpec(kaiv1.SchedulingShardSpec{
			PartitionLabelValue: "team-a",
			Args:                map[string]string{"verbosity": "4"},
			PlacementStrategy:   &kaiv1.PlacementStrategy{GPU: ptr.To("spread"), CPU: ptr.To("binpack")},
			QueueDepthPerAction: map[string]int{"allocate": 50},
			MinRuntime:          &kaiv1.MinRuntime{PreemptMinRuntime: ptr.To("5m")},
			Plugins:             map[string]kaiv1.PluginConfig{"gpuspread": {Enabled: ptr.To(true)}},
			Actions:             map[string]kaiv1.ActionConfig{"reclaim": {Enabled: ptr.To(false)}},
			KValue:              ptr.To(0.5),
			UsageDBConfig: &usagedbapi.UsageDBConfig{
				ClientType:       "prometheus",
				ConnectionString: "http://team-a-prometheus:9090",
				UsageParams: &usagedbapi.UsageParams{
					HalfLifePeriod:          &metav1.Duration{Duration: 90 * time.Minute},
					WindowSize:              ptr.To(monitoringv1.Duration("3d")),
					WindowType:              ptr.To(usagedbapi.TumblingWindow),
					TumblingWindowStartTime: &start,
					FetchInterval:           &metav1.Duration{Duration: 30 * time.Second},
					StalenessPeriod:         &metav1.Duration{Duration: 10 * time.Minute},
					WaitTimeout:             &metav1.Duration{Duration: 2 * time.Minute},
					ExtraParams:             map[string]string{"gpuCapacityMetric": "my_gpu_capacity"},
				},
			},
		})

		config, err := SchedulingShardConfigFromShard(shard)
		Expect(err).NotTo(HaveOccurred())
		scheme := runtime.NewScheme()
		Expect(kaiv1.AddToScheme(scheme)).To(Succeed())
		nodePool := &v1alpha1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Name: "team-a"},
			Spec:       v1alpha1.NodePoolSpec{SchedulingShardConfig: config},
		}
		obj, err := SchedulingShardForNodePool(context.Background(), fakeIndexersClient(scheme), nodePool,
			&common.NodePoolControllerParams{}, nodePool.Name)
		Expect(err).NotTo(HaveOccurred())
		roundTrip := obj.(*kaiv1.SchedulingShard).Spec

		want := shard.Spec
		Expect(roundTrip.Args).To(HaveKeyWithValue("verbosity", "4"))
		Expect(roundTrip.PartitionLabelValue).To(Equal(want.PartitionLabelValue))
		Expect(roundTrip.PlacementStrategy).To(Equal(want.PlacementStrategy))
		Expect(roundTrip.QueueDepthPerAction).To(Equal(want.QueueDepthPerAction))
		Expect(roundTrip.MinRuntime).To(Equal(want.MinRuntime))
		Expect(roundTrip.Plugins).To(Equal(want.Plugins))
		Expect(roundTrip.Actions).To(Equal(want.Actions))
		Expect(roundTrip.KValue).To(Equal(want.KValue))

		got, wantDB := roundTrip.UsageDBConfig, want.UsageDBConfig
		Expect(got.ClientType).To(Equal(wantDB.ClientType))
		Expect(got.ConnectionString).To(Equal(wantDB.ConnectionString))
		gotParams, wantParams := got.UsageParams, wantDB.UsageParams
		Expect(gotParams.HalfLifePeriod).To(Equal(wantParams.HalfLifePeriod))
		Expect(gotParams.WindowSize).To(Equal(wantParams.WindowSize))
		Expect(gotParams.WindowType).To(Equal(wantParams.WindowType))
		Expect(gotParams.TumblingWindowStartTime.Time).To(BeTemporally("==", start.Time))
		Expect(gotParams.FetchInterval).To(Equal(wantParams.FetchInterval))
		Expect(gotParams.StalenessPeriod).To(Equal(wantParams.StalenessPeriod))
		Expect(gotParams.WaitTimeout).To(Equal(wantParams.WaitTimeout))
		// KAI's defaults written out for the keys the shard left unset read the same data.
		Expect(gotParams.ExtraParams).To(Equal(withKAIDefaultUsageQueries(wantParams.ExtraParams)))
	})
})
