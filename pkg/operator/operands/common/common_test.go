// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"testing"

	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

func TestCommon(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operand common suite")
}

const testNamespace = "kai-resource-management"

func newClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func newKRMConfig() *krmv1alpha1.KRMConfig {
	return &krmv1alpha1.KRMConfig{
		ObjectMeta: metav1.ObjectMeta{Name: krmv1alpha1.KRMConfigSingletonName},
		Spec: krmv1alpha1.KRMConfigSpec{
			Namespace: testNamespace,
			Global:    &krmv1alpha1.GlobalConfig{},
		},
	}
}

func newService(imageName string) *kaicommon.Service {
	service := &kaicommon.Service{}
	service.SetDefaultsWhereNeeded(imageName)
	return service
}

var _ = Describe("ObjectForKRMConfig", func() {
	ctx := context.Background()

	It("returns an empty object when none exists yet", func() {
		object, err := ObjectForKRMConfig(ctx, newClient(), &corev1.ConfigMap{}, "settings", testNamespace)

		Expect(err).ToNot(HaveOccurred())
		Expect(object.GetName()).To(Equal("settings"))
		Expect(object.GetNamespace()).To(Equal(testNamespace))
		Expect(object.GetLabels()).To(HaveKeyWithValue("app", "settings"))
	})

	// Labels the operator does not set are not the operator's to remove.
	It("keeps labels somebody else added", func() {
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "settings",
				Namespace: testNamespace,
				Labels:    map[string]string{"added-by": "an-admin"},
			},
		}

		object, err := ObjectForKRMConfig(ctx, newClient(existing), &corev1.ConfigMap{}, "settings", testNamespace)

		Expect(err).ToNot(HaveOccurred())
		Expect(object.GetLabels()).To(HaveKeyWithValue("added-by", "an-admin"))
		Expect(object.GetLabels()).To(HaveKeyWithValue("app", "settings"))
	})

	// Reading the live object first is what preserves the fields the API server
	// owns, without every builder having to know which those are.
	It("carries the live object's server-owned fields", func() {
		existing := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "metrics", Namespace: testNamespace},
			Spec:       corev1.ServiceSpec{ClusterIP: "10.0.0.1"},
		}

		object, err := ObjectForKRMConfig(ctx, newClient(existing), &corev1.Service{}, "metrics", testNamespace)

		Expect(err).ToNot(HaveOccurred())
		Expect(object.(*corev1.Service).Spec.ClusterIP).To(Equal("10.0.0.1"))
		Expect(object.GetResourceVersion()).ToNot(BeEmpty())
	})
})

var _ = Describe("FipsGodebugEnvVar", func() {
	ctx := context.Background()

	DescribeTable("renders the mode as GODEBUG",
		func(mode *krmv1alpha1.FipsMode, expected string) {
			Expect(FipsGodebugEnvVar(ctx, &krmv1alpha1.GlobalConfig{FipsMode: mode})).To(
				Equal(corev1.EnvVar{Name: "GODEBUG", Value: expected}))
		},
		Entry("off", ptr.To(krmv1alpha1.FipsModeOff), "fips140=off"),
		Entry("on", ptr.To(krmv1alpha1.FipsModeOn), "fips140=on"),
		Entry("only", ptr.To(krmv1alpha1.FipsModeOnly), "fips140=only"),
		// Unset and out-of-enum both disable rather than reach a pod spec: the
		// latter is only possible for a CR stored before the enum existed.
		Entry("unset", nil, "fips140=off"),
		Entry("out of enum", ptr.To(krmv1alpha1.FipsMode("ON")), "fips140=off"),
	)
})

var _ = Describe("DeploymentForKRMConfig", func() {
	ctx := context.Background()

	It("builds the shape every service shares", func() {
		krmConfig := newKRMConfig()

		deployment, err := DeploymentForKRMConfig(ctx, newClient(), krmConfig, newService("nodepool-controller"), "nodepool-controller")

		Expect(err).ToNot(HaveOccurred())
		// The engine keys on the GVK, so a builder that forgets it produces an
		// object that is recreated on every reconcile.
		Expect(deployment.TypeMeta.Kind).To(Equal("Deployment"))
		Expect(deployment.Namespace).To(Equal(testNamespace))
		Expect(deployment.Labels).To(HaveKeyWithValue(OperatorManagedByLabelKey, OperatorManagedByLabelValue))
		Expect(deployment.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app", "nodepool-controller"))
		Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("app", "nodepool-controller"))
		Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal("nodepool-controller"))
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
	})

	It("puts every operand container into the configured FIPS mode", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.FipsMode = ptr.To(krmv1alpha1.FipsModeOnly)

		deployment, err := DeploymentForKRMConfig(ctx, newClient(), krmConfig, newService("project-controller"), "project-controller")

		Expect(err).ToNot(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(
			ContainElement(corev1.EnvVar{Name: "GODEBUG", Value: "fips140=only"}))
	})

	It("drops the security context on OpenShift, which assigns the uid range itself", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.Openshift = ptr.To(true)
		krmConfig.Spec.Global.SecurityContext = &corev1.SecurityContext{RunAsUser: ptr.To(int64(10000))}

		deployment, err := DeploymentForKRMConfig(ctx, newClient(), krmConfig, newService("project-controller"), "project-controller")

		Expect(err).ToNot(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].SecurityContext).To(BeNil())
	})
})

var _ = Describe("AllControllersAvailable", func() {
	ctx := context.Background()

	availableDeployment := func() *appsv1.Deployment {
		return &appsv1.Deployment{
			TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: testNamespace},
			Spec:       appsv1.DeploymentSpec{Replicas: ptr.To(int32(1))},
			Status: appsv1.DeploymentStatus{
				UpdatedReplicas: 1,
				Conditions: []appsv1.DeploymentCondition{{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				}},
			},
		}
	}

	It("reports available when every Deployment is", func() {
		deployment := availableDeployment()

		available, err := AllControllersAvailable(ctx, newClient(deployment), []client.Object{deployment.DeepCopy()})

		Expect(err).ToNot(HaveOccurred())
		Expect(available).To(BeTrue())
	})

	It("reports unavailable while replicas are still rolling out", func() {
		deployment := availableDeployment()
		deployment.Status.UpdatedReplicas = 0

		available, err := AllControllersAvailable(ctx, newClient(deployment), []client.Object{deployment.DeepCopy()})

		Expect(available).To(BeFalse())
		Expect(err).To(MatchError(ContainSubstring("is not available")))
	})

	// A ConfigMap is available as soon as it exists; only workloads have a rollout.
	It("ignores objects that are not workloads", func() {
		configMap := &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "settings", Namespace: testNamespace},
		}

		available, err := AllControllersAvailable(ctx, newClient(configMap), []client.Object{configMap.DeepCopy()})

		Expect(err).ToNot(HaveOccurred())
		Expect(available).To(BeTrue())
	})
})

var _ = Describe("MergeAffinities", func() {
	It("returns the global affinity when the service sets none", func() {
		global := &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}

		Expect(MergeAffinities(nil, global, nil, false)).To(Equal(global))
	})

	It("returns the service's affinity when there is no global one", func() {
		local := &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}

		Expect(MergeAffinities(local, nil, nil, false)).To(Equal(local))
	})

	It("synthesises a preferred per-host anti-affinity when neither sets one", func() {
		merged := MergeAffinities(&corev1.Affinity{}, &corev1.Affinity{}, map[string]string{"app": "service"}, false)

		Expect(merged.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
		Expect(merged.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).To(BeEmpty())
	})

	It("makes it required when asked", func() {
		merged := MergeAffinities(&corev1.Affinity{}, &corev1.Affinity{}, map[string]string{"app": "service"}, true)

		Expect(merged.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
	})
})

var _ = Describe("BuildVPAFromObjects", func() {
	It("builds nothing when VPA is disabled", func() {
		vpaSpec := &kaicommon.VPASpec{Enabled: ptr.To(false)}
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "service"}}

		Expect(BuildVPAFromObjects(vpaSpec, []client.Object{deployment}, testNamespace)).To(BeNil())
	})

	It("targets the Deployment it is given", func() {
		vpaSpec := &kaicommon.VPASpec{Enabled: ptr.To(true)}
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "service"}}

		verticalPodAutoscaler := BuildVPAFromObjects(vpaSpec, []client.Object{deployment}, testNamespace)

		Expect(verticalPodAutoscaler).ToNot(BeNil())
		Expect(verticalPodAutoscaler.GetName()).To(Equal("service"))
		Expect(verticalPodAutoscaler.GetNamespace()).To(Equal(testNamespace))
	})

	It("builds nothing when there is no workload to target", func() {
		vpaSpec := &kaicommon.VPASpec{Enabled: ptr.To(true)}
		configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "settings"}}

		Expect(BuildVPAFromObjects(vpaSpec, []client.Object{configMap}, testNamespace)).To(BeNil())
	})
})

var _ = Describe("argument helpers", func() {
	It("renders the client config flags that are set", func() {
		args := AddK8sClientConfigToArgs(&kaicommon.K8sClientConfig{QPS: ptr.To(50)}, []string{})

		Expect(args).To(Equal([]string{"--qps", "50"}))
	})

	It("renders nothing when there is no client config", func() {
		Expect(AddK8sClientConfigToArgs(nil, []string{})).To(BeEmpty())
	})

	It("switches controller-runtime to JSON logging only when asked", func() {
		Expect(AddControllerRuntimeJSONLogArg(ptr.To(true), []string{})).To(Equal([]string{"--zap-devel=false"}))
		Expect(AddControllerRuntimeJSONLogArg(ptr.To(false), []string{})).To(BeEmpty())
	})
})

func newMonitoringClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

var _ = Describe("ServiceMonitorForKRMConfig", func() {
	ctx := context.Background()

	It("names the monitor after the service by default", func() {
		monitor, err := ServiceMonitorForKRMConfig(
			ctx, newMonitoringClient(), newKRMConfig(), "nodepool-controller", "metrics",
			ServiceMonitorOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(monitor.Name).To(Equal("nodepool-controller"))
		Expect(monitor.Labels).To(HaveKeyWithValue("app", "nodepool-controller"))
		Expect(monitor.Spec.JobLabel).To(Equal("nodepool-controller"))
		Expect(monitor.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app", "nodepool-controller"))
		Expect(monitor.Spec.NamespaceSelector.MatchNames).To(Equal([]string{testNamespace}))
		Expect(monitor.Spec.Endpoints).To(HaveLen(1))
		Expect(monitor.Spec.Endpoints[0].Port).To(Equal("metrics"))
	})

	// A second monitor on one endpoint is only reachable by the other Prometheus if
	// it selects the service it is not named after.
	It("keeps selecting the service when the monitor has its own name", func() {
		monitor, err := ServiceMonitorForKRMConfig(
			ctx, newMonitoringClient(), newKRMConfig(), "nodepool-controller", "metrics",
			ServiceMonitorOptions{
				Name:        "nodepool-controller-accounting",
				ExtraLabels: map[string]string{"kai.scheduler/accounting": "true"},
			})

		Expect(err).ToNot(HaveOccurred())
		Expect(monitor.Name).To(Equal("nodepool-controller-accounting"))
		Expect(monitor.Labels).To(HaveKeyWithValue("app", "nodepool-controller"))
		Expect(monitor.Labels).To(HaveKeyWithValue("kai.scheduler/accounting", "true"))
		Expect(monitor.Spec.JobLabel).To(Equal("nodepool-controller"))
		Expect(monitor.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app", "nodepool-controller"))
	})

	It("reads back the monitor it already created rather than a second one", func() {
		existing := &monitoringv1.ServiceMonitor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nodepool-controller-accounting",
				Namespace: testNamespace,
				Labels:    map[string]string{"added-by": "an-admin"},
			},
		}

		monitor, err := ServiceMonitorForKRMConfig(
			ctx, newMonitoringClient(existing), newKRMConfig(), "nodepool-controller", "metrics",
			ServiceMonitorOptions{Name: "nodepool-controller-accounting"})

		Expect(err).ToNot(HaveOccurred())
		Expect(monitor.Labels).To(HaveKeyWithValue("added-by", "an-admin"))
		Expect(monitor.Labels).To(HaveKeyWithValue("app", "nodepool-controller"))
	})

	// Deploying on a cluster with no Prometheus operator is not an error.
	It("reports nothing when the ServiceMonitor kind is unknown", func() {
		monitor, err := ServiceMonitorForKRMConfig(
			ctx, newClient(), newKRMConfig(), "nodepool-controller", "metrics", ServiceMonitorOptions{})

		Expect(err).ToNot(HaveOccurred())
		Expect(monitor).To(BeNil())
	})
})
