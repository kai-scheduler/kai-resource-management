// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package crds carries the chart's CRD manifests into the helm-hooks binary.
// The manifests are generated output, synced from the pinned API module by
// `make sync-crds`; this file is the only hand-written one in the directory.
package crds

import (
	"embed"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// Embedded rather than copied into the image: the shared Dockerfile carries the
// service binary and nothing else.
//
//go:embed *.yaml
var embeddedCRDs embed.FS

// LoadEmbeddedCRDs returns the manifests as written, without typed round-tripping,
// so they can be server-side applied verbatim.
func LoadEmbeddedCRDs() ([]*unstructured.Unstructured, error) {
	entries, err := embeddedCRDs.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read the embedded crds directory: %w", err)
	}

	var objects []*unstructured.Unstructured
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		content, err := embeddedCRDs.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded CRD %s: %w", entry.Name(), err)
		}
		jsonContent, err := yaml.ToJSON(content)
		if err != nil {
			return nil, fmt.Errorf("failed to convert CRD %s to JSON: %w", entry.Name(), err)
		}
		object := &unstructured.Unstructured{}
		if err := object.UnmarshalJSON(jsonContent); err != nil {
			return nil, fmt.Errorf("failed to unmarshal CRD %s: %w", entry.Name(), err)
		}
		objects = append(objects, object)
	}

	if len(objects) == 0 {
		return nil, fmt.Errorf("no CRD manifests are embedded")
	}
	return objects, nil
}
