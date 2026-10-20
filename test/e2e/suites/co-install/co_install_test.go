// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package coinstall installs KRM onto a cluster already running KAI Scheduler on its own, and
// checks that the admin's scheduler, shards, queues and workloads come through it.
package coinstall

import (
	"fmt"
	"strings"

	kaiconstants "github.com/kai-scheduler/api/constants"
	kaischedulerv1 "github.com/kai-scheduler/api/kai/v1"
	kaiv2 "github.com/kai-scheduler/api/scheduling/v2"
	kaiv2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/helmhooks"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/nodes"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

const (
	// KAI's chart ships its default shard under this name, serving partition "".
	defaultShardName = "default"
	// The NodePool the migration creates for that shard.
	defaultNodePoolName = "default"
)

var _ = Describe("KRM installed onto a running KAI Scheduler", Ordered, Label("co-install"), func() {
	var (
		// One worker per partition: the admin's own, the one left unmanaged, and default.
		adminNode, unmanagedNode, defaultNode string

		adminPartition, unmanagedPartition string
		adminShard, unmanagedShard         *kaischedulerv1.SchedulingShard
		unmanagedShardSpec                 kaischedulerv1.SchedulingShardSpec

		// A node pool created through KRM once installed, whose shard KRM creates rather than adopts.
		createdNodePool string

		// Root first, leaf last: the three levels KRM cannot model and must leave alone.
		adminQueues     []*kaiv2.Queue
		adminQueueSpecs map[string]kaiv2.QueueSpec

		adminNamespace string
		// What KAI's pod-grouper gave each admin pod before KRM existed, keyed by pod name.
		adminPodGroupQueues     map[string]string
		adminPodGroupPartitions map[string]string
	)

	adminLeafQueue := func() string { return adminQueues[len(adminQueues)-1].Name }

	expectAdminShardSettings := func(g Gomega) {
		shard := &kaischedulerv1.SchedulingShard{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(adminShard), shard)).To(Succeed())
		g.Expect(shard.Labels).To(HaveKeyWithValue(adminShardLabelKey, adminShardLabelValue))
		g.Expect(shard.Spec.Args).To(HaveKeyWithValue("v", "4"))
		g.Expect(shard.Spec.PlacementStrategy).To(HaveField("GPU", HaveValue(Equal("spread"))))
		g.Expect(shard.Spec.GoMemLimitRatio).To(Equal(adminShard.Spec.GoMemLimitRatio))
	}

	// runAdminPod submits a pod the way a workload reaches KAI without KRM: by scheduler name
	// and queue label, plus a partition label unless it belongs to the default partition.
	runAdminPod := func(partition, expectedNode string) *corev1.Pod {
		options := []resources.PodOption{
			resources.WithSchedulerName(kaiconstants.DefaultSchedulerName),
			resources.WithQueueLabel(adminQueueLabelKey, adminLeafQueue()),
		}
		if partition != "" {
			options = append(options, resources.WithNodePoolLabel(adminNodePoolLabelKey, partition))
		}
		pod := resources.Pod(utils.GenerateName("coi-admin-pod"), adminNamespace, options...)
		Expect(testClient.Create(ctx, pod)).To(Succeed())

		running := wait.ForPodRunning(ctx, testClient, adminNamespace, pod.Name)
		Expect(running.Spec.NodeName).To(Equal(expectedNode),
			"pod %s should run in partition %q", pod.Name, partition)

		return running
	}

	// createExpectingRefusal cleans up after an object a webhook wrongly admitted, so a
	// failing spec does not leave it behind for the next run's preflight.
	createExpectingRefusal := func(obj client.Object) error {
		err := testClient.Create(ctx, obj)
		if err == nil {
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, obj))).To(Succeed())
				wait.ForDeleted(ctx, testClient, obj)
			})
		}
		return err
	}

	BeforeAll(func() {
		workers, err := nodes.Workers(ctx, testClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(workers)).To(BeNumerically(">=", 3), "one worker per partition")
		adminNode, unmanagedNode, defaultNode = workers[0], workers[1], workers[2]

		By("installing KAI Scheduler on its own")
		installKAIScheduler()
		DeferCleanup(uninstallKAIScheduler)

		adminPartition = utils.GenerateName("coi-admin")
		unmanagedPartition = utils.GenerateName("coi-unmanaged")
		createdNodePool = utils.GenerateName("coi-created")

		// Registered before the admin world, so it runs after it is gone: these node pools
		// outlive the uninstall by design, and nothing else removes them when a spec fails early.
		DeferCleanup(func() {
			uninstallKRM(krmReleaseName, kaiNamespace)
			uninstallKRM(krmReleaseName, constant.ReleaseNamespace)
			deleteNodePools(adminPartition, defaultNodePoolName, createdNodePool)
		})

		By("partitioning nodes the way KAI does without KRM")
		DeferCleanup(func() {
			// KRM may have relabelled any worker, so every one is restored.
			for _, node := range workers {
				Expect(nodes.RemoveLabel(ctx, testClient, node, adminNodePoolLabelKey)).To(Succeed())
			}
		})
		Expect(nodes.SetLabel(ctx, testClient, adminNode, adminNodePoolLabelKey, adminPartition)).To(Succeed())
		Expect(nodes.SetLabel(ctx, testClient, unmanagedNode, adminNodePoolLabelKey, unmanagedPartition)).
			To(Succeed())

		// Named apart from its partition, so a migration that renamed shards would show. Its
		// settings are ones a node pool models, args and placement, and one it does not.
		adminShard = resources.SchedulingShard(utils.GenerateName("coi-admin-shard"), adminPartition)
		adminShard.Labels[adminShardLabelKey] = adminShardLabelValue
		adminShard.Spec.Args = map[string]string{"v": "4"}
		adminShard.Spec.PlacementStrategy = &kaischedulerv1.PlacementStrategy{GPU: ptr.To("spread")}
		adminShard.Spec.GoMemLimitRatio = ptr.To(0.8)
		unmanagedShard = resources.SchedulingShard(utils.GenerateName("coi-unmanaged-shard"), unmanagedPartition,
			resources.Unmanaged())
		for _, shard := range []*kaischedulerv1.SchedulingShard{adminShard, unmanagedShard} {
			Expect(testClient.Create(ctx, shard)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, shard))).To(Succeed())
				wait.ForDeleted(ctx, testClient, shard)
			})
		}
		wait.ForSchedulingShardReady(ctx, testClient, adminShard.Name)
		unmanagedShardSpec = wait.ForSchedulingShardReady(ctx, testClient, unmanagedShard.Name).Spec

		By("building a three-level queue tree")
		adminQueueSpecs = map[string]kaiv2.QueueSpec{}
		parent := ""
		for _, level := range []string{"coi-admin-root", "coi-admin-mid", "coi-admin-leaf"} {
			queue := resources.Queue(utils.GenerateName(level), parent)
			Expect(testClient.Create(ctx, queue)).To(Succeed())
			// Children are deleted before their parents, which KAI's queue webhook requires.
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, queue))).To(Succeed())
				wait.ForDeleted(ctx, testClient, queue)
			})
			adminQueues = append(adminQueues, queue)
			adminQueueSpecs[queue.Name] = wait.ForQueue(ctx, testClient, queue.Name).Spec
			parent = queue.Name
		}

		By("running the admin's workloads in each partition")
		adminNamespace = utils.GenerateName("coi-admin")
		namespace := resources.Namespace(adminNamespace)
		Expect(testClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, namespace))).To(Succeed())
			wait.ForDeleted(ctx, testClient, namespace)
		})

		adminPodGroupQueues = map[string]string{}
		adminPodGroupPartitions = map[string]string{}
		for partition, node := range map[string]string{
			adminPartition: adminNode, unmanagedPartition: unmanagedNode, "": defaultNode,
		} {
			pod := runAdminPod(partition, node)
			podGroup := wait.ForPodGroup(ctx, testClient, adminNamespace, pod.Name)
			adminPodGroupQueues[pod.Name] = podGroup.Spec.Queue
			adminPodGroupPartitions[pod.Name] = podGroup.Labels[adminNodePoolLabelKey]
		}
	})

	Context("on a KAI Scheduler older than the minimum", func() {
		It("refuses to install, before changing anything", func() {
			release := utils.GenerateName("krm-version-check")
			output, err := installKRM(release, constant.ReleaseNamespace,
				"--set", "versionCheck.kaiSchedulerVersion="+unsupportedKAIVersion)
			// The failed release stays listed until uninstalled. Its pre-install hooks never
			// got past the version check, so there are no post-delete ones to run either.
			DeferCleanup(func() { uninstallKRM(release, constant.ReleaseNamespace, "--no-hooks") })

			Expect(err).To(HaveOccurred(), "the install should fail: %s", output)
			Expect(versionCheckMessage()).To(ContainSubstring("is older than the minimum supported"))

			nodePools := &kaires.NodePoolList{}
			Expect(testClient.List(ctx, nodePools)).To(Succeed())
			Expect(nodePools.Items).To(BeEmpty(), "no shard should have been migrated")

			shard := &kaischedulerv1.SchedulingShard{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(adminShard), shard)).To(Succeed())
			Expect(shard.OwnerReferences).To(BeEmpty())
		})
	})

	Context("installed into its own namespace", func() {
		BeforeAll(func() {
			output, err := installKRM(krmReleaseName, constant.ReleaseNamespace,
				"--set", "schedulerNamespace="+kaiNamespace,
				"--set", "commonArgs.allowExternalQueues=true")
			Expect(err).ToNot(HaveOccurred(), "installing KRM failed: %s", output)

			wait.ForKRMConfigDeployed(ctx, testClient)
		})

		It("speaks the label keys and scheduler name of the KAI it found", func() {
			global := wait.ForKRMConfig(ctx, testClient).Spec.Global
			Expect(global).ToNot(BeNil())
			Expect(global.NodePoolLabelKey).To(HaveValue(Equal(adminNodePoolLabelKey)))
			Expect(global.QueueLabelKey).To(HaveValue(Equal(adminQueueLabelKey)))
			// KAI's chart never writes a scheduler name, so KAI's own default must apply.
			Expect(global.SchedulerName).To(Or(BeNil(), HaveValue(Equal(kaiconstants.DefaultSchedulerName))))

			for _, name := range []string{"nodepool-controller", "project-controller", "pod-group-assigner"} {
				deployment := &appsv1.Deployment{}
				key := types.NamespacedName{Namespace: constant.ReleaseNamespace, Name: name}
				Expect(testClient.Get(ctx, key, deployment)).To(Succeed())

				args := deployment.Spec.Template.Spec.Containers[0].Args
				Expect(flagValue(args, "--nodepool-label-key")).To(Equal(adminNodePoolLabelKey), name)
			}
		})

		It("adopts every shard to a node pool named after its partition, keeping the shard's name", func() {
			nodePool := wait.ForNodePoolPhase(ctx, testClient, adminPartition, kaires.NodePoolReady)
			Expect(nodePool.Spec.LabelKey).To(Equal(adminNodePoolLabelKey))
			Expect(nodePool.Spec.LabelValue).To(Equal(adminPartition))
			Expect(nodePool.Status.Nodes).To(ContainElement(HaveField("Name", adminNode)))

			for shardName, nodePoolName := range map[string]string{
				adminShard.Name:  adminPartition,
				defaultShardName: defaultNodePoolName,
			} {
				shard := wait.ForSchedulingShardReady(ctx, testClient, shardName)
				Expect(shard.OwnerReferences).To(ContainElement(And(
					HaveField("Kind", "NodePool"),
					HaveField("Name", nodePoolName),
					HaveField("Controller", HaveValue(BeTrue())),
				)), "shard %s", shardName)
				Expect(shard.Labels).To(HaveKeyWithValue(helmhooks.AdoptionLabelKey, helmhooks.AdoptionLabelValue),
					"shard %s", shardName)
			}

			shards := &kaischedulerv1.SchedulingShardList{}
			Expect(testClient.List(ctx, shards)).To(Succeed())
			partitions := map[string][]string{}
			for _, shard := range shards.Items {
				partitions[shard.Spec.PartitionLabelValue] = append(partitions[shard.Spec.PartitionLabelValue], shard.Name)
			}
			for partition, names := range partitions {
				Expect(names).To(HaveLen(1), "partition %q is served by more than one shard", partition)
			}
		})

		It("carries the admin shard's settings to its node pool, and keeps the rest on the shard", func() {
			config := wait.ForNodePoolPhase(ctx, testClient, adminPartition, kaires.NodePoolReady).
				Spec.SchedulingShardConfig
			Expect(config).ToNot(BeNil())
			Expect(config.Args).To(HaveKeyWithValue("v", "4"))
			Expect(config.PlacementStrategy).To(HaveField("GPU", HaveValue(Equal("spread"))))

			// Held for a while: the controller rewrites the shard on every reconcile, and must
			// merge into it rather than replace what a node pool cannot express.
			Consistently(expectAdminShardSettings).WithTimeout(5 * constant.Interval).Should(Succeed())
		})

		It("leaves the admin's queue tree and pod groups as they were", func() {
			for _, queue := range adminQueues {
				current := &kaiv2.Queue{}
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(queue), current)).To(Succeed())
				Expect(current.Spec).To(Equal(adminQueueSpecs[queue.Name]), "queue %s", queue.Name)
				Expect(current.OwnerReferences).To(BeEmpty(), "queue %s", queue.Name)
			}

			for podName, queue := range adminPodGroupQueues {
				podGroup := wait.ForPodGroup(ctx, testClient, adminNamespace, podName)
				Expect(podGroup.Spec.Queue).To(Equal(queue), "pod group of %s", podName)
				Expect(podGroup.Labels[adminNodePoolLabelKey]).To(Equal(adminPodGroupPartitions[podName]),
					"pod group of %s", podName)
			}
		})

		It("still schedules the admin's new workloads", func() {
			runAdminPod(adminPartition, adminNode)
			runAdminPod("", defaultNode)
		})

		// KRM moves its own unschedulable pod groups to another node pool; an admin's must stay
		// where KAI put it.
		It("leaves an admin pod group alone when it cannot be scheduled", func() {
			pod := resources.Pod(utils.GenerateName("coi-admin-pending"), adminNamespace,
				resources.WithSchedulerName(kaiconstants.DefaultSchedulerName),
				resources.WithQueueLabel(adminQueueLabelKey, adminLeafQueue()),
				resources.WithNodePoolLabel(adminNodePoolLabelKey, adminPartition))
			pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1000"),
			}
			Expect(testClient.Create(ctx, pod)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
				wait.ForDeleted(ctx, testClient, pod)
			})

			podGroup := wait.ForPodGroup(ctx, testClient, adminNamespace, pod.Name)
			queue, partition := podGroup.Spec.Queue, podGroup.Labels[adminNodePoolLabelKey]
			Expect(partition).To(Equal(adminPartition))

			getPodGroup := func(g Gomega) *kaiv2alpha2.PodGroup {
				current := &kaiv2alpha2.PodGroup{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), current)).To(Succeed())
				return current
			}
			Eventually(func(g Gomega) {
				g.Expect(getPodGroup(g).Status.SchedulingConditions).ToNot(BeEmpty(),
					"KAI should have reported the pod group unschedulable")
			}).WithTimeout(constant.PodTimeout).Should(Succeed())

			Consistently(func(g Gomega) {
				current := getPodGroup(g)
				g.Expect(current.Spec.Queue).To(Equal(queue))
				g.Expect(current.Labels).To(HaveKeyWithValue(adminNodePoolLabelKey, partition))
			}).WithTimeout(5 * constant.Interval).Should(Succeed())
		})

		It("leaves an unmanaged shard, its spec and its nodes alone", func() {
			shard := &kaischedulerv1.SchedulingShard{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(unmanagedShard), shard)).To(Succeed())
			Expect(shard.Spec).To(Equal(unmanagedShardSpec))
			Expect(shard.OwnerReferences).To(BeEmpty())
			Expect(shard.Labels).ToNot(HaveKey(helmhooks.AdoptionLabelKey))

			err := testClient.Get(ctx, types.NamespacedName{Name: unmanagedPartition}, &kaires.NodePool{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no node pool should exist for it: %v", err)

			expectNodeLabel(unmanagedNode, adminNodePoolLabelKey, unmanagedPartition)

			runAdminPod(unmanagedPartition, unmanagedNode)
		})

		It("refuses a node pool on an unmanaged shard's partition", func() {
			nodePool := resources.NodePool(unmanagedPartition, adminNodePoolLabelKey, unmanagedPartition)

			Expect(createExpectingRefusal(nodePool)).To(MatchError(
				ContainSubstring("labelled " + unmanaged_shards.IgnoreShardLabelKey)))
		})

		It("creates a shard of its own for a new node pool, without marking it adopted", func() {
			// On a partition no node carries: only the shard is under test here.
			nodePool := resources.NodePool(createdNodePool, adminNodePoolLabelKey, createdNodePool)
			Expect(testClient.Create(ctx, nodePool)).To(Succeed())

			shard := wait.ForSchedulingShardReady(ctx, testClient, createdNodePool)
			Expect(shard.Spec.PartitionLabelValue).To(Equal(createdNodePool))
			Expect(shard.OwnerReferences).To(ContainElement(And(
				HaveField("Kind", "NodePool"),
				HaveField("Name", createdNodePool),
			)))
			Expect(shard.Labels).ToNot(HaveKey(helmhooks.AdoptionLabelKey))
		})

		It("refuses a shard with no node pool and no ignore label", func() {
			shard := resources.SchedulingShard(utils.GenerateName("coi-orphan"), utils.GenerateName("coi-orphan"))

			Expect(createExpectingRefusal(shard)).To(MatchError(ContainSubstring("no nodepool")))
		})

		It("refuses to hand an adopted shard back, or move it to another partition", func() {
			shard := &kaischedulerv1.SchedulingShard{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(adminShard), shard)).To(Succeed())

			ignored := shard.DeepCopy()
			ignored.Labels[unmanaged_shards.IgnoreShardLabelKey] = "true"
			Expect(testClient.Patch(ctx, ignored, client.MergeFrom(shard))).To(MatchError(
				ContainSubstring("cannot add, change or remove")))

			moved := shard.DeepCopy()
			moved.Spec.PartitionLabelValue = utils.GenerateName("coi-moved")
			Expect(testClient.Patch(ctx, moved, client.MergeFrom(shard))).To(MatchError(
				ContainSubstring("cannot change partitionLabelValue")))
		})

		Context("a project", Ordered, func() {
			var (
				project   *kaires.Project
				namespace string
			)

			BeforeAll(func() {
				department := resources.Department(utils.GenerateName("coi-dept"), []string{adminPartition})
				Expect(testClient.Create(ctx, department)).To(Succeed())

				project = resources.Project(utils.GenerateName("coi-proj"), []string{adminPartition},
					resources.WithParentDepartment(department.Name),
					resources.WithEnforceScheduler(true))
				Expect(testClient.Create(ctx, project)).To(Succeed())

				namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

				// The project's queues are parented to the department's, so it goes first.
				DeferCleanup(func() {
					Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
					wait.ForDeleted(ctx, testClient, project)

					Expect(client.IgnoreNotFound(testClient.Delete(ctx, department))).To(Succeed())
					wait.ForDeleted(ctx, testClient, department)
				})
			})

			It("schedules its pods through its own queue on the existing scheduler", func() {
				pod := resources.Pod(utils.GenerateName("coi-pod"), namespace)
				Expect(testClient.Create(ctx, pod)).To(Succeed())

				podGroup := wait.ForPodGroup(ctx, testClient, namespace, pod.Name)
				Expect(podGroup.Spec.Queue).To(Equal(resources.QueueName(project.Name, adminPartition)))
				Expect(podGroup.Labels).To(HaveKeyWithValue(adminNodePoolLabelKey, adminPartition))

				running := wait.ForPodRunning(ctx, testClient, namespace, pod.Name)
				Expect(running.Spec.SchedulerName).To(Equal(kaiconstants.DefaultSchedulerName))
				Expect(running.Spec.NodeName).To(Equal(adminNode))
			})

			It("refuses an admin queue grafted onto its tree", func() {
				queue := resources.Queue(utils.GenerateName("coi-graft"),
					resources.QueueName(project.Name, adminPartition))

				Expect(createExpectingRefusal(queue)).To(MatchError(
					ContainSubstring("managed by KAI Resource Management")))
			})
		})

		It("puts each shard's service monitor in the scheduler's namespace, where it scrapes", func() {
			name := fmt.Sprintf("%s-%s", kaiconstants.DefaultSchedulerName, adminPartition)
			monitor := wait.ForServiceMonitorInNamespace(ctx, testClient, kaiNamespace, name)

			err := testClient.Get(ctx,
				types.NamespacedName{Namespace: constant.ReleaseNamespace, Name: name}, monitor.DeepCopy())
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "nothing should be written to the release namespace: %v", err)

			Expect(monitor.Spec.Endpoints).ToNot(BeEmpty())
			endpoint := monitor.Spec.Endpoints[0]
			path := endpoint.Path
			if path == "" {
				path = "/metrics"
			}

			// Through the API server's service proxy, the same Service and port Prometheus
			// would scrape, without running a Prometheus.
			Eventually(func(g Gomega) {
				services := &corev1.ServiceList{}
				g.Expect(testClient.List(ctx, services, client.InNamespace(kaiNamespace),
					client.MatchingLabels(monitor.Spec.Selector.MatchLabels))).To(Succeed())
				g.Expect(services.Items).To(HaveLen(1), "the monitor should select the shard's one Service")

				body, err := clientset.CoreV1().Services(kaiNamespace).
					ProxyGet("http", services.Items[0].Name, endpoint.Port, path, nil).DoRaw(ctx)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(string(body)).To(ContainSubstring("# TYPE"))
			}).Should(Succeed())
		})
	})

	// The migrated node pool selects on the scheduler's own partition key, so relabelling a node
	// moves its partition directly, and the controller's wait for running pods never gets a say.
	// The pods are removed first; the move with pods running is a documented limitation.
	Context("moving a node of a migrated node pool", func() {
		BeforeAll(func() {
			Expect(testClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(adminNamespace),
				client.MatchingLabels{adminNodePoolLabelKey: adminPartition})).To(Succeed())

			Eventually(func(g Gomega) {
				pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
					FieldSelector: "spec.nodeName=" + adminNode,
					LabelSelector: adminNodePoolLabelKey + "=" + adminPartition,
				})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(pods.Items).To(BeEmpty(), "pods of partition %q are still on %s", adminPartition, adminNode)
			}).WithTimeout(constant.PodTimeout).Should(Succeed())
		})

		It("selects on the scheduler's node pool label key", func() {
			nodePool := wait.ForNodePoolPhase(ctx, testClient, adminPartition, kaires.NodePoolReady)
			Expect(nodePool.Spec.LabelKey).To(Equal(adminNodePoolLabelKey))
			expectNodeInNodePool(adminNode, adminPartition)
		})

		It("moves the node to the default node pool when the label is removed", func() {
			Expect(nodes.RemoveLabel(ctx, testClient, adminNode, adminNodePoolLabelKey)).To(Succeed())

			expectNodeInNodePool(adminNode, defaultNodePoolName)
			expectNodeNotInNodePool(adminNode, adminPartition)
			expectNodeLabel(adminNode, adminNodePoolLabelKey, "")
		})

		It("moves it back when the label is restored", func() {
			Expect(nodes.SetLabel(ctx, testClient, adminNode, adminNodePoolLabelKey, adminPartition)).To(Succeed())

			expectNodeInNodePool(adminNode, adminPartition)
			expectNodeNotInNodePool(adminNode, defaultNodePoolName)
			expectNodeLabel(adminNode, adminNodePoolLabelKey, adminPartition)
		})
	})

	Context("after helm uninstall", func() {
		BeforeAll(func() {
			uninstallKRM(krmReleaseName, constant.ReleaseNamespace)
		})

		It("leaves every adopted shard running and owned by nothing of KRM's", func() {
			for _, name := range []string{adminShard.Name, defaultShardName} {
				shard := wait.ForSchedulingShardReady(ctx, testClient, name)
				Expect(shard.OwnerReferences).ToNot(ContainElement(HaveField("Kind", "NodePool")), "shard %s", name)
				// This strips the suite's own kai.resources/e2e labels too; cleanup deletes by name.
				Expect(keysWithPrefix(shard.Labels, "kai.resources/")).To(BeEmpty(), "shard %s", name)
				Expect(keysWithPrefix(shard.Annotations, "kai.resources/")).To(BeEmpty(), "shard %s", name)
			}

			Eventually(expectAdminShardSettings).Should(Succeed())
		})

		It("still schedules the admin's workloads", func() {
			runAdminPod(adminPartition, adminNode)
			runAdminPod("", defaultNode)
		})

		// The counterpart to the adopted shards: uninstall releases only those, so a shard KRM
		// created still belongs to its node pool and goes with it.
		It("deletes a shard KRM created along with its node pool", func() {
			shard := wait.ForSchedulingShardReady(ctx, testClient, createdNodePool)
			Expect(shard.OwnerReferences).To(ContainElement(HaveField("Name", createdNodePool)),
				"uninstall should not release a shard KRM created")

			deleteNodePools(createdNodePool)

			wait.ForDeleted(ctx, testClient, shard)
		})

		// Helm deletes no node pool, so this is what proves the shards were released: an owned
		// shard would be garbage-collected with its node pool, and the finalizer would hang.
		It("deletes a node pool without taking its shard or hanging on a finalizer", func() {
			deleteNodePools(adminPartition, defaultNodePoolName)

			wait.ForSchedulingShardReady(ctx, testClient, adminShard.Name)
			wait.ForSchedulingShardReady(ctx, testClient, defaultShardName)
			runAdminPod(adminPartition, adminNode)
		})
	})

	// A smoke test of the recommended layout: no schedulerNamespace, because the release
	// namespace is the scheduler's. It also re-migrates the shards released above, this time
	// with KAI's default shard labelled to be ignored, which the first install did not.
	Context("installed into the scheduler's own namespace", func() {
		It("installs and comes up", func() {
			// With KRM gone nothing refuses the label.
			shard := &kaischedulerv1.SchedulingShard{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: defaultShardName}, shard)).To(Succeed())
			ignored := shard.DeepCopy()
			if ignored.Labels == nil {
				ignored.Labels = map[string]string{}
			}
			ignored.Labels[unmanaged_shards.IgnoreShardLabelKey] = "true"
			Expect(testClient.Patch(ctx, ignored, client.MergeFrom(shard))).To(Succeed())

			output, err := installKRM(krmReleaseName, kaiNamespace)
			Expect(err).ToNot(HaveOccurred(), "installing KRM failed: %s", output)

			Expect(wait.ForKRMConfigDeployed(ctx, testClient).Spec.Namespace).To(Equal(kaiNamespace))
			for _, name := range []string{"nodepool-controller", "project-controller", "pod-group-assigner"} {
				deployment := &appsv1.Deployment{}
				key := types.NamespacedName{Namespace: kaiNamespace, Name: name}
				Expect(testClient.Get(ctx, key, deployment)).To(Succeed(), name)
			}
		})

		// The default partition always needs a node pool, so the label cannot keep it out.
		It("adopts KAI's default shard even when it is labelled to be ignored", func() {
			shard := wait.ForSchedulingShardReady(ctx, testClient, defaultShardName)
			Expect(shard.Labels).ToNot(HaveKey(unmanaged_shards.IgnoreShardLabelKey))
			Expect(shard.Labels).To(HaveKeyWithValue(helmhooks.AdoptionLabelKey, helmhooks.AdoptionLabelValue))
			Expect(shard.OwnerReferences).To(ContainElement(HaveField("Name", defaultNodePoolName)))
		})

		It("uninstalls, and its node pools still delete", func() {
			uninstallKRM(krmReleaseName, kaiNamespace)
			deleteNodePools(adminPartition, defaultNodePoolName)

			wait.ForSchedulingShardReady(ctx, testClient, adminShard.Name)
			wait.ForSchedulingShardReady(ctx, testClient, defaultShardName)
		})
	})
})

func expectNodeInNodePool(node, nodePoolName string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		nodePool := &kaires.NodePool{}
		g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodePoolName}, nodePool)).To(Succeed())
		g.Expect(nodePool.Status.Nodes).To(ContainElement(HaveField("Name", node)),
			"node %s should belong to node pool %s", node, nodePoolName)
	}).Should(Succeed())
}

func expectNodeNotInNodePool(node, nodePoolName string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		nodePool := &kaires.NodePool{}
		g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodePoolName}, nodePool)).To(Succeed())
		g.Expect(nodePool.Status.Nodes).ToNot(ContainElement(HaveField("Name", node)),
			"node %s should have left node pool %s", node, nodePoolName)
	}).Should(Succeed())
}

// expectNodeLabel holds for a while, not only once: the controller must settle on the label,
// not rewrite it on its next reconcile. An empty value means the label is absent.
func expectNodeLabel(nodeName, key, value string) {
	GinkgoHelper()
	matcher := HaveKeyWithValue(key, value)
	if value == "" {
		matcher = Not(HaveKey(key))
	}

	Consistently(func(g Gomega) {
		node := &corev1.Node{}
		g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
		g.Expect(node.Labels).To(matcher)
		g.Expect(node.Spec.Unschedulable).To(BeFalse(), "node %s should not be cordoned", nodeName)
	}).WithTimeout(5 * constant.Interval).Should(Succeed())
}

func keysWithPrefix(values map[string]string, prefix string) []string {
	var keys []string
	for key := range values {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	return keys
}

// versionCheckMessage is the reason the version-check hook failed. Helm reports only that the
// Job hit its backoff limit; the container's termination message carries why.
func versionCheckMessage() string {
	pods, err := clientset.CoreV1().Pods(constant.ReleaseNamespace).List(ctx,
		metav1.ListOptions{LabelSelector: "app=kai-resource-management-version-check"})
	Expect(err).ToNot(HaveOccurred())
	Expect(pods.Items).ToNot(BeEmpty(), "the version check should have run")

	var messages []string
	for _, pod := range pods.Items {
		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Terminated != nil {
				messages = append(messages, status.State.Terminated.Message)
			}
		}
	}

	return strings.Join(messages, "\n")
}
