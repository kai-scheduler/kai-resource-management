// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"time"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	usagedbapi "github.com/kai-scheduler/api/usagedb"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/operands/kai-scheduler/resources"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

var _ = Describe("MigrateShards", func() {
	const labelKey = "kai.scheduler/node-pool"

	var (
		ctx  context.Context
		opts MigrateShardsOptions
	)

	BeforeEach(func() {
		ctx = context.Background()
		opts = MigrateShardsOptions{NodePoolLabelKey: labelKey, DefaultNodePoolName: "default"}
	})

	newClient := func(objects ...client.Object) client.Client {
		scheme := runtime.NewScheme()
		utilruntime.Must(kaiv1.AddToScheme(scheme))
		utilruntime.Must(kaires.AddToScheme(scheme))
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	}
	shard := func(name, partition string) *kaiv1.SchedulingShard {
		return &kaiv1.SchedulingShard{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       kaiv1.SchedulingShardSpec{PartitionLabelValue: partition},
		}
	}
	getShard := func(k8sClient client.Client, name string) *kaiv1.SchedulingShard {
		s := &kaiv1.SchedulingShard{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, s)).To(Succeed())
		return s
	}
	getNodePool := func(k8sClient client.Client, name string) *kaires.NodePool {
		nodePool := &kaires.NodePool{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, nodePool)).To(Succeed())
		return nodePool
	}
	nodePoolCount := func(k8sClient client.Client) int {
		nodePools := &kaires.NodePoolList{}
		Expect(k8sClient.List(ctx, nodePools)).To(Succeed())
		return len(nodePools.Items)
	}
	kaiConfig := func(nodePoolLabelKey string) *kaiv1.Config {
		return &kaiv1.Config{
			ObjectMeta: metav1.ObjectMeta{Name: "kai-config"},
			Spec:       kaiv1.ConfigSpec{Global: &kaiv1.GlobalConfig{NodePoolLabelKey: ptr.To(nodePoolLabelKey)}},
		}
	}

	It("does nothing when there are no shards", func() {
		k8sClient := newClient()

		Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())
		Expect(nodePoolCount(k8sClient)).To(BeZero())
	})

	Context("a shard serving a partition", func() {
		It("gets a NodePool selecting that partition's nodes, which owns it", func() {
			k8sClient := newClient(shard("team-a-shard", "team-a"))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			nodePool := getNodePool(k8sClient, "team-a")
			Expect(nodePool.Spec.LabelKey).To(Equal(labelKey))
			Expect(nodePool.Spec.LabelValue).To(Equal("team-a"))
			Expect(nodePool.Labels).NotTo(HaveKey("app.kubernetes.io/managed-by"))

			migrated := getShard(k8sClient, "team-a-shard")
			Expect(unmanaged_shards.OwningNodePoolName(migrated)).To(Equal("team-a"))
			Expect(migrated.Labels).To(HaveKeyWithValue(AdoptionLabelKey, AdoptionLabelValue))
		})

		It("keeps the shard's args as the admin's", func() {
			s := shard("team-a", "team-a")
			s.Spec.Args = map[string]string{"verbosity": "5"}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			migrated := getShard(k8sClient, "team-a")
			Expect(migrated.Spec.Args).To(Equal(map[string]string{"verbosity": "5"}))
			Expect(migrated.Annotations).To(HaveKeyWithValue(resources.ManagedShardArgsAnnotation, ""))
			Expect(getNodePool(k8sClient, "team-a").Spec.SchedulingShardConfig).To(BeNil())
		})

		It("is skipped when an admin excluded it from KRM", func() {
			s := shard("team-a", "team-a")
			s.Labels = map[string]string{unmanaged_shards.IgnoreShardLabelKey: "true"}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(nodePoolCount(k8sClient)).To(BeZero())
			Expect(metav1.GetControllerOf(getShard(k8sClient, "team-a"))).To(BeNil())
		})

		// Such as a NodePool kind of an older installer, whose own upgrade hook re-parents
		// its shards after this one runs.
		It("is skipped when something else already controls it", func() {
			s := shard("team-a", "team-a")
			s.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "run.ai/v1alpha1", Kind: "NodePool", Name: "team-a", UID: "legacy-uid",
				Controller: ptr.To(true),
			}}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(nodePoolCount(k8sClient)).To(BeZero())
			Expect(metav1.GetControllerOf(getShard(k8sClient, "team-a")).APIVersion).To(Equal("run.ai/v1alpha1"))
		})
	})

	Context("the default shard", func() {
		It("gets the default NodePool, selecting no label, and loses its ignore label", func() {
			s := shard("default", "")
			s.Labels = map[string]string{unmanaged_shards.IgnoreShardLabelKey: "true"}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			nodePool := getNodePool(k8sClient, "default")
			Expect(nodePool.Spec.LabelKey).To(BeEmpty())
			Expect(nodePool.Spec.LabelValue).To(BeEmpty())
			migrated := getShard(k8sClient, "default")
			Expect(unmanaged_shards.OwningNodePoolName(migrated)).To(Equal("default"))
			Expect(migrated.Labels).NotTo(HaveKey(unmanaged_shards.IgnoreShardLabelKey))
		})

		It("marks a default NodePool it creates as the release's, so Helm adopts it", func() {
			opts.ReleaseName = "krm"
			opts.ReleaseNamespace = "kai-resource-management"
			k8sClient := newClient(shard("default", ""))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			nodePool := getNodePool(k8sClient, "default")
			Expect(nodePool.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "Helm"))
			Expect(nodePool.Annotations).To(And(
				HaveKeyWithValue("meta.helm.sh/release-name", "krm"),
				HaveKeyWithValue("meta.helm.sh/release-namespace", "kai-resource-management"),
				HaveKeyWithValue("helm.sh/resource-policy", "keep"),
			))
		})

		It("uses the configured default NodePool name", func() {
			opts.DefaultNodePoolName = "catch-all"
			k8sClient := newClient(shard("default", ""))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(unmanaged_shards.OwningNodePoolName(getShard(k8sClient, "default"))).To(Equal("catch-all"))
		})
	})

	Context("settings", func() {
		It("copies every setting a NodePool models", func() {
			s := shard("team-a", "team-a")
			s.Spec.PlacementStrategy = &kaiv1.PlacementStrategy{GPU: ptr.To("spread")}
			s.Spec.QueueDepthPerAction = map[string]int{"allocate": 50}
			s.Spec.MinRuntime = &kaiv1.MinRuntime{PreemptMinRuntime: ptr.To("5m")}
			s.Spec.Plugins = map[string]kaiv1.PluginConfig{"gpuspread": {Enabled: ptr.To(true)}}
			s.Spec.Actions = map[string]kaiv1.ActionConfig{"reclaim": {Enabled: ptr.To(false)}}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			config := getNodePool(k8sClient, "team-a").Spec.SchedulingShardConfig
			Expect(config.PlacementStrategy).To(Equal(s.Spec.PlacementStrategy))
			Expect(config.QueueDepthPerAction).To(Equal(s.Spec.QueueDepthPerAction))
			Expect(config.MinRuntime).To(Equal(s.Spec.MinRuntime))
			Expect(config.Plugins).To(Equal(s.Spec.Plugins))
			Expect(config.Actions).To(Equal(s.Spec.Actions))
			Expect(config.TimeBasedFairShare).To(BeNil())
		})

		It("leaves settings a NodePool does not model on the shard", func() {
			s := shard("team-a", "team-a")
			s.Spec.GoMemLimitRatio = ptr.To(0.8)
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getShard(k8sClient, "team-a").Spec.GoMemLimitRatio).To(Equal(ptr.To(0.8)))
		})

		It("maps the usage DB back to time-based fair share", func() {
			start := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			s := shard("team-a", "team-a")
			s.Spec.KValue = ptr.To(0.5)
			s.Spec.UsageDBConfig = &usagedbapi.UsageDBConfig{
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
			}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			fairShare := getNodePool(k8sClient, "team-a").Spec.SchedulingShardConfig.TimeBasedFairShare
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
			s := shard("team-a", "team-a")
			s.Spec.UsageDBConfig = &usagedbapi.UsageDBConfig{
				ClientType: "prometheus",
				UsageParams: &usagedbapi.UsageParams{
					ExtraParams: map[string]string{"gpuCapacityMetric": "my_gpu_capacity"},
				},
			}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			expected := map[string]string{}
			for key, query := range kaiDefaultUsageQueries {
				expected[key] = query
			}
			expected["gpuCapacityMetric"] = "my_gpu_capacity"
			Expect(getNodePool(k8sClient, "team-a").Spec.SchedulingShardConfig.TimeBasedFairShare.ExtraParams).
				To(Equal(expected))
		})

		It("drops a kValue that has no usage DB to weigh", func() {
			s := shard("team-a", "team-a")
			s.Spec.KValue = ptr.To(0.8)
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getNodePool(k8sClient, "team-a").Spec.SchedulingShardConfig).To(BeNil())
		})
	})

	Context("reruns", func() {
		It("skips shards a NodePool already owns", func() {
			k8sClient := newClient(shard("team-a", "team-a"))
			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())
			before := getShard(k8sClient, "team-a")

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getShard(k8sClient, "team-a").ResourceVersion).To(Equal(before.ResourceVersion))
			Expect(nodePoolCount(k8sClient)).To(Equal(1))
		})

		It("finishes a run that stopped after creating the NodePool", func() {
			s := shard("team-a", "team-a")
			s.Spec.QueueDepthPerAction = map[string]int{"allocate": 50}
			created := &kaires.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "team-a"},
				Spec: kaires.NodePoolSpec{
					LabelKey:   labelKey,
					LabelValue: "team-a",
					SchedulingShardConfig: &kaires.SchedulingShardConfig{
						QueueDepthPerAction: map[string]int{"allocate": 50},
					},
				},
			}
			k8sClient := newClient(s, created)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(unmanaged_shards.OwningNodePoolName(getShard(k8sClient, "team-a"))).To(Equal("team-a"))
		})
	})

	Context("failures, which change nothing", func() {
		expectNothingChanged := func(k8sClient client.Client, shardNames ...string) {
			for _, name := range shardNames {
				Expect(metav1.GetControllerOf(getShard(k8sClient, name))).To(BeNil())
			}
		}

		It("fails when KAI reads node pools from another label", func() {
			k8sClient := newClient(shard("team-a", "team-a"), kaiConfig("example.com/pool"))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(And(
				ContainSubstring(`"example.com/pool"`),
				ContainSubstring("kai-scheduler.global.nodePoolLabelKey=example.com/pool"))))
			Expect(nodePoolCount(k8sClient)).To(BeZero())
		})

		It("accepts KAI's label when it matches", func() {
			k8sClient := newClient(shard("team-a", "team-a"), kaiConfig(labelKey))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())
		})

		DescribeTable("fails on a partition that cannot be a NodePool name",
			func(partition string) {
				k8sClient := newClient(shard("team-a", partition))

				Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(ContainSubstring("partition")))
				expectNothingChanged(k8sClient, "team-a")
			},
			Entry("uppercase and underscore", "Team_A"),
			Entry("the default NodePool's name", "default"),
		)

		It("fails on an arg KRM overwrites with another value", func() {
			s := shard("team-a", "team-a")
			s.Spec.Args = map[string]string{
				"gpu-worker-node-label-key": "example.com/gpu",
				"verbosity":                 "5",
			}
			opts.SchedulingShardArgs = map[string]string{"verbosity": "3"}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(And(
				ContainSubstring(`gpu-worker-node-label-key="example.com/gpu"`),
				ContainSubstring(`verbosity="5" (KRM sets "3")`))))
			expectNothingChanged(k8sClient, "team-a")
		})

		It("accepts an arg KRM sets to the same value", func() {
			s := shard("team-a", "team-a")
			s.Spec.Args = map[string]string{"gpu-worker-node-label-key": "node-role.kubernetes.io/gpu-worker"}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())
		})

		It("fails when a NodePool of that name selects other nodes", func() {
			other := &kaires.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "team-a"},
				Spec:       kaires.NodePoolSpec{LabelKey: labelKey, LabelValue: "team-b"},
			}
			k8sClient := newClient(shard("team-a", "team-a"), other)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(ContainSubstring("already exists")))
			expectNothingChanged(k8sClient, "team-a")
		})

		It("fails, never overwriting the shard's settings, when the NodePool exists with other settings", func() {
			s := shard("team-a", "team-a")
			s.Spec.QueueDepthPerAction = map[string]int{"allocate": 50}
			existing := &kaires.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "team-a"},
				Spec: kaires.NodePoolSpec{
					LabelKey:   labelKey,
					LabelValue: "team-a",
					SchedulingShardConfig: &kaires.SchedulingShardConfig{
						QueueDepthPerAction: map[string]int{"allocate": 10},
					},
				},
			}
			k8sClient := newClient(s, existing)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(ContainSubstring("different settings")))
			expectNothingChanged(k8sClient, "team-a")
			Expect(getNodePool(k8sClient, "team-a").Spec.SchedulingShardConfig.QueueDepthPerAction).
				To(Equal(map[string]int{"allocate": 10}))
		})

		It("fails when two shards serve the same partition", func() {
			k8sClient := newClient(shard("team-a-1", "team-a"), shard("team-a-2", "team-a"))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(ContainSubstring("[team-a-1 team-a-2]")))
			expectNothingChanged(k8sClient, "team-a-1", "team-a-2")
		})

		It("migrates no shard when any one of them fails", func() {
			k8sClient := newClient(shard("good", "good"), shard("bad", "Bad_Name"))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(HaveOccurred())
			expectNothingChanged(k8sClient, "good", "bad")
			Expect(nodePoolCount(k8sClient)).To(BeZero())
		})
	})
})
