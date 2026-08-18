// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
)

// PodGroupAssigner configures the pod-group-assigner service.
type PodGroupAssigner struct {
	// Service is the common deployment configuration: enablement, image, resources.
	// +optional
	Service *kaicommon.Service `json:"service,omitempty"`

	// ControllerService describes the ports the service listens on and the Service
	// publishes. Named to avoid colliding with Service above.
	// +optional
	ControllerService *PodGroupAssignerService `json:"controllerService,omitempty"`

	// Webhooks configures the admission webhooks the service serves. The webhook
	// configurations themselves and their TLS certificates stay with the Helm chart.
	// +optional
	Webhooks *PodGroupAssignerWebhooks `json:"webhooks,omitempty"`

	// Args holds the service's own command line flags. An unset field means the
	// flag is not passed at all and the binary's own default applies.
	// +optional
	Args *PodGroupAssignerArgs `json:"args,omitempty"`

	// ExtraArgs are appended verbatim after every other flag, so a flag repeated
	// here overrides the one built above it. The escape hatch for a flag this API
	// does not model yet.
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// Replicas overrides global.replicaCount for this service.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// VPA overrides global.vpa for this service.
	// +optional
	VPA *kaicommon.VPASpec `json:"vpa,omitempty"`
}

// PodGroupAssignerService describes the pod-group-assigner's published ports. It
// exposes no metrics, so the webhook is the only one.
type PodGroupAssignerService struct {
	// Webhook is the admission webhook port.
	// +optional
	Webhook *PortMapping `json:"webhook,omitempty"`
}

// PodGroupAssignerWebhooks configures the service's admission webhooks.
//
// The PodGroup mutating webhook has no toggle: the binary registers its handler
// unconditionally, so the TLS server always runs and the certificate is always
// required.
type PodGroupAssignerWebhooks struct {
	// EnablePodWebhook serves the Pod mutating webhook, which enforces the
	// scheduler name, labels a pod with its project, and applies the project's
	// default node pools as node affinity.
	// +optional
	EnablePodWebhook *bool `json:"enablePodWebhook,omitempty"`

	// CertSecretName is the TLS Secret the service mounts. The Helm chart mints
	// it, or on OpenShift the service-CA operator does.
	// +optional
	CertSecretName *string `json:"certSecretName,omitempty"`
}

// PodGroupAssignerArgs are the pod-group-assigner's own command line flags.
type PodGroupAssignerArgs struct {
	// Debug raises the service's log verbosity.
	// +optional
	Debug *bool `json:"debug,omitempty"`

	// LeaderElect overrides global.leaderElection for this service. Unset means
	// leader election follows global.leaderElection, or a replica count above one.
	// +optional
	LeaderElect *bool `json:"leaderElect,omitempty"`

	// UnexistingNodepoolSentinel is the label value marking a PodGroup as not yet
	// assigned to any node pool.
	// +optional
	UnexistingNodepoolSentinel *string `json:"unexistingNodepoolSentinel,omitempty"`

	// AnnotationNodepoolsKey is the Pod annotation whose value lists the node pools
	// explicitly requested for it.
	// +optional
	AnnotationNodepoolsKey *string `json:"annotationNodepoolsKey,omitempty"`
}
