// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
	"k8s.io/utils/ptr"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

// PodGroupAssignerCertSecretName is minted by the Helm chart, or on OpenShift by
// the service-CA operator from the Service's serving-cert annotation. The operand
// only mounts it.
const PodGroupAssignerCertSecretName = "pod-group-assigner-tls-secret"

func setPodGroupAssignerDefaults(
	podGroupAssigner *krmv1alpha1.PodGroupAssigner, global *krmv1alpha1.GlobalConfig,
) {
	podGroupAssigner.Service = setServiceDefaults(
		podGroupAssigner.Service, PodGroupAssignerImageName, podGroupAssignerResources())
	podGroupAssigner.Replicas = kaicommon.SetDefault(podGroupAssigner.Replicas, global.ReplicaCount)
	podGroupAssigner.VPA = kaicommon.SetDefault(podGroupAssigner.VPA, global.VPA)

	podGroupAssigner.ControllerService = kaicommon.SetDefault(
		podGroupAssigner.ControllerService, &krmv1alpha1.PodGroupAssignerService{})
	podGroupAssigner.ControllerService.Webhook = setPortMappingDefaults(
		podGroupAssigner.ControllerService.Webhook, webhookPortName, webhookPort, webhookTargetPort)

	podGroupAssigner.Webhooks = kaicommon.SetDefault(
		podGroupAssigner.Webhooks, &krmv1alpha1.PodGroupAssignerWebhooks{})
	podGroupAssigner.Webhooks.EnablePodWebhook = kaicommon.SetDefault(
		podGroupAssigner.Webhooks.EnablePodWebhook, ptr.To(true))
	podGroupAssigner.Webhooks.CertSecretName = kaicommon.SetDefault(
		podGroupAssigner.Webhooks.CertSecretName, ptr.To(PodGroupAssignerCertSecretName))

	podGroupAssigner.Args = kaicommon.SetDefault(podGroupAssigner.Args, &krmv1alpha1.PodGroupAssignerArgs{})
}
