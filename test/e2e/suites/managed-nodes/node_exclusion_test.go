// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package managed_nodes covers the managed-nodes half of nodepool-controller: which nodes
// the resource management stack manages, and what happens to one it stops managing.
package managed_nodes

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

// Serial: the config is a cluster-wide singleton under a name the controller fixes, and
// excluding a node takes it out of whatever pool another suite might be using.
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

			// Managed means matching the inclusion criteria, so excluding this node is
			// expressed as including every node that does not carry its label.
			config = resources.ManagedNodesConfig(managedNodesConfigName,
				resources.WithoutNodeLabel(excludeLabelKey))
			Expect(testClient.Create(ctx, config)).To(Succeed())

			DeferCleanup(func() {
				// Config first: while it stands, the node stays excluded and putting it
				// back in its pool would just be undone on the next reconcile.
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, config))).To(Succeed())
				wait.ForDeleted(ctx, testClient, config)

				Expect(nodes.RemoveLabel(ctx, testClient, nodeName, excludeLabelKey)).To(Succeed())

				// Leaving the node parked in the excluded pool would strand it for every
				// suite that runs after this one.
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
			// True with the reason below means no node still needs draining before it can
			// be excluded, which is the same thing as the move above having completed.
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
