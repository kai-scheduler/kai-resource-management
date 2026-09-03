// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	goctx "context"
	"fmt"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
)

const (
	// releaseName is what every harness naming this chart's release uses.
	releaseName = "krm"

	// helmTimeout covers the CRD hook, every operand and the post-upgrade hook Job.
	helmTimeout = "10m"

	// chartPathEnv is required: a silent skip would pass CI having tested nothing.
	chartPathEnv = "UPGRADE_CHART_PATH"

	// See upgradeRelease for why these cannot be left out.
	valuesFileEnv = "UPGRADE_VALUES_FILE"
	imageTagEnv   = "UPGRADE_IMAGE_TAG"
)

// chartPath requires an absolute path: ginkgo runs each suite from its own directory.
func chartPath() string {
	path := os.Getenv(chartPathEnv)
	Expect(path).ToNot(BeEmpty(), "%s must point at the chart to upgrade to", chartPathEnv)
	Expect(path).To(BeAnExistingFile(), "%s=%s does not exist", chartPathEnv, path)

	return path
}

// upgradeRelease re-passes the install's values explicitly: any -f or --set discards
// the previous ones, and passing none reuses image.tag so nothing would upgrade.
func upgradeRelease(ctx goctx.Context, path string) {
	args := []string{
		"upgrade", releaseName, path,
		"--namespace", constant.ReleaseNamespace,
	}

	if valuesFile := os.Getenv(valuesFileEnv); valuesFile != "" {
		Expect(valuesFile).To(BeAnExistingFile(), "%s=%s does not exist", valuesFileEnv, valuesFile)
		args = append(args, "--values", valuesFile)
	}

	if imageTag := os.Getenv(imageTagEnv); imageTag != "" {
		args = append(args, "--set", "image.tag="+imageTag)
	}

	args = append(args, "--wait", "--timeout", helmTimeout)

	By("running helm " + fmt.Sprint(args))

	// #nosec G702 -- the harness owning these arguments already holds the kubeconfig.
	output, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	fmt.Fprintln(GinkgoWriter, string(output))
	Expect(err).ToNot(HaveOccurred(), "helm upgrade failed: %s", string(output))
}
