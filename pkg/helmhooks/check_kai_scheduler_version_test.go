// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("DetectKAISchedulerVersion", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	kaiConfig := &kaiv1.Config{
		ObjectMeta: metav1.ObjectMeta{Name: "kai-config"},
		Spec:       kaiv1.ConfigSpec{Namespace: "kai-scheduler"},
	}
	kaiOperator := func(image string) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "kai-operator", Namespace: "kai-scheduler"},
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "operator", Image: image}},
			}}},
		}
	}
	detect := func(objects ...client.Object) (string, string, error) {
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(kaiv1.AddToScheme(scheme))
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
		return DetectKAISchedulerVersion(ctx, reader)
	}

	It("reads the kai-operator image tag in the namespace kai-config names", func() {
		version, source, err := detect(kaiConfig, kaiOperator("ghcr.io/kai-scheduler/operator:v0.18.2"))

		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal("v0.18.2"))
		Expect(source).To(Equal("the kai-operator image tag"))
	})

	It("fails, pointing at the explicit version, when KAI Scheduler is not installed", func() {
		_, _, err := detect()

		Expect(err).To(MatchError(And(ContainSubstring("kai-config"), ContainSubstring("versionCheck.kaiSchedulerVersion"))))
	})

	It("fails when kai-config names no running kai-operator", func() {
		_, _, err := detect(kaiConfig)

		Expect(err).To(MatchError(ContainSubstring("kai-scheduler/kai-operator")))
	})
})

var _ = Describe("CheckKAISchedulerVersion", func() {
	const (
		minimum = "v0.18.0"
		source  = "the kai-operator image tag"
	)
	ctx := context.Background()

	DescribeTable("accepts a supported version",
		func(installed string) {
			Expect(CheckKAISchedulerVersion(ctx, installed, source, minimum)).To(Succeed())
		},
		Entry("the minimum itself", "v0.18.0"),
		Entry("a newer patch", "v0.18.2"),
		Entry("a newer minor", "v0.19.0"),
		Entry("a release candidate of a newer minor", "v0.19.0-rc.1"),
		Entry("the FIPS build of the minimum", "v0.18.0-fips"),
		Entry("a main branch build", "0.0.0-1db3d56"),
	)

	DescribeTable("rejects an older version, naming it, its source and the minimum",
		func(installed string) {
			Expect(CheckKAISchedulerVersion(ctx, installed, source, minimum)).To(MatchError(And(
				ContainSubstring(installed),
				ContainSubstring(source),
				ContainSubstring("older than the minimum supported "+minimum))))
		},
		Entry("an older minor", "v0.17.3"),
		Entry("a release candidate of the minimum", "v0.18.0-rc.1"),
	)

	It("rejects a version that is not a version", func() {
		Expect(CheckKAISchedulerVersion(ctx, "latest", source, minimum)).
			To(MatchError(ContainSubstring(`"latest"`)))
		Expect(CheckKAISchedulerVersion(ctx, "v0.18.2", source, "latest")).
			To(MatchError(ContainSubstring(`"latest"`)))
	})
})
