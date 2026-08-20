// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deletion

import (
	"context"
	"time"

	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/handlers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// blockerListTimeout bounds each blocker GVK List. The cache lists through a lazily-started
// informer; if the controller can't watch the configured GVK (e.g. missing cluster-scoped
// RBAC), that informer never syncs and a List on context.Background() would block forever,
// hanging finalization before any condition is written. With a deadline the List instead
// returns an error, which OnDelete surfaces as the group's (False) project condition.
const blockerListTimeout = 15 * time.Second

// ConfigurableBlocker is a generic, config-driven deletion blocker. It lists the
// configured GVK(s) (optionally filtered by a label selector) in the project namespace
// and, if any matching resource remains, blocks deletion by reporting its configured
// project condition as False with a ProjectIsNotEmptyError. It replaces the historical
// per-resource blockers (Workload/DataVolume/Secret/PVC).
type ConfigurableBlocker struct {
	CommonResourceDeletionHandler
	group BlockerGroup
}

func NewConfigurableBlocker(client client.Client, group BlockerGroup) ConfigurableBlocker {
	return ConfigurableBlocker{
		CommonResourceDeletionHandler: CommonResourceDeletionHandler{
			WithLoggerAndCli: common.WithLoggerAndCli{
				Client: client,
				Log:    ctrl.Log.WithName("deletion_handlers").WithName(group.DisplayName),
			},
		},
		group: group,
	}
}

func (handler ConfigurableBlocker) OnDelete(project *kaiv1alpha1.Project) ([]kaiv1alpha1.ProjectCondition, error) {
	err := handler.onDeleteInner(project)

	reason := ""
	if err != nil {
		reason = handler.group.Reason()
	}
	return []kaiv1alpha1.ProjectCondition{{
		Type:    kaiv1alpha1.ProjectConditionType(handler.group.ConditionType()),
		Status:  handlers.GetStatusFromError(err),
		Reason:  reason,
		Message: handlers.GetMessageFromError(err),
	}}, err
}

func (handler ConfigurableBlocker) onDeleteInner(project *kaiv1alpha1.Project) error {
	namespaceName, err := handler.KaiProjectToNamespace(project)
	if err != nil {
		return err
	}

	remainingObjects := []client.Object{}
	for _, blocker := range handler.group.Blockers {
		items, listErr := handler.listBlockers(namespaceName, blocker)
		if listErr != nil {
			// Match the historical blockers: a list failure aborts and requeues.
			return listErr
		}
		remainingObjects = append(remainingObjects, items...)
	}

	if len(remainingObjects) == 0 {
		return nil
	}

	return &ProjectIsNotEmptyError{RemainingItems: remainingObjects}
}

func (handler ConfigurableBlocker) listBlockers(namespace string, blocker Blocker) ([]client.Object, error) {
	objectList := &unstructured.UnstructuredList{}
	objectList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   blocker.Group,
		Version: blocker.Version,
		Kind:    blocker.Kind + "List",
	})

	listOptions := []client.ListOption{client.InNamespace(namespace)}
	if blocker.LabelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(blocker.LabelSelector)
		if err != nil {
			handler.Log.Error(err, "Failed to build label selector for blocker",
				common.LogGvkTag, blocker.Kind, common.LogNamespaceTag, namespace)
			return nil, err
		}
		listOptions = append(listOptions, client.MatchingLabelsSelector{Selector: selector})
	}

	ctx, cancel := context.WithTimeout(context.Background(), blockerListTimeout)
	defer cancel()
	if err := handler.Client.List(ctx, objectList, listOptions...); err != nil {
		handler.Log.Error(err, "Failed to list resources for deletion blocker",
			common.LogGvkTag, blocker.Kind, common.LogNamespaceTag, namespace)
		return nil, err
	}

	objects := make([]client.Object, 0, len(objectList.Items))
	for i := range objectList.Items {
		item := objectList.Items[i]
		objects = append(objects, &item)
	}
	return objects, nil
}
