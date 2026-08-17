// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"slices"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/config"
	statusreconciler "github.com/kai-scheduler/kai-resource-management/pkg/operator/controller/status-reconciler"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/deployable"
	knowntypes "github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/known-types"
	projectcontroller "github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/project-controller"
)

// KRMConfigReconcilerOperands is the ordered set of services this operator
// installs. nodepool-controller and pod-group-assigner are added by their own
// tickets; until then the chart still deploys them directly.
var KRMConfigReconcilerOperands = []operands.Operand{
	&projectcontroller.ProjectController{},
}

type KRMConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	deployable *deployable.DeployableOperands
	*statusreconciler.StatusReconciler
}

func NewKRMConfigReconciler(runtimeClient client.Client, scheme *runtime.Scheme) *KRMConfigReconciler {
	return &KRMConfigReconciler{Client: runtimeClient, Scheme: scheme}
}

func (r *KRMConfigReconciler) SetOperands(operandsToDeploy []operands.Operand) {
	r.deployable = deployable.New(operandsToDeploy, knowntypes.KRMConfigOwned)
}

func (r *KRMConfigReconciler) Reconcile(
	ctx context.Context, request ctrl.Request,
) (result ctrl.Result, err error) {
	logger := log.FromContext(ctx)

	// A singleton: a second KRMConfig is ignored rather than merged, so two of
	// them can never fight over the same objects.
	if request.Name != krmv1alpha1.KRMConfigSingletonName {
		logger.Info("KRMConfig is not in the singleton name, ignoring it.", "Name", request.Name)
		return ctrl.Result{}, nil
	}

	krmConfig := &krmv1alpha1.KRMConfig{}
	if err = r.Get(ctx, request.NamespacedName, krmConfig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Get strips TypeMeta, and both the owner reference stamped on every object
	// and the index key they are looked up by are built from the Kind.
	krmConfig.SetGroupVersionKind(krmv1alpha1.GroupVersion.WithKind(krmv1alpha1.KRMConfigKind))

	// Deferred so a failure below still reports as a non-ready condition rather
	// than only as a log line.
	defer func() {
		err = errors.Join(err, r.ReconcileStatus(ctx, krmConfig))
	}()

	config.SetDefaultsWhereNeeded(&krmConfig.Spec)

	if err = r.UpdateStartReconcileStatus(ctx, krmConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err = r.deployable.Deploy(ctx, r.Client, krmConfig, krmConfig); err != nil {
		return ctrl.Result{}, err
	}

	if err = r.deployable.Monitor(ctx, r.Client, krmConfig); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KRMConfigReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if r.deployable == nil {
		r.SetOperands(KRMConfigReconcilerOperands)
	}
	r.StatusReconciler = statusreconciler.New(r.Client, r.deployable)

	for _, collectable := range knowntypes.KRMConfigOwned {
		if slices.Contains(knowntypes.Initiated, collectable) {
			continue
		}
		if err := collectable.InitWithManager(ctx, mgr); err != nil {
			return err
		}
		knowntypes.Initiated = append(knowntypes.Initiated, collectable)
	}

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).For(&krmv1alpha1.KRMConfig{})
	for _, collectable := range knowntypes.KRMConfigOwned {
		controllerBuilder = collectable.InitWithBuilder(controllerBuilder)
	}

	return controllerBuilder.Complete(r)
}
