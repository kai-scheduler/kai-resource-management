// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroupassigner

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
	defaultResourceName = "pod-group-assigner"

	// certVolumeName is mounted from a Secret the Helm chart owns, or that the
	// OpenShift service-CA operator mints. The operand only references it by name.
	certVolumeName = "webhook-certs"
	certMountPath  = "/etc/webhook/certs"

	// openshiftServingCertAnnotation asks the OpenShift service-CA operator to mint
	// the serving certificate into the named Secret.
	openshiftServingCertAnnotation = "service.beta.openshift.io/serving-cert-secret-name"
)

func (p *PodGroupAssigner) deploymentForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	config := krmConfig.Spec.PodGroupAssigner

	deployment, err := common.DeploymentForKRMConfig(
		ctx, runtimeClient, krmConfig, config.Service, p.BaseResourceName)
	if err != nil {
		return nil, err
	}

	deployment.Spec.Replicas = config.Replicas

	container := &deployment.Spec.Template.Spec.Containers[0]
	container.Args = buildArgsList(krmConfig)
	container.Ports = []corev1.ContainerPort{
		{
			Name:          *config.ControllerService.Webhook.Name,
			ContainerPort: *config.ControllerService.Webhook.TargetPort,
		},
	}

	// Unconditional: the binary registers its PodGroup mutating handler whatever
	// the toggles say, so the TLS server always starts and a missing certificate
	// should fail the pod rather than leave a webhook that never answers.
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

	return deployment, nil
}

func (p *PodGroupAssigner) serviceAccountForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	return common.ServiceAccountForKRMConfig(ctx, runtimeClient, krmConfig, p.BaseResourceName)
}

func (p *PodGroupAssigner) serviceForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	webhook := krmConfig.Spec.PodGroupAssigner.ControllerService.Webhook

	service, err := common.ServiceForKRMConfig(
		ctx, runtimeClient, krmConfig, p.BaseResourceName, []corev1.ServicePort{
			{
				Name:       *webhook.Name,
				Protocol:   corev1.ProtocolTCP,
				Port:       *webhook.Port,
				TargetPort: intstr.FromInt32(*webhook.TargetPort),
			},
		})
	if err != nil {
		return nil, err
	}

	// On OpenShift the platform mints the serving certificate, and this annotation
	// is the only thing that asks it to. Without it the Secret the Deployment mounts
	// is never created and the pod never starts.
	if ptr.Deref(krmConfig.Spec.Global.Openshift, false) {
		annotations := service.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[openshiftServingCertAnnotation] = *krmConfig.Spec.PodGroupAssigner.Webhooks.CertSecretName
		service.SetAnnotations(annotations)
	}

	return service, nil
}

// A flag left out entirely is what lets the binary's own default apply, so an
// unset field must add nothing rather than add an empty value.
func buildArgsList(krmConfig *krmv1alpha1.KRMConfig) []string {
	config := krmConfig.Spec.PodGroupAssigner
	global := krmConfig.Spec.Global

	args := []string{
		"--webhook-port", strconv.Itoa(int(*config.ControllerService.Webhook.TargetPort)),
		// Spelled out either way: the flag decides whether the Pod handler is
		// registered at all, so omitting it is not the same as passing false.
		"--enable-pod-webhook=" +
			strconv.FormatBool(ptr.Deref(config.Webhooks.EnablePodWebhook, true)),
	}

	args = appendStringArg(args, "--nodepool-label-key", global.NodePoolLabelKey)
	args = appendStringArg(args, "--queue-label-key", global.QueueLabelKey)
	args = appendStringArg(args, "--default-nodepool-name", global.DefaultNodePoolName)
	args = appendStringArg(args, "--scheduler-name", global.SchedulerName)
	args = appendStringArg(args, "--namespace-project-label-key", global.NamespaceProjectLabelKey)
	args = appendStringArg(args, "--project-label-key", global.ProjectLabelKey)
	args = appendStringArg(args, "--enforce-scheduler-annotation-key", global.EnforceSchedulerAnnotationKey)

	args = appendStringArg(args, "--unexisting-nodepool-sentinel", config.Args.UnexistingNodepoolSentinel)
	args = appendStringArg(args, "--annotation-nodepools-key", config.Args.AnnotationNodepoolsKey)

	args = appendSwitch(args, "--debug", config.Args.Debug)
	args = appendSwitch(args, "--leader-elect", ptr.To(leaderElect(config, global)))

	args = common.AddK8sClientConfigToArgs(config.Service.K8sClientConfig, args)

	// Last, so a flag repeated here wins over the one built above it.
	args = append(args, config.ExtraArgs...)

	return args
}

// Leader election follows the per-service override when set, else the global
// setting, and is forced on by a replica count above one — where two instances
// acting at once would fight.
func leaderElect(config *krmv1alpha1.PodGroupAssigner, global *krmv1alpha1.GlobalConfig) bool {
	if config.Args.LeaderElect != nil {
		return *config.Args.LeaderElect
	}
	if ptr.Deref(global.LeaderElection, false) {
		return true
	}
	return config.Replicas != nil && *config.Replicas > 1
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
