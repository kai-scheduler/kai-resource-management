// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
)

// NodePoolController configures the nodepool-controller service.
type NodePoolController struct {
	// Service is the common deployment configuration: enablement, image, resources.
	// +optional
	Service *kaicommon.Service `json:"service,omitempty"`

	// ControllerService describes the ports the controller listens on and the
	// Service publishes. Named to avoid colliding with Service above.
	// +optional
	ControllerService *NodePoolControllerService `json:"controllerService,omitempty"`

	// Webhooks configures the admission webhook the controller serves. The webhook
	// configuration itself and its TLS certificate stay with the Helm chart; this
	// toggle must agree with it, or the controller serves a webhook nothing calls,
	// or the API server calls one nothing serves.
	// +optional
	Webhooks *NodePoolControllerWebhooks `json:"webhooks,omitempty"`

	// Args holds the controller's own command line flags. An unset field means the
	// flag is not passed at all and the binary's own default applies.
	// +optional
	Args *NodePoolControllerArgs `json:"args,omitempty"`

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

// NodePoolControllerService describes the nodepool-controller's published ports.
type NodePoolControllerService struct {
	// Metrics is the Prometheus metrics port, and the one both ServiceMonitors scrape.
	// +optional
	Metrics *PortMapping `json:"metrics,omitempty"`

	// NodePoolMetrics is a second port the Service publishes, which no other KRM
	// service has. Published but not scraped by either ServiceMonitor.
	// +optional
	NodePoolMetrics *PortMapping `json:"nodePoolMetrics,omitempty"`

	// Webhook is the admission webhook port.
	// +optional
	Webhook *PortMapping `json:"webhook,omitempty"`
}

// NodePoolControllerWebhooks configures the controller's admission webhook.
type NodePoolControllerWebhooks struct {
	// EnableNodePoolValidation serves the NodePool validating webhook, which rejects
	// a NodePool created without a unique label key and value, and refuses to delete
	// the default node pool, which nothing recreates.
	//
	// The binary defaults this to true and would then start a TLS server with no
	// certificate mounted, so the operand always passes it explicitly.
	// +optional
	EnableNodePoolValidation *bool `json:"enableNodePoolValidation,omitempty"`

	// CertSecretName is the TLS Secret the controller mounts. The Helm chart mints
	// it, or on OpenShift the service-CA operator does.
	// +optional
	CertSecretName *string `json:"certSecretName,omitempty"`
}

// NodePoolControllerArgs are the nodepool-controller's own command line flags.
type NodePoolControllerArgs struct {
	// Debug raises the controller's log verbosity.
	// +optional
	Debug *bool `json:"debug,omitempty"`

	// LeaderElect overrides global.leaderElection for this service. Unset means
	// leader election follows global.leaderElection, or a replica count above one.
	// +optional
	LeaderElect *bool `json:"leaderElect,omitempty"`

	// MetricsNamespace prefixes every Prometheus metric the controller emits or
	// queries, including the queue-allocation metric names written into each
	// SchedulingShard.
	// +optional
	MetricsNamespace *string `json:"metricsNamespace,omitempty"`

	// DcgmExporterNamespace is the namespace of the DCGM exporter Service.
	// +optional
	DcgmExporterNamespace *string `json:"dcgmExporterNamespace,omitempty"`

	// SchedulingShardArgs is a JSON-encoded map of cluster-wide KAI scheduler args
	// merged into every SchedulingShard the controller writes. Empty means no
	// overrides, so each shard runs on the scheduler's own defaults.
	// +optional
	SchedulingShardArgs *string `json:"schedulingShardArgs,omitempty"`

	// ExcludedNodepoolName is the reserved node pool unmanaged nodes are moved into.
	// +optional
	ExcludedNodepoolName *string `json:"excludedNodepoolName,omitempty"`

	// ManagedNodesConfigName is the singleton ManagedNodesConfig the controller reconciles.
	// +optional
	ManagedNodesConfigName *string `json:"managedNodesConfigName,omitempty"`

	// ToExcludeLabel is the node label key marking a node pending graceful drain
	// before exclusion.
	// +optional
	ToExcludeLabel *string `json:"toExcludeLabel,omitempty"`

	// UnschedulableLabel is the node label key marking a node the controller made
	// unschedulable.
	// +optional
	UnschedulableLabel *string `json:"unschedulableLabel,omitempty"`

	// GroveTopologyAnnotation is the annotation key linking a topology to its Grove
	// topology.
	// +optional
	GroveTopologyAnnotation *string `json:"groveTopologyAnnotation,omitempty"`

	// GroveTopologyResourceVersionAnnotation is the annotation key tracking the
	// synced Grove topology resource version.
	// +optional
	GroveTopologyResourceVersionAnnotation *string `json:"groveTopologyResourceVersionAnnotation,omitempty"`
}
