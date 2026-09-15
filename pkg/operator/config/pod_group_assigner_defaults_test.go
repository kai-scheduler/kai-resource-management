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
var _ = Describe("pod-group-assigner defaults", func() {
	var podGroupAssigner *krmv1alpha1.PodGroupAssigner

	BeforeEach(func() {
		spec := &krmv1alpha1.KRMConfigSpec{}
		SetDefaultsWhereNeeded(spec)
		podGroupAssigner = spec.PodGroupAssigner
	})

	It("publishes only a webhook port", func() {
		webhook := podGroupAssigner.ControllerService.Webhook
		Expect(webhook.Name).To(Equal(ptr.To("webhook")))
		Expect(webhook.Port).To(Equal(ptr.To(int32(443))))
		Expect(webhook.TargetPort).To(Equal(ptr.To(int32(8443))))
	})

	It("serves the pod webhook and mounts the chart's Secret", func() {
		Expect(podGroupAssigner.Webhooks.EnablePodWebhook).To(Equal(ptr.To(true)))
		Expect(podGroupAssigner.Webhooks.CertSecretName).To(Equal(ptr.To(PodGroupAssignerCertSecretName)))
	})

	// Unset means the flag is never passed, so the binary's own default applies.
	It("leaves every optional flag unset", func() {
		Expect(podGroupAssigner.Args.Debug).To(BeNil())
		Expect(podGroupAssigner.Args.LeaderElect).To(BeNil())
		Expect(podGroupAssigner.Args.UnexistingNodepoolSentinel).To(BeNil())
		Expect(podGroupAssigner.Args.AnnotationNodepoolsKey).To(BeNil())
		Expect(podGroupAssigner.ExtraArgs).To(BeEmpty())
	})

	It("keeps the pod webhook turned off when it is", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			PodGroupAssigner: &krmv1alpha1.PodGroupAssigner{
				Webhooks: &krmv1alpha1.PodGroupAssignerWebhooks{EnablePodWebhook: ptr.To(false)},
			},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.PodGroupAssigner.Webhooks.EnablePodWebhook).To(Equal(ptr.To(false)))
		Expect(spec.PodGroupAssigner.Webhooks.CertSecretName).To(
			Equal(ptr.To(PodGroupAssignerCertSecretName)))
	})
})
