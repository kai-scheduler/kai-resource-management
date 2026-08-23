// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package wait polls the cluster until a controller has converged.
//
// Every helper polls an observable condition and never sleeps for a fixed
// period, so a spec is neither flaky on a slow runner nor slower than the
// controller it is waiting for.
package wait

import (
	goctx "context"

	kaischedulerv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
)

// ForProjectReady waits until project-controller has finished with the project.
// Its Ready phase is what tells a spec the namespace and queues now exist;
// asserting on them earlier races the controller.
func ForProjectReady(ctx goctx.Context, k8sClient client.Client, name string) *kaires.Project {
	project := &kaires.Project{}

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, project)).To(gomega.Succeed())
		g.Expect(project.Status.Phase).To(gomega.Equal(kaires.Ready),
			"project %q: %s", name, project.Status.Message)
		g.Expect(project.Status.Namespace).ToNot(gomega.BeEmpty(), "project %q has no namespace", name)
	}).WithContext(ctx).WithTimeout(constant.Timeout).WithPolling(constant.Interval).Should(gomega.Succeed())

	return project
}

// ForNodePoolPhase waits for a pool to settle on the expected phase. Ready and
// Empty are both healthy - Empty simply means no node matches the pool's labels -
// so the caller says which one it expects rather than the helper guessing.
func ForNodePoolPhase(
	ctx goctx.Context, k8sClient client.Client, name string, phase kaires.NodePoolPhase,
) *kaires.NodePool {
	pool := &kaires.NodePool{}

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, pool)).To(gomega.Succeed())
		g.Expect(pool.Status.Phase).To(gomega.Equal(phase),
			"nodepool %q: %s", name, pool.Status.Message)
	}).WithContext(ctx).WithTimeout(constant.Timeout).WithPolling(constant.Interval).Should(gomega.Succeed())

	return pool
}

// ForSchedulingShardReady waits for the shard nodepool-controller derives from a
// pool to be reconciled by the KAI operator, not merely to exist. Ready is what
// says the scheduler deployment behind it actually came up.
func ForSchedulingShardReady(
	ctx goctx.Context, k8sClient client.Client, name string,
) *kaischedulerv1.SchedulingShard {
	shard := &kaischedulerv1.SchedulingShard{}

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, shard)).To(gomega.Succeed())
		g.Expect(meta.IsStatusConditionTrue(shard.Status.Conditions, string(kaischedulerv1.ConditionTypeReady))).
			To(gomega.BeTrue(), "scheduling shard %q is not Ready: %v", name, conditionSummary(shard.Status.Conditions))
	}).WithContext(ctx).WithTimeout(constant.Timeout).WithPolling(constant.Interval).Should(gomega.Succeed())

	return shard
}

// conditionSummary renders conditions compactly, so a timeout says which one
// was unmet rather than dumping the whole object.
func conditionSummary(conditions []metav1.Condition) map[string]string {
	summary := map[string]string{}
	for _, condition := range conditions {
		summary[condition.Type] = string(condition.Status)
	}

	return summary
}

// ForQueue waits for a queue the controller derives from a project or department.
func ForQueue(ctx goctx.Context, k8sClient client.Client, name string) *kaiv2.Queue {
	queue := &kaiv2.Queue{}
	getObject(ctx, k8sClient, types.NamespacedName{Name: name}, queue, "queue "+name)

	return queue
}

// ForNamespace waits for the namespace project-controller creates for a project.
func ForNamespace(ctx goctx.Context, k8sClient client.Client, name string) *corev1.Namespace {
	namespace := &corev1.Namespace{}
	getObject(ctx, k8sClient, types.NamespacedName{Name: name}, namespace, "namespace "+name)

	return namespace
}

// ForPodRunning waits for a pod to be scheduled and running, which is the proof
// that the mutations applied to it did not make it unschedulable.
func ForPodRunning(ctx goctx.Context, k8sClient client.Client, namespace, name string) *corev1.Pod {
	pod := &corev1.Pod{}

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pod)).To(gomega.Succeed())
		g.Expect(pod.Status.Phase).To(gomega.Equal(corev1.PodRunning),
			"pod %s/%s is %s", namespace, name, pod.Status.Phase)
	}).WithContext(ctx).WithTimeout(constant.PodTimeout).WithPolling(constant.Interval).Should(gomega.Succeed())

	return pod
}

// getObject polls until the object exists, for kinds whose creation is the whole
// signal and which publish no status of their own.
func getObject(
	ctx goctx.Context, k8sClient client.Client, key types.NamespacedName,
	obj client.Object, description string,
) {
	gomega.EventuallyWithOffset(2, func() error {
		return k8sClient.Get(ctx, key, obj)
	}).WithContext(ctx).WithTimeout(constant.Timeout).WithPolling(constant.Interval).
		Should(gomega.Succeed(), "waiting for %s", description)
}
