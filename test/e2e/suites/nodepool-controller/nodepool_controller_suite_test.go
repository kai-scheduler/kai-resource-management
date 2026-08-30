// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	goctx "context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
)

// nodePoolLabelKey is the node label this suite's node pools select on. It is the
// suite's own key rather than a shared one, so a label another suite left on a node
// can never put that node into one of these pools.
const nodePoolLabelKey = "kai.resources/e2e-nodepool-controller"

var (
	ctx        goctx.Context
	testClient client.Client
)

func TestNodePoolController(t *testing.T) {
	RegisterFailHandler(Fail)

	// Gomega's default is one second, shorter than a reconcile.
	SetDefaultEventuallyTimeout(constant.Timeout)
	SetDefaultEventuallyPollingInterval(constant.Interval)

	RunSpecs(t, "NodePool Controller Suite")
}

var _ = BeforeSuite(func() {
	ctx = goctx.Background()
	// Also the point at which preflight runs and can refuse.
	testClient = testcontext.GetConnectivity(ctx, Default).Client
})
