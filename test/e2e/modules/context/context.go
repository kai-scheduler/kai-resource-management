// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package context connects the e2e suites to a cluster and guards that cluster
// before any spec mutates it.
package context

import (
	goctx "context"

	"github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestContext is what a spec uses to talk to the cluster.
type TestContext struct {
	KubeConfig    *rest.Config
	KubeClientset *kubernetes.Clientset
	Client        client.Client
}

// GetConnectivity is the only way a suite obtains a client, so that the
// preflight guard cannot be bypassed by reaching for a client directly.
// Both connecting and the guard run once per process; later callers get the
// same verdict without re-listing the cluster.
func GetConnectivity(ctx goctx.Context, asserter gomega.Gomega) *TestContext {
	asserter.Expect(connect()).To(gomega.Succeed(), "connecting to the cluster")
	asserter.Expect(runPreflight(ctx, controllerClient)).To(gomega.Succeed(), "preflight")

	return &TestContext{
		KubeConfig:    kubeConfig,
		KubeClientset: kubeClientset,
		Client:        controllerClient,
	}
}
