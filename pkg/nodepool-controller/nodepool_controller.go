// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package nodepoolcontroller hosts the nodepool-controller implementation.
//
// TEMPORARY: this is a placeholder that exists only so the build, image and
// release infrastructure has Go code to compile and publish. Replace it with
// the real controller.
package nodepoolcontroller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ControllerName identifies this controller in logs and metrics.
const ControllerName = "nodepool-controller"

// Controller runs the nodepool-controller service.
type Controller struct {
	name   string
	scheme *runtime.Scheme
}

func New(scheme *runtime.Scheme) *Controller {
	return &Controller{name: ControllerName, scheme: scheme}
}

// Name returns the controller name.
func (c *Controller) Name() string {
	return c.name
}

func (c *Controller) Scheme() *runtime.Scheme {
	return c.scheme
}

// Run starts the controller and blocks until ctx is canceled.
func (c *Controller) Run(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx).WithName(c.name)

	logger.Info("Starting controller")
	<-ctx.Done()
	logger.Info("Stopping controller")

	return nil
}
