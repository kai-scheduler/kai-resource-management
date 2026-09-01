// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package statusreconciler

import (
	"context"
	"errors"
	"testing"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

// fakeChecker stands in for an installation-wide dependency.
type fakeChecker struct {
	message string
	err     error
}

func (c *fakeChecker) Check(_ context.Context, _, _ client.Reader) (string, error) {
	return c.message, c.err
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
		// The fakes ignore the reader, so one client stands in for both.
		reconciler = New(runtimeClient, runtimeClient, deployable)
	})

	conditionOf := func(conditionType krmv1alpha1.KRMConfigConditionType) *metav1.Condition {
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

			reconciling := conditionOf(krmv1alpha1.KRMConfigConditionTypeReconciling)
			Expect(reconciling).ToNot(BeNil())
			Expect(reconciling.Status).To(Equal(metav1.ConditionTrue))
			Expect(reconciling.ObservedGeneration).To(Equal(int64(1)))
		})

		// A stale Deployed=True from the previous generation would read as
		// "already done" while a new spec is rolling out.
		It("refreshes Deployed at the same time", func() {
			deployable.deployed = false

			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeDeployed).Status).To(Equal(metav1.ConditionFalse))
		})

		It("leaves the caller's spec alone", func() {
			krmConfig.Spec.Namespace = "defaulted-in-memory"
			krmConfig.Spec.Global = &krmv1alpha1.GlobalConfig{SchedulerName: ptr.To("defaulted")}

			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(krmConfig.Spec.Namespace).To(Equal("defaulted-in-memory"))
			Expect(krmConfig.Spec.Global).ToNot(BeNil())
			Expect(krmConfig.Spec.Global.SchedulerName).To(Equal(ptr.To("defaulted")))
		})

		// The patch response carries the new resourceVersion; without it the next
		// patch in the same reconcile is rejected as a conflict.
		It("keeps the resource version current for the next patch", func() {
			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())

			stored := &krmv1alpha1.KRMConfig{}
			Expect(runtimeClient.Get(ctx, types.NamespacedName{Name: krmConfig.Name}, stored)).To(Succeed())
			Expect(krmConfig.ResourceVersion).To(Equal(stored.ResourceVersion))
		})

		// Otherwise the status patch triggers the reconcile that writes it again.
		It("does nothing when it already ran for this generation", func() {
			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())
			afterFirst := conditionOf(krmv1alpha1.KRMConfigConditionTypeReconciling).LastTransitionTime

			deployable.deployed = false
			Expect(reconciler.UpdateStartReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeReconciling).LastTransitionTime).To(Equal(afterFirst))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeDeployed).Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("finishing a reconcile", func() {
		It("reports ready when everything is deployed and available", func() {
			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeDeployed).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeAvailable).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeDependenciesFulfilled).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeReady).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeReconciling).Status).To(Equal(metav1.ConditionFalse))
		})

		It("is not ready while the services are unavailable", func() {
			deployable.available = false

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeAvailable).Status).To(Equal(metav1.ConditionFalse))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeReady).Status).To(Equal(metav1.ConditionFalse))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeReady).Reason).To(Equal(string(krmv1alpha1.KRMConfigReasonNotReady)))
		})

		// Ready is what Helm --wait and kstatus watch, so it must not claim the
		// installation is ready while a dependency is missing.
		It("is not ready while a dependency is missing", func() {
			deployable.missing = "FakeOperand is missing the prometheus operator"

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeAvailable).Status).To(Equal(metav1.ConditionTrue))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeReady).Status).To(Equal(metav1.ConditionFalse))
			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeReady).Message).To(
				Equal("FakeOperand is missing the prometheus operator"))
		})

		It("is not ready while nothing is deployed", func() {
			deployable.deployed = false

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeReady).Status).To(Equal(metav1.ConditionFalse))
		})

		It("names what is missing rather than only logging it", func() {
			deployable.missing = "FakeOperand is missing the prometheus operator"

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			fulfilled := conditionOf(krmv1alpha1.KRMConfigConditionTypeDependenciesFulfilled)
			Expect(fulfilled.Status).To(Equal(metav1.ConditionFalse))
			Expect(fulfilled.Message).To(Equal("FakeOperand is missing the prometheus operator"))
		})

		// Two separate sources, both belonging on the one condition.
		It("reports an installation-wide checker alongside the operands", func() {
			deployable.missing = "FakeOperand is missing the prometheus operator"
			reconciler = New(runtimeClient, runtimeClient, deployable,
				&fakeChecker{message: "KAI Scheduler is not installed"})

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			fulfilled := conditionOf(krmv1alpha1.KRMConfigConditionTypeDependenciesFulfilled)
			Expect(fulfilled.Status).To(Equal(metav1.ConditionFalse))
			Expect(fulfilled.Message).To(Equal(
				"FakeOperand is missing the prometheus operator; KAI Scheduler is not installed"))
		})

		It("stays fulfilled when a checker reports nothing", func() {
			reconciler = New(runtimeClient, runtimeClient, deployable, &fakeChecker{})

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			Expect(conditionOf(krmv1alpha1.KRMConfigConditionTypeDependenciesFulfilled).Status).
				To(Equal(metav1.ConditionTrue))
		})

		It("reports a failed checker as the condition message", func() {
			reconciler = New(runtimeClient, runtimeClient, deployable,
				&fakeChecker{err: errors.New("apiserver unavailable")})

			Expect(reconciler.ReconcileStatus(ctx, krmConfig)).To(Succeed())

			fulfilled := conditionOf(krmv1alpha1.KRMConfigConditionTypeDependenciesFulfilled)
			Expect(fulfilled.Status).To(Equal(metav1.ConditionFalse))
			Expect(fulfilled.Message).To(ContainSubstring("apiserver unavailable"))
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
