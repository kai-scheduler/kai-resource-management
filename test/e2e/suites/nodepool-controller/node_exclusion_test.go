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

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/nodes"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
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
	})
