// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"flag"
	"testing"

	kaiconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodePool Controller Config Suite")
}

var _ = Describe("AddLabelFlags", func() {
	var cfg *NodePoolControllerConfig

	BeforeEach(func() {
		cfg = &NodePoolControllerConfig{}
	})

	Context("when the binary is started without any vocabulary flags", func() {
		It("defaults the worker-node label keys to the KAI vocabulary", func() {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			AddLabelFlags(fs, cfg)
			Expect(fs.Parse(nil)).To(Succeed())

			Expect(cfg.CPUWorkerNodeLabelKey).To(Equal(kaiconstants.DefaultCPUWorkerNodeLabelKey))
			Expect(cfg.GPUWorkerNodeLabelKey).To(Equal(kaiconstants.DefaultGPUWorkerNodeLabelKey))
			Expect(cfg.MIGWorkerNodeLabelKey).To(Equal(kaiconstants.DefaultMIGWorkerNodeLabelKey))
		})
	})

	Context("when a vendor vocabulary is supplied", func() {
		It("carries the overridden worker-node label keys", func() {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			AddLabelFlags(fs, cfg)
			Expect(fs.Parse([]string{
				"--cpu-worker-node-label-key=node-role.kubernetes.io/runai-cpu-worker",
				"--gpu-worker-node-label-key=node-role.kubernetes.io/runai-gpu-worker",
				"--mig-worker-node-label-key=node-role.kubernetes.io/runai-mig-enabled",
			})).To(Succeed())

			Expect(cfg.CPUWorkerNodeLabelKey).To(Equal("node-role.kubernetes.io/runai-cpu-worker"))
			Expect(cfg.GPUWorkerNodeLabelKey).To(Equal("node-role.kubernetes.io/runai-gpu-worker"))
			Expect(cfg.MIGWorkerNodeLabelKey).To(Equal("node-role.kubernetes.io/runai-mig-enabled"))
		})
	})
})
