// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"maps"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// UninstallDetectionRef identifies the CR whose deletion means the installing
// operator is going away. An empty Key.Namespace means a cluster-scoped resource.
type UninstallDetectionRef struct {
	GVK schema.GroupVersionKind
	Key types.NamespacedName
}

type NodePoolControllerParams struct {
	// SchedulingShardArgs are cluster-wide KAI scheduler args merged into every
	// SchedulingShard. Loaded from the --scheduling-shard-args flag; empty means the
	// scheduling shard will use its own KAI-scheduler defaults.
	SchedulingShardArgs map[string]string

	// UninstallDetection is the CR whose deletion force-deletes nodepools.
	// Nil means the check is disabled.
	UninstallDetection *UninstallDetectionRef
}

func (in *NodePoolControllerParams) DeepCopy() *NodePoolControllerParams {
	if in == nil {
		return nil
	}
	out := new(NodePoolControllerParams)
	out.SchedulingShardArgs = maps.Clone(in.SchedulingShardArgs)
	if in.UninstallDetection != nil {
		ref := *in.UninstallDetection
		out.UninstallDetection = &ref
	}
	return out
}
