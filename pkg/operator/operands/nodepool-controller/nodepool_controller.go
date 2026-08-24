// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package nodepoolcontroller installs the nodepool-controller service.
//
// The chart still owns this service's RBAC and its admission webhook
// configuration. Everything else — the Deployment, its ServiceAccount and Service,
// and both of its ServiceMonitors — is built here.
package nodepoolcontroller

import (
	"context"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

// NodePoolController is the operand. It is a long-lived singleton, so
// lastDesiredState survives between reconciles and is what the status reconciler
// later asks about.
type NodePoolController struct {
	namespace        string
	lastDesiredState []client.Object

	// BaseResourceName names every object this operand creates. Overridable so a
	// test can build one without reaching for the package const.
	BaseResourceName string
}

// DesiredState requires a KRMConfig that config.SetDefaultsWhereNeeded has already
// filled, which is what the reconciler does before deploying. It dereferences the
// defaulted fields without checking them: a caller that skips defaulting gets a nil
// pointer panic on the first one rather than a half-configured Deployment.
func (n *NodePoolController) DesiredState(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) ([]client.Object, error) {
	n.namespace = krmConfig.Spec.Namespace
	if n.BaseResourceName == "" {
		n.BaseResourceName = defaultResourceName
	}

	if !*krmConfig.Spec.NodePoolController.Service.Enabled {
		n.lastDesiredState = []client.Object{}
		return nil, nil
	}

	var objects []client.Object
	for _, resourceFunc := range []operands.ResourceFunc{
		n.serviceAccountForKRMConfig,
		n.deploymentForKRMConfig,
		n.serviceForKRMConfig,
		n.serviceMonitorForKRMConfig,
		n.accountingServiceMonitorForKRMConfig,
	} {
		object, err := resourceFunc(ctx, runtimeClient, krmConfig)
		if err != nil {
			return nil, err
		}
		if object == nil {
			continue
		}
		objects = append(objects, object)
	}

	if vpa := common.BuildVPAFromObjects(
		krmConfig.Spec.NodePoolController.VPA, objects, krmConfig.Spec.Namespace); vpa != nil {
		objects = append(objects, vpa)
	}

	n.lastDesiredState = objects
	return objects, nil
}

func (n *NodePoolController) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, n.lastDesiredState)
}

func (n *NodePoolController) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, n.lastDesiredState)
}

func (n *NodePoolController) Name() string {
	return "NodePoolController"
}

func (n *NodePoolController) Monitor(
	_ context.Context, _ client.Reader, _ *krmv1alpha1.KRMConfig,
) error {
	return nil
}

func (n *NodePoolController) HasMissingDependencies(
	_ context.Context, _ client.Reader, _ *krmv1alpha1.KRMConfig,
) (string, error) {
	return "", nil
}
