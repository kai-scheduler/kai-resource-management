// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package podgroupassigner installs the pod-group-assigner service.
//
// The chart still owns this service's RBAC and its admission webhook
// configurations. The Deployment, its ServiceAccount and Service are built here.
package podgroupassigner

import (
	"context"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/dependencies"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

// PodGroupAssigner is the operand. It is a long-lived singleton, so
// lastDesiredState survives between reconciles and is what the status reconciler
// later asks about.
type PodGroupAssigner struct {
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
func (p *PodGroupAssigner) DesiredState(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) ([]client.Object, error) {
	p.namespace = krmConfig.Spec.Namespace
	if p.BaseResourceName == "" {
		p.BaseResourceName = defaultResourceName
	}

	if !*krmConfig.Spec.PodGroupAssigner.Service.Enabled {
		p.lastDesiredState = []client.Object{}
		return nil, nil
	}

	var objects []client.Object
	for _, resourceFunc := range []operands.ResourceFunc{
		p.serviceAccountForKRMConfig,
		p.deploymentForKRMConfig,
		p.serviceForKRMConfig,
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
		krmConfig.Spec.PodGroupAssigner.VPA, objects, krmConfig.Spec.Namespace); vpa != nil {
		objects = append(objects, vpa)
	}

	p.lastDesiredState = objects
	return objects, nil
}

func (p *PodGroupAssigner) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllObjectsExists(ctx, readerClient, p.lastDesiredState)
}

func (p *PodGroupAssigner) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return common.AllControllersAvailable(ctx, readerClient, p.lastDesiredState)
}

func (p *PodGroupAssigner) Name() string {
	return "PodGroupAssigner"
}

func (p *PodGroupAssigner) Monitor(
	_ context.Context, _ client.Reader, _ *krmv1alpha1.KRMConfig,
) error {
	return nil
}

// HasMissingDependencies reports the KAI Scheduler CRDs pod-group-assigner reads
// and the cluster does not have. Keep the list in step with the kinds
// pkg/pod-group-assigner/scheme registers: a kind whose CRD is absent is exactly
// what the service fails on at runtime.
func (p *PodGroupAssigner) HasMissingDependencies(
	ctx context.Context, reader client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (string, error) {
	// A service the configuration does not install needs nothing.
	if !*krmConfig.Spec.PodGroupAssigner.Service.Enabled {
		return "", nil
	}

	return dependencies.MissingCRDs(ctx, reader,
		dependencies.CRDRequirement{Name: "queues.scheduling.run.ai", Version: "v2"},
		dependencies.CRDRequirement{Name: "podgroups.scheduling.run.ai", Version: "v2alpha2"},
		dependencies.CRDRequirement{Name: "topologies.kai.scheduler", Version: "v1alpha1"},
	)
}
