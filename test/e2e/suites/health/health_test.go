// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package health checks the installation is up before the other suites lean on
// it, so a broken install fails here and says why, rather than showing up as
// every other suite timing out on a wait.
package health

import (
	goctx "context"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("Installation", Label("health"), func() {
	var (
		ctx       goctx.Context
		k8sClient client.Client
	)

	BeforeEach(func() {
		ctx = goctx.Background()
		// Also the point at which preflight runs and can refuse.
		k8sClient = testcontext.GetConnectivity(ctx, Default).Client
	})

	Context("of the resource-management controllers", func() {
		It("has every controller available", func() {
			for _, name := range []string{
				"krm-operator", "project-controller", "nodepool-controller", "pod-group-assigner",
			} {
				deployment := &appsv1.Deployment{}
				Expect(k8sClient.Get(ctx,
					client.ObjectKey{Namespace: constant.ReleaseNamespace, Name: name}, deployment)).
					To(Succeed(), "deployment %q is missing", name)

				Expect(deployment.Status.AvailableReplicas).To(BeNumerically(">=", 1),
					"deployment %q has no available replica", name)
			}
		})
	})

	Context("of the default node nodePool the chart creates", func() {
		// The catch-all node pool. On a kind cluster its one node matches nothing
		// else, so the node pool is Ready rather than Empty.
		It("reconciles to Ready with a scheduling shard behind it", func() {
			nodePool := wait.ForNodePoolPhase(ctx, k8sClient,
				testcontext.DefaultNodePoolName, kaires.NodePoolReady)
			Expect(nodePool.Status.Nodes).ToNot(BeEmpty(), "the default node pool claimed no nodes")

			shard := wait.ForSchedulingShardReady(ctx, k8sClient, testcontext.DefaultNodePoolName)
			Expect(shard.OwnerReferences).To(ContainElement(HaveField("Kind", "NodePool")))
		})
	})
})
