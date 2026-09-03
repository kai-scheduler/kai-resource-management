// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package wait

import (
	goctx "context"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
)

// Ready is derived from these three, so asserting it too would hide which failed.
var krmConfigDeployedConditions = []kaires.KRMConfigConditionType{
	kaires.KRMConfigConditionTypeDeployed,
	kaires.KRMConfigConditionTypeAvailable,
	kaires.KRMConfigConditionTypeDependenciesFulfilled,
}

// ForKRMConfig returns the singleton; its generation is what tells an upgrade
// apart from the install before it.
func ForKRMConfig(ctx goctx.Context, k8sClient client.Client) *kaires.KRMConfig {
	krmConfig := &kaires.KRMConfig{}
	key := types.NamespacedName{Name: kaires.KRMConfigSingletonName}
	getObject(ctx, k8sClient, key, krmConfig, "krm config "+kaires.KRMConfigSingletonName)

	return krmConfig
}

// ForKRMConfigGenerationAfter guards against reading the previous installation's
// conditions. It assumes a differing chart: the same one never bumps the generation.
func ForKRMConfigGenerationAfter(
	ctx goctx.Context, k8sClient client.Client, previous int64,
) *kaires.KRMConfig {
	krmConfig := &kaires.KRMConfig{}
	key := types.NamespacedName{Name: kaires.KRMConfigSingletonName}

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, key, krmConfig)).To(gomega.Succeed())
		g.Expect(krmConfig.Generation).To(gomega.BeNumerically(">", previous),
			"krm config is still at generation %d, so the post-upgrade hook has not applied the new spec",
			krmConfig.Generation)
	}).WithContext(ctx).WithTimeout(constant.UpgradeTimeout).WithPolling(constant.Interval).
		Should(gomega.Succeed())

	return krmConfig
}

// ForKRMConfigDeployed waits for every operand at the currently observed generation.
func ForKRMConfigDeployed(ctx goctx.Context, k8sClient client.Client) *kaires.KRMConfig {
	krmConfig := &kaires.KRMConfig{}
	key := types.NamespacedName{Name: kaires.KRMConfigSingletonName}

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, key, krmConfig)).To(gomega.Succeed())

		for _, conditionType := range krmConfigDeployedConditions {
			g.Expect(isConditionTrueForGeneration(
				krmConfig.Status.Conditions, string(conditionType), krmConfig.Generation,
			)).To(gomega.BeTrue(),
				"krm config is not %s at generation %d: %v",
				conditionType, krmConfig.Generation, conditionSummary(krmConfig.Status.Conditions))
		}
	}).WithContext(ctx).WithTimeout(constant.UpgradeTimeout).WithPolling(constant.Interval).
		Should(gomega.Succeed())

	return krmConfig
}

// isConditionTrueForGeneration is meta.IsStatusConditionTrue plus a generation guard.
func isConditionTrueForGeneration(
	conditions []metav1.Condition, conditionType string, generation int64,
) bool {
	for _, condition := range conditions {
		if condition.Type != conditionType {
			continue
		}

		return condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == generation
	}

	return false
}
