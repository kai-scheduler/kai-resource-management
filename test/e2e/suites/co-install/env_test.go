// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package coinstall

import (
	"os"

	. "github.com/onsi/gomega"
)

const (
	// chartPathEnv and the two KAI variables are required: a silent skip would pass CI
	// having tested nothing.
	chartPathEnv  = "CO_INSTALL_CHART_PATH"
	kaiChartEnv   = "CO_INSTALL_KAI_CHART"
	kaiVersionEnv = "CO_INSTALL_KAI_VERSION"

	// Optional, as for the upgrade suite: the values file trims requests to fit kind, and
	// the tag selects the images the harness loaded.
	valuesFileEnv = "CO_INSTALL_VALUES_FILE"
	imageTagEnv   = "CO_INSTALL_IMAGE_TAG"
)

type environment struct {
	chartPath  string
	valuesFile string
	imageTag   string
	kaiChart   string
	kaiVersion string
}

// readEnvironment requires an absolute chart path: ginkgo runs each suite from its own directory.
func readEnvironment() environment {
	result := environment{
		chartPath:  os.Getenv(chartPathEnv),
		valuesFile: os.Getenv(valuesFileEnv),
		imageTag:   os.Getenv(imageTagEnv),
		kaiChart:   os.Getenv(kaiChartEnv),
		kaiVersion: os.Getenv(kaiVersionEnv),
	}

	Expect(result.chartPath).ToNot(BeEmpty(), "%s must point at the chart under test", chartPathEnv)
	Expect(result.chartPath).To(BeAnExistingFile(), "%s=%s does not exist", chartPathEnv, result.chartPath)
	Expect(result.kaiChart).ToNot(BeEmpty(), "%s must name the KAI Scheduler chart to install", kaiChartEnv)
	Expect(result.kaiVersion).ToNot(BeEmpty(), "%s must pin the KAI Scheduler chart version", kaiVersionEnv)

	if result.valuesFile != "" {
		Expect(result.valuesFile).To(BeAnExistingFile(), "%s=%s does not exist", valuesFileEnv, result.valuesFile)
	}

	return result
}
