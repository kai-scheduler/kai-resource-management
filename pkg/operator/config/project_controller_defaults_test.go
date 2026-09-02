// Copyright 2026 NVIDIA CORPORATION
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
var _ = Describe("project-controller defaults", func() {
	var projectController *krmv1alpha1.ProjectController

	BeforeEach(func() {
		spec := &krmv1alpha1.KRMConfigSpec{}
		SetDefaultsWhereNeeded(spec)
		projectController = spec.ProjectController
	})

	It("publishes the metrics and webhook ports", func() {
		metrics := projectController.ControllerService.Metrics
		Expect(metrics.Name).To(Equal(ptr.To("metrics")))
		Expect(metrics.Port).To(Equal(ptr.To(int32(9400))))
		Expect(metrics.TargetPort).To(Equal(ptr.To(int32(9400))))

		webhook := projectController.ControllerService.Webhook
		Expect(webhook.Name).To(Equal(ptr.To("webhook")))
		Expect(webhook.Port).To(Equal(ptr.To(int32(443))))
		Expect(webhook.TargetPort).To(Equal(ptr.To(int32(8443))))
	})

	It("serves both validating webhooks and mounts the chart's Secret", func() {
		Expect(projectController.Webhooks.EnableProjectValidation).To(Equal(ptr.To(true)))
		Expect(projectController.Webhooks.EnableDepartmentValidation).To(Equal(ptr.To(true)))
		Expect(projectController.Webhooks.CertSecretName).To(Equal(ptr.To(ProjectControllerCertSecretName)))
	})

	// Each of these matches the binary's default for the same flag.
	It("defaults every feature the way the binary does", func() {
		features := projectController.Features
		Expect(features.CreateNamespaces).To(Equal(ptr.To(true)))
		Expect(features.CreateRoleBindings).To(Equal(ptr.To(true)))
		Expect(features.LimitRange).To(Equal(ptr.To(false)))
	})

	It("configures the profiler without starting it", func() {
		Expect(projectController.Profiling.Enabled).To(Equal(ptr.To(false)))
		Expect(projectController.Profiling.APIPort).To(Equal(ptr.To(int32(8182))))
	})

	// Unset means the flag is never passed, so the binary's own default applies.
	It("leaves every optional flag unset", func() {
		Expect(projectController.Args.ProjectNamePrefix).To(BeNil())
		Expect(projectController.Args.LimitRangeName).To(BeNil())
		Expect(projectController.Args.LeaderElect).To(BeNil())
		Expect(projectController.ExtraArgs).To(BeEmpty())
		Expect(projectController.DeleteBlockers).To(BeEmpty())
	})

	It("keeps a feature that was explicitly turned off", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			ProjectController: &krmv1alpha1.ProjectController{
				Features: &krmv1alpha1.ProjectControllerFeatures{CreateRoleBindings: ptr.To(false)},
			},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.ProjectController.Features.CreateRoleBindings).To(Equal(ptr.To(false)))
		Expect(spec.ProjectController.Features.CreateNamespaces).To(Equal(ptr.To(true)))
	})

	It("defaults the shared settings the controller reads from global", func() {
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.Global.LeaderElection).To(Equal(ptr.To(false)))
		Expect(spec.Global.DefaultNodePoolName).To(Equal(ptr.To(DefaultNodePoolName)))
		Expect(spec.Global.ServiceMonitor.Enabled).To(Equal(ptr.To(true)))
	})
})
