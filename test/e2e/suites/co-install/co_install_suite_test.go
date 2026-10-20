// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package coinstall

import (
	goctx "context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
)

var (
	ctx        goctx.Context
	testClient client.Client
	// For what the controller-runtime client cannot do: pod logs and the service proxy.
	clientset *kubernetes.Clientset
	env       environment
)

func TestCoInstall(t *testing.T) {
	RegisterFailHandler(Fail)

	// Gomega's default is one second, shorter than a reconcile.
	SetDefaultEventuallyTimeout(constant.Timeout)
	SetDefaultEventuallyPollingInterval(constant.Interval)

	// Labelled here too, so an excluding filter also skips BeforeSuite.
	RunSpecs(t, "Co-install Suite", Label("co-install"))
}

var _ = BeforeSuite(func() {
	ctx = goctx.Background()

	// Before connecting: a missing chart should fail the run before preflight touches the cluster.
	env = readEnvironment()

	// Also where preflight runs and can refuse.
	connectivity := testcontext.GetConnectivity(ctx, Default)
	testClient = connectivity.Client
	clientset = connectivity.KubeClientset
})
