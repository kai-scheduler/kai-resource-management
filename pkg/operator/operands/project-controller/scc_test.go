// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// sccTemplate grants the OpenShift SecurityContextConstraints. It is a Helm
// template, so it names ServiceAccounts as text and nothing links the two.
const sccTemplate = "../../../../deployments/kai-resource-management-chart/templates/rbac/scc.yaml"

// The containers request uid 10000, which restricted-v2 rejects, so a ServiceAccount
// missing from the SCC means its pods never start on OpenShift. hack/scc-check.sh
// covers the accounts the chart renders; this operand's is created here instead, so
// rendering the chart cannot discover it and only a check from this side will do.
var _ = Describe("OpenShift SCC coverage", func() {
	It("grants the SCC to the ServiceAccount this operand creates", func() {
		scc, err := os.ReadFile(sccTemplate)
		Expect(err).ToNot(HaveOccurred())

		Expect(string(scc)).To(ContainSubstring(":"+defaultResourceName+"\n"),
			"add %s to the SCC users in %s", defaultResourceName, sccTemplate)
		Expect(strings.Count(string(scc), defaultResourceName)).To(BeNumerically(">=", 2),
			"%s needs both an SCC user entry and a ClusterRoleBinding subject", defaultResourceName)
	})
})
