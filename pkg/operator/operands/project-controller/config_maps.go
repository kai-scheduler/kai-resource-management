// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

const (
	roleBindingsConfigMapName   = "project-controller-rolebindings-plugin"
	deleteBlockersConfigMapName = "project-delete-blockers"

	// blockersConfigMapKey is the file name the controller reads the blocker list
	// from. Like the DeleteBlocker field names, it is a wire contract, not a choice.
	blockersConfigMapKey = "blockers.yaml"
)

// builtinRoleBinding is a binding this operand ships, gated by the feature it
// belongs to. Each grants the controller a permission it can only hold namespace by
// namespace. The ClusterRole comes from the Helm chart under the same feature flag;
// a binding whose ClusterRole does not exist grants nothing.
type builtinRoleBinding struct {
	// key is the ConfigMap data key.
	key string

	// name is both the RoleBinding's name and the ClusterRole it binds.
	name string

	enabledBy func(*krmv1alpha1.ProjectControllerFeatures) *bool
}

var builtinRoleBindings = []builtinRoleBinding{
	{
		key:  "project-secret.yaml",
		name: "kai-project-controller-cluster-secret-per-project",
		enabledBy: func(features *krmv1alpha1.ProjectControllerFeatures) *bool {
			return features.ClusterWideSecret
		},
	},
	{
		key:  "project-configmap.yaml",
		name: "kai-project-controller-cluster-configmap-per-project",
		enabledBy: func(features *krmv1alpha1.ProjectControllerFeatures) *bool {
			return features.ClusterWideConfigMap
		},
	},
	{
		key:  "project-pvc.yaml",
		name: "kai-project-controller-cluster-pvc-per-project",
		enabledBy: func(features *krmv1alpha1.ProjectControllerFeatures) *bool {
			return features.ClusterWidePvc
		},
	},
	{
		key:  "limit-range.yaml",
		name: "kai-project-controller-limit-range-per-project",
		enabledBy: func(features *krmv1alpha1.ProjectControllerFeatures) *bool {
			return features.LimitRange
		},
	},
}

func (p *ProjectController) roleBindingsConfigMapForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	configMap, err := configMapForKRMConfig(ctx, runtimeClient, krmConfig, roleBindingsConfigMapName)
	if err != nil {
		return nil, err
	}

	config := krmConfig.Spec.ProjectController
	namespace := krmConfig.Spec.Namespace
	data := map[string]string{}

	add := func(key string, binding krmv1alpha1.ProjectRoleBinding) error {
		encoded, err := yaml.Marshal(roleBindingFor(binding, namespace))
		if err != nil {
			return fmt.Errorf("marshalling project RoleBinding %s: %w", binding.Name, err)
		}
		data[key] = string(encoded)
		return nil
	}

	for _, builtin := range builtinRoleBindings {
		if !ptr.Deref(builtin.enabledBy(config.Features), false) {
			continue
		}
		if err = add(builtin.key, krmv1alpha1.ProjectRoleBinding{
			Name:               builtin.name,
			ServiceAccountName: p.BaseResourceName,
		}); err != nil {
			return nil, err
		}
	}

	// Last, so an entry shipped above can be deliberately overridden by naming it.
	for _, extra := range config.ExtraProjectRoleBindings {
		if err = add(extra.Name+".yaml", extra); err != nil {
			return nil, err
		}
	}

	configMap.Data = data
	return configMap, nil
}

func roleBindingFor(binding krmv1alpha1.ProjectRoleBinding, namespace string) *rbacv1.RoleBinding {
	clusterRoleName := binding.ClusterRoleName
	if clusterRoleName == "" {
		clusterRoleName = binding.Name
	}

	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		// Deliberately no namespace: the controller sets it per project namespace.
		ObjectMeta: metav1.ObjectMeta{Name: binding.Name},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			{Kind: "ServiceAccount", Name: binding.ServiceAccountName, Namespace: namespace},
		},
	}
}

// The controller reads this at startup to decide what blocks deleting a project.
// An empty list means nothing does, which is the default.
func (p *ProjectController) deleteBlockersConfigMapForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	configMap, err := configMapForKRMConfig(ctx, runtimeClient, krmConfig, deleteBlockersConfigMapName)
	if err != nil {
		return nil, err
	}

	blockers := krmConfig.Spec.ProjectController.DeleteBlockers
	if blockers == nil {
		// A nil slice marshals to "null", which is not a list the controller can range over.
		blockers = []krmv1alpha1.DeleteBlocker{}
	}

	encoded, err := yaml.Marshal(blockers)
	if err != nil {
		return nil, fmt.Errorf("marshalling project delete blockers: %w", err)
	}

	configMap.Data = map[string]string{blockersConfigMapKey: string(encoded)}
	return configMap, nil
}

func configMapForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig, name string,
) (*corev1.ConfigMap, error) {
	object, err := common.ObjectForKRMConfig(
		ctx, runtimeClient, &corev1.ConfigMap{}, name, krmConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}

	configMap := object.(*corev1.ConfigMap)
	configMap.TypeMeta = metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"}
	return configMap, nil
}
