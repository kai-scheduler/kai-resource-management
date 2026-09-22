// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	krmConfigKind      = "KRMConfig"
	configFieldManager = "krm-config-deployer"
)

// ApplyConfig server-side applies the KRMConfig manifest at manifestPath. Applying
// rather than creating keeps the CR out of the Helm release, so its UID survives every
// upgrade: the operator hangs ownerReferences off it, and a recreated CR would
// cascade-delete everything it owns.
func ApplyConfig(ctx context.Context, c client.Client, manifestPath string) error {
	logger := logf.FromContext(ctx)

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest %s: %w", manifestPath, err)
	}
	object, err := decodeManifest(content)
	if err != nil {
		return fmt.Errorf("failed to decode manifest %s: %w", manifestPath, err)
	}
	if object.GetKind() != krmConfigKind {
		return fmt.Errorf("manifest %s has kind %q, expected %q", manifestPath, object.GetKind(), krmConfigKind)
	}
	if object.GetName() == "" {
		return fmt.Errorf("manifest %s has no metadata.name", manifestPath)
	}

	// One apply patch and nothing else: the deployer's ClusterRole grants create and
	// patch deliberately, so a read-modify-write would be forbidden.
	if err := c.Apply(ctx, client.ApplyConfigurationFromUnstructured(object),
		client.FieldOwner(configFieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply %s %s: %w", object.GetKind(), object.GetName(), err)
	}
	logger.Info("Applied KRMConfig", "name", object.GetName())
	return nil
}

func decodeManifest(content []byte) (*unstructured.Unstructured, error) {
	jsonContent, err := yaml.ToJSON(content)
	if err != nil {
		return nil, err
	}
	object := &unstructured.Unstructured{}
	if err := object.UnmarshalJSON(jsonContent); err != nil {
		return nil, err
	}
	return object, nil
}
