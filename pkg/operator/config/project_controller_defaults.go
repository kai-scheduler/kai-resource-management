// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	kaicommon "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"k8s.io/utils/ptr"
)

const (
	// ProjectControllerCertSecretName is minted by the Helm chart, or on OpenShift by
	// the service-CA operator from the Service's serving-cert annotation. The operand
	// only mounts it.
	ProjectControllerCertSecretName = "project-controller-tls-secret"

	projectControllerProfilerAPIPort = 8182
)

func setProjectControllerDefaults(
	projectController *krmv1alpha1.ProjectController, global *krmv1alpha1.GlobalConfig,
) {
	projectController.Service = setServiceDefaults(
		projectController.Service, ProjectControllerImageName, projectControllerResources())
	projectController.Replicas = kaicommon.SetDefault(projectController.Replicas, global.ReplicaCount)
	projectController.VPA = kaicommon.SetDefault(projectController.VPA, global.VPA)

	projectController.ControllerService = kaicommon.SetDefault(
		projectController.ControllerService, &krmv1alpha1.ProjectControllerService{})
	projectController.ControllerService.Metrics = setPortMappingDefaults(
		projectController.ControllerService.Metrics, metricsPortName, metricsPort, metricsPort)
	projectController.ControllerService.Webhook = setPortMappingDefaults(
		projectController.ControllerService.Webhook, webhookPortName, webhookPort, webhookTargetPort)

	projectController.Webhooks = kaicommon.SetDefault(
		projectController.Webhooks, &krmv1alpha1.ProjectControllerWebhooks{})
	setProjectControllerWebhookDefaults(projectController.Webhooks)

	projectController.Features = kaicommon.SetDefault(
		projectController.Features, &krmv1alpha1.ProjectControllerFeatures{})
	setProjectControllerFeatureDefaults(projectController.Features)

	projectController.Profiling = kaicommon.SetDefault(projectController.Profiling, &krmv1alpha1.Profiling{})
	projectController.Profiling.Enabled = kaicommon.SetDefault(projectController.Profiling.Enabled, ptr.To(false))
	projectController.Profiling.APIPort = kaicommon.SetDefault(
		projectController.Profiling.APIPort, ptr.To(int32(projectControllerProfilerAPIPort)))

	projectController.Args = kaicommon.SetDefault(projectController.Args, &krmv1alpha1.ProjectControllerArgs{})
}

// Both webhooks are on by default, and must agree with the webhook configurations
// the chart renders: one served here but not configured there is never called, and
// one configured there but not served here fails closed and blocks the resource it
// guards.
func setProjectControllerWebhookDefaults(webhooks *krmv1alpha1.ProjectControllerWebhooks) {
	webhooks.EnableProjectValidation = kaicommon.SetDefault(webhooks.EnableProjectValidation, ptr.To(true))
	webhooks.EnableDepartmentValidation = kaicommon.SetDefault(webhooks.EnableDepartmentValidation, ptr.To(true))
	webhooks.CertSecretName = kaicommon.SetDefault(
		webhooks.CertSecretName, ptr.To(ProjectControllerCertSecretName))
}

// Each default matches the binary's own default for the same flag, so running the
// controller standalone and running it under the operator behave alike. Limit ranges
// stay off because they make the controller own another object in every project
// namespace.
func setProjectControllerFeatureDefaults(features *krmv1alpha1.ProjectControllerFeatures) {
	features.CreateNamespaces = kaicommon.SetDefault(features.CreateNamespaces, ptr.To(true))
	features.CreateRoleBindings = kaicommon.SetDefault(features.CreateRoleBindings, ptr.To(true))
	features.LimitRange = kaicommon.SetDefault(features.LimitRange, ptr.To(false))
}
