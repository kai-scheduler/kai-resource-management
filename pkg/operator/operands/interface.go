// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package operands

import (
	"context"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourceFunc builds one object of an operand's desired state. Returning a nil
// object means the configuration does not want it, which is how an operand skips
// a resource without special-casing its builder loop.
type ResourceFunc func(
	ctx context.Context, reader client.Reader, config *krmv1alpha1.KRMConfig,
) (client.Object, error)

// Operand is one installable service. Implementations record what DesiredState
// returned; the report methods answer from it rather than recomputing.
type Operand interface {
	DesiredState(ctx context.Context, reader client.Reader, config *krmv1alpha1.KRMConfig) ([]client.Object, error)
	IsDeployed(ctx context.Context, reader client.Reader) (bool, error)
	IsAvailable(ctx context.Context, reader client.Reader) (bool, error)
	Monitor(ctx context.Context, reader client.Reader, config *krmv1alpha1.KRMConfig) error
	HasMissingDependencies(ctx context.Context, reader client.Reader, config *krmv1alpha1.KRMConfig) (string, error)
	Name() string
}
