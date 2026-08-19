// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operator config suite")
}

var _ = Describe("SetDefaultsWhereNeeded", func() {
	It("fills in an entirely empty spec", func() {
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.Namespace).To(Equal(OperatorNamespace()))
		Expect(spec.Global.ReplicaCount).To(Equal(ptr.To(int32(1))))
		// Off rather than on: a FIPS-built binary already runs with fips140=on, so
		// defaulting to anything else would change the behaviour of an image chosen
		// for how it was built.
		Expect(spec.Global.FipsMode).To(Equal(ptr.To(krmv1alpha1.FipsModeOff)))
		Expect(spec.NodePoolController.Service.Image.Name).To(Equal(ptr.To(NodePoolControllerImageName)))
		Expect(spec.ProjectController.Service.Image.Name).To(Equal(ptr.To(ProjectControllerImageName)))
		Expect(spec.PodGroupAssigner.Service.Image.Name).To(Equal(ptr.To(PodGroupAssignerImageName)))
		Expect(spec.Global.ServiceMonitor.Enabled).To(Equal(ptr.To(true)))
		Expect(spec.Global.ServiceMonitor.Accounting).To(Equal(ptr.To(true)))
	})

	It("keeps the accounting monitor turned off when it was asked to", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			Global: &krmv1alpha1.GlobalConfig{
				ServiceMonitor: &krmv1alpha1.ServiceMonitorSpec{Accounting: ptr.To(false)},
			},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.Global.ServiceMonitor.Accounting).To(Equal(ptr.To(false)))
		Expect(spec.Global.ServiceMonitor.Enabled).To(Equal(ptr.To(true)))
	})

	// The services belong beside the operator, wherever the chart was installed.
	It("deploys into the namespace the operator runs in", func() {
		GinkgoT().Setenv(podNamespaceEnvVar, "somewhere-else")
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.Namespace).To(Equal("somewhere-else"))
	})

	It("keeps every value that was set", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			Namespace: "elsewhere",
			Global: &krmv1alpha1.GlobalConfig{
				ReplicaCount: ptr.To(int32(3)),
				Openshift:    ptr.To(true),
				FipsMode:     ptr.To(krmv1alpha1.FipsModeOnly),
			},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.Namespace).To(Equal("elsewhere"))
		Expect(spec.Global.ReplicaCount).To(Equal(ptr.To(int32(3))))
		Expect(spec.Global.Openshift).To(Equal(ptr.To(true)))
		Expect(spec.Global.FipsMode).To(Equal(ptr.To(krmv1alpha1.FipsModeOnly)))
	})

	// An explicit false must not be mistaken for an unset field, which is why every
	// optional value is a pointer.
	It("does not overwrite an explicit false", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			Global: &krmv1alpha1.GlobalConfig{JSONLog: ptr.To(false)},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.Global.JSONLog).To(Equal(ptr.To(false)))
	})

	It("gives each service the global replica count unless it sets its own", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			Global:            &krmv1alpha1.GlobalConfig{ReplicaCount: ptr.To(int32(2))},
			ProjectController: &krmv1alpha1.ProjectController{Replicas: ptr.To(int32(5))},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.NodePoolController.Replicas).To(Equal(ptr.To(int32(2))))
		Expect(spec.ProjectController.Replicas).To(Equal(ptr.To(int32(5))))
	})

	It("gives each service the global VPA unless it sets its own", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			Global: &krmv1alpha1.GlobalConfig{
				VPA: &kaicommon.VPASpec{Enabled: ptr.To(true)},
			},
			PodGroupAssigner: &krmv1alpha1.PodGroupAssigner{
				VPA: &kaicommon.VPASpec{Enabled: ptr.To(false)},
			},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.NodePoolController.VPA.Enabled).To(Equal(ptr.To(true)))
		Expect(spec.PodGroupAssigner.VPA.Enabled).To(Equal(ptr.To(false)))
	})

	It("hardens the container security context by default", func() {
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)

		securityContext := spec.Global.SecurityContext
		Expect(securityContext.RunAsNonRoot).To(Equal(ptr.To(true)))
		Expect(securityContext.RunAsUser).To(Equal(ptr.To(int64(10000))))
		Expect(securityContext.AllowPrivilegeEscalation).To(Equal(ptr.To(false)))
		Expect(securityContext.Capabilities.Drop).To(ConsistOf(corev1.Capability("all")))
	})

	// The scheduler settings stay empty so the operator can tell "not configured"
	// from "configured to the default" and fall back to the scheduler's own config.
	It("leaves the scheduler settings unset", func() {
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.Global.SchedulerName).To(BeNil())
		Expect(spec.Global.QueueLabelKey).To(BeNil())
		Expect(spec.Global.NodePoolLabelKey).To(BeNil())
	})

	// The generic kai/v1/common default is 100m/512Mi, which would quarter
	// project-controller's memory limit relative to what the chart deploys today.
	It("gives each service its own resource default", func() {
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.ProjectController.Service.Resources.Limits.Memory().String()).To(Equal("2Gi"))
		Expect(spec.ProjectController.Service.Resources.Limits.Cpu().String()).To(Equal("300m"))
		Expect(spec.NodePoolController.Service.Resources.Limits.Memory().String()).To(Equal("1Gi"))
		Expect(spec.NodePoolController.Service.Resources.Limits.Cpu().String()).To(Equal("900m"))
		Expect(spec.PodGroupAssigner.Service.Resources.Limits.Memory().String()).To(Equal("512Mi"))
	})

	// The rate each binary asks for itself. kai/v1/common defaults to 20/100, which
	// would throttle every controller to a fraction of what it expects.
	It("gives each service the client rate its binary defaults to", func() {
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)

		for _, clientConfig := range []*kaicommon.K8sClientConfig{
			spec.NodePoolController.Service.K8sClientConfig,
			spec.ProjectController.Service.K8sClientConfig,
			spec.PodGroupAssigner.Service.K8sClientConfig,
		} {
			Expect(clientConfig.QPS).To(Equal(ptr.To(DefaultClientQPS)))
			Expect(clientConfig.Burst).To(Equal(ptr.To(DefaultClientBurst)))
		}
	})

	It("keeps a client rate that was set", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			ProjectController: &krmv1alpha1.ProjectController{
				Service: &kaicommon.Service{
					K8sClientConfig: &kaicommon.K8sClientConfig{QPS: ptr.To(5)},
				},
			},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.ProjectController.Service.K8sClientConfig.QPS).To(Equal(ptr.To(5)))
		Expect(spec.ProjectController.Service.K8sClientConfig.Burst).To(Equal(ptr.To(DefaultClientBurst)))
	})

	It("keeps resources that were set", func() {
		spec := &krmv1alpha1.KRMConfigSpec{
			ProjectController: &krmv1alpha1.ProjectController{
				Service: &kaicommon.Service{
					Resources: &kaicommon.Resources{
						Limits: corev1.ResourceList{corev1.ResourceMemory: apiresource.MustParse("4Gi")},
					},
				},
			},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.ProjectController.Service.Resources.Limits.Memory().String()).To(Equal("4Gi"))
	})

	It("is idempotent", func() {
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)
		first := spec.DeepCopy()
		SetDefaultsWhereNeeded(spec)

		Expect(spec).To(Equal(first))
	})
})
