// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	goctx "context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
)

// nodePoolLabelKey is this suite's own key, so another suite's pool cannot match.
const nodePoolLabelKey = "kai.resources/e2e-upgrade"

var (
	ctx        goctx.Context
	testClient client.Client
)

func TestUpgrade(t *testing.T) {
	RegisterFailHandler(Fail)

	// Gomega's default is one second, shorter than a reconcile.
	SetDefaultEventuallyTimeout(constant.Timeout)
	SetDefaultEventuallyPollingInterval(constant.Interval)

	// Labelled here too, so an excluding filter also skips BeforeSuite.
	RunSpecs(t, "Upgrade Suite", Label("upgrade"))
}

var _ = BeforeSuite(func() {
	ctx = goctx.Background()

	// Also where preflight runs and can refuse.
	testClient = testcontext.GetConnectivity(ctx, Default).Client
})
