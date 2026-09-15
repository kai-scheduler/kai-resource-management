// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"
	"errors"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	configName   = kaiconstants.DefaultKAIConfigSingeltonInstanceName
	kaiNamespace = "kai-scheduler"
	minimum      = "v0.16.9"
)

// kaiOperator carries the image tag the version is read from.
func kaiOperator(image, msTag string) *appsv1.Deployment {
	container := corev1.Container{Name: "operator", Image: image}
	if msTag != "" {
		container.Env = []corev1.EnvVar{{Name: "MS_TAG", Value: msTag}}
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "kai-operator", Namespace: kaiNamespace},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{container}},
			},
		},
	}
}

func kaiScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(appsv1.AddToScheme(scheme)).To(Succeed())
	Expect(kaiv1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

// kaiConfig builds the Config CR; an empty status omits the condition entirely.
func kaiConfig(readyStatus metav1.ConditionStatus, message string) *kaiv1.Config {
	config := &kaiv1.Config{
		ObjectMeta: metav1.ObjectMeta{Name: configName},
		Spec:       kaiv1.ConfigSpec{Namespace: kaiNamespace},
	}
	if readyStatus == "" {
		return config
	}
	config.Status.Conditions = []metav1.Condition{{
		Type:               string(kaiv1.ConditionTypeReady),
		Status:             readyStatus,
		Reason:             "ForTest",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}}
	return config
}

// check runs a Checker against one fake cluster, standing in for both readers.
func check(
	k *KAIScheduler, ctx context.Context, objects ...client.Object,
) (string, error) {
	reader := kaiReader(objects...)
	return k.Check(ctx, reader, reader)
}

func kaiReader(objects ...client.Object) client.Reader {
	return fake.NewClientBuilder().WithScheme(kaiScheme()).WithObjects(objects...).Build()
}

var _ = Describe("KAIScheduler.Check", func() {
	ctx := context.Background()
	It("reports nothing when the Config exists and is ready", func() {
		reader := kaiReader(kaiConfig(metav1.ConditionTrue, ""))

		message, err := (&KAIScheduler{}).Check(ctx, reader, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	It("reports the scheduler as uninstalled when its API is absent", func() {
		reader := fake.NewClientBuilder().WithScheme(kaiScheme()).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object,
					_ ...client.GetOption,
				) error {
					return &meta.NoKindMatchError{
						GroupKind: schema.GroupKind{Group: "kai.scheduler", Kind: "Config"},
					}
				},
			}).Build()

		message, err := (&KAIScheduler{}).Check(ctx, reader, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal("KAI Scheduler is not installed: no kai.scheduler/v1 Config API"))
	})

	It("reports a missing Config when the API is there without it", func() {
		message, err := check(&KAIScheduler{}, ctx)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(`KAI Scheduler Config "kai-config" does not exist`))
	})

	It("repeats what KAI says about itself when it is not ready", func() {
		reader := kaiReader(kaiConfig(metav1.ConditionFalse, "binder is not available"))

		message, err := (&KAIScheduler{}).Check(ctx, reader, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(
			`KAI Scheduler Config "kai-config" is not ready: binder is not available`))
	})

	It("reports an unready Config that gives no message", func() {
		reader := kaiReader(kaiConfig(metav1.ConditionFalse, ""))

		message, err := (&KAIScheduler{}).Check(ctx, reader, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(`KAI Scheduler Config "kai-config" is not ready`))
	})

	It("reports a Config that has not been reconciled yet", func() {
		reader := kaiReader(kaiConfig("", ""))

		message, err := (&KAIScheduler{}).Check(ctx, reader, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(`KAI Scheduler Config "kai-config" has not reported readiness`))
	})

	It("treats an Unknown readiness as not ready", func() {
		reader := kaiReader(kaiConfig(metav1.ConditionUnknown, "still starting"))

		message, err := (&KAIScheduler{}).Check(ctx, reader, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(
			`KAI Scheduler Config "kai-config" is not ready: still starting`))
	})

	It("returns an error rather than a message when reading the Config fails", func() {
		reader := fake.NewClientBuilder().WithScheme(kaiScheme()).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					_ context.Context, runtimeClient client.WithWatch, key client.ObjectKey,
					obj client.Object, _ ...client.GetOption,
				) error {
					if _, isConfig := obj.(*kaiv1.Config); isConfig {
						return errors.New("apiserver unavailable")
					}
					return runtimeClient.Get(ctx, key, obj)
				},
			}).Build()

		message, err := (&KAIScheduler{}).Check(ctx, reader, reader)

		Expect(err).To(MatchError(ContainSubstring("apiserver unavailable")))
		Expect(err).To(MatchError(ContainSubstring("kai-config")))
		Expect(message).To(BeEmpty())
	})
})

var _ = Describe("KAIScheduler.Check version", func() {
	ctx := context.Background()
	checker := &KAIScheduler{MinimumVersion: minimum}

	checkWith := func(image, msTag string) string {
		message, err := check(checker, ctx,
			kaiConfig(metav1.ConditionTrue, ""), kaiOperator(image, msTag))
		Expect(err).ToNot(HaveOccurred())
		return message
	}

	It("accepts the minimum itself", func() {
		Expect(checkWith("repo/operator:v0.16.9", "")).To(BeEmpty())
	})

	It("accepts a newer patch and minor", func() {
		Expect(checkWith("repo/operator:v0.17.3", "")).To(BeEmpty())
		Expect(checkWith("repo/operator:v0.18.0", "")).To(BeEmpty())
	})

	It("reports one older than the minimum", func() {
		Expect(checkWith("repo/operator:v0.14.2", "")).To(Equal(
			"KAI Scheduler v0.14.2 is older than the minimum supported v0.16.9"))
	})

	It("accepts a newer major", func() {
		Expect(checkWith("repo/operator:v1.0.0", "")).To(BeEmpty())
	})

	// Semver orders a prerelease below its release, so the suffix must come off.
	It("accepts the FIPS build of the minimum", func() {
		Expect(checkWith("repo/operator:v0.16.9-fips", "")).To(BeEmpty())
	})

	It("reads the tag through a registry port", func() {
		Expect(checkWith("registry.local:5000/kai/operator:v0.14.0", "")).To(
			ContainSubstring("older than"))
	})

	It("falls back to MS_TAG when the image is pinned by digest", func() {
		Expect(checkWith("repo/operator@sha256:"+
			"1111111111111111111111111111111111111111111111111111111111111111", "v0.14.0")).
			To(ContainSubstring("older than"))
	})

	DescribeTable("skips a version it cannot read",
		func(image, msTag string) {
			Expect(checkWith(image, msTag)).To(BeEmpty())
		},
		Entry("a tag that is not a version", "repo/operator:latest", ""),
		Entry("no tag at all", "repo/operator", ""),
		Entry("a digest with no MS_TAG", "repo/operator@sha256:"+
			"1111111111111111111111111111111111111111111111111111111111111111", ""),
	)

	It("skips the check when no minimum is configured", func() {
		message, err := check(&KAIScheduler{}, ctx,
			kaiConfig(metav1.ConditionTrue, ""), kaiOperator("repo/operator:v0.1.0", ""))

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	It("skips the check when a minimum is not a version", func() {
		message, err := check(&KAIScheduler{MinimumVersion: "not-a-version"}, ctx,
			kaiConfig(metav1.ConditionTrue, ""), kaiOperator("repo/operator:v0.1.0", ""))

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	It("skips the check when the operator Deployment is absent", func() {
		message, err := check(checker, ctx, kaiConfig(metav1.ConditionTrue, ""))

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	It("skips the check when the Config names no namespace", func() {
		config := kaiConfig(metav1.ConditionTrue, "")
		config.Spec.Namespace = ""

		message, err := check(checker, ctx, config)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	// A downgrade past the minimum tends to make KAI report itself unready too,
	// so readiness first would hide the message that names the cause.
	It("reports both the version and unreadiness when both are wrong", func() {
		message, err := check(checker, ctx,
			kaiConfig(metav1.ConditionFalse, "starting"), kaiOperator("repo/operator:v0.1.0", ""))

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(ContainSubstring("older than"))
		Expect(message).To(ContainSubstring("is not ready"))
	})

	It("reports unreadiness when the version is supported", func() {
		message, err := check(checker, ctx,
			kaiConfig(metav1.ConditionFalse, "starting"), kaiOperator("repo/operator:v0.16.9", ""))

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(ContainSubstring("is not ready"))
	})
})
