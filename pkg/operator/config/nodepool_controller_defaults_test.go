// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

// The defaults are the whole configuration of a stock installation, so each one is
// pinned: a change here silently changes every installation that sets nothing.
var _ = Describe("nodepool-controller defaults", func() {
	var nodePoolController *krmv1alpha1.NodePoolController

	BeforeEach(func() {
		spec := &krmv1alpha1.KRMConfigSpec{}
		SetDefaultsWhereNeeded(spec)
		nodePoolController = spec.NodePoolController
	})

	It("publishes the shared metrics and webhook ports", func() {
		metrics := nodePoolController.ControllerService.Metrics
		Expect(metrics.Name).To(Equal(ptr.To("metrics")))
		Expect(metrics.Port).To(Equal(ptr.To(int32(9400))))
		Expect(metrics.TargetPort).To(Equal(ptr.To(int32(9400))))

		webhook := nodePoolController.ControllerService.Webhook
		Expect(webhook.Name).To(Equal(ptr.To("webhook")))
		Expect(webhook.Port).To(Equal(ptr.To(int32(443))))
		Expect(webhook.TargetPort).To(Equal(ptr.To(int32(8443))))
	})

	// This port is this service's alone, which is why it is not one of the shared ones.
	It("publishes its own node-pool metrics port", func() {
		nodePoolMetrics := nodePoolController.ControllerService.NodePoolMetrics
		Expect(nodePoolMetrics.Name).To(Equal(ptr.To("np-metrics")))
		Expect(nodePoolMetrics.Port).To(Equal(ptr.To(int32(8080))))
		Expect(nodePoolMetrics.TargetPort).To(Equal(ptr.To(int32(8080))))
	})

	// True matches the binary's own default and the chart, which renders the
	// ValidatingWebhookConfiguration from the same value.
	It("serves the nodepool webhook and mounts the chart's Secret", func() {
		Expect(nodePoolController.Webhooks.EnableNodePoolValidation).To(Equal(ptr.To(true)))
		Expect(nodePoolController.Webhooks.CertSecretName).To(Equal(ptr.To(NodePoolControllerCertSecretName)))
	})

	It("takes its replica count and autoscaler from the global settings", func() {
		Expect(nodePoolController.Replicas).To(Equal(ptr.To(int32(1))))
		Expect(nodePoolController.VPA).ToNot(BeNil())
	})

	It("gives the client the shared qps and burst", func() {
		Expect(nodePoolController.Service.K8sClientConfig.QPS).To(Equal(ptr.To(DefaultClientQPS)))
		Expect(nodePoolController.Service.K8sClientConfig.Burst).To(Equal(ptr.To(DefaultClientBurst)))
	})

	// Unset means the flag is never passed, so the binary's own default applies.
	It("leaves every optional flag unset", func() {
		args := nodePoolController.Args
		Expect(args.Debug).To(BeNil())
		Expect(args.LeaderElect).To(BeNil())
		Expect(args.MetricsNamespace).To(BeNil())
		Expect(args.DcgmExporterNamespace).To(BeNil())
		Expect(args.SchedulingShardArgs).To(BeNil())
		Expect(args.ExcludedNodepoolName).To(BeNil())
		Expect(args.ManagedNodesConfigName).To(BeNil())
		Expect(args.ToExcludeLabel).To(BeNil())
		Expect(args.UnschedulableLabel).To(BeNil())
		Expect(args.GroveTopologyAnnotation).To(BeNil())
		Expect(args.GroveTopologyResourceVersionAnnotation).To(BeNil())
	})
})
