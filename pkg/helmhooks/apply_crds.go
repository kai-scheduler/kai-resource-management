// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-resource-management/deployments/kai-resource-management-chart/crds"
)

const crdFieldManager = "krm-crd-upgrader"

// ApplyCRDs server-side applies the CRDs embedded in the binary, taking ownership of
// fields another manager holds.
func ApplyCRDs(ctx context.Context, k8sClient client.Client) error {
	logger := logf.FromContext(ctx)

	objects, err := crds.LoadEmbeddedCRDs()
	if err != nil {
		return err
	}

	for _, object := range objects {
		if err := k8sClient.Apply(ctx, client.ApplyConfigurationFromUnstructured(object),
			client.FieldOwner(crdFieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("failed to apply CRD %s: %w", object.GetName(), err)
		}
		logger.Info("Applied CRD", "name", object.GetName())
	}
	return nil
}
