// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package flows

import (
	goctx "context"
	"testing"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/nodes"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// nodePoolLabelKey is the node label this suite's node pools select on. Each
// node pool takes a different value, so none collide on the webhook's rule.
const nodePoolLabelKey = "kai.resources/e2e-pool"

// Shared, and read-only to specs: every node pool gets a scheduler deployment of
// its own, so one per spec would cost a pod per spec. Projects and departments
// are cheap, so specs make their own.
var (
	ctx          goctx.Context
	testClient   client.Client
	testNodePool *kaires.NodePool
	testNodeName string
)

func TestFlows(t *testing.T) {
	RegisterFailHandler(Fail)

	// Gomega's default is one second, shorter than a reconcile.
	SetDefaultEventuallyTimeout(constant.Timeout)
	SetDefaultEventuallyPollingInterval(constant.Interval)

	RunSpecs(t, "Flows Suite")
}

var _ = BeforeSuite(func() {
	ctx = goctx.Background()
	testClient = testcontext.GetConnectivity(ctx, Default).Client

	testNodePool = resources.GeneratedNodePool("flows", nodePoolLabelKey)
	Expect(testClient.Create(ctx, testNodePool)).To(Succeed())

	var err error
	testNodeName, err = nodes.LabelWorker(ctx, testClient,
		testNodePool.Spec.LabelKey, testNodePool.Spec.LabelValue)
	Expect(err).ToNot(HaveOccurred())

	wait.ForNodePoolPhase(ctx, testClient, testNodePool.Name, kaires.NodePoolReady)
})

// The node has to leave the node pool before the pool can finish deleting.
var _ = AfterSuite(func() {
	if testClient == nil {
		return
	}

	if testNodeName != "" {
		Expect(nodes.RemoveLabel(ctx, testClient, testNodeName, nodePoolLabelKey)).To(Succeed())
	}

	if testNodePool != nil {
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, testNodePool))).To(Succeed())
	}
})
