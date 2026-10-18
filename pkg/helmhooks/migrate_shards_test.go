// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

var _ = Describe("MigrateShards", func() {
	const partitionLabelKey = "kai.scheduler/node-pool"

	var (
		ctx  context.Context
		opts MigrateShardsOptions
	)

	BeforeEach(func() {
		ctx = context.Background()
		opts = MigrateShardsOptions{PartitionLabelKey: partitionLabelKey, DefaultNodePoolName: "default"}
	})

	newClientWith := func(funcs interceptor.Funcs, objects ...client.Object) client.Client {
		scheme := runtime.NewScheme()
		utilruntime.Must(kaiv1.AddToScheme(scheme))
		utilruntime.Must(kaires.AddToScheme(scheme))
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithInterceptorFuncs(funcs).Build()
	}
	newClient := func(objects ...client.Object) client.Client {
		return newClientWith(interceptor.Funcs{}, objects...)
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
			Expect(nodePool.Spec.LabelKey).To(Equal(partitionLabelKey))
			Expect(nodePool.Spec.LabelValue).To(Equal("team-a"))
			Expect(nodePool.Labels).NotTo(HaveKey("app.kubernetes.io/managed-by"))

			migrated := getShard(k8sClient, "team-a-shard")
			Expect(unmanaged_shards.OwningNodePoolName(migrated)).To(Equal("team-a"))
			Expect(migrated.Labels).To(HaveKeyWithValue(AdoptionLabelKey, AdoptionLabelValue))
		})

		It("moves the shard's args into its NodePool", func() {
			s := shard("team-a", "team-a")
			s.Spec.Args = map[string]string{"verbosity": "5"}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getNodePool(k8sClient, "team-a").Spec.SchedulingShardConfig.Args).
				To(Equal(map[string]string{"verbosity": "5"}))
			Expect(getShard(k8sClient, "team-a").Spec.Args).To(Equal(map[string]string{"verbosity": "5"}))
		})

		It("updates a NodePool that already selects its partition to the shard's settings", func() {
			s := shard("team-a", "team-a")
			s.Spec.QueueDepthPerAction = map[string]int{"allocate": 50}
			existing := &kaires.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "team-a"},
				Spec: kaires.NodePoolSpec{
					LabelKey:   partitionLabelKey,
					LabelValue: "team-a",
					SchedulingShardConfig: &kaires.SchedulingShardConfig{
						QueueDepthPerAction: map[string]int{"allocate": 10},
					},
				},
			}
			k8sClient := newClient(s, existing)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getNodePool(k8sClient, "team-a").Spec.SchedulingShardConfig).To(Equal(
				&kaires.SchedulingShardConfig{QueueDepthPerAction: map[string]int{"allocate": 50}}))
			Expect(unmanaged_shards.OwningNodePoolName(getShard(k8sClient, "team-a"))).To(Equal("team-a"))
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

	Context("the default NodePool", func() {
		// Beside an existing KAI Scheduler the chart leaves the default NodePool to the hook.
		BeforeEach(func() {
			opts.EnsureDefaultNodePool = true
			opts.DefaultNodePoolSpec = kaires.NodePoolSpec{
				SchedulingShardConfig: &kaires.SchedulingShardConfig{QueueDepthPerAction: map[string]int{"allocate": 10}},
			}
		})

		It("is built from the default shard, which it owns, selecting no label", func() {
			s := shard("default", "")
			s.Labels = map[string]string{unmanaged_shards.IgnoreShardLabelKey: "true"}
			s.Spec.PlacementStrategy = &kaiv1.PlacementStrategy{GPU: ptr.To("binpack")}
			k8sClient := newClient(s)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			nodePool := getNodePool(k8sClient, "default")
			Expect(nodePool.Spec.LabelKey).To(BeEmpty())
			Expect(nodePool.Spec.LabelValue).To(BeEmpty())
			Expect(nodePool.Spec.SchedulingShardConfig.PlacementStrategy).To(Equal(s.Spec.PlacementStrategy))
			Expect(nodePool.Labels).NotTo(HaveKey("app.kubernetes.io/managed-by"))
			migrated := getShard(k8sClient, "default")
			Expect(unmanaged_shards.OwningNodePoolName(migrated)).To(Equal("default"))
			Expect(migrated.Labels).NotTo(HaveKey(unmanaged_shards.IgnoreShardLabelKey))
		})

		It("is built from the chart's spec when there is no default shard", func() {
			k8sClient := newClient(shard("team-a", "team-a"))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getNodePool(k8sClient, "default").Spec).To(Equal(opts.DefaultNodePoolSpec))
		})

		It("is created even when there are no shards at all", func() {
			k8sClient := newClient()

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getNodePool(k8sClient, "default").Spec).To(Equal(opts.DefaultNodePoolSpec))
		})

		It("is updated to the default shard's settings when it already exists", func() {
			s := shard("default", "")
			s.Spec.PlacementStrategy = &kaiv1.PlacementStrategy{GPU: ptr.To("binpack")}
			existing := &kaires.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			k8sClient := newClient(s, existing)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getNodePool(k8sClient, "default").Spec.SchedulingShardConfig.PlacementStrategy).
				To(Equal(s.Spec.PlacementStrategy))
			Expect(unmanaged_shards.OwningNodePoolName(getShard(k8sClient, "default"))).To(Equal("default"))
		})

		It("is left as it is when it already exists and there is no default shard", func() {
			existing := &kaires.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
				Spec: kaires.NodePoolSpec{
					SchedulingShardConfig: &kaires.SchedulingShardConfig{QueueDepthPerAction: map[string]int{"allocate": 99}},
				},
			}
			k8sClient := newClient(existing)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(getNodePool(k8sClient, "default").Spec.SchedulingShardConfig.QueueDepthPerAction).
				To(Equal(map[string]int{"allocate": 99}))
		})

		// On an upgrade the NodePool webhook refuses a duplicate before the API server can
		// answer that it exists.
		It("does not try to create a default NodePool that exists", func() {
			existing := &kaires.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			k8sClient := newClientWith(interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					return apierrors.NewBadRequest("admission webhook denied the request")
				},
			}, existing)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())
		})

		It("uses the configured name", func() {
			opts.DefaultNodePoolName = "catch-all"
			k8sClient := newClient(shard("default", ""))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(unmanaged_shards.OwningNodePoolName(getShard(k8sClient, "default"))).To(Equal("catch-all"))
		})

		It("is not created when defaultNodePool is disabled", func() {
			opts.EnsureDefaultNodePool = false
			k8sClient := newClient(shard("team-a", "team-a"))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default"}, &kaires.NodePool{})).
				To(MatchError(ContainSubstring("not found")))
		})

		It("fails on a default shard when defaultNodePool is disabled", func() {
			opts.EnsureDefaultNodePool = false
			k8sClient := newClient(shard("default", ""))

			Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(ContainSubstring("defaultNodePool.enabled=false")))
			Expect(nodePoolCount(k8sClient)).To(BeZero())
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
					LabelKey:   partitionLabelKey,
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
			k8sClient := newClient(shard("team-a", "team-a"), kaiConfig(partitionLabelKey))

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
				Spec:       kaires.NodePoolSpec{LabelKey: partitionLabelKey, LabelValue: "team-b"},
			}
			k8sClient := newClient(shard("team-a", "team-a"), other)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(ContainSubstring("already exists")))
			expectNothingChanged(k8sClient, "team-a")
		})

		It("fails when a NodePool of that name selects its nodes by another label", func() {
			other := &kaires.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "team-a"},
				Spec:       kaires.NodePoolSpec{LabelKey: "gpu-type", LabelValue: "a100"},
			}
			k8sClient := newClient(shard("team-a", "team-a"), other)

			Expect(MigrateShards(ctx, k8sClient, opts)).To(MatchError(And(
				ContainSubstring("selects nodes labelled gpu-type=a100"),
				ContainSubstring("select kai.scheduler/node-pool=team-a in the NodePool"))))
			expectNothingChanged(k8sClient, "team-a")
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
