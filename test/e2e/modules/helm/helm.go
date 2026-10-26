// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package helm runs the helm CLI for the suites that install, upgrade or uninstall a
// release themselves.
package helm

import (
	goctx "context"
	"fmt"
	"os/exec"

	"github.com/onsi/ginkgo/v2"
)

// Timeout covers the CRD hook, every operand and the hook Jobs around them.
const Timeout = "10m"

// Run returns the combined output with the error, so a spec expecting helm to fail
// can assert on why.
func Run(ctx goctx.Context, args ...string) (string, error) {
	ginkgo.By("running helm " + fmt.Sprint(args))

	// #nosec G204 -- the harness owning these arguments already holds the kubeconfig.
	output, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	fmt.Fprintln(ginkgo.GinkgoWriter, string(output))

	return string(output), err
}
