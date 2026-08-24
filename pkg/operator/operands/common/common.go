// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	kaicommon "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	OperatorManagedByLabelKey   = "app.kubernetes.io/managed-by"
	OperatorManagedByLabelValue = "krm-operator"

	// serviceAccountTokenPath is what Prometheus authenticates a scrape with.
	serviceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // a path, not a credential
)

var controllerTypes = []string{"Deployment"}

func ObjectForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, object client.Object,
	resourceName string, resourceNamespace string,
) (client.Object, error) {
	err := runtimeClient.Get(ctx, client.ObjectKey{
		Name:      resourceName,
		Namespace: resourceNamespace,
	}, object)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}

	object.SetName(resourceName)
	object.SetNamespace(resourceNamespace)
	if object.GetLabels() == nil {
		object.SetLabels(map[string]string{})
	}
	object.GetLabels()["app"] = resourceName

	return object, nil
}

func DeploymentForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
	service *kaicommon.Service, deploymentName string,
) (*appsv1.Deployment, error) {
	deploymentObj, err := ObjectForKRMConfig(
		ctx, runtimeClient, &appsv1.Deployment{}, deploymentName, krmConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	deployment := deploymentObj.(*appsv1.Deployment)
	deployment.Labels[OperatorManagedByLabelKey] = OperatorManagedByLabelValue
	deployment.TypeMeta = metav1.TypeMeta{
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}

	deployment.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app": deploymentName,
		},
	}

	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = map[string]string{}
	}
	deployment.Spec.Template.Labels["app"] = deploymentName

	deployment.Spec.Template.Spec.ServiceAccountName = deploymentName
	deployment.Spec.Template.Spec.NodeSelector = krmConfig.Spec.Global.NodeSelector
	deployment.Spec.Template.Spec.Tolerations = krmConfig.Spec.Global.Tolerations
	deployment.Spec.Template.Spec.PriorityClassName = ptr.Deref(krmConfig.Spec.Global.PriorityClassName, "")

	deployment.Spec.Template.Spec.Affinity = MergeAffinities(service.Affinity,
		krmConfig.Spec.Global.Affinity,
		deployment.Spec.Selector.MatchLabels,
		ptr.Deref(krmConfig.Spec.Global.RequireDefaultPodAntiAffinityTerm, false))

	deployment.Spec.Template.Spec.Containers = []corev1.Container{
		{
			Name:            deploymentName,
			Image:           service.Image.Url(),
			ImagePullPolicy: *service.Image.PullPolicy,
			Resources:       corev1.ResourceRequirements(*service.Resources),
			SecurityContext: krmConfig.Spec.Global.GetSecurityContext(),
			Env:             []corev1.EnvVar{FipsGodebugEnvVar(ctx, krmConfig.Spec.Global)},
		},
	}

	deployment.Spec.Template.Spec.ImagePullSecrets = GetGlobalImagePullSecrets(krmConfig.Spec.Global)

	return deployment, nil
}

func ServiceAccountForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
	serviceAccountName string,
) (*corev1.ServiceAccount, error) {
	serviceAccountObj, err := ObjectForKRMConfig(
		ctx, runtimeClient, &corev1.ServiceAccount{}, serviceAccountName, krmConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	serviceAccount := serviceAccountObj.(*corev1.ServiceAccount)
	serviceAccount.TypeMeta = metav1.TypeMeta{
		Kind:       "ServiceAccount",
		APIVersion: "v1",
	}

	return serviceAccount, nil
}

func ServiceForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
	serviceName string, ports []corev1.ServicePort,
) (*corev1.Service, error) {
	serviceObj, err := ObjectForKRMConfig(
		ctx, runtimeClient, &corev1.Service{}, serviceName, krmConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}
	service := serviceObj.(*corev1.Service)
	service.TypeMeta = metav1.TypeMeta{
		Kind:       "Service",
		APIVersion: "v1",
	}

	service.Spec.Selector = map[string]string{"app": serviceName}
	service.Spec.Ports = ports
	// Set rather than left to the API server, so the field has an owner under
	// server-side apply and a foreign controller cannot claim it.
	service.Spec.Type = corev1.ServiceTypeClusterIP

	return service, nil
}

// ServiceMonitorOptions varies a monitor from the default "one per service". A
// second monitor on one endpoint is how a metric reaches two Prometheuses that
// select on the same label, since a label routes a monitor to exactly one of them.
type ServiceMonitorOptions struct {
	// Name overrides the monitor's own name. Empty means the service's name.
	Name string

	// ExtraLabels are added to the monitor. The app label is not one of them: it
	// always names the scraped service, whatever the monitor is called.
	ExtraLabels map[string]string
}

// ServiceMonitorForKRMConfig scrapes the named port of the Service named
// serviceName. metricsPortName must name a port that Service publishes; Prometheus
// resolves the endpoint by name, not by number.
//
// Returns nil when the Prometheus operator is not installed, so a service can be
// deployed on a cluster that has nothing to scrape it with.
func ServiceMonitorForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
	serviceName string, metricsPortName string, options ServiceMonitorOptions,
) (*monitoringv1.ServiceMonitor, error) {
	monitorName := options.Name
	if monitorName == "" {
		monitorName = serviceName
	}

	serviceMonitorObj, err := ObjectForKRMConfig(
		ctx, runtimeClient, &monitoringv1.ServiceMonitor{}, monitorName, krmConfig.Spec.Namespace)
	if err != nil {
		if meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err) {
			return nil, nil
		}
		return nil, err
	}
	serviceMonitor := serviceMonitorObj.(*monitoringv1.ServiceMonitor)
	serviceMonitor.TypeMeta = metav1.TypeMeta{
		Kind:       monitoringv1.ServiceMonitorsKind,
		APIVersion: monitoringv1.SchemeGroupVersion.String(),
	}

	// ObjectForKRMConfig labels an object after itself, which is wrong here as soon
	// as the monitor is not named after the service it selects.
	labels := serviceMonitor.GetLabels()
	for key, value := range options.ExtraLabels {
		labels[key] = value
	}
	labels["app"] = serviceName

	serviceMonitor.Spec = monitoringv1.ServiceMonitorSpec{
		JobLabel:          serviceName,
		NamespaceSelector: monitoringv1.NamespaceSelector{MatchNames: []string{krmConfig.Spec.Namespace}},
		Selector:          metav1.LabelSelector{MatchLabels: map[string]string{"app": serviceName}},
		Endpoints: []monitoringv1.Endpoint{
			{
				Port:            metricsPortName,
				BearerTokenFile: serviceAccountTokenPath,
				MetricRelabelConfigs: []monitoringv1.RelabelConfig{
					{Action: "replace", TargetLabel: "type", Replacement: ptr.To("stats")},
				},
			},
		},
	}

	return serviceMonitor, nil
}

func AllObjectsExists(
	ctx context.Context, runtimeClient client.Reader, objects []client.Object,
) (bool, error) {
	for _, object := range objects {
		err := runtimeClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
	}

	return true, nil
}

func AllControllersAvailable(
	ctx context.Context, readerClient client.Reader, objects []client.Object,
) (bool, error) {
	errorMessages := []string{}

	for _, object := range objects {
		objectKind := object.GetObjectKind().GroupVersionKind().Kind
		err := readerClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil {
			errorMessages = append(errorMessages, err.Error())
			continue
		}

		if slices.Contains(controllerTypes, objectKind) {
			available, err := isControllerAvailable(object, objectKind)
			if err != nil {
				errorMessages = append(errorMessages, err.Error())
				continue
			}
			if !available {
				errorMessages = append(errorMessages, fmt.Sprintf(
					"%s [%s] is not available", objectKind, object.GetName()))
				continue
			}
		}
	}

	if len(errorMessages) > 0 {
		return false, fmt.Errorf("%s", strings.Join(errorMessages, "\n"))
	}

	return true, nil
}

func isControllerAvailable(object client.Object, objectKind string) (bool, error) {
	if objectKind != "Deployment" {
		return false, nil
	}

	deployment, ok := object.(*appsv1.Deployment)
	if !ok {
		return false, fmt.Errorf("failed to process deployment %s/%s", object.GetNamespace(), object.GetName())
	}

	if deployment.Spec.Replicas == nil {
		return false, nil
	}

	if deployment.Status.UpdatedReplicas != *deployment.Spec.Replicas {
		return false, nil
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
			return true, nil
		}
	}

	return false, nil
}

func MergeAffinities(localAffinity *corev1.Affinity,
	globalAffinity *corev1.Affinity,
	podAntiAffinityLabel map[string]string,
	requireDefaultPodAntiAffinityTerm bool) *corev1.Affinity {
	if localAffinity == nil {
		return globalAffinity
	}

	if globalAffinity == nil {
		return localAffinity
	}

	affinity := &corev1.Affinity{}

	// If NodeAffinity is defined in localAffinity, use it; otherwise use from globalAffinity
	if localAffinity.NodeAffinity != nil {
		affinity.NodeAffinity = localAffinity.NodeAffinity
	} else if globalAffinity.NodeAffinity != nil {
		affinity.NodeAffinity = globalAffinity.NodeAffinity
	}

	// If PodAffinity is defined in localAffinity, use it; otherwise use from globalAffinity
	if localAffinity.PodAffinity != nil {
		affinity.PodAffinity = localAffinity.PodAffinity
	} else if globalAffinity.PodAffinity != nil {
		affinity.PodAffinity = globalAffinity.PodAffinity
	}

	podAntiAffinity := &corev1.PodAntiAffinity{}

	switch {
	case localAffinity.PodAntiAffinity != nil:
		podAntiAffinity = localAffinity.PodAntiAffinity
	case globalAffinity.PodAntiAffinity != nil:
		podAntiAffinity = globalAffinity.PodAntiAffinity
	case len(podAntiAffinityLabel) > 0:
		podAffinityTerm := corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: podAntiAffinityLabel,
			},
			TopologyKey: "kubernetes.io/hostname",
		}

		podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
			podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			corev1.WeightedPodAffinityTerm{
				Weight:          100,
				PodAffinityTerm: podAffinityTerm,
			},
		)

		if requireDefaultPodAntiAffinityTerm {
			podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(
				podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
				podAffinityTerm,
			)
		}
	}

	affinity.PodAntiAffinity = podAntiAffinity

	return affinity
}

func AddK8sClientConfigToArgs(k8sClientConfig *kaicommon.K8sClientConfig, args []string) []string {
	if k8sClientConfig != nil {
		if k8sClientConfig.QPS != nil {
			args = append(args, "--qps", strconv.Itoa(*k8sClientConfig.QPS))
		}
		if k8sClientConfig.Burst != nil {
			args = append(args, "--burst", strconv.Itoa(*k8sClientConfig.Burst))
		}
	}

	return args
}

// FipsGodebugEnvVar renders global.fipsMode as the GODEBUG entry that selects the
// Go FIPS 140-3 run-time mode.
//
// Emitted even for "off", so the value is always owned by us: a FIPS-built image
// defaults to fips140=on, and omitting the variable there would leave no way to
// turn FIPS back off. An unrecognised mode falls back to off rather than reaching
// a pod spec, where it would make the container fail to start; the CRD enum
// rejects those, but a KRMConfig stored before the enum existed is not revalidated.
func FipsGodebugEnvVar(ctx context.Context, global *krmv1alpha1.GlobalConfig) corev1.EnvVar {
	mode := ptr.Deref(global.FipsMode, krmv1alpha1.FipsModeOff)
	if !slices.Contains(
		[]krmv1alpha1.FipsMode{
			krmv1alpha1.FipsModeOff, krmv1alpha1.FipsModeOn, krmv1alpha1.FipsModeOnly}, mode) {
		log.FromContext(ctx).Error(
			fmt.Errorf("unsupported fips mode %q", mode),
			"Falling back to a disabled FIPS mode", "fipsMode", mode)
		mode = krmv1alpha1.FipsModeOff
	}

	return corev1.EnvVar{Name: "GODEBUG", Value: fmt.Sprintf("fips140=%s", mode)}
}

func GetGlobalImagePullSecrets(global *krmv1alpha1.GlobalConfig) []corev1.LocalObjectReference {
	imagePullSecrets := []corev1.LocalObjectReference{}
	for _, secretName := range global.ImagePullSecrets {
		imagePullSecrets = append(imagePullSecrets, corev1.LocalObjectReference{Name: secretName})
	}

	if len(imagePullSecrets) == 0 {
		return nil
	}
	return imagePullSecrets
}
