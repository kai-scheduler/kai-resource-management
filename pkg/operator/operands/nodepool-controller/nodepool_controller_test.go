// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nodepoolcontroller

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

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/config"
)

func TestNodePoolController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodePoolController operand suite")
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
	objects, err := (&NodePoolController{}).DesiredState(context.Background(), newClient(existing...), krmConfig)
	Expect(err).ToNot(HaveOccurred())
	return objects
}

func desiredStateWithoutPrometheus() []client.Object {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	runtimeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	objects, err := (&NodePoolController{}).DesiredState(context.Background(), runtimeClient, newKRMConfig())
	Expect(err).ToNot(HaveOccurred())
	return objects
}

var _ = Describe("DesiredState", func() {
	It("builds the whole service", func() {
		objects := desiredState(newKRMConfig())

		Expect(objects).To(HaveLen(5))
		Expect(findType[*appsv1.Deployment](objects)).ToNot(BeNil())
		Expect(findType[*corev1.ServiceAccount](objects)).ToNot(BeNil())
		Expect(findType[*corev1.Service](objects)).ToNot(BeNil())
		Expect(monitorNamed(objects, defaultResourceName)).ToNot(BeNil())
		Expect(monitorNamed(objects, defaultResourceName+accountingMonitorSuffix)).ToNot(BeNil())
	})

	// Every object is keyed by GVK in the diff, so a blank TypeMeta would make the
	// operator fail to match what it already created and recreate it every reconcile.
	It("stamps the group, version and kind on every object", func() {
		for _, object := range desiredState(newKRMConfig()) {
			Expect(object.GetObjectKind().GroupVersionKind().Kind).ToNot(BeEmpty(),
				"%T carries no Kind", object)
		}
	})

	It("puts every object in the configured namespace", func() {
		for _, object := range desiredState(newKRMConfig()) {
			Expect(object.GetNamespace()).To(Equal(testNamespace))
		}
	})

	It("builds nothing when the service is disabled", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.NodePoolController.Service.Enabled = ptr.To(false)

		Expect(desiredState(krmConfig)).To(BeEmpty())
	})

	// Monitoring is on by default, so a cluster without the Prometheus operator must
	// still get the service rather than a failed build.
	It("skips both ServiceMonitors when the Prometheus operator is absent", func() {
		objects := desiredStateWithoutPrometheus()

		Expect(objects).To(HaveLen(3))
		for _, object := range objects {
			Expect(object).ToNot(BeAssignableToTypeOf(&monitoringv1.ServiceMonitor{}))
		}
	})

	It("drops both ServiceMonitors when monitoring is disabled", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.ServiceMonitor.Enabled = ptr.To(false)

		Expect(desiredState(krmConfig)).To(HaveLen(3))
	})

	// The accounting monitor is the only one of the two that is separately toggled.
	It("drops only the accounting ServiceMonitor when accounting is disabled", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.ServiceMonitor.Accounting = ptr.To(false)

		objects := desiredState(krmConfig)

		Expect(objects).To(HaveLen(4))
		Expect(monitorNamed(objects, defaultResourceName)).ToNot(BeNil())
		Expect(monitorNamed(objects, defaultResourceName+accountingMonitorSuffix)).To(BeNil())
	})

	It("adds a VerticalPodAutoscaler only when one is configured", func() {
		Expect(desiredState(newKRMConfig())).To(HaveLen(5))

		krmConfig := newKRMConfig()
		krmConfig.Spec.NodePoolController.VPA.Enabled = ptr.To(true)

		Expect(desiredState(krmConfig)).To(HaveLen(6))
	})
})

var _ = Describe("Name", func() {
	It("reports the operand name the status conditions carry", func() {
		Expect((&NodePoolController{}).Name()).To(Equal("NodePoolController"))
	})
})
