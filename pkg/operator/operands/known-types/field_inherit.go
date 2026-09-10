// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package knowntypes

import (
	"reflect"

	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FieldInherit copies fields of the live object into the desired one before the
// two are compared. Most kinds need none: builders start from the live object, so
// server-owned fields are already there. It is for the kinds that are built fresh.
type FieldInherit func(current, desired client.Object)

// VPAFieldInherit is needed because the VPA is built from scratch rather than read
// first, so without this the recommender's status reads as drift on every resync.
func VPAFieldInherit(current, desired client.Object) {
	if current == nil {
		return
	}
	desired.SetResourceVersion(current.GetResourceVersion())
	desired.SetUID(current.GetUID())
	desired.SetCreationTimestamp(current.GetCreationTimestamp())
	desired.SetGeneration(current.GetGeneration())
	desired.SetOwnerReferences(current.GetOwnerReferences())
	desired.SetManagedFields(current.GetManagedFields())
	desired.SetAnnotations(mergeAnnotations(desired.GetAnnotations(), current.GetAnnotations()))

	currentVPA, ok := current.(*vpav1.VerticalPodAutoscaler)
	if !ok {
		return
	}
	desiredVPA, ok := desired.(*vpav1.VerticalPodAutoscaler)
	if !ok {
		return
	}
	desiredVPA.Status = currentVPA.Status
}

// mergeAnnotations keeps annotations the operator does not set; an admin's or an
// injector's annotation is not ours to remove.
func mergeAnnotations(desired, current map[string]string) map[string]string {
	if desired == nil {
		desired = map[string]string{}
	}
	for key, value := range current {
		if _, isOverride := desired[key]; !isOverride {
			desired[key] = value
		}
	}
	return desired
}

func DefaultFieldInherits() map[reflect.Type]FieldInherit {
	return map[reflect.Type]FieldInherit{
		reflect.TypeOf(&vpav1.VerticalPodAutoscaler{}): VPAFieldInherit,
	}
}
