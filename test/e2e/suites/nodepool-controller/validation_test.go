// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("The node pool webhook", Label("nodepool-controller"), func() {
	It("refuses a second pool selecting on a pair another pool already has", func() {
		first := resources.GeneratedNodePool("npc-pair", nodePoolLabelKey)
		Expect(testClient.Create(ctx, first)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, first))).To(Succeed())
			wait.ForDeleted(ctx, testClient, first)
		})

		duplicate := resources.NodePool(utils.GenerateName("npc-dup"),
			first.Spec.LabelKey, first.Spec.LabelValue)

		err := testClient.Create(ctx, duplicate)

		Expect(err).To(HaveOccurred(), "a duplicate label pair was accepted")
		Expect(err.Error()).To(ContainSubstring(first.Name),
			"the rejection should name the pool already holding the pair")
	})

	It("refuses a pool that selects on nothing", func() {
		poolWithoutPair := resources.NodePool(utils.GenerateName("npc-nopair"), "", "")

		Expect(testClient.Create(ctx, poolWithoutPair)).ToNot(Succeed())
	})

	It("refuses to let the default pool go", func() {
		defaultPool := &kaires.NodePool{}
		Expect(testClient.Get(ctx,
			client.ObjectKey{Name: testcontext.DefaultNodePoolName}, defaultPool)).To(Succeed())

		Expect(testClient.Delete(ctx, defaultPool)).ToNot(Succeed())
	})
})

var _ = Describe("The node pool CRD", Label("nodepool-controller"), func() {
	It("refuses to change labelKey or labelValue after creation", func() {
		nodePool := resources.GeneratedNodePool("npc-immutable", nodePoolLabelKey)
		Expect(testClient.Create(ctx, nodePool)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, nodePool)
		})

		// Re-read each time: a rejected update leaves the local copy dirty.
		changedKey := &kaires.NodePool{}
		Expect(testClient.Get(ctx, client.ObjectKey{Name: nodePool.Name}, changedKey)).To(Succeed())
		changedKey.Spec.LabelKey = nodePoolLabelKey + "-changed"

		Expect(testClient.Update(ctx, changedKey)).
			To(MatchError(ContainSubstring("immutable")), "labelKey was allowed to change")

		changedValue := &kaires.NodePool{}
		Expect(testClient.Get(ctx, client.ObjectKey{Name: nodePool.Name}, changedValue)).To(Succeed())
		changedValue.Spec.LabelValue = nodePool.Spec.LabelValue + "-changed"

		Expect(testClient.Update(ctx, changedValue)).
			To(MatchError(ContainSubstring("immutable")), "labelValue was allowed to change")
	})
})
