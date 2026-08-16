// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package statusreconciler

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

func TestStatusReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Status reconciler suite")
}

// fakeDeployable reports whatever the test needs, so conditions can be driven
// without any real operands.
type fakeDeployable struct {
	deployed  bool
	available bool
	missing   string
}

func (d *fakeDeployable) Deploy(
	_ context.Context, _ client.Client, _ *krmv1alpha1.KRMConfig, _ client.Object,
) error {
	return nil
}

func (d *fakeDeployable) IsDeployed(_ context.Context, _ client.Reader) (bool, error) {
	return d.deployed, nil
}

func (d *fakeDeployable) IsAvailable(_ context.Context, _ client.Reader) (bool, error) {
	return d.available, nil
}

func (d *fakeDeployable) Monitor(_ context.Context, _ client.Reader, _ *krmv1alpha1.KRMConfig) error {
	return nil
}

func (d *fakeDeployable) HasMissingDependencies(
	_ context.Context, _ client.Reader, _ *krmv1alpha1.KRMConfig,
) (string, error) {
	return d.missing, nil
}

var _ = Describe("StatusReconciler", func() {
	var (
		ctx           context.Context
		krmConfig     *krmv1alpha1.KRMConfig
		runtimeClient client.Client
		deployable    *fakeDeployable
		reconciler    *StatusReconciler
	)

	BeforeEach(func() {
		ctx = context.Background()

		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(krmv1alpha1.AddToScheme(scheme)).To(Succeed())

		krmConfig = &krmv1alpha1.KRMConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:       krmv1alpha1.KRMConfigSingletonName,
				Generation: 1,
			},
		}
		runtimeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(krmConfig).
			WithStatusSubresource(krmConfig).
			Build()
		deployable = &fakeDeployable{deployed: true, available: true}
		reconciler = New(runtimeClient, deployable)
	})

	conditionOf := func(conditionType krmv1alpha1.ConditionType) *metav1.Condition {
		stored := &krmv1alpha1.KRMConfig{}
		Expect(runtimeClient.Get(ctx,
			types.NamespacedName{Name: krmv1alpha1.KRMConfigSingletonName}, stored)).To(Succeed())
		for index, condition := range stored.Status.Conditions {
			if condition.Type == string(conditionType) {
				return &stored.Status.Conditions[index]
			}
		}
		return nil
	}

	Context("starting a reconcile", func() {
		It("reports that reconciliation is in progress", func() {
			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())

			reconciling := conditionOf(krmv1alpha1.ConditionTypeReconciling)
			Expect(reconciling).ToNot(BeNil())
			Expect(reconciling.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconciling.ObservedGeneration).To(Equal(int64(1)))
		})

		// A stale Deployed=True from the previous generation would read as
		// "already done" while a new spec is rolling out.
		It("refreshes Deployed at the same time", func() {
			deployable.deployed = false

			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.ConditionTypeDeployed).Status).To(Equal(metav1.ConditionFalse))
		})

		// Otherwise the status patch triggers the reconcile that writes it again.
		It("does nothing when it already ran for this generation", func() {
			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())
			afterFirst := conditionOf(krmv1alpha1.ConditionTypeReconciling).LastTransitionTime

			deployable.deployed = false
			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.ConditionTypeReconciling).LastTransitionTime).To(Equal(afterFirst))
			Expect(conditionOf(krmv1alpha1.ConditionTypeDeployed).Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("finishing a reconcile", func() {
		It("reports ready when everything is deployed and available", func() {
			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.ConditionTypeDeployed).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.ConditionTypeAvailable).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.ConditionTypeDependenciesFulfilled).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.ConditionTypeReady).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.ConditionTypeReconciling).Status).To(Equal(metav1.ConditionFalse))
		})

		It("is not ready while the services are unavailable", func() {
			deployable.available = false

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.ConditionTypeAvailable).Status).To(Equal(metav1.ConditionFalse))
			Expect(conditionOf(krmv1alpha1.ConditionTypeReady).Status).To(Equal(metav1.ConditionFalse))
			Expect(conditionOf(krmv1alpha1.ConditionTypeReady).Reason).To(Equal(string(krmv1alpha1.ReasonNotReady)))
		})

		// Ready is what Helm --wait and kstatus watch, so it must not claim the
		// installation is ready while a dependency is missing.
		It("is not ready while a dependency is missing", func() {
			deployable.missing = "FakeOperand is missing the prometheus operator"

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.ConditionTypeAvailable).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.ConditionTypeReady).Status).To(Equal(metav1.ConditionFalse))
			Expect(conditionOf(krmv1alpha1.ConditionTypeReady).Message).To(
				Equal("FakeOperand is missing the prometheus operator"))
		})

		It("is not ready while nothing is deployed", func() {
			deployable.deployed = false

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.ConditionTypeReady).Status).To(Equal(metav1.ConditionFalse))
		})

		It("names what is missing rather than only logging it", func() {
			deployable.missing = "FakeOperand is missing the prometheus operator"

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			fulfilled := conditionOf(krmv1alpha1.ConditionTypeDependenciesFulfilled)
			Expect(fulfilled.Status).To(Equal(metav1.ConditionFalse))
			Expect(fulfilled.Message).To(Equal("FakeOperand is missing the prometheus operator"))
		})

		// An unchanged condition must not be patched, or the write wakes the
		// reconcile that writes it again.
		It("does not rewrite an unchanged condition", func() {
			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			stored := &krmv1alpha1.KRMConfig{}
			Expect(runtimeClient.Get(ctx,
				types.NamespacedName{Name: krmv1alpha1.KRMConfigSingletonName}, stored)).To(Succeed())
			afterFirst := stored.ResourceVersion

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(runtimeClient.Get(ctx,
				types.NamespacedName{Name: krmv1alpha1.KRMConfigSingletonName}, stored)).To(Succeed())
			Expect(stored.ResourceVersion).To(Equal(afterFirst))
		})
	})
})
