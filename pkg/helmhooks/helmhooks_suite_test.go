// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHelmHooks(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Helm hooks suite")
}
