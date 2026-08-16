// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/kai-scheduler/api/constants"
	kaicommon "github.com/kai-scheduler/api/kai/v1/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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
		Expect(spec.SchedulerConfigRef.Name).To(Equal(constants.DefaultKAIConfigSingeltonInstanceName))
		Expect(spec.Global.ReplicaCount).To(Equal(ptr.To(int32(1))))
		Expect(spec.NodePoolController.Service.Image.Name).To(Equal(ptr.To(NodePoolControllerImageName)))
		Expect(spec.ProjectController.Service.Image.Name).To(Equal(ptr.To(ProjectControllerImageName)))
		Expect(spec.PodGroupAssigner.Service.Image.Name).To(Equal(ptr.To(PodGroupAssignerImageName)))
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
			},
			SchedulerConfigRef: &krmv1alpha1.SchedulerConfigRef{Name: "other-config"},
		}

		SetDefaultsWhereNeeded(spec)

		Expect(spec.Namespace).To(Equal("elsewhere"))
		Expect(spec.Global.ReplicaCount).To(Equal(ptr.To(int32(3))))
		Expect(spec.Global.Openshift).To(Equal(ptr.To(true)))
		Expect(spec.SchedulerConfigRef.Name).To(Equal("other-config"))
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

	It("is idempotent", func() {
		spec := &krmv1alpha1.KRMConfigSpec{}

		SetDefaultsWhereNeeded(spec)
		first := spec.DeepCopy()
		SetDefaultsWhereNeeded(spec)

		Expect(spec).To(Equal(first))
	})
})
