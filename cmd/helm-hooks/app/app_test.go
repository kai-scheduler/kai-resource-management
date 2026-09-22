// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"flag"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestApp(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Helm hooks app suite")
}

// unreachableClient fails the spec if a subcommand connects to a cluster; every case
// here is expected to be rejected while validating its arguments.
func unreachableClient() (client.Client, error) {
	return nil, errors.New("the cluster client should not have been created")
}

var _ = Describe("Subcommand arguments", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("requires --file for apply-config", func() {
		Expect(applyConfig(ctx, unreachableClient, nil)).
			To(MatchError(ContainSubstring("--file is required")))
	})

	It("requires --namespace for cleanup", func() {
		Expect(cleanup(ctx, unreachableClient, []string{"--delete-config=krm-config"})).
			To(MatchError(ContainSubstring("--namespace is required")))
	})

	It("rejects an unknown flag", func() {
		Expect(applyConfig(ctx, unreachableClient, []string{"--nonesuch=1"})).To(HaveOccurred())
	})

	It("rejects a positional argument the flag package would otherwise drop", func() {
		Expect(parseFlags(flag.NewFlagSet("apply-crds", flag.ContinueOnError), []string{"extra"})).
			To(MatchError(ContainSubstring("unexpected arguments")))
	})
})

var _ = Describe("Run", func() {
	It("reports a missing subcommand", func() {
		Expect(Run(nil)).To(MatchError(ContainSubstring("subcommand required")))
	})
})
