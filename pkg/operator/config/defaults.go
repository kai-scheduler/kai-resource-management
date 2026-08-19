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

	// DefaultNodePoolName matches the chart's defaultNodePool.name, which also names
	// the NodePool the chart creates.
	DefaultNodePoolName = "default"

	// The values each service's own --qps/--burst flags default to.
	DefaultClientQPS   = 50
	DefaultClientBurst = 300
)

// Ports shared by every KRM service. A service needing its own port defines it
// beside its own defaults rather than changing these.
const (
	metricsPortName = "metrics"
	metricsPort     = 9400

	webhookPortName   = "webhook"
	webhookPort       = 443
	webhookTargetPort = 8443
)

// SetDefaultsWhereNeeded runs on every reconcile against an in-memory copy and is
// never written back, so the stored KRMConfig keeps showing only what was set.
func SetDefaultsWhereNeeded(spec *krmv1alpha1.KRMConfigSpec) {
	if len(spec.Namespace) == 0 {
		spec.Namespace = OperatorNamespace()
	}

	spec.Global = kaicommon.SetDefault(spec.Global, &krmv1alpha1.GlobalConfig{})
	setGlobalDefaults(spec.Global)

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

// The scheduler vocabulary is deliberately left unset: an unset field renders no
// flag, so each service keeps its own built-in default rather than being pinned
// to one the operator invented.
func setGlobalDefaults(global *krmv1alpha1.GlobalConfig) {
	global.ReplicaCount = kaicommon.SetDefault(global.ReplicaCount, ptr.To(int32(1)))
	global.Openshift = kaicommon.SetDefault(global.Openshift, ptr.To(false))
	global.FipsMode = kaicommon.SetDefault(global.FipsMode, ptr.To(krmv1alpha1.FipsModeOff))
	global.JSONLog = kaicommon.SetDefault(global.JSONLog, ptr.To(false))
	global.RequireDefaultPodAntiAffinityTerm = kaicommon.SetDefault(global.RequireDefaultPodAntiAffinityTerm, ptr.To(false))
	global.SecurityContext = kaicommon.SetDefault(global.SecurityContext, &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		RunAsNonRoot:             ptr.To(true),
		RunAsUser:                ptr.To(int64(10000)),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"all"}},
	})
	global.LeaderElection = kaicommon.SetDefault(global.LeaderElection, ptr.To(false))
	global.DefaultNodePoolName = kaicommon.SetDefault(global.DefaultNodePoolName, ptr.To(DefaultNodePoolName))
	global.VPA = kaicommon.SetDefault(global.VPA, &kaicommon.VPASpec{})
	global.VPA.SetDefaultsWhereNeeded()
	global.ServiceMonitor = kaicommon.SetDefault(global.ServiceMonitor, &krmv1alpha1.ServiceMonitorSpec{})
	global.ServiceMonitor.Enabled = kaicommon.SetDefault(global.ServiceMonitor.Enabled, ptr.To(true))
	global.ServiceMonitor.Accounting = kaicommon.SetDefault(global.ServiceMonitor.Accounting, ptr.To(true))
}

func setNodePoolControllerDefaults(
	nodePoolController *krmv1alpha1.NodePoolController, global *krmv1alpha1.GlobalConfig,
) {
	nodePoolController.Service = setServiceDefaults(
		nodePoolController.Service, NodePoolControllerImageName, nodePoolControllerResources())
	nodePoolController.Replicas = kaicommon.SetDefault(nodePoolController.Replicas, global.ReplicaCount)
	nodePoolController.VPA = kaicommon.SetDefault(nodePoolController.VPA, global.VPA)
}

func setPortMappingDefaults(
	portMapping *krmv1alpha1.PortMapping, name string, port, targetPort int32,
) *krmv1alpha1.PortMapping {
	portMapping = kaicommon.SetDefault(portMapping, &krmv1alpha1.PortMapping{})
	portMapping.Name = kaicommon.SetDefault(portMapping.Name, ptr.To(name))
	portMapping.Port = kaicommon.SetDefault(portMapping.Port, ptr.To(port))
	portMapping.TargetPort = kaicommon.SetDefault(portMapping.TargetPort, ptr.To(targetPort))
	return portMapping
}

// Set before SetDefaultsWhereNeeded, which only fills keys that are still absent.
func setServiceDefaults(
	service *kaicommon.Service, imageName string, defaultResources *kaicommon.Resources,
) *kaicommon.Service {
	service = kaicommon.SetDefault(service, &kaicommon.Service{})
	service.Resources = kaicommon.SetDefault(service.Resources, defaultResources)
	service.K8sClientConfig = kaicommon.SetDefault(service.K8sClientConfig, &kaicommon.K8sClientConfig{})
	service.K8sClientConfig.QPS = kaicommon.SetDefault(service.K8sClientConfig.QPS, ptr.To(DefaultClientQPS))
	service.K8sClientConfig.Burst = kaicommon.SetDefault(service.K8sClientConfig.Burst, ptr.To(DefaultClientBurst))
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
