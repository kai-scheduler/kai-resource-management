// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package reconcilers

import (
	"context"
	"reflect"
	"strings"

	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/config"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var log = ctrl.Log.WithName("Event Filter")

var ProjectEventFilter = predicate.NewPredicateFuncs(FilterProjectEvent)

func FilterProjectEvent(object client.Object) (shouldAllowEvent bool) {
	switch object.(type) {
	case *rbacv1.RoleBinding:
		shouldAllowEvent = true
	case *corev1.ConfigMap:
		shouldAllowEvent = config.Get().CreateRoleBindings &&
			object.GetName() == config.Get().RoleBindingsCm &&
			object.GetNamespace() == config.Get().RoleBindingsCmNamespace
	case *corev1.Namespace:
		shouldAllowEvent = isNamespaceObjectOfProjectByLabel(object)
	case *corev1.LimitRange:
		shouldAllowEvent = isProjectPrefixedNamespace(object.GetNamespace())
	case *corev1.Secret:
		shouldAllowEvent = object.GetNamespace() == config.Get().InstallNamespace || isProjectPrefixedNamespace(object.GetNamespace())
	case *kaiv2.Queue, *kaiv1alpha1.Project:
		shouldAllowEvent = true
	}
	return skipIfOverridden(object, shouldAllowEvent)
}

func skipIfOverridden(object client.Object, shouldAllowEvent bool) bool {
	resourceType := reflect.TypeOf(object)
	if common.IsManuallyOverridden(object) {
		log.Info("Encountered a manually overridden resource, skipping it.", "Resource Kind", resourceType.String(), "Resource Namespace", object.GetNamespace(), "Resource Name", object.GetName())
		shouldAllowEvent = false
	}
	return shouldAllowEvent
}

func isProjectPrefixedNamespace(namespace string) bool {
	prefix := config.NamespacePrefix()
	return namespace != "" &&
		namespace != prefix &&
		strings.HasPrefix(namespace, prefix)
}

func isNamespaceObjectOfProjectByLabel(object client.Object) bool {
	namespace, ok := object.(*corev1.Namespace)
	if !ok {
		return false
	}

	projectName := getProjectNameFromNamespace(namespace)
	return projectName != ""
}

func getProjectNameFromNamespaceName(namespaceName string, clientReader client.Reader) (string, error) {
	namespace := &corev1.Namespace{}
	err := clientReader.Get(context.Background(), client.ObjectKey{Name: namespaceName}, namespace)
	if err != nil {
		return "", err
	}

	return getProjectNameFromNamespace(namespace), nil
}

func getProjectNameFromNamespace(namespace *corev1.Namespace) string {
	projectName, found := namespace.Labels[config.Get().NamespaceProjectLabelKey]
	if found {
		return projectName
	}
	return ""
}

func (reconciler *ProjectReconciler) MapNamespaceToProjectEvent(_ context.Context, object client.Object) []reconcile.Request {
	namespace, ok := object.(*corev1.Namespace)
	if !ok {
		reconciler.Log.Info("Warning: Cannot convert object to a Namespace, ignoring the event",
			common.LogNamespaceTag, object)
		return []reconcile.Request{}
	}

	projectName := getProjectNameFromNamespace(namespace)
	if projectName == "" {
		return []reconcile.Request{}
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: projectName}}}
}

func (reconciler *ProjectReconciler) MapRoleBindingToProjectEvent(_ context.Context, object client.Object) []reconcile.Request {
	roleBinding, ok := object.(*rbacv1.RoleBinding)
	if !ok {
		reconciler.Log.Info("Warning: Cannot convert object to a RoleBinding, ignoring the event",
			common.LogRoleBindingTag, object)
		return []reconcile.Request{}
	}

	if len(roleBinding.OwnerReferences) == 1 {
		owner := roleBinding.OwnerReferences[0]
		if owner.Kind == "Project" {
			return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: owner.Name}}}
		}
	}

	projectName, err := getProjectNameFromNamespaceName(roleBinding.Namespace, reconciler.Client)
	if err != nil {
		reconciler.Log.Error(err, "Failed getting the project name of the namespace",
			common.LogNamespaceTag, roleBinding.Namespace)
		return []reconcile.Request{}
	}

	if projectName == "" {
		return []reconcile.Request{}
	}

	return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: projectName}}}
}

// MapRoleBindingsConfigMapToProjectEvents fans an edit of the role bindings
// configmap out to every Project, since the configmap describes the desired
// RoleBindings for all of them.
func (reconciler *ProjectReconciler) MapRoleBindingsConfigMapToProjectEvents(
	ctx context.Context, object client.Object,
) []reconcile.Request {
	configMap, ok := object.(*corev1.ConfigMap)
	if !ok {
		reconciler.Log.Info("Warning: Cannot convert object to a ConfigMap, ignoring the event", "object", object)
		return nil
	}

	if !reconciler.config.CreateRoleBindings ||
		configMap.Name != reconciler.config.RoleBindingsCm ||
		configMap.Namespace != reconciler.config.RoleBindingsCmNamespace {
		return nil
	}

	projects := &kaiv1alpha1.ProjectList{}
	if err := reconciler.List(ctx, projects); err != nil {
		reconciler.Log.Error(err, "Failed listing Projects after role-bindings ConfigMap update")
		return nil
	}

	requests := make([]reconcile.Request, 0, len(projects.Items))
	for i := range projects.Items {
		project := &projects.Items[i]
		if common.IsManuallyOverridden(project) {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKey{Name: project.Name},
		})
	}
	return requests
}
