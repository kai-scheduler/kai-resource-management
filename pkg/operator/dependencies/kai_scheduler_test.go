// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"
	"errors"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const configName = kaiconstants.DefaultKAIConfigSingeltonInstanceName

func kaiScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(kaiv1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

// kaiConfig builds the Config CR with the given Ready condition. Passing an empty
// status leaves the condition off entirely, as it is before KAI first reconciles.
func kaiConfig(readyStatus metav1.ConditionStatus, message string) *kaiv1.Config {
	config := &kaiv1.Config{ObjectMeta: metav1.ObjectMeta{Name: configName}}
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

func kaiReader(objects ...client.Object) client.Reader {
	return fake.NewClientBuilder().WithScheme(kaiScheme()).WithObjects(objects...).Build()
}

var _ = Describe("KAIScheduler.Check", func() {
	ctx := context.Background()
	It("reports nothing when the Config exists and is ready", func() {
		reader := kaiReader(kaiConfig(metav1.ConditionTrue, ""))

		message, err := (&KAIScheduler{}).Check(ctx, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(BeEmpty())
	})

	// The kind cannot be mapped at all, which is what an uninstalled KAI looks
	// like — distinct from the Config merely being absent, below.
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

		message, err := (&KAIScheduler{}).Check(ctx, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal("KAI Scheduler is not installed: no kai.scheduler/v1 Config API"))
	})

	It("reports a missing Config when the API is there without it", func() {
		message, err := (&KAIScheduler{}).Check(ctx, kaiReader())

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(`KAI Scheduler Config "kai-config" does not exist`))
	})

	It("repeats what KAI says about itself when it is not ready", func() {
		reader := kaiReader(kaiConfig(metav1.ConditionFalse, "binder is not available"))

		message, err := (&KAIScheduler{}).Check(ctx, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(
			`KAI Scheduler Config "kai-config" is not ready: binder is not available`))
	})

	It("reports an unready Config that gives no message", func() {
		reader := kaiReader(kaiConfig(metav1.ConditionFalse, ""))

		message, err := (&KAIScheduler{}).Check(ctx, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(`KAI Scheduler Config "kai-config" is not ready`))
	})

	// The few seconds after KAI is installed, before its operator gets to it.
	It("reports a Config that has not been reconciled yet", func() {
		reader := kaiReader(kaiConfig("", ""))

		message, err := (&KAIScheduler{}).Check(ctx, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(`KAI Scheduler Config "kai-config" has not reported readiness`))
	})

	It("treats an Unknown readiness as not ready", func() {
		reader := kaiReader(kaiConfig(metav1.ConditionUnknown, "still starting"))

		message, err := (&KAIScheduler{}).Check(ctx, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(Equal(
			`KAI Scheduler Config "kai-config" is not ready: still starting`))
	})

	// A cluster that could not be asked is not a cluster that answered "absent".
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

		message, err := (&KAIScheduler{}).Check(ctx, reader)

		Expect(err).To(MatchError(ContainSubstring("apiserver unavailable")))
		Expect(err).To(MatchError(ContainSubstring("kai-config")))
		Expect(message).To(BeEmpty())
	})
})
