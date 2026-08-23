// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package managed_nodes_config

import (
	"reflect"

	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
)

type MNCPredicate struct {
}

func (m MNCPredicate) Create(e event.TypedCreateEvent[*v1alpha1.ManagedNodesConfig]) bool {
	return true
}

func (m MNCPredicate) Delete(e event.TypedDeleteEvent[*v1alpha1.ManagedNodesConfig]) bool {
	return true
}

func (m MNCPredicate) Update(e event.TypedUpdateEvent[*v1alpha1.ManagedNodesConfig]) bool {
	return filterMNCUpdates(e.ObjectOld, e.ObjectNew)
}

func (m MNCPredicate) Generic(e event.TypedGenericEvent[*v1alpha1.ManagedNodesConfig]) bool {
	return true
}

type NodePredicate struct {
}

func (m NodePredicate) Create(e event.TypedCreateEvent[*corev1.Node]) bool {
	return true
}

func (m NodePredicate) Delete(e event.TypedDeleteEvent[*corev1.Node]) bool {
	return true
}

func (m NodePredicate) Update(e event.TypedUpdateEvent[*corev1.Node]) bool {
	return filterNodeUpdatesForMNCController(e.ObjectOld, e.ObjectNew)
}

func (m NodePredicate) Generic(e event.TypedGenericEvent[*corev1.Node]) bool {
	return true
}

func filterNodeUpdatesForMNCController(nodeOld, nodeNew *corev1.Node) bool {
	return !reflect.DeepEqual(nodeOld.Labels, nodeNew.Labels) || nodeOld.Generation != nodeNew.Generation
}

func filterMNCUpdates(mncOld, mncNew *v1alpha1.ManagedNodesConfig) bool {
	return mncNew.GetName() == config.Get().ManagedNodesConfigName && mncOld.Generation != mncNew.Generation
}
