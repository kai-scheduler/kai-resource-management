// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package operands

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

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
