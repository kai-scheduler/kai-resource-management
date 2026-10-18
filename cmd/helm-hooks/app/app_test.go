// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"flag"
	"testing"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

	It("strips the NodePool finalizer composed from --finalizer-domain", func() {
		nodePool := &kaires.NodePool{ObjectMeta: metav1.ObjectMeta{
			Name: "default", Finalizers: []string{"nodepool.example.com/finalize", "nodepool.kai.scheduler/finalize"}}}
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodePool).Build()

		Expect(cleanup(ctx, func() (client.Client, error) { return k8sClient, nil }, []string{
			"--namespace=kai-resource-management", "--strip-nodepool-finalizers", "--finalizer-domain=example.com",
		})).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, nodePool)).To(Succeed())
		Expect(nodePool.Finalizers).To(ConsistOf("nodepool.kai.scheduler/finalize"))
	})

	It("leaves NodePool finalizers alone without --strip-nodepool-finalizers", func() {
		nodePool := &kaires.NodePool{ObjectMeta: metav1.ObjectMeta{
			Name: "default", Finalizers: []string{"nodepool.kai.scheduler/finalize"}}}
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodePool).Build()

		Expect(cleanup(ctx, func() (client.Client, error) { return k8sClient, nil },
			[]string{"--namespace=kai-resource-management"})).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: "default"}, nodePool)).To(Succeed())
		Expect(nodePool.Finalizers).To(ConsistOf("nodepool.kai.scheduler/finalize"))
	})

	It("checks an explicit KAI Scheduler version without connecting to the cluster", func() {
		Expect(checkKAISchedulerVersion(ctx, unreachableClient,
			[]string{"--kai-scheduler-version=v99.0.0"})).To(Succeed())
		Expect(checkKAISchedulerVersion(ctx, unreachableClient,
			[]string{"--kai-scheduler-version=v0.1.0"})).To(MatchError(ContainSubstring("older than")))
	})

	It("checks against --minimum-version when given", func() {
		Expect(checkKAISchedulerVersion(ctx, unreachableClient,
			[]string{"--minimum-version=0.0.0", "--kai-scheduler-version=v0.1.0"})).To(Succeed())
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
