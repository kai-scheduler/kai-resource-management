// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_assigner

import (
	kaitopologyv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/podgroup/assigner"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/nodes"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// The topology's levels, highest first. The assigner stamps the lowest one.
//
// kind nodes carry the hostname label already but no zone label, and nodepool-controller
// reports a node missing any of a topology's levels as a mismatch - so the suite puts the
// zone label on the node itself rather than dropping to a single-level topology, which
// would make "the lowest level" the only level and prove nothing.
const (
	highestTopologyLevel = "topology.kubernetes.io/zone"
	lowestTopologyLevel  = "kubernetes.io/hostname"
	topologyZone         = "e2e-zone"
)

var _ = Describe("A pod group in a node pool with a network topology", Ordered, Label("pod-group-assigner"), func() {
	var (
		topology  *kaitopologyv1alpha1.Topology
		nodePool  *kaires.NodePool
		nodeName  string
		project   *kaires.Project
		namespace string
		pod       *corev1.Pod
	)

	BeforeAll(func() {
		topology = resources.Topology(utils.GenerateName("pga-topo"),
			highestTopologyLevel, lowestTopologyLevel)
		Expect(testClient.Create(ctx, topology)).To(Succeed())

		nodePool = resources.GeneratedNodePool("pga-topo-pool", nodePoolLabelKey,
			resources.WithPreferredNetworkTopology(topology.Name))
		Expect(testClient.Create(ctx, nodePool)).To(Succeed())

		var err error
		nodeName, err = nodes.LabelWorker(ctx, testClient, nodePool.Spec.LabelKey, nodePool.Spec.LabelValue)
		Expect(err).ToNot(HaveOccurred())
		Expect(nodes.SetLabel(ctx, testClient, nodeName, highestTopologyLevel, topologyZone)).To(Succeed())

		wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolReady)

		project = resources.Project(utils.GenerateName("pga-topo-proj"), []string{nodePool.Name},
			resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		pod = resources.Pod(utils.GenerateName("pga-topo-pod"), namespace)
		Expect(testClient.Create(ctx, pod)).To(Succeed())

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			wait.ForDeleted(ctx, testClient, pod)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			Expect(nodes.RemoveLabel(ctx, testClient, nodeName, nodePoolLabelKey)).To(Succeed())
			Expect(nodes.RemoveLabel(ctx, testClient, nodeName, highestTopologyLevel)).To(Succeed())

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, nodePool)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, topology))).To(Succeed())
			wait.ForDeleted(ctx, testClient, topology)
		})
	})

	It("is stamped with the pool's topology, at its lowest level and marked system-sourced", func() {
		podGroup := wait.ForPodGroup(ctx, testClient, namespace, pod.Name)

		// The pod-grouper creates the pod group; the assigner writes the constraint onto
		// it afterwards, so the value has to be polled rather than read once.
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
})
