// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package podgroupassigner hosts the pod-group-assigner implementation.
//
// TEMPORARY: this is a placeholder that exists only so the build, image and
// release infrastructure has Go code to compile and publish. Replace it with
// the real service.
package podgroupassigner

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

// ServiceName identifies this service in logs and metrics.
const ServiceName = "pod-group-assigner"

// Assigner runs the pod-group-assigner service.
type Assigner struct {
	name string
}

// New returns an Assigner for the pod-group-assigner service.
func New() *Assigner {
	return &Assigner{name: ServiceName}
}

// Name returns the service name.
func (a *Assigner) Name() string {
	return a.name
}

// Run starts the service and blocks until ctx is canceled.
func (a *Assigner) Run(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx).WithName(a.name)

	logger.Info("Starting service")
	<-ctx.Done()
	logger.Info("Stopping service")

	return nil
}
