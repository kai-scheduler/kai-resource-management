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
