// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package health

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
)

func TestHealth(t *testing.T) {
	RegisterFailHandler(Fail)

	SetDefaultEventuallyTimeout(constant.Timeout)
	SetDefaultEventuallyPollingInterval(constant.Interval)

	RunSpecs(t, "Health Suite")
}
