// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"testing"

	kaiconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
)

// The suite baseline pins the runai worker-node labels, so specs that don't
// override the config assert the vendor vocabulary still flows into the shard.
const (
	runaiCPUWorkerNodeLabelKey = "node-role.kubernetes.io/runai-cpu-worker"
	runaiGPUWorkerNodeLabelKey = "node-role.kubernetes.io/runai-gpu-worker"
	runaiMIGWorkerNodeLabelKey = "node-role.kubernetes.io/runai-mig-enabled"
)

var suite = "KAI Scheduler Operand Resources"

func TestResources(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, suite)
}

// Seed the package-level config singleton with runai-vocabulary values so
// production code paths under test (which now read scheduler-name / metrics
// namespace / default-nodepool-name from config.Get()) exercise the runai
// variants by default. Specs that need KAI values override per-It via
// config.SetForTest(...) + DeferCleanup (see kai_config_test.go).
var _ = BeforeSuite(func() {
	config.SetCurrent(&config.NodePoolControllerConfig{
		NodePoolNameLabel:   kaiconstants.DefaultNodePoolLabelKey,
		DefaultNodepoolName: kaiconstants.DefaultNodePoolName,
		SchedulerName:       "runai-scheduler",
		SchedulerNamespace:  "runai",
		MetricsNamespace:    "runai",
		FinalizerDomain:     "run.ai",

		CPUWorkerNodeLabelKey: runaiCPUWorkerNodeLabelKey,
		GPUWorkerNodeLabelKey: runaiGPUWorkerNodeLabelKey,
		MIGWorkerNodeLabelKey: runaiMIGWorkerNodeLabelKey,
	})
})
