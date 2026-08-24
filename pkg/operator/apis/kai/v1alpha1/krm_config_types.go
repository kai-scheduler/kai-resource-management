// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// TEMPORARY: these types belong in github.com/kai-scheduler/kai-resource-management-api
// alongside the other kai.resources kinds, and this repository normally defines no
// CRDs at all. They live here only while the shape of KRMConfig settles through
// review. Once it has, a follow-up moves them to kai/v1alpha1 in the API module and
// deletes this package, the generated CRD manifest under
// deployments/kai-resource-management-chart/templates/krm-operator/, and the
// gen-krmconfig Makefile target. See docs/updating-the-api-module.md.
//
// Package v1alpha1 contains the KRMConfig API type: the cluster-scoped, singleton
// resource the KRM operator reconciles.
// +kubebuilder:object:generate=true
// +groupName=kai.resources
package v1alpha1

import (
	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	// KRMConfigSingletonName is the only name the operator reconciles. A second
	// KRMConfig is ignored rather than merged, so two of them cannot fight.
	KRMConfigSingletonName = "krm-config"

	// KRMConfigKind is needed explicitly because a typed object read back from the
	// API server carries no TypeMeta, and owner references are built from the Kind.
	KRMConfigKind = "KRMConfig"
)

var (
	// GroupVersion is the group version used to register these objects.
	//
	// TEMPORARY: deliberately a local SchemeBuilder rather than the API module's, so
	// that removing this package is a plain delete. The group/version match the API
	// module on purpose — KRMConfig does not exist there yet, so no kind collides.
	GroupVersion = schema.GroupVersion{Group: "kai.resources", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=krmconfig
// +kubebuilder:storageversion

// KRMConfig is the Schema for the KAI Resource Management configuration API.
type KRMConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KRMConfigSpec   `json:"spec,omitempty"`
	Status KRMConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KRMConfigList contains a list of KRMConfig.
type KRMConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KRMConfig `json:"items"`
}

// KRMConfigSpec is the desired state of the KAI Resource Management installation.
//
// Every optional field is a pointer so that "unset" stays distinguishable from the
// zero value: the operator applies defaults imperatively on each reconcile rather
// than through a defaulting webhook, and it must not mistake an explicit false or 0
// for an absent field.
type KRMConfigSpec struct {
	// Namespace is where the operator deploys the KRM services. Defaults to the
	// namespace the operator itself runs in, which is the Helm release namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Global holds settings shared by every KRM service.
	// +optional
	Global *GlobalConfig `json:"global,omitempty"`

	// NodePoolController configures the nodepool-controller service.
	// +optional
	NodePoolController *NodePoolController `json:"nodePoolController,omitempty"`

	// ProjectController configures the project-controller service.
	// +optional
	ProjectController *ProjectController `json:"projectController,omitempty"`

	// PodGroupAssigner configures the pod-group-assigner service.
	// +optional
	PodGroupAssigner *PodGroupAssigner `json:"podGroupAssigner,omitempty"`
}

// FipsMode is the Go FIPS 140-3 run-time mode of a service, as GODEBUG=fips140.
// +kubebuilder:validation:Enum=off;on;only
type FipsMode string

const (
	// FipsModeOff runs the ordinary crypto paths even on a FIPS-built binary.
	FipsModeOff FipsMode = "off"
	// FipsModeOn serves approved algorithms from the validated module. Non-approved
	// algorithms keep working, outside the boundary. A FIPS build defaults to this.
	FipsModeOn FipsMode = "on"
	// FipsModeOnly additionally makes non-approved algorithms error or panic, so
	// nothing outside the validated boundary is reachable.
	FipsModeOnly FipsMode = "only"
)

// GlobalConfig holds the settings every KRM service inherits.
//
// SchedulerName, QueueLabelKey and NodePoolLabelKey must match the scheduler the
// services talk to, and nothing reconciles them against it. A value set here is
// passed to every service as a flag; left empty, no flag is passed and each
// service uses its own built-in default. Installed from this chart they are set
// from the scheduler it installs, so the two agree by construction; a KRMConfig
// created by anything else has to set them itself.
type GlobalConfig struct {
	// SchedulerName is the scheduler the controllers bind workloads to.
	//
	// Install-time only: set it at install and do not change it afterwards. The
	// scheduler and the controllers must agree on it, and nothing reconciles the
	// two — changing it on a running cluster stops workloads being scheduled until
	// both sides are brought back into line.
	// +optional
	SchedulerName *string `json:"schedulerName,omitempty"`

	// QueueLabelKey is the label key carrying a workload's queue.
	//
	// Install-time only, for the same reason as SchedulerName.
	// +optional
	QueueLabelKey *string `json:"queueLabelKey,omitempty"`

	// NodePoolLabelKey is the label key carrying a node's node pool.
	//
	// Install-time only, for the same reason as SchedulerName.
	// +optional
	NodePoolLabelKey *string `json:"nodePoolLabelKey,omitempty"`

	// DefaultNodePoolName is the node pool a workload falls back to.
	// +optional
	DefaultNodePoolName *string `json:"defaultNodePoolName,omitempty"`

	// FinalizerDomain is the domain prefix for finalizers the controllers set.
	// +optional
	FinalizerDomain *string `json:"finalizerDomain,omitempty"`

	// NamespaceProjectLabelKey is the label key linking a namespace to its project.
	// +optional
	NamespaceProjectLabelKey *string `json:"namespaceProjectLabelKey,omitempty"`

	// ProjectLabelKey is the label key carrying a workload's project.
	// +optional
	ProjectLabelKey *string `json:"projectLabelKey,omitempty"`

	// EnforceSchedulerAnnotationKey is the annotation key that forces a workload
	// onto the configured scheduler.
	// +optional
	EnforceSchedulerAnnotationKey *string `json:"enforceSchedulerAnnotationKey,omitempty"`

	// ReplicaCount is the default replica count for services that do not set their own.
	// +optional
	ReplicaCount *int32 `json:"replicaCount,omitempty"`

	// LeaderElection turns on leader election for every service. A replica count
	// above one turns it on regardless.
	// +optional
	LeaderElection *bool `json:"leaderElection,omitempty"`

	// ImagePullSecrets are added to every service pod.
	// +optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// NodeSelector is applied to every service pod.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations are applied to every service pod.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity is applied to every service pod that does not set its own.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// RequireDefaultPodAntiAffinityTerm makes the default per-host pod
	// anti-affinity required rather than preferred, for services that set no
	// anti-affinity of their own.
	// +optional
	RequireDefaultPodAntiAffinityTerm *bool `json:"requireDefaultPodAntiAffinityTerm,omitempty"`

	// SecurityContext is applied to every service container. Ignored on OpenShift,
	// where the platform assigns the UID range.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// PriorityClassName is applied to every service pod.
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Openshift forces OpenShift behavior instead of detecting it from the cluster.
	// +optional
	Openshift *bool `json:"openshift,omitempty"`

	// FipsMode sets the FIPS 140-3 run-time mode of every service. It does not
	// choose the image: a FIPS-built binary is a build-time property, and the chart
	// selects it by tag. Setting this against a non-FIPS image has no effect.
	// +optional
	FipsMode *FipsMode `json:"fipsMode,omitempty"`

	// VPA is the default Vertical Pod Autoscaler configuration for services that do
	// not set their own.
	// +optional
	VPA *kaicommon.VPASpec `json:"vpa,omitempty"`

	// ServiceMonitor configures Prometheus scraping of every service. Enabling it
	// without the Prometheus operator installed is not an error: the operator skips
	// the ServiceMonitor while its CRD is absent.
	// +optional
	ServiceMonitor *ServiceMonitorSpec `json:"serviceMonitor,omitempty"`
}

// ServiceMonitorSpec configures Prometheus scraping of the KRM services.
type ServiceMonitorSpec struct {
	// Enabled creates a ServiceMonitor for every service that exposes metrics.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Accounting additionally creates the nodepool-controller accounting
	// ServiceMonitor, labeled kai.scheduler/accounting=true. The node-to-nodepool
	// metrics are read by two Prometheuses, and that label routes a ServiceMonitor
	// to exactly one of them, so reaching both takes two. Ignored when Enabled is
	// false, and harmless where no accounting Prometheus is deployed.
	// +optional
	Accounting *bool `json:"accounting,omitempty"`
}

// PortMapping is one named port, published on Port and served on TargetPort.
type PortMapping struct {
	// Port is the port the Service publishes.
	// +optional
	Port *int32 `json:"port,omitempty"`

	// TargetPort is the port the container listens on.
	// +optional
	TargetPort *int32 `json:"targetPort,omitempty"`

	// Name is the port name, which a ServiceMonitor endpoint refers to.
	// +optional
	Name *string `json:"name,omitempty"`
}

// Profiling exposes a service's profiler API.
type Profiling struct {
	// Enabled starts the profiler API server.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// APIPort is the port the profiler API listens on.
	// +optional
	APIPort *int32 `json:"apiPort,omitempty"`
}

// KRMConfigStatus is the observed state of the installation.
type KRMConfigStatus struct {
	// Conditions report reconciliation progress and readiness.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ConditionType enumerates the conditions the operator reports.
type ConditionType string

const (
	// ConditionTypeReconciling is true while a reconcile is in flight.
	ConditionTypeReconciling ConditionType = "Reconciling"
	// ConditionTypeDeployed is true once every desired object exists.
	ConditionTypeDeployed ConditionType = "Deployed"
	// ConditionTypeAvailable is true once every deployed workload reports available.
	ConditionTypeAvailable ConditionType = "Available"
	// ConditionTypeDependenciesFulfilled is true when nothing the installation needs is missing.
	ConditionTypeDependenciesFulfilled ConditionType = "DependenciesFulfilled"
	// ConditionTypeReady summarizes the above.
	ConditionTypeReady ConditionType = "Ready"
)

// ConditionReason enumerates the reasons attached to the conditions above.
type ConditionReason string

const (
	ReasonDeployed              ConditionReason = "Deployed"
	ReasonNotDeployed           ConditionReason = "NotDeployed"
	ReasonAvailable             ConditionReason = "Available"
	ReasonNotAvailable          ConditionReason = "NotAvailable"
	ReasonReconciled            ConditionReason = "Reconciled"
	ReasonReconciling           ConditionReason = "Reconciling"
	ReasonReady                 ConditionReason = "Ready"
	ReasonNotReady              ConditionReason = "NotReady"
	ReasonDependenciesFulfilled ConditionReason = "DependenciesFulfilled"
	ReasonDependenciesMissing   ConditionReason = "DependenciesMissing"
)

// GetSecurityContext returns nil on OpenShift, which assigns the UID range itself
// and rejects a pod that pins one.
func (g *GlobalConfig) GetSecurityContext() *corev1.SecurityContext {
	if g.Openshift != nil && *g.Openshift {
		return nil
	}
	return g.SecurityContext
}

// GetConditions implements the status-reconciler's objectWithConditions contract.
func (c *KRMConfig) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions implements the status-reconciler's objectWithConditions contract.
func (c *KRMConfig) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&KRMConfig{}, &KRMConfigList{})
}
