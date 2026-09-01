// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_assigner

import (
	"time"

	kaitopologyv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/podgroup/assigner"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// Topology levels, highest first. The assigner stamps the lowest.
const (
	highestTopologyLevel = "topology.kubernetes.io/zone"
	lowestTopologyLevel  = "kubernetes.io/hostname"
)

const userConstraintGracePeriod = 7 * time.Second

var _ = Describe("A pod group in node pools with a network topology", Ordered, Label("pod-group-assigner"), func() {
	var (
		topology  *kaitopologyv1alpha1.Topology
		nodePools []*kaires.NodePool
		namespace string
		pod       *corev1.Pod
		podGroup  *kaiv2alpha2.PodGroup
	)

	setTopologyOnPools := func(topologyName string) {
		for _, nodePool := range nodePools {
			setNodePoolTopology(nodePool.Name, topologyName)
		}
	}

	BeforeAll(func() {
		topology = resources.Topology(utils.GenerateName("pga-topo"),
			highestTopologyLevel, lowestTopologyLevel)
		Expect(testClient.Create(ctx, topology)).To(Succeed())

		poolNames := []string{}
		for _, prefix := range []string{"pga-topo-a", "pga-topo-b"} {
			nodePool := resources.GeneratedNodePool(prefix, nodePoolLabelKey,
				resources.WithPreferredNetworkTopology(topology.Name))
			Expect(testClient.Create(ctx, nodePool)).To(Succeed())
			wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolEmpty)

			nodePools = append(nodePools, nodePool)
			poolNames = append(poolNames, nodePool.Name)
		}

		project := resources.Project(utils.GenerateName("pga-topo-proj"), poolNames,
			resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		pod = resources.Pod(utils.GenerateName("pga-topo-pod"), namespace)
		Expect(testClient.Create(ctx, pod)).To(Succeed())
		podGroup = wait.ForPodGroup(ctx, testClient, namespace, pod.Name)

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, pod)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			for _, nodePool := range nodePools {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
				wait.ForDeleted(ctx, testClient, nodePool)
			}

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, topology))).To(Succeed())
			wait.ForDeleted(ctx, testClient, topology)
		})
	})

	It("is stamped with the topology, at its lowest level and marked system-sourced", func() {
		Eventually(func(g Gomega) {
			assigned := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), assigned)).To(Succeed())

			g.Expect(assigned.Spec.TopologyConstraint).To(Equal(kaiv2alpha2.TopologyConstraint{
				Topology:               topology.Name,
				PreferredTopologyLevel: lowestTopologyLevel,
			}))
			g.Expect(assigned.Annotations).To(HaveKeyWithValue(
				assigner.TopologySourceAnnotationKey, assigner.TopologySourceSystem))
		}).Should(Succeed())
	})

	It("has it cleared again once no pool names a topology", func() {
		setTopologyOnPools("")

		Eventually(func(g Gomega) {
			cleared := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), cleared)).To(Succeed())

			g.Expect(cleared.Spec.TopologyConstraint).To(Equal(kaiv2alpha2.TopologyConstraint{}))
			g.Expect(cleared.Annotations).ToNot(HaveKey(assigner.TopologySourceAnnotationKey))
		}).Should(Succeed())
	})

	It("keeps a constraint the user set, even once the pools name a topology again", func() {
		// No source annotation goes with it, which is what marks it as the user's.
		userConstraint := kaiv2alpha2.TopologyConstraint{
			Topology:               topology.Name,
			PreferredTopologyLevel: highestTopologyLevel,
		}
		Eventually(func(g Gomega) {
			latest := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), latest)).To(Succeed())
			latest.Spec.TopologyConstraint = userConstraint
			g.Expect(testClient.Update(ctx, latest)).To(Succeed())
		}).Should(Succeed())

		setTopologyOnPools(topology.Name)

		Consistently(func(g Gomega) {
			untouched := &kaiv2alpha2.PodGroup{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(podGroup), untouched)).To(Succeed())

			g.Expect(untouched.Spec.TopologyConstraint).To(Equal(userConstraint))
			g.Expect(untouched.Annotations).ToNot(HaveKey(assigner.TopologySourceAnnotationKey))
		}).WithContext(ctx).WithTimeout(userConstraintGracePeriod).
			WithPolling(constant.Interval).Should(Succeed())
	})
})

// setNodePoolTopology retries because nodepool-controller writes the pool's status alongside.
func setNodePoolTopology(name, topologyName string) {
	EventuallyWithOffset(1, func(g Gomega) {
		nodePool := &kaires.NodePool{}
		g.Expect(testClient.Get(ctx, types.NamespacedName{Name: name}, nodePool)).To(Succeed())
		nodePool.Spec.PreferredNetworkTopologyName = topologyName
		g.Expect(testClient.Update(ctx, nodePool)).To(Succeed())
	}).Should(Succeed())
}
