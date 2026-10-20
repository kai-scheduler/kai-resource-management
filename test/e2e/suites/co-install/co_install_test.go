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
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

		// Root first, leaf last: the three levels KRM cannot model and must leave alone.
		adminQueues     []*kaiv2.Queue
		adminQueueSpecs map[string]kaiv2.QueueSpec

		adminNamespace string
		// What KAI's pod-grouper gave each admin pod before KRM existed, keyed by pod name.
		adminPodGroupQueues     map[string]string
		adminPodGroupPartitions map[string]string
	)

	adminLeafQueue := func() string { return adminQueues[len(adminQueues)-1].Name }

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

		// Registered before the admin world, so it runs after it is gone: the migration
		// created these node pools, and nothing else removes them when a spec fails early.
		DeferCleanup(func() {
			uninstallKRM(krmReleaseName, kaiNamespace)
			uninstallKRM(krmReleaseName, constant.ReleaseNamespace)
			deleteNodePools(adminPartition, defaultNodePoolName)
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

		// Named apart from its partition, so a migration that renamed shards would show.
		adminShard = resources.SchedulingShard(utils.GenerateName("coi-admin-shard"), adminPartition)
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

		It("leaves an unmanaged shard, its spec and its nodes alone", func() {
			shard := &kaischedulerv1.SchedulingShard{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(unmanagedShard), shard)).To(Succeed())
			Expect(shard.Spec).To(Equal(unmanagedShardSpec))
			Expect(shard.OwnerReferences).To(BeEmpty())
			Expect(shard.Labels).ToNot(HaveKey(helmhooks.AdoptionLabelKey))

			err := testClient.Get(ctx, types.NamespacedName{Name: unmanagedPartition}, &kaires.NodePool{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no node pool should exist for it: %v", err)

			node := &corev1.Node{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: unmanagedNode}, node)).To(Succeed())
			Expect(node.Labels).To(HaveKeyWithValue(adminNodePoolLabelKey, unmanagedPartition))

			runAdminPod(unmanagedPartition, unmanagedNode)
		})

		It("refuses a node pool on an unmanaged shard's partition", func() {
			nodePool := resources.NodePool(unmanagedPartition, adminNodePoolLabelKey, unmanagedPartition)

			Expect(createExpectingRefusal(nodePool)).To(MatchError(
				ContainSubstring("labelled " + unmanaged_shards.IgnoreShardLabelKey)))
		})

		It("refuses a shard with no node pool and no ignore label", func() {
			shard := resources.SchedulingShard(utils.GenerateName("coi-orphan"), utils.GenerateName("coi-orphan"))

			Expect(createExpectingRefusal(shard)).To(MatchError(ContainSubstring("no nodepool")))
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
})

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
