// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProjectController configures the project-controller service.
type ProjectController struct {
	// Service is the common deployment configuration: enablement, image, resources.
	// +optional
	Service *kaicommon.Service `json:"service,omitempty"`

	// ControllerService describes the ports the controller listens on and the
	// Service publishes. Named to avoid colliding with Service above.
	// +optional
	ControllerService *ProjectControllerService `json:"controllerService,omitempty"`

	// Webhooks configures the admission webhooks the controller serves. The webhook
	// configurations themselves and their TLS certificates stay with the Helm chart;
	// these toggles must agree with it, or the controller serves a webhook nothing
	// calls, or mounts a certificate that was never minted.
	// +optional
	Webhooks *ProjectControllerWebhooks `json:"webhooks,omitempty"`

	// Features selects which per-project resources the controller manages. Each flag
	// also gates the matching chart ClusterRole and the RoleBinding replicated into
	// project namespaces, so turning one on here without the chart's matching grant
	// produces bindings to a ClusterRole that does not exist.
	// +optional
	Features *ProjectControllerFeatures `json:"features,omitempty"`

	// Profiling exposes the controller's profiler API.
	// +optional
	Profiling *Profiling `json:"profiling,omitempty"`

	// Args holds the controller's own command line flags. An unset field means the
	// flag is not passed at all and the binary's own default applies.
	// +optional
	Args *ProjectControllerArgs `json:"args,omitempty"`

	// ExtraArgs are appended verbatim after every other flag, so a flag repeated
	// here overrides the one built above it. The escape hatch for a flag this API
	// does not model yet.
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`

	// ExtraProjectRoleBindings are additional RoleBindings to replicate into every
	// project namespace, for components this installation does not own.
	// +optional
	ExtraProjectRoleBindings []ProjectRoleBinding `json:"extraProjectRoleBindings,omitempty"`

	// RoleBindingsConfigMapName names the ConfigMap this operator builds the
	// per-project RoleBindings into, and points the controller at.
	// +optional
	RoleBindingsConfigMapName *string `json:"roleBindingsConfigMapName,omitempty"`

	// DeleteBlockers are the resources whose presence blocks deleting a project.
	// Empty means nothing blocks deletion.
	// +optional
	DeleteBlockers []DeleteBlocker `json:"deleteBlockers,omitempty"`

	// Replicas overrides global.replicaCount for this service.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// VPA overrides global.vpa for this service.
	// +optional
	VPA *kaicommon.VPASpec `json:"vpa,omitempty"`
}

// ProjectRoleBinding is a RoleBinding the project-controller replicates into every
// namespace it creates for a project.
type ProjectRoleBinding struct {
	// Name of the RoleBinding created in each project namespace.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// ClusterRoleName is the ClusterRole to bind, which must already exist.
	// Defaults to Name.
	// +optional
	ClusterRoleName string `json:"clusterRoleName,omitempty"`

	// ServiceAccountName is the subject, taken from the installation namespace.
	// +kubebuilder:validation:MinLength=1
	ServiceAccountName string `json:"serviceAccountName"`
}

// ProjectControllerService describes the project-controller's published ports.
type ProjectControllerService struct {
	// Metrics is the Prometheus metrics port.
	// +optional
	Metrics *PortMapping `json:"metrics,omitempty"`

	// Webhook is the admission webhook port.
	// +optional
	Webhook *PortMapping `json:"webhook,omitempty"`
}

// ProjectControllerWebhooks configures the controller's admission webhooks.
type ProjectControllerWebhooks struct {
	// EnableProjectValidation serves the Project validating webhook.
	// +optional
	EnableProjectValidation *bool `json:"enableProjectValidation,omitempty"`

	// EnableDepartmentValidation serves the Department validating webhook.
	// +optional
	EnableDepartmentValidation *bool `json:"enableDepartmentValidation,omitempty"`

	// CertSecretName is the TLS Secret the controller mounts. The Helm chart mints
	// it, or on OpenShift the service-CA operator does.
	// +optional
	CertSecretName *string `json:"certSecretName,omitempty"`
}

// ProjectControllerFeatures selects which per-project resources the controller manages.
type ProjectControllerFeatures struct {
	// CreateNamespaces creates a namespace for every project.
	// +optional
	CreateNamespaces *bool `json:"createNamespaces,omitempty"`

	// CreateRoleBindings replicates the rolebindings plugin entries into project namespaces.
	// +optional
	CreateRoleBindings *bool `json:"createRoleBindings,omitempty"`

	// ClusterWideSecret replicates cluster-wide Secrets into project namespaces.
	// +optional
	ClusterWideSecret *bool `json:"clusterWideSecret,omitempty"`

	// ClusterWideConfigMap replicates cluster-wide ConfigMaps into project namespaces.
	// +optional
	ClusterWideConfigMap *bool `json:"clusterWideConfigMap,omitempty"`

	// ClusterWidePvc replicates cluster-wide PersistentVolumeClaims into project namespaces.
	// +optional
	ClusterWidePvc *bool `json:"clusterWidePvc,omitempty"`

	// LimitRange manages a LimitRange in every project namespace.
	// +optional
	LimitRange *bool `json:"limitRange,omitempty"`
}

// ProjectControllerArgs are the project-controller's own command line flags.
type ProjectControllerArgs struct {
	// Debug raises the controller's log verbosity.
	// +optional
	Debug *bool `json:"debug,omitempty"`

	// LeaderElect overrides global.leaderElection for this service. Unset means
	// leader election follows global.leaderElection, or a replica count above one.
	// +optional
	LeaderElect *bool `json:"leaderElect,omitempty"`

	// ProjectNamePrefix prefixes the namespaces created for projects.
	// +optional
	ProjectNamePrefix *string `json:"projectNamePrefix,omitempty"`

	// ProjectIDLabelKey is the label key carrying a project's ID.
	// +optional
	ProjectIDLabelKey *string `json:"projectIdLabelKey,omitempty"`

	// QueueDepartmentNameLabelKey is the label key linking a queue to its department.
	// +optional
	QueueDepartmentNameLabelKey *string `json:"queueDepartmentNameLabelKey,omitempty"`

	// NamespaceVersionLabelKey is the label key carrying a project namespace's version.
	// +optional
	NamespaceVersionLabelKey *string `json:"namespaceVersionLabelKey,omitempty"`

	// ResourceManualOverrideLabelKey is the label key marking a resource as
	// manually overridden, which stops the controller reconciling it.
	// +optional
	ResourceManualOverrideLabelKey *string `json:"resourceManualOverrideLabelKey,omitempty"`

	// LimitRangeName is the name of the LimitRange managed in project namespaces.
	// +optional
	LimitRangeName *string `json:"limitRangeName,omitempty"`
}

// DeleteBlocker is one resource kind whose presence in a project's namespaces
// blocks deleting that project. Blockers sharing a display name are grouped by the
// controller into a single project condition.
//
// The field names are a wire contract: the project-controller parses this out of a
// ConfigMap.
type DeleteBlocker struct {
	// DisplayName is what the project's blocked condition reports.
	DisplayName string `json:"displayName"`

	// Group of the blocking resource; empty for core.
	// +optional
	Group string `json:"group,omitempty"`

	// Version of the blocking resource.
	Version string `json:"version"`

	// Kind of the blocking resource.
	Kind string `json:"kind"`

	// LabelSelector narrows which objects of that kind block deletion.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}
