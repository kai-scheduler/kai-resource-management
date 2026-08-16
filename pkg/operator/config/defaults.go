// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package config defaults a KRMConfig before the operator acts on it.
//
// These are functions rather than methods on the API types on purpose: the types
// move to the API module, but the defaults are deployment policy — image names
// track the chart's SERVICE_NAMES, resources track its values — and stay here, on
// this repository's release cadence.
package config

import (
	"os"

	"github.com/kai-scheduler/api/constants"
	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

const (
	// podNamespaceEnvVar is set on the operator Deployment from the downward API.
	podNamespaceEnvVar = "POD_NAMESPACE"

	// FallbackNamespace applies only when the operator runs outside a cluster,
	// such as in tests, where the downward API env var is absent.
	FallbackNamespace = "kai-resource-management"

	NodePoolControllerImageName = "nodepool-controller"
	ProjectControllerImageName  = "project-controller"
	PodGroupAssignerImageName   = "pod-group-assigner"
)

// SetDefaultsWhereNeeded runs on every reconcile against an in-memory copy and is
// never written back, so the stored KRMConfig keeps showing only what was set.
func SetDefaultsWhereNeeded(spec *krmv1alpha1.KRMConfigSpec) {
	if len(spec.Namespace) == 0 {
		spec.Namespace = OperatorNamespace()
	}

	spec.Global = kaicommon.SetDefault(spec.Global, &krmv1alpha1.GlobalConfig{})
	setGlobalDefaults(spec.Global)

	spec.SchedulerConfigRef = kaicommon.SetDefault(spec.SchedulerConfigRef, &krmv1alpha1.SchedulerConfigRef{})
	setSchedulerConfigRefDefaults(spec.SchedulerConfigRef)

	spec.NodePoolController = kaicommon.SetDefault(spec.NodePoolController, &krmv1alpha1.NodePoolController{})
	setNodePoolControllerDefaults(spec.NodePoolController, spec.Global)

	spec.ProjectController = kaicommon.SetDefault(spec.ProjectController, &krmv1alpha1.ProjectController{})
	setProjectControllerDefaults(spec.ProjectController, spec.Global)

	spec.PodGroupAssigner = kaicommon.SetDefault(spec.PodGroupAssigner, &krmv1alpha1.PodGroupAssigner{})
	setPodGroupAssignerDefaults(spec.PodGroupAssigner, spec.Global)
}

// OperatorNamespace reports the namespace the operator is running in, which is
// the Helm release namespace. Reading it from the pod is what keeps the services
// beside the operator whichever namespace the chart was installed into; a
// hard-coded default would send them somewhere that need not even exist.
func OperatorNamespace() string {
	if namespace := os.Getenv(podNamespaceEnvVar); namespace != "" {
		return namespace
	}
	return FallbackNamespace
}

func setSchedulerConfigRefDefaults(ref *krmv1alpha1.SchedulerConfigRef) {
	if len(ref.Name) == 0 {
		ref.Name = constants.DefaultKAIConfigSingeltonInstanceName
	}
}

// The scheduler vocabulary is deliberately left unset: empty means "inherit from
// the referenced kai-config", which the operator resolves at reconcile time.
func setGlobalDefaults(global *krmv1alpha1.GlobalConfig) {
	global.ReplicaCount = kaicommon.SetDefault(global.ReplicaCount, ptr.To(int32(1)))
	global.Openshift = kaicommon.SetDefault(global.Openshift, ptr.To(false))
	global.Fips = kaicommon.SetDefault(global.Fips, ptr.To(false))
	global.JSONLog = kaicommon.SetDefault(global.JSONLog, ptr.To(false))
	global.RequireDefaultPodAntiAffinityTerm = kaicommon.SetDefault(global.RequireDefaultPodAntiAffinityTerm, ptr.To(false))
	global.SecurityContext = kaicommon.SetDefault(global.SecurityContext, &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		RunAsNonRoot:             ptr.To(true),
		RunAsUser:                ptr.To(int64(10000)),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"all"}},
	})
	global.VPA = kaicommon.SetDefault(global.VPA, &kaicommon.VPASpec{})
	global.VPA.SetDefaultsWhereNeeded()
}

func setNodePoolControllerDefaults(
	nodePoolController *krmv1alpha1.NodePoolController, global *krmv1alpha1.GlobalConfig,
) {
	nodePoolController.Service = setServiceDefaults(
		nodePoolController.Service, NodePoolControllerImageName, nodePoolControllerResources())
	nodePoolController.Replicas = kaicommon.SetDefault(nodePoolController.Replicas, global.ReplicaCount)
	nodePoolController.VPA = kaicommon.SetDefault(nodePoolController.VPA, global.VPA)
}

func setProjectControllerDefaults(
	projectController *krmv1alpha1.ProjectController, global *krmv1alpha1.GlobalConfig,
) {
	projectController.Service = setServiceDefaults(
		projectController.Service, ProjectControllerImageName, projectControllerResources())
	projectController.Replicas = kaicommon.SetDefault(projectController.Replicas, global.ReplicaCount)
	projectController.VPA = kaicommon.SetDefault(projectController.VPA, global.VPA)
}

func setPodGroupAssignerDefaults(
	podGroupAssigner *krmv1alpha1.PodGroupAssigner, global *krmv1alpha1.GlobalConfig,
) {
	podGroupAssigner.Service = setServiceDefaults(
		podGroupAssigner.Service, PodGroupAssignerImageName, podGroupAssignerResources())
	podGroupAssigner.Replicas = kaicommon.SetDefault(podGroupAssigner.Replicas, global.ReplicaCount)
	podGroupAssigner.VPA = kaicommon.SetDefault(podGroupAssigner.VPA, global.VPA)
}

// Each service gets its own resource default rather than the generic one in
// kai/v1/common, which is far smaller than these services need — project-controller
// alone asks for four times its memory limit. Set before SetDefaultsWhereNeeded,
// which only fills the keys that are still absent.
func setServiceDefaults(
	service *kaicommon.Service, imageName string, defaultResources *kaicommon.Resources,
) *kaicommon.Service {
	service = kaicommon.SetDefault(service, &kaicommon.Service{})
	service.Resources = kaicommon.SetDefault(service.Resources, defaultResources)
	service.SetDefaultsWhereNeeded(imageName)
	return service
}

func nodePoolControllerResources() *kaicommon.Resources {
	return resources("900m", "1Gi", "450m", "512Mi")
}

func projectControllerResources() *kaicommon.Resources {
	return resources("300m", "2Gi", "150m", "1Gi")
}

func podGroupAssignerResources() *kaicommon.Resources {
	return resources("200m", "512Mi", "100m", "256Mi")
}

func resources(cpuLimit, memoryLimit, cpuRequest, memoryRequest string) *kaicommon.Resources {
	return &kaicommon.Resources{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    apiresource.MustParse(cpuLimit),
			corev1.ResourceMemory: apiresource.MustParse(memoryLimit),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    apiresource.MustParse(cpuRequest),
			corev1.ResourceMemory: apiresource.MustParse(memoryRequest),
		},
	}
}
