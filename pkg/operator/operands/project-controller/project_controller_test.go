// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"context"
	"testing"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
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
	"sigs.k8s.io/yaml"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/config"
)

func TestProjectController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ProjectController operand suite")
}

const testNamespace = "kai-resource-management"

func newClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

// Built the way the operator builds it: an empty spec put through the real
// defaulting, so a test never asserts against a hand-made config the operator
// would never see.
func newKRMConfig() *krmv1alpha1.KRMConfig {
	krmConfig := &krmv1alpha1.KRMConfig{
		ObjectMeta: metav1.ObjectMeta{Name: krmv1alpha1.KRMConfigSingletonName},
		Spec:       krmv1alpha1.KRMConfigSpec{Namespace: testNamespace},
	}
	config.SetDefaultsWhereNeeded(&krmConfig.Spec)
	return krmConfig
}

func findType[T client.Object](objects []client.Object) T {
	var found T
	for _, object := range objects {
		if typed, matches := object.(T); matches {
			return typed
		}
	}
	return found
}

func desiredState(krmConfig *krmv1alpha1.KRMConfig, existing ...client.Object) []client.Object {
	objects, err := (&ProjectController{}).DesiredState(context.Background(), newClient(existing...), krmConfig)
	Expect(err).ToNot(HaveOccurred())
	return objects
}

func newClientWithoutPrometheus() client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

func desiredStateWithoutPrometheus() []client.Object {
	objects, err := (&ProjectController{}).DesiredState(
		context.Background(), newClientWithoutPrometheus(), newKRMConfig())
	Expect(err).ToNot(HaveOccurred())
	return objects
}

var _ = Describe("DesiredState", func() {
	It("builds the whole service", func() {
		objects := desiredState(newKRMConfig())

		Expect(findType[*appsv1.Deployment](objects)).ToNot(BeNil())
		Expect(findType[*corev1.ServiceAccount](objects)).ToNot(BeNil())
		Expect(findType[*corev1.Service](objects)).ToNot(BeNil())
		Expect(findType[*monitoringv1.ServiceMonitor](objects)).ToNot(BeNil())

		names := []string{}
		for _, object := range objects {
			names = append(names, object.GetName())
		}
		Expect(names).To(ContainElements(roleBindingsConfigMapName, deleteBlockersConfigMapName))
	})

	// Every object is keyed by GVK in the diff, so a blank TypeMeta would make the
	// operator fail to match what it already created and recreate it every reconcile.
	// The operand rewrites the container's args, ports and mounts after the shared
	// builder has filled it in, so this pins that the FIPS mode survives that pass.
	It("keeps the FIPS mode the shared builder put on the container", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.FipsMode = ptr.To(krmv1alpha1.FipsModeOnly)

		objects := desiredState(krmConfig)

		deployment := findType[*appsv1.Deployment](objects)
		Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(
			ContainElement(corev1.EnvVar{Name: "GODEBUG", Value: "fips140=only"}))
	})

	It("stamps the group, version and kind on every object", func() {
		for _, object := range desiredState(newKRMConfig()) {
			Expect(object.GetObjectKind().GroupVersionKind().Kind).ToNot(BeEmpty(),
				"missing Kind on %s", object.GetName())
			Expect(object.GetObjectKind().GroupVersionKind().Version).ToNot(BeEmpty(),
				"missing Version on %s", object.GetName())
		}
	})

	It("builds nothing when the service is disabled", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Service.Enabled = ptr.To(false)

		Expect(desiredState(krmConfig)).To(BeEmpty())
	})

	// The ServiceAccount name is what the chart's ClusterRoleBinding names as its
	// subject, and what DeploymentForKRMConfig puts on the pod.
	It("names the ServiceAccount after the Deployment", func() {
		objects := desiredState(newKRMConfig())

		deployment := findType[*appsv1.Deployment](objects)
		Expect(findType[*corev1.ServiceAccount](objects).Name).To(Equal(deployment.Name))
		Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(deployment.Name))
	})

	// Reconciliation must not undo a label somebody put on the live object.
	It("keeps labels already on the objects in the cluster", func() {
		krmConfig := newKRMConfig()
		objects := desiredState(krmConfig)

		deployment := findType[*appsv1.Deployment](objects)
		deployment.Labels["mine"] = "kept"
		service := findType[*corev1.Service](objects)
		service.Labels["mine"] = "kept"

		objects = desiredState(krmConfig, deployment, service)

		Expect(findType[*appsv1.Deployment](objects).Labels).To(HaveKeyWithValue("mine", "kept"))
		Expect(findType[*corev1.Service](objects).Labels).To(HaveKeyWithValue("mine", "kept"))
	})

	It("creates a VPA only when one is enabled", func() {
		Expect(desiredState(newKRMConfig())).To(HaveLen(6))

		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.VPA.Enabled = ptr.To(true)
		Expect(desiredState(krmConfig)).To(HaveLen(7))
	})

	// One app label across every object, so a single selector finds them all — the
	// ConfigMaps are not named after the service, so they do not get it for free.
	It("labels every object with the service name", func() {
		for _, object := range desiredState(newKRMConfig()) {
			Expect(object.GetLabels()).To(HaveKeyWithValue("app", defaultResourceName),
				"wrong app label on %s", object.GetName())
		}
	})

	It("builds the rolebindings ConfigMap under the name it is given", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.RoleBindingsConfigMapName = ptr.To("runai-rolebindings-plugin")

		names := []string{}
		for _, object := range desiredState(krmConfig) {
			names = append(names, object.GetName())
		}

		Expect(names).To(ContainElement("runai-rolebindings-plugin"))
		Expect(names).ToNot(ContainElement(roleBindingsConfigMapName))
		Expect(buildArgsList(krmConfig)).To(
			ContainElements("--rolebindings-configmap-name", "runai-rolebindings-plugin"))
	})

	It("still fills that ConfigMap with the bindings it owns", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.RoleBindingsConfigMapName = ptr.To("runai-rolebindings-plugin")
		krmConfig.Spec.ProjectController.Features.LimitRange = ptr.To(true)

		for _, object := range desiredState(krmConfig) {
			if configMap, isConfigMap := object.(*corev1.ConfigMap); isConfigMap &&
				configMap.Name == "runai-rolebindings-plugin" {
				Expect(configMap.Data).To(HaveKey(roleBindingKey("kai-project-controller-limit-range-per-project")))
				return
			}
		}
		Fail("the renamed ConfigMap was not built")
	})

	It("creates no ServiceMonitor when monitoring is off", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.ServiceMonitor.Enabled = ptr.To(false)

		Expect(findType[*monitoringv1.ServiceMonitor](desiredState(krmConfig))).To(BeNil())
	})

	It("deploys the service on a cluster with no Prometheus operator", func() {
		objects, err := (&ProjectController{}).DesiredState(
			context.Background(), newClientWithoutPrometheus(), newKRMConfig())

		Expect(err).ToNot(HaveOccurred())
		Expect(objects).To(HaveLen(5))
		Expect(findType[*appsv1.Deployment](objects)).ToNot(BeNil())
		Expect(findType[*monitoringv1.ServiceMonitor](objects)).To(BeNil())
	})

	It("returns no object at all when there is no ServiceMonitor to build", func() {
		for _, object := range desiredStateWithoutPrometheus() {
			Expect(object).ToNot(BeNil())
			Expect(object.GetName()).ToNot(BeEmpty())
		}
	})

	// The endpoint refers to the Service port by name, and both come from the same
	// CR field, so they cannot drift.
	It("scrapes the Service port it also publishes", func() {
		objects := desiredState(newKRMConfig())

		portName := findType[*monitoringv1.ServiceMonitor](objects).Spec.Endpoints[0].Port
		Expect(portName).To(Equal("metrics"))
		Expect(findType[*corev1.Service](objects).Spec.Ports[0].Name).To(Equal(portName))
	})
})

var _ = Describe("webhook wiring", func() {
	It("mounts the chart's TLS Secret and publishes the webhook port", func() {
		objects := desiredState(newKRMConfig())

		deployment := findType[*appsv1.Deployment](objects)
		Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName).
			To(Equal(config.ProjectControllerCertSecretName))
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal(certMountPath))

		Expect(findType[*corev1.Service](objects).Spec.Ports).To(HaveLen(2))
	})

	// Both off means the binary starts no TLS server, and the chart renders no
	// Secret, so mounting one would block the pod on a volume that never resolves.
	It("mounts nothing when both webhooks are off", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Webhooks.EnableProjectValidation = ptr.To(false)
		krmConfig.Spec.ProjectController.Webhooks.EnableDepartmentValidation = ptr.To(false)

		objects := desiredState(krmConfig)

		deployment := findType[*appsv1.Deployment](objects)
		Expect(deployment.Spec.Template.Spec.Volumes).To(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(BeEmpty())
		Expect(findType[*corev1.Service](objects).Spec.Ports).To(HaveLen(1))
	})

	It("still mounts the certificate when only one webhook is on", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Webhooks.EnableDepartmentValidation = ptr.To(false)

		deployment := findType[*appsv1.Deployment](desiredState(krmConfig))
		Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
	})

	// Without this annotation the service-CA operator never mints the Secret, the
	// volume never resolves and the pod never starts.
	It("asks OpenShift to mint the serving certificate", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.Openshift = ptr.To(true)

		service := findType[*corev1.Service](desiredState(krmConfig))

		Expect(service.Annotations).To(HaveKeyWithValue(
			openshiftServingCertAnnotation, config.ProjectControllerCertSecretName))
	})

	It("adds no OpenShift annotation elsewhere", func() {
		service := findType[*corev1.Service](desiredState(newKRMConfig()))

		Expect(service.Annotations).ToNot(HaveKey(openshiftServingCertAnnotation))
	})
})

var _ = Describe("rolebindings plugin ConfigMap", func() {
	dataOf := func(krmConfig *krmv1alpha1.KRMConfig) map[string]string {
		for _, object := range desiredState(krmConfig) {
			if configMap, isConfigMap := object.(*corev1.ConfigMap); isConfigMap &&
				configMap.Name == roleBindingsConfigMapName {
				return configMap.Data
			}
		}
		Fail("no rolebindings ConfigMap was built")
		return nil
	}

	// Limit ranges, the one feature that ships a per-project binding, are off by default.
	It("carries no entry while every feature that ships one is off", func() {
		Expect(dataOf(newKRMConfig())).ToNot(
			HaveKey(roleBindingKey("kai-project-controller-limit-range-per-project")))
	})

	// The controller's own ClusterRole is bound cluster-wide by the chart, so a
	// per-project RoleBinding to it would grant nothing it does not already hold.
	It("does not re-bind the cluster-wide ClusterRole per project", func() {
		for _, entry := range dataOf(newKRMConfig()) {
			Expect(entry).ToNot(ContainSubstring("name: kai-project-controller\n"))
		}
	})

	It("follows the feature flags", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Features.LimitRange = ptr.To(true)

		data := dataOf(krmConfig)

		Expect(data).To(HaveKey(roleBindingKey("kai-project-controller-limit-range-per-project")))
	})

	// Each entry is gated by exactly its own feature and no other.
	It("gates every entry on its own feature", func() {
		for _, builtin := range builtinRoleBindings {
			krmConfig := newKRMConfig()
			features := krmConfig.Spec.ProjectController.Features
			for _, other := range builtinRoleBindings {
				*other.enabledBy(features) = other.name == builtin.name
			}

			data := dataOf(krmConfig)

			Expect(data).To(HaveKey(roleBindingKey(builtin.name)))
			Expect(data).To(HaveLen(1), "%s brought other entries with it", builtin.name)
		}
	})

	// Each names a ClusterRole the chart creates under the same feature flag; a
	// binding whose ClusterRole does not exist grants nothing.
	It("binds every entry to a ClusterRole in this installation's namespace", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Features.LimitRange = ptr.To(true)

		data := dataOf(krmConfig)
		Expect(data).To(HaveLen(len(builtinRoleBindings)))

		for _, builtin := range builtinRoleBindings {
			roleBinding := &rbacRoleBinding{}
			Expect(yaml.Unmarshal([]byte(data[roleBindingKey(builtin.name)]), roleBinding)).To(Succeed())

			Expect(roleBinding.Kind).To(Equal("RoleBinding"))
			Expect(roleBinding.Metadata.Name).To(Equal(builtin.name))
			Expect(roleBinding.Metadata.Namespace).To(BeEmpty())
			Expect(roleBinding.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(roleBinding.RoleRef.Name).To(Equal(builtin.name))
			Expect(roleBinding.Subjects).To(HaveLen(1))
			Expect(roleBinding.Subjects[0].Name).To(Equal(defaultResourceName))
			Expect(roleBinding.Subjects[0].Namespace).To(Equal(testNamespace))
		}
	})

	It("adds an entry for a component this installation does not own", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.ExtraProjectRoleBindings = []krmv1alpha1.ProjectRoleBinding{
			{Name: "acme-widget-project", ServiceAccountName: "acme-widget"},
		}

		roleBinding := &rbacRoleBinding{}
		Expect(yaml.Unmarshal(
			[]byte(dataOf(krmConfig)["acme-widget-project.yaml"]), roleBinding)).To(Succeed())

		Expect(roleBinding.Metadata.Name).To(Equal("acme-widget-project"))
		Expect(roleBinding.RoleRef.Name).To(Equal("acme-widget-project"), "ClusterRole defaults to Name")
		Expect(roleBinding.Subjects[0].Name).To(Equal("acme-widget"))
		Expect(roleBinding.Subjects[0].Namespace).To(Equal(testNamespace))
	})

	It("binds a ClusterRole under a different name when asked", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.ExtraProjectRoleBindings = []krmv1alpha1.ProjectRoleBinding{
			{Name: "acme-widget-project", ClusterRoleName: "acme-shared", ServiceAccountName: "acme-widget"},
		}

		roleBinding := &rbacRoleBinding{}
		Expect(yaml.Unmarshal(
			[]byte(dataOf(krmConfig)["acme-widget-project.yaml"]), roleBinding)).To(Succeed())

		Expect(roleBinding.RoleRef.Name).To(Equal("acme-shared"))
	})

	It("ignores an extra that reuses a shipped binding name", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Features.LimitRange = ptr.To(true)
		krmConfig.Spec.ProjectController.ExtraProjectRoleBindings = []krmv1alpha1.ProjectRoleBinding{
			{Name: "kai-project-controller-limit-range-per-project", ServiceAccountName: "someone-else"},
		}

		data := dataOf(krmConfig)

		Expect(data).To(HaveLen(1), "the extra must not add a second entry")
		Expect(data[roleBindingKey("kai-project-controller-limit-range-per-project")]).
			To(ContainSubstring(defaultResourceName))
		Expect(data[roleBindingKey("kai-project-controller-limit-range-per-project")]).
			ToNot(ContainSubstring("someone-else"))
	})

	It("accepts that name once the feature that ships it is off", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Features.LimitRange = ptr.To(false)
		krmConfig.Spec.ProjectController.ExtraProjectRoleBindings = []krmv1alpha1.ProjectRoleBinding{
			{Name: "kai-project-controller-limit-range-per-project", ServiceAccountName: "someone-else"},
		}

		data := dataOf(krmConfig)

		Expect(data[roleBindingKey("kai-project-controller-limit-range-per-project")]).
			To(ContainSubstring("someone-else"))
	})

})

// rbacRoleBinding decodes only what the assertions read, so a test failure points
// at the field that changed rather than at a whole-object comparison.
type rbacRoleBinding struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	RoleRef struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"roleRef"`
	Subjects []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"subjects"`
}

var _ = Describe("delete blockers ConfigMap", func() {
	dataOf := func(krmConfig *krmv1alpha1.KRMConfig) map[string]string {
		for _, object := range desiredState(krmConfig) {
			if configMap, isConfigMap := object.(*corev1.ConfigMap); isConfigMap &&
				configMap.Name == deleteBlockersConfigMapName {
				return configMap.Data
			}
		}
		Fail("no delete blockers ConfigMap was built")
		return nil
	}

	// A nil slice would marshal to "null", which the controller cannot range over.
	It("writes an empty list when nothing blocks deletion", func() {
		Expect(dataOf(newKRMConfig())).To(HaveKeyWithValue(blockersConfigMapKey, "[]\n"))
	})

	It("writes the configured blockers", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.DeleteBlockers = []krmv1alpha1.DeleteBlocker{
			{DisplayName: "Pvcs", Version: "v1", Kind: "PersistentVolumeClaim"},
		}

		encoded := dataOf(krmConfig)[blockersConfigMapKey]

		var decoded []krmv1alpha1.DeleteBlocker
		Expect(yaml.Unmarshal([]byte(encoded), &decoded)).To(Succeed())
		Expect(decoded).To(HaveLen(1))
		Expect(decoded[0].DisplayName).To(Equal("Pvcs"))
		Expect(decoded[0].Kind).To(Equal("PersistentVolumeClaim"))
	})
})
