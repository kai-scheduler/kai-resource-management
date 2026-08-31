// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package managed_nodes

import (
	goctx "context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
)

// The names nodepool-controller uses by default, and which this suite therefore cannot
// choose: the config it watches, the node pool it parks unmanaged nodes in, and the label
// marking a node that should be excluded but still has work on it.
const (
	managedNodesConfigName = "kai-managed-nodes-config"
	excludedNodePoolName   = "kai-excluded-nodes"
	toExcludeLabelKey      = "kai.scheduler/to-exclude"
)

// excludeLabelKey marks the node this suite takes out of the managed set. Its own key, so
// no other suite's leftover label can pull a node out from under it.
const excludeLabelKey = "kai.resources/e2e-exclude"

var (
	ctx        goctx.Context
	testClient client.Client
)

func TestManagedNodes(t *testing.T) {
	RegisterFailHandler(Fail)

	// Gomega's default is one second, shorter than a reconcile.
	SetDefaultEventuallyTimeout(constant.Timeout)
	SetDefaultEventuallyPollingInterval(constant.Interval)

	RunSpecs(t, "Managed Nodes Suite")
}

var _ = BeforeSuite(func() {
	ctx = goctx.Background()
	// Also the point at which preflight runs and can refuse.
	testClient = testcontext.GetConnectivity(ctx, Default).Client
})
