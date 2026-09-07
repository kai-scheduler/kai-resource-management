// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	kaiconstants "github.com/kai-scheduler/api/constants"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/nodes"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// Names nodepool-controller uses by default, so this suite cannot choose them.
const (
	managedNodesConfigName = "kai-managed-nodes-config"
	excludedNodePoolName   = "kai-excluded-nodes"
	toExcludeLabelKey      = "kai.scheduler/to-exclude"
)

// excludeLabelKey marks the node these specs take out of the managed set.
const excludeLabelKey = "kai.resources/e2e-exclude"

// Serial: the config is a cluster-wide singleton, and excluding a node takes it out of
// whatever pool another spec is using.
var _ = Describe("A node the managed-nodes config excludes", Ordered, Serial,
	Label("managed-nodes"), func() {
		var (
			config   *kaires.ManagedNodesConfig
			nodeName string
		)

		BeforeAll(func() {
			var err error
			nodeName, err = nodes.LabelWorker(ctx, testClient, excludeLabelKey, "true")
			Expect(err).ToNot(HaveOccurred())

			// Excluding a node is expressed as including every node without its label.
			config = resources.ManagedNodesConfig(managedNodesConfigName,
				resources.WithoutNodeLabel(excludeLabelKey))
			Expect(testClient.Create(ctx, config)).To(Succeed())

			DeferCleanup(func() {
				// Config first: while it stands the node would just be excluded again.
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, config))).To(Succeed())
				wait.ForDeleted(ctx, testClient, config)

				Expect(nodes.RemoveLabel(ctx, testClient, nodeName, excludeLabelKey)).To(Succeed())

				// A node left in the excluded pool is stranded for every later spec.
				Eventually(func(g Gomega) {
					node := &corev1.Node{}
					g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
					g.Expect(node.Labels).ToNot(HaveKey(kaiconstants.DefaultNodePoolLabelKey))
				}).Should(Succeed())
			})
		})

		It("is moved into the excluded node pool", func() {
			Eventually(func(g Gomega) {
				node := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
				g.Expect(node.Labels).To(HaveKeyWithValue(
					kaiconstants.DefaultNodePoolLabelKey, excludedNodePoolName))
			}).Should(Succeed())
		})

		It("leaves the config reporting that every node is where it belongs", func() {
			applied := wait.ForManagedNodesCondition(ctx, testClient, config.Name,
				string(kaires.MNCConditionTypeApplied), metav1.ConditionTrue)

			Expect(applied.Reason).To(Equal(string(kaires.MNCConditionReasonAllNodesIncludedCorrectly)))
			Expect(applied.Message).ToNot(ContainSubstring(nodeName),
				"a node named in the message is one that could not be excluded")
		})

		It("is not marked as still needing to be drained", func() {
			node := &corev1.Node{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())

			Expect(node.Labels).ToNot(HaveKey(toExcludeLabelKey))
		})

		// Last: widening the criteria undoes what the specs above set up.
		It("returns to its pool once the criteria no longer excludes it", func() {
			Expect(nodes.RemoveLabel(ctx, testClient, nodeName, excludeLabelKey)).To(Succeed())

			Eventually(func(g Gomega) {
				node := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
				g.Expect(node.Labels).ToNot(HaveKey(kaiconstants.DefaultNodePoolLabelKey))
			}).Should(Succeed())
		})
	})

var _ = Describe("A node the config excludes while it still runs work", Ordered, Serial,
	Label("managed-nodes"), func() {
		var (
			config    *kaires.ManagedNodesConfig
			project   *kaires.Project
			pod       *corev1.Pod
			namespace string
			nodeName  string
		)

		BeforeAll(func() {
			workers, err := nodes.Workers(ctx, testClient)
			Expect(err).ToNot(HaveOccurred())
			nodeName = workers[0]

			// Only a running pod whose pod group names the node's own pool blocks it.
			project = resources.Project(utils.GenerateName("mnc-drain-proj"),
				[]string{testcontext.DefaultNodePoolName},
				resources.WithEnforceScheduler(true))
			Expect(testClient.Create(ctx, project)).To(Succeed())
			namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

			pod = resources.Pod(utils.GenerateName("mnc-drain-pod"), namespace,
				resources.WithNodeAffinity(resources.NodeSelectorPair{
					Key: corev1.LabelHostname, Value: nodeName}))
			Expect(testClient.Create(ctx, pod)).To(Succeed())
			wait.ForPodRunning(ctx, testClient, namespace, pod.Name)

			Expect(nodes.SetLabel(ctx, testClient, nodeName, excludeLabelKey, "true")).To(Succeed())
			config = resources.ManagedNodesConfig(managedNodesConfigName,
				resources.WithoutNodeLabel(excludeLabelKey))
			Expect(testClient.Create(ctx, config)).To(Succeed())

			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, config))).To(Succeed())
				wait.ForDeleted(ctx, testClient, config)

				Expect(nodes.RemoveLabel(ctx, testClient, nodeName, excludeLabelKey)).To(Succeed())

				Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
				wait.ForDeleted(ctx, testClient, pod)

				Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
				wait.ForDeleted(ctx, testClient, project)

				// Cordoned above and never uncordoned by the config going away.
				Eventually(func(g Gomega) {
					node := &corev1.Node{}
					g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
					g.Expect(node.Labels).ToNot(HaveKey(toExcludeLabelKey))
					g.Expect(node.Spec.Unschedulable).To(BeFalse())
				}).Should(Succeed())
			})
		})

		It("is marked as needing to be drained first", func() {
			Eventually(func(g Gomega) {
				node := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
				g.Expect(node.Labels).To(HaveKeyWithValue(toExcludeLabelKey, "true"))
			}).Should(Succeed())
		})

		It("is cordoned so nothing new lands on it", func() {
			Eventually(func(g Gomega) {
				node := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
				g.Expect(node.Spec.Unschedulable).To(BeTrue())
			}).Should(Succeed())
		})

		It("stays in its pool rather than moving to the excluded one", func() {
			node := &corev1.Node{}
			Expect(testClient.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())

			Expect(node.Labels).ToNot(HaveKeyWithValue(
				kaiconstants.DefaultNodePoolLabelKey, excludedNodePoolName))
		})

		It("leaves the config reporting the node as still to be drained", func() {
			applied := wait.ForManagedNodesCondition(ctx, testClient, config.Name,
				string(kaires.MNCConditionTypeApplied), metav1.ConditionFalse)

			Expect(applied.Reason).To(Equal(string(kaires.MNCConditionReasonToBeExcludedNodes)))
			Expect(applied.Message).To(ContainSubstring(nodeName))
		})
	})
