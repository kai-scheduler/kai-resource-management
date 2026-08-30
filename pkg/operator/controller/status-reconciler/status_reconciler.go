// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package statusreconciler

import (
	"context"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/deployable"
)

type StatusReconciler struct {
	client.Client
	apiReader  client.Reader
	deployable deployable.Deployable
}

func New(
	runtimeClient client.Client, apiReader client.Reader, deployableOperands deployable.Deployable,
) *StatusReconciler {
	return &StatusReconciler{
		Client:     runtimeClient,
		apiReader:  apiReader,
		deployable: deployableOperands,
	}
}

// UpdateStartReconcileStatus sets Reconciling=True to signal to observers (e.g.
// kstatus/Helm --wait) that reconciliation is in progress, and refreshes Deployed
// so a stale True from the previous generation does not read as "already done".
//
// It is a no-op when the Reconciling condition already exists for the current
// generation, which means this reconcile was re-triggered by our own status patch
// rather than by a spec change — skipping the patch breaks the status-update →
// reconcile loop.
func (r *StatusReconciler) UpdateStartReconcileStatus(
	ctx context.Context, krmConfig *krmv1alpha1.KRMConfig,
) error {
	if r.hasReconcilingConditionForGeneration(krmConfig) {
		return nil
	}
	if err := r.reconcileCondition(
		ctx, krmConfig, r.getReconcilingCondition(krmConfig.GetGeneration(), true)); err != nil {
		return err
	}
	return r.reconcileCondition(
		ctx, krmConfig, r.getDeployedCondition(ctx, krmConfig.GetGeneration()))
}

func (r *StatusReconciler) ReconcileStatus(ctx context.Context, krmConfig *krmv1alpha1.KRMConfig) error {
	generation := krmConfig.GetGeneration()

	// Computed once and passed to the Ready condition, which is derived from them
	// rather than asking the operands a second time.
	deployed := r.getDeployedCondition(ctx, generation)
	available := r.getAvailableCondition(ctx, generation)
	dependenciesFulfilled := r.getDependenciesFulfilledCondition(ctx, krmConfig)

	conditions := []metav1.Condition{
		deployed,
		available,
		dependenciesFulfilled,
		readyCondition(generation, deployed, available, dependenciesFulfilled),
		r.getReconcilingCondition(generation, false),
	}

	for _, condition := range conditions {
		if err := r.reconcileCondition(ctx, krmConfig, condition); err != nil {
			return err
		}
	}
	return nil
}

func (r *StatusReconciler) hasReconcilingConditionForGeneration(krmConfig *krmv1alpha1.KRMConfig) bool {
	for _, existingCondition := range krmConfig.Status.Conditions {
		if existingCondition.Type == string(krmv1alpha1.KRMConfigConditionTypeReconciling) {
			return existingCondition.ObservedGeneration == krmConfig.GetGeneration()
		}
	}
	return false
}

func (r *StatusReconciler) reconcileCondition(
	ctx context.Context, krmConfig *krmv1alpha1.KRMConfig, condition metav1.Condition,
) error {
	patch := client.MergeFrom(krmConfig.DeepCopy())
	updatedConditions := krmConfig.DeepCopy().Status.Conditions
	found := false

	for index, existingCondition := range krmConfig.Status.Conditions {
		if existingCondition.Type == condition.Type {
			if existingCondition.ObservedGeneration == condition.ObservedGeneration &&
				existingCondition.Status == condition.Status &&
				existingCondition.Reason == condition.Reason &&
				existingCondition.Message == condition.Message {
				return nil
			}
			found = true
			updatedConditions[index] = condition
			break
		}
	}

	if !found {
		updatedConditions = append(updatedConditions, condition)
	}

	updated := krmConfig.DeepCopy()
	updated.Status.Conditions = updatedConditions
	if err := r.Status().Patch(ctx, updated, patch); err != nil {
		return err
	}

	krmConfig.Status = updated.Status
	krmConfig.ResourceVersion = updated.ResourceVersion
	return nil
}

func (r *StatusReconciler) getDeployedCondition(ctx context.Context, generation int64) metav1.Condition {
	deployed, err := r.deployable.IsDeployed(ctx, r.Client)
	if err != nil {
		return newCondition(krmv1alpha1.KRMConfigConditionTypeDeployed, false,
			krmv1alpha1.KRMConfigReasonNotDeployed, err.Error(), generation)
	}
	if deployed {
		return newCondition(krmv1alpha1.KRMConfigConditionTypeDeployed, true,
			krmv1alpha1.KRMConfigReasonDeployed, "Resources deployed", generation)
	}
	return newCondition(krmv1alpha1.KRMConfigConditionTypeDeployed, false,
		krmv1alpha1.KRMConfigReasonNotDeployed, "Resources not deployed yet", generation)
}

func (r *StatusReconciler) getReconcilingCondition(generation int64, reconciling bool) metav1.Condition {
	if reconciling {
		return newCondition(krmv1alpha1.KRMConfigConditionTypeReconciling, true,
			krmv1alpha1.KRMConfigReasonReconciling, "Reconciliation in progress", generation)
	}
	return newCondition(krmv1alpha1.KRMConfigConditionTypeReconciling, false,
		krmv1alpha1.KRMConfigReasonReconciled, "Reconciliation completed successfully", generation)
}

func (r *StatusReconciler) getAvailableCondition(ctx context.Context, generation int64) metav1.Condition {
	available, err := r.deployable.IsAvailable(ctx, r.Client)
	if err != nil {
		return newCondition(krmv1alpha1.KRMConfigConditionTypeAvailable, false,
			krmv1alpha1.KRMConfigReasonNotAvailable, err.Error(), generation)
	}
	if available {
		return newCondition(krmv1alpha1.KRMConfigConditionTypeAvailable, true,
			krmv1alpha1.KRMConfigReasonAvailable, "System available", generation)
	}
	return newCondition(krmv1alpha1.KRMConfigConditionTypeAvailable, false,
		krmv1alpha1.KRMConfigReasonNotAvailable, "System not available", generation)
}

func (r *StatusReconciler) getDependenciesFulfilledCondition(
	ctx context.Context, krmConfig *krmv1alpha1.KRMConfig,
) metav1.Condition {
	generation := krmConfig.GetGeneration()

	missingDependencies, err := r.deployable.HasMissingDependencies(ctx, r.apiReader, krmConfig)
	if err != nil {
		return newCondition(krmv1alpha1.KRMConfigConditionTypeDependenciesFulfilled, false,
			krmv1alpha1.KRMConfigReasonDependenciesMissing, err.Error(), generation)
	}
	if len(missingDependencies) > 0 {
		return newCondition(krmv1alpha1.KRMConfigConditionTypeDependenciesFulfilled, false,
			krmv1alpha1.KRMConfigReasonDependenciesMissing, missingDependencies, generation)
	}
	return newCondition(krmv1alpha1.KRMConfigConditionTypeDependenciesFulfilled, true,
		krmv1alpha1.KRMConfigReasonDependenciesFulfilled, "Dependencies are fulfilled", generation)
}

// readyCondition summarises the others, so a consumer watching only Ready — Helm
// --wait, kstatus — is not told the installation is ready while a dependency is
// missing. It reports the first unmet condition's message rather than a generic
// one, since that is the part that needs attention.
func readyCondition(generation int64, conditions ...metav1.Condition) metav1.Condition {
	for _, condition := range conditions {
		if condition.Status != metav1.ConditionTrue {
			return newCondition(krmv1alpha1.KRMConfigConditionTypeReady, false,
				krmv1alpha1.KRMConfigReasonNotReady, condition.Message, generation)
		}
	}
	return newCondition(krmv1alpha1.KRMConfigConditionTypeReady, true,
		krmv1alpha1.KRMConfigReasonReady, "System is ready", generation)
}

func newCondition(
	conditionType krmv1alpha1.KRMConfigConditionType,
	met bool,
	reason krmv1alpha1.KRMConfigConditionReason,
	message string,
	generation int64,
) metav1.Condition {
	status := metav1.ConditionFalse
	if met {
		status = metav1.ConditionTrue
	}
	return metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
	}
}
