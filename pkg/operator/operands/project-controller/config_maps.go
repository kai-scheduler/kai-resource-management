// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"context"
	"fmt"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

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
	// name is the RoleBinding, the ClusterRole it binds, and its data key.
	name string

	enabledBy func(*krmv1alpha1.ProjectControllerFeatures) *bool
}

var builtinRoleBindings = []builtinRoleBinding{
	{
		name: "kai-project-controller-limit-range-per-project",
		enabledBy: func(features *krmv1alpha1.ProjectControllerFeatures) *bool {
			return features.LimitRange
		},
	},
}

func roleBindingKey(name string) string { return name + ".yaml" }

func roleBindingsConfigMapNameFor(config *krmv1alpha1.ProjectController) string {
	if config.RoleBindingsConfigMapName != nil && *config.RoleBindingsConfigMapName != "" {
		return *config.RoleBindingsConfigMapName
	}
	return roleBindingsConfigMapName
}

func (p *ProjectController) roleBindingsConfigMapForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (client.Object, error) {
	config := krmConfig.Spec.ProjectController

	configMap, err := p.configMapForKRMConfig(
		ctx, runtimeClient, krmConfig, roleBindingsConfigMapNameFor(config))
	if err != nil {
		return nil, err
	}

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

	shipped := map[string]bool{}
	for _, builtin := range builtinRoleBindings {
		if !ptr.Deref(builtin.enabledBy(config.Features), false) {
			continue
		}
		shipped[builtin.name] = true
		if err = add(roleBindingKey(builtin.name), krmv1alpha1.ProjectRoleBinding{
			Name:               builtin.name,
			ServiceAccountName: p.BaseResourceName,
		}); err != nil {
			return nil, err
		}
	}

	for _, extra := range config.ExtraProjectRoleBindings {
		if shipped[extra.Name] {
			log.FromContext(ctx).Info("ignoring an extra project RoleBinding that this operator already ships",
				"name", extra.Name, "configMap", configMap.Name)
			continue
		}
		if err = add(roleBindingKey(extra.Name), extra); err != nil {
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
	configMap, err := p.configMapForKRMConfig(ctx, runtimeClient, krmConfig, deleteBlockersConfigMapName)
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

func (p *ProjectController) configMapForKRMConfig(
	ctx context.Context, runtimeClient client.Reader, krmConfig *krmv1alpha1.KRMConfig, name string,
) (*corev1.ConfigMap, error) {
	object, err := common.ObjectForKRMConfig(
		ctx, runtimeClient, &corev1.ConfigMap{}, name, krmConfig.Spec.Namespace)
	if err != nil {
		return nil, err
	}

	configMap := object.(*corev1.ConfigMap)
	configMap.TypeMeta = metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"}
	// ObjectForKRMConfig labels an object after itself, which is right for the ones
	// named after the service. These are not, and the label groups every object of
	// this controller under one selector.
	configMap.Labels["app"] = p.BaseResourceName
	return configMap, nil
}
