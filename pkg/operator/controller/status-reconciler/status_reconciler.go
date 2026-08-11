// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package statusreconciler

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/deployable"
)

type StatusReconciler struct {
	client.Client
	deployable deployable.Deployable
}

func New(runtimeClient client.Client, deployableOperands deployable.Deployable) *StatusReconciler {
	return &StatusReconciler{
		Client:     runtimeClient,
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
	if err := r.reconcileCondition(
		ctx, krmConfig, r.getDeployedCondition(ctx, krmConfig.GetGeneration())); err != nil {
		return err
	}
	if err := r.reconcileCondition(
		ctx, krmConfig, r.getAvailableCondition(ctx, krmConfig.GetGeneration())); err != nil {
		return err
	}
	if err := r.reconcileCondition(
		ctx, krmConfig, r.getDependenciesFulfilledCondition(ctx, krmConfig)); err != nil {
		return err
	}
	if err := r.reconcileCondition(
		ctx, krmConfig, r.getReadyCondition(ctx, krmConfig.GetGeneration())); err != nil {
		return err
	}
	return r.reconcileCondition(
		ctx, krmConfig, r.getReconcilingCondition(krmConfig.GetGeneration(), false))
}

func (r *StatusReconciler) hasReconcilingConditionForGeneration(krmConfig *krmv1alpha1.KRMConfig) bool {
	for _, existingCondition := range krmConfig.Status.Conditions {
		if existingCondition.Type == string(krmv1alpha1.ConditionTypeReconciling) {
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

	krmConfig.Status.Conditions = updatedConditions
	return r.Status().Patch(ctx, krmConfig, patch)
}

func (r *StatusReconciler) getDeployedCondition(ctx context.Context, generation int64) metav1.Condition {
	deployed, err := r.deployable.IsDeployed(ctx, r.Client)
	if err != nil {
		return newCondition(krmv1alpha1.ConditionTypeDeployed, false,
			krmv1alpha1.ReasonNotDeployed, err.Error(), generation)
	}
	if deployed {
		return newCondition(krmv1alpha1.ConditionTypeDeployed, true,
			krmv1alpha1.ReasonDeployed, "Resources deployed", generation)
	}
	return newCondition(krmv1alpha1.ConditionTypeDeployed, false,
		krmv1alpha1.ReasonNotDeployed, "Resources not deployed yet", generation)
}

func (r *StatusReconciler) getReconcilingCondition(generation int64, reconciling bool) metav1.Condition {
	if reconciling {
		return newCondition(krmv1alpha1.ConditionTypeReconciling, true,
			krmv1alpha1.ReasonReconciling, "Reconciliation in progress", generation)
	}
	return newCondition(krmv1alpha1.ConditionTypeReconciling, false,
		krmv1alpha1.ReasonReconciled, "Reconciliation completed successfully", generation)
}

func (r *StatusReconciler) getAvailableCondition(ctx context.Context, generation int64) metav1.Condition {
	available, err := r.deployable.IsAvailable(ctx, r.Client)
	if err != nil {
		return newCondition(krmv1alpha1.ConditionTypeAvailable, false,
			krmv1alpha1.ReasonNotAvailable, err.Error(), generation)
	}
	if available {
		return newCondition(krmv1alpha1.ConditionTypeAvailable, true,
			krmv1alpha1.ReasonAvailable, "System available", generation)
	}
	return newCondition(krmv1alpha1.ConditionTypeAvailable, false,
		krmv1alpha1.ReasonNotAvailable, "System not available", generation)
}

func (r *StatusReconciler) getDependenciesFulfilledCondition(
	ctx context.Context, krmConfig *krmv1alpha1.KRMConfig,
) metav1.Condition {
	generation := krmConfig.GetGeneration()

	missingDependencies, err := r.deployable.HasMissingDependencies(ctx, r.Client, krmConfig)
	if err != nil {
		return newCondition(krmv1alpha1.ConditionTypeDependenciesFulfilled, false,
			krmv1alpha1.ReasonDependenciesMissing, err.Error(), generation)
	}
	if len(missingDependencies) > 0 {
		return newCondition(krmv1alpha1.ConditionTypeDependenciesFulfilled, false,
			krmv1alpha1.ReasonDependenciesMissing, missingDependencies, generation)
	}
	return newCondition(krmv1alpha1.ConditionTypeDependenciesFulfilled, true,
		krmv1alpha1.ReasonDependenciesFulfilled, "Dependencies are fulfilled", generation)
}

func (r *StatusReconciler) getReadyCondition(ctx context.Context, generation int64) metav1.Condition {
	condition := r.getAvailableCondition(ctx, generation)
	condition.Type = string(krmv1alpha1.ConditionTypeReady)
	if condition.Status == metav1.ConditionTrue {
		condition.Reason = string(krmv1alpha1.ReasonReady)
		condition.Message = "System is ready"
	} else {
		condition.Reason = string(krmv1alpha1.ReasonNotReady)
		condition.Message = "System not ready"
	}
	return condition
}

func newCondition(
	conditionType krmv1alpha1.ConditionType,
	met bool,
	reason krmv1alpha1.ConditionReason,
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
