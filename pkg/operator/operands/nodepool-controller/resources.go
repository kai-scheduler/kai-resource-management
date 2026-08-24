// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodepoolcontroller

import (
	"context"
	"strconv"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

const (
	defaultResourceName = "nodepool-controller"

	// accountingMonitorSuffix names the second ServiceMonitor on the same endpoint.
	accountingMonitorSuffix = "-accounting"

	// accountingLabel routes a ServiceMonitor to the KAI accounting Prometheus, which
	// selects on it; the primary Prometheus excludes it. A monitor therefore reaches
	// exactly one of the two, which is why the node-to-nodepool metrics need both.
	accountingLabelKey   = "kai.scheduler/accounting"
	accountingLabelValue = "true"

	// certVolumeName is mounted from a Secret the Helm chart owns, or that the
	// OpenShift service-CA operator mints. The operand only references it by name.
	certVolumeName = "webhook-certs"
	certMountPath  = "/etc/webhook/certs"

	// openshiftServingCertAnnotation asks the OpenShift service-CA operator to mint
	// the serving certificate into the named Secret.
	openshiftServingCertAnnotation = "service.beta.openshift.io/serving-cert-secret-name"
)

func (n *NodePoolController) deploymentForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	config := krmConfig.Spec.NodePoolController

	deployment, err := common.DeploymentForKRMConfig(
		ctx, runtimeClient, krmConfig, config.Service, n.BaseResourceName)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Replicas = config.Replicas

	container := &deployment.Spec.Template.Spec.Containers[0]
	container.Args = buildArgsList(krmConfig)
	container.Ports = containerPorts(config)

	// Only when the webhook is served: with it off the binary starts no TLS server,
	// and mounting a Secret the chart did not mint would leave the pod pending.
	if webhookEnabled(config.Webhooks) {
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

func (n *NodePoolController) serviceAccountForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	return common.ServiceAccountForKRMConfig(ctx, runtimeClient, krmConfig, n.BaseResourceName)
}

func (n *NodePoolController) serviceForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	config := krmConfig.Spec.NodePoolController

	service, err := common.ServiceForKRMConfig(
		ctx, runtimeClient, krmConfig, n.BaseResourceName, servicePorts(config))
	if err != nil {
		return nil, err
	}

	// On OpenShift the platform mints the serving certificate, and this annotation
	// is the only thing that asks it to. Without it the Secret the Deployment mounts
	// is never created and the pod never starts.
	if webhookEnabled(config.Webhooks) && ptr.Deref(krmConfig.Spec.Global.Openshift, false) {
		annotations := service.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[openshiftServingCertAnnotation] = *config.Webhooks.CertSecretName
		service.SetAnnotations(annotations)
	}

	return service, nil
}

func (n *NodePoolController) serviceMonitorForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	return n.buildServiceMonitor(ctx, runtimeClient, krmConfig, common.ServiceMonitorOptions{})
}

// The accounting monitor scrapes the same endpoint as the primary one. Enabling it
// where no accounting Prometheus is deployed costs nothing: nothing selects it.
func (n *NodePoolController) accountingServiceMonitorForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	serviceMonitor := krmConfig.Spec.Global.ServiceMonitor
	if serviceMonitor == nil || !ptr.Deref(serviceMonitor.Accounting, false) {
		return nil, nil
	}

	return n.buildServiceMonitor(ctx, runtimeClient, krmConfig, common.ServiceMonitorOptions{
		Name:        n.BaseResourceName + accountingMonitorSuffix,
		ExtraLabels: map[string]string{accountingLabelKey: accountingLabelValue},
	})
}

func (n *NodePoolController) buildServiceMonitor(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
	options common.ServiceMonitorOptions,
) (client.Object, error) {
	serviceMonitor := krmConfig.Spec.Global.ServiceMonitor
	if serviceMonitor == nil || !ptr.Deref(serviceMonitor.Enabled, false) {
		return nil, nil
	}

	monitor, err := common.ServiceMonitorForKRMConfig(ctx, runtimeClient, krmConfig, n.BaseResourceName,
		*krmConfig.Spec.NodePoolController.ControllerService.Metrics.Name, options)
	// A typed nil would reach the caller as a non-nil client.Object.
	if err != nil || monitor == nil {
		return nil, err
	}
	return monitor, nil
}

// The container listens on the target ports; the Service publishes the others. The
// node-pool metrics port is deliberately absent: the chart published it without ever
// declaring a container port for it.
func containerPorts(config *krmv1alpha1.NodePoolController) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{
			Name:          *config.ControllerService.Metrics.Name,
			ContainerPort: *config.ControllerService.Metrics.TargetPort,
		},
	}

	if webhookEnabled(config.Webhooks) {
		ports = append(ports, corev1.ContainerPort{
			Name:          *config.ControllerService.Webhook.Name,
			ContainerPort: *config.ControllerService.Webhook.TargetPort,
		})
	}

	return ports
}

func servicePorts(config *krmv1alpha1.NodePoolController) []corev1.ServicePort {
	nodePoolMetrics := config.ControllerService.NodePoolMetrics
	metrics := config.ControllerService.Metrics

	ports := []corev1.ServicePort{
		{
			Name:       *nodePoolMetrics.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       *nodePoolMetrics.Port,
			TargetPort: intstr.FromInt32(*nodePoolMetrics.TargetPort),
		},
		{
			Name:       *metrics.Name,
			Protocol:   corev1.ProtocolTCP,
			Port:       *metrics.Port,
			TargetPort: intstr.FromInt32(*metrics.TargetPort),
		},
	}

	if webhookEnabled(config.Webhooks) {
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

// Defaults to true on nil, matching the binary's own default for the flag, so a
// half-defaulted config cannot leave the webhook served with no certificate.
func webhookEnabled(webhooks *krmv1alpha1.NodePoolControllerWebhooks) bool {
	return ptr.Deref(webhooks.EnableNodePoolValidation, true)
}

// A flag left out entirely is what lets the binary's own default apply, so an
// unset field must add nothing rather than add an empty value.
func buildArgsList(krmConfig *krmv1alpha1.KRMConfig) []string {
	config := krmConfig.Spec.NodePoolController
	global := krmConfig.Spec.Global

	args := []string{
		"--metrics-port", strconv.Itoa(int(*config.ControllerService.Metrics.TargetPort)),
		"--scheduler-namespace", krmConfig.Spec.Namespace,
		// Spelled out either way: the binary defaults this to true and would then
		// start a TLS server with no certificate mounted.
		"--enable-nodepool-validation-webhook=" + strconv.FormatBool(webhookEnabled(config.Webhooks)),
		"--default-nodepool-name", *global.DefaultNodePoolName,
	}

	if webhookEnabled(config.Webhooks) {
		args = append(args, "--webhook-port", strconv.Itoa(int(*config.ControllerService.Webhook.TargetPort)))
	}

	args = appendStringArg(args, "--nodepool-label-key", global.NodePoolLabelKey)
	args = appendStringArg(args, "--scheduler-name", global.SchedulerName)
	args = appendStringArg(args, "--finalizer-domain", global.FinalizerDomain)

	args = appendStringArg(args, "--metrics-namespace", config.Args.MetricsNamespace)
	args = appendStringArg(args, "--dcgm-exporter-namespace", config.Args.DcgmExporterNamespace)
	args = appendStringArg(args, "--scheduling-shard-args", config.Args.SchedulingShardArgs)
	args = appendStringArg(args, "--excluded-nodepool-name", config.Args.ExcludedNodepoolName)
	args = appendStringArg(args, "--managed-nodes-config-name", config.Args.ManagedNodesConfigName)
	args = appendStringArg(args, "--to-exclude-label", config.Args.ToExcludeLabel)
	args = appendStringArg(args, "--unschedulable-label", config.Args.UnschedulableLabel)
	args = appendStringArg(args, "--grove-topology-annotation", config.Args.GroveTopologyAnnotation)
	args = appendStringArg(args, "--grove-topology-resource-version-annotation",
		config.Args.GroveTopologyResourceVersionAnnotation)

	args = appendSwitch(args, "--debug", config.Args.Debug)
	args = appendSwitch(args, "--leader-elect", ptr.To(leaderElect(config, global)))

	args = common.AddK8sClientConfigToArgs(config.Service.K8sClientConfig, args)

	// Last, so a flag repeated here wins over the one built above it.
	return append(args, config.ExtraArgs...)
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
func leaderElect(config *krmv1alpha1.NodePoolController, global *krmv1alpha1.GlobalConfig) bool {
	if config.Args.
		LeaderElect != nil {
		return *config.Args.LeaderElect
	}
	if ptr.Deref(global.LeaderElection, false) {
		return true
	}
	return config.Replicas != nil && *config.Replicas > 1
}
