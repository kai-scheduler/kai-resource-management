// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
	"k8s.io/utils/ptr"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

const (
	// NodePoolControllerCertSecretName is minted by the Helm chart, or on OpenShift by
	// the service-CA operator from the Service's serving-cert annotation. The operand
	// only mounts it.
	NodePoolControllerCertSecretName = "nodepool-controller-tls-secret"

	// The node-pool metrics port is this service's alone, so it lives here rather
	// than in the ports every KRM service shares.
	nodePoolMetricsPortName   = "np-metrics"
	nodePoolMetricsPort       = 8080
	nodePoolMetricsTargetPort = 8080
)

func setNodePoolControllerDefaults(
	nodePoolController *krmv1alpha1.NodePoolController, global *krmv1alpha1.GlobalConfig,
) {
	nodePoolController.Service = setServiceDefaults(
		nodePoolController.Service, NodePoolControllerImageName, nodePoolControllerResources())
	nodePoolController.Replicas = kaicommon.SetDefault(nodePoolController.Replicas, global.ReplicaCount)
	nodePoolController.VPA = kaicommon.SetDefault(nodePoolController.VPA, global.VPA)

	nodePoolController.ControllerService = kaicommon.SetDefault(
		nodePoolController.ControllerService, &krmv1alpha1.NodePoolControllerService{})
	nodePoolController.ControllerService.Metrics = setPortMappingDefaults(
		nodePoolController.ControllerService.Metrics, metricsPortName, metricsPort, metricsPort)
	nodePoolController.ControllerService.NodePoolMetrics = setPortMappingDefaults(
		nodePoolController.ControllerService.NodePoolMetrics,
		nodePoolMetricsPortName, nodePoolMetricsPort, nodePoolMetricsTargetPort)
	nodePoolController.ControllerService.Webhook = setPortMappingDefaults(
		nodePoolController.ControllerService.Webhook, webhookPortName, webhookPort, webhookTargetPort)

	nodePoolController.Webhooks = kaicommon.SetDefault(
		nodePoolController.Webhooks, &krmv1alpha1.NodePoolControllerWebhooks{})
	setNodePoolControllerWebhookDefaults(nodePoolController.Webhooks)

	nodePoolController.Args = kaicommon.SetDefault(nodePoolController.Args, &krmv1alpha1.NodePoolControllerArgs{})
}

// On by default, matching the binary's own default for the same flag and the chart,
// which renders the ValidatingWebhookConfiguration from the same value. A webhook
// configured there but not served here fails closed and blocks every NodePool.
func setNodePoolControllerWebhookDefaults(webhooks *krmv1alpha1.NodePoolControllerWebhooks) {
	webhooks.EnableNodePoolValidation = kaicommon.SetDefault(webhooks.EnableNodePoolValidation, ptr.To(true))
	webhooks.CertSecretName = kaicommon.SetDefault(
		webhooks.CertSecretName, ptr.To(NodePoolControllerCertSecretName))
}
