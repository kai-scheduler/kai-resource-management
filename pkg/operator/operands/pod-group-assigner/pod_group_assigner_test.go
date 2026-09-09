// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package podgroupassigner

import (
	"context"
	"testing"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/config"
)

func TestPodGroupAssigner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PodGroupAssigner operand suite")
}

const testNamespace = "kai-resource-management"

func newClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
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
	objects, err := (&PodGroupAssigner{}).DesiredState(context.Background(), newClient(existing...), krmConfig)
	Expect(err).ToNot(HaveOccurred())
	return objects
}

var _ = Describe("DesiredState", func() {
	It("builds the whole service", func() {
		objects := desiredState(newKRMConfig())

		Expect(objects).To(HaveLen(3))
		Expect(findType[*appsv1.Deployment](objects)).ToNot(BeNil())
		Expect(findType[*corev1.ServiceAccount](objects)).ToNot(BeNil())
		Expect(findType[*corev1.Service](objects)).ToNot(BeNil())
	})

	// Every object is keyed by GVK in the diff, so a blank TypeMeta would make the
	// operator fail to match what it already created and recreate it every reconcile.
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
		krmConfig.Spec.PodGroupAssigner.Service.Enabled = ptr.To(false)

		Expect(desiredState(krmConfig)).To(BeEmpty())
	})

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

	It("labels every object with the service name", func() {
		for _, object := range desiredState(newKRMConfig()) {
			Expect(object.GetLabels()).To(HaveKeyWithValue("app", defaultResourceName),
				"wrong app label on %s", object.GetName())
		}
	})

	It("creates a VPA only when one is enabled", func() {
		Expect(desiredState(newKRMConfig())).To(HaveLen(3))

		krmConfig := newKRMConfig()
		krmConfig.Spec.PodGroupAssigner.VPA.Enabled = ptr.To(true)
		Expect(desiredState(krmConfig)).To(HaveLen(4))
	})

	It("exposes no metrics", func() {
		objects := desiredState(newKRMConfig())

		Expect(findType[*corev1.Service](objects).Spec.Ports).To(HaveLen(1))
		Expect(findType[*appsv1.Deployment](objects).Spec.Template.Spec.Containers[0].Ports).To(HaveLen(1))
		Expect(buildArgsList(newKRMConfig())).ToNot(ContainElement("--metrics-port"))
	})

	// FIPS mode reaches every service through the shared Deployment builder.
	It("carries the FIPS mode as GODEBUG", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.FipsMode = ptr.To(krmv1alpha1.FipsModeOnly)

		deployment := findType[*appsv1.Deployment](desiredState(krmConfig))

		Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(
			ContainElement(corev1.EnvVar{Name: "GODEBUG", Value: "fips140=only"}))
	})
})

// The binary registers its PodGroup mutating handler unconditionally, so unlike
// project-controller none of this is gated on a toggle.
var _ = Describe("webhook wiring", func() {
	It("mounts the chart's TLS Secret and publishes the webhook port", func() {
		objects := desiredState(newKRMConfig())

		deployment := findType[*appsv1.Deployment](objects)
		Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName).
			To(Equal(config.PodGroupAssignerCertSecretName))
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath).To(Equal(certMountPath))

		service := findType[*corev1.Service](objects)
		Expect(service.Spec.Ports[0].Name).To(Equal("webhook"))
		Expect(service.Spec.Ports[0].Port).To(Equal(int32(443)))
		Expect(service.Spec.Ports[0].TargetPort.IntVal).To(Equal(int32(8443)))
	})

	// A missing Secret has to stop the pod, not leave a webhook that never answers.
	It("requires the Secret rather than tolerating its absence", func() {
		deployment := findType[*appsv1.Deployment](desiredState(newKRMConfig()))

		Expect(deployment.Spec.Template.Spec.Volumes[0].Secret.Optional).To(BeNil())
	})

	It("keeps the certificate mounted with the pod webhook off", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.PodGroupAssigner.Webhooks.EnablePodWebhook = ptr.To(false)

		deployment := findType[*appsv1.Deployment](desiredState(krmConfig))

		Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(1))
	})

	It("asks OpenShift to mint the serving certificate", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.Openshift = ptr.To(true)

		service := findType[*corev1.Service](desiredState(krmConfig))

		Expect(service.Annotations).To(HaveKeyWithValue(
			openshiftServingCertAnnotation, config.PodGroupAssignerCertSecretName))
	})

	It("still asks for it with the pod webhook off", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.Openshift = ptr.To(true)
		krmConfig.Spec.PodGroupAssigner.Webhooks.EnablePodWebhook = ptr.To(false)

		service := findType[*corev1.Service](desiredState(krmConfig))

		Expect(service.Annotations).To(HaveKey(openshiftServingCertAnnotation))
	})

	It("adds no OpenShift annotation elsewhere", func() {
		service := findType[*corev1.Service](desiredState(newKRMConfig()))

		Expect(service.Annotations).ToNot(HaveKey(openshiftServingCertAnnotation))
	})
})
