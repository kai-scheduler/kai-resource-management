// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"context"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

const (
	defaultResourceName = "project-controller"

	// certVolumeName is mounted from a Secret the Helm chart owns, or that the
	// OpenShift service-CA operator mints. The operand only references it by name.
	certVolumeName = "webhook-certs"
	certMountPath  = "/etc/webhook/certs"

	// openshiftServingCertAnnotation asks the OpenShift service-CA operator to mint
	// the serving certificate into the named Secret.
	openshiftServingCertAnnotation = "service.beta.openshift.io/serving-cert-secret-name"

	terminationGracePeriodSeconds = 10
)

func (p *ProjectController) deploymentForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	config := krmConfig.Spec.ProjectController

	deployment, err := common.DeploymentForKRMConfig(
		ctx, runtimeClient, krmConfig, config.Service, p.BaseResourceName)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Replicas = config.Replicas
	deployment.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr.To(int64(terminationGracePeriodSeconds))

	container := &deployment.Spec.Template.Spec.Containers[0]
	container.Args = buildArgsList(krmConfig)
	container.Ports = containerPorts(config)

	if webhooksEnabled(config.Webhooks) {
		container.VolumeMounts = []corev1.VolumeMount{
			{Name: certVolumeName, MountPath: certMountPath, ReadOnly: true},
		}
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: certVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: *config.Webhooks.CertSecretName},
				},
			},
		}
	}

	return deployment, nil
}

func (p *ProjectController) serviceAccountForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	return common.ServiceAccountForKRMConfig(ctx, runtimeClient, krmConfig, p.BaseResourceName)
}

func (p *ProjectController) serviceForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	config := krmConfig.Spec.ProjectController

	service, err := common.ServiceForKRMConfig(
		ctx, runtimeClient, krmConfig, p.BaseResourceName, servicePorts(config))
	if err != nil {
		return nil, err
	}

	// On OpenShift the platform mints the serving certificate, and this annotation
	// is the only thing that asks it to. Without it the Secret the Deployment mounts
	// is never created and the pod never starts.
	if webhooksEnabled(config.Webhooks) && ptr.Deref(krmConfig.Spec.Global.Openshift, false) {
		annotations := service.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[openshiftServingCertAnnotation] = *config.Webhooks.CertSecretName
		service.SetAnnotations(annotations)
	}

	return service, nil
}

func (p *ProjectController) serviceMonitorForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	serviceMonitor := krmConfig.Spec.Global.ServiceMonitor
	if serviceMonitor == nil || !ptr.Deref(serviceMonitor.Enabled, false) {
		return nil, nil
	}

	monitor, err := common.ServiceMonitorForKRMConfig(ctx, runtimeClient, krmConfig, p.BaseResourceName,
		*krmConfig.Spec.ProjectController.ControllerService.Metrics.Name, common.ServiceMonitorOptions{})
	// A typed nil would reach the caller as a non-nil client.Object.
	if err != nil || monitor == nil {
		return nil, err
	}
	return monitor, nil
}

// The container listens on the target ports; the Service publishes the others.
func containerPorts(config *krmv1alpha1.ProjectController) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{
			Name:          *config.ControllerService.Metrics.Name,
			ContainerPort: *config.ControllerService.Metrics.TargetPort,
		},
	}

	if webhooksEnabled(config.Webhooks) {
		ports = append(ports, corev1.ContainerPort{
			Name:          *config.ControllerService.Webhook.Name,
			ContainerPort: *config.ControllerService.Webhook.TargetPort,
		})
	}

	return ports
}

func servicePorts(config *krmv1alpha1.ProjectController) []corev1.ServicePort {
	metrics := config.ControllerService.Metrics
	ports := []corev1.ServicePort{
		{
			Name:       *metrics.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       *metrics.Port,
			TargetPort: intstr.FromInt32(*metrics.TargetPort),
		},
	}

	if webhooksEnabled(config.Webhooks) {
		webhook := config.ControllerService.Webhook
		ports = append(ports, corev1.ServicePort{
			Name:       *webhook.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       *webhook.Port,
			TargetPort: intstr.FromInt32(*webhook.TargetPort),
		})
	}

	return ports
}

// The certificate covers both webhooks, so it is wanted whenever either is on,
// matching the binary, which starts no TLS server when both are off.
func webhooksEnabled(webhooks *krmv1alpha1.ProjectControllerWebhooks) bool {
	return ptr.Deref(webhooks.EnableProjectValidation, false) ||
		ptr.Deref(webhooks.EnableDepartmentValidation, false)
}

// A flag left out entirely is what lets the binary's own default apply, so an
// unset field must add nothing rather than add an empty value.
func buildArgsList(krmConfig *krmv1alpha1.KRMConfig) []string {
	config := krmConfig.Spec.ProjectController
	global := krmConfig.Spec.Global
	namespace := krmConfig.Spec.Namespace

	args := []string{
		"--metrics-port", strconv.Itoa(int(*config.ControllerService.Metrics.TargetPort)),
		"--profiler-api-port", strconv.Itoa(int(*config.Profiling.APIPort)),
		"--rolebindings-configmap-name", roleBindingsConfigMapNameFor(config),
		"--rolebindings-configmap-namespace", namespace,
		"--project-delete-blockers-namespace", namespace,
		"--install-namespace", namespace,
		"--default-nodepool-name", *global.DefaultNodePoolName,
		// Spelled out either way: the flag also tells the binary whether to start
		// its TLS server, so omitting it is not the same as passing false.
		"--enable-project-validation-webhook=" +
			strconv.FormatBool(ptr.Deref(config.Webhooks.EnableProjectValidation, false)),
		"--enable-department-validation-webhook=" +
			strconv.FormatBool(ptr.Deref(config.Webhooks.EnableDepartmentValidation, false)),
	}

	if webhooksEnabled(config.Webhooks) {
		args = append(args, "--webhook-port", strconv.Itoa(int(*config.ControllerService.Webhook.TargetPort)))
	}

	args = appendStringArg(args, "--nodepool-label-key", global.NodePoolLabelKey)
	args = appendStringArg(args, "--queue-label-key", global.QueueLabelKey)
	args = appendStringArg(args, "--enforce-scheduler-annotation-key", global.EnforceSchedulerAnnotationKey)
	args = appendStringArg(args, "--finalizer-domain", global.FinalizerDomain)
	args = appendStringArg(args, "--namespace-project-label-key", global.NamespaceProjectLabelKey)
	args = appendStringArg(args, "--project-label-key", global.ProjectLabelKey)

	args = appendStringArg(args, "--project-name-prefix", config.Args.ProjectNamePrefix)
	args = appendStringArg(args, "--project-id-label-key", config.Args.ProjectIDLabelKey)
	args = appendStringArg(args, "--queue-department-name-label-key", config.Args.QueueDepartmentNameLabelKey)
	args = appendStringArg(args, "--namespace-version-label-key", config.Args.NamespaceVersionLabelKey)
	args = appendStringArg(args, "--resource-manual-override-label-key", config.Args.ResourceManualOverrideLabelKey)
	args = appendStringArg(args, "--limit-range-name", config.Args.LimitRangeName)

	args = appendSwitch(args, "--openshift", global.Openshift)
	args = appendSwitch(args, "--namespaces", config.Features.CreateNamespaces)
	args = appendSwitch(args, "--role-bindings", config.Features.CreateRoleBindings)
	args = appendSwitch(args, "--cluster-wide-secrets", config.Features.ClusterWideSecret)
	args = appendSwitch(args, "--cluster-wide-pvcs", config.Features.ClusterWidePvc)
	args = appendSwitch(args, "--cluster-wide-config-maps", config.Features.ClusterWideConfigMap)
	args = appendSwitch(args, "--limit-range", config.Features.LimitRange)
	args = appendSwitch(args, "--enable-profiling", config.Profiling.Enabled)
	args = appendSwitch(args, "--debug", config.Args.Debug)
	args = appendSwitch(args, "--leader-elect", ptr.To(leaderElect(config, global)))

	args = common.AddK8sClientConfigToArgs(config.Service.K8sClientConfig, args)

	// Last, so a flag repeated here wins over the one built above it.
	args = append(args, config.ExtraArgs...)

	return args
}

// Empty counts as unset: passing a flag with an empty value would override the
// binary's own default rather than leave it alone.
func appendStringArg(args []string, flag string, value *string) []string {
	if value == nil || *value == "" {
		return args
	}
	return append(args, flag, *value)
}

// A switch flag carries no value, so it is either present or absent.
func appendSwitch(args []string, flag string, enabled *bool) []string {
	if !ptr.Deref(enabled, false) {
		return args
	}
	return append(args, flag)
}

// Leader election follows the per-service override when set, else the global
// setting, and is forced on by a replica count above one — where two controllers
// acting at once would fight.
func leaderElect(config *krmv1alpha1.ProjectController, global *krmv1alpha1.GlobalConfig) bool {
	if config.Args.LeaderElect != nil {
		return *config.Args.LeaderElect
	}
	if ptr.Deref(global.LeaderElection, false) {
		return true
	}
	return config.Replicas != nil && *config.Replicas > 1
}
