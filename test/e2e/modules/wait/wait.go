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
	"fmt"

	kaischedulerv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	kaiconstants "github.com/kai-scheduler/api/constants"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// ForNodePoolPhase waits for a nodePool to settle on the expected phase. Ready and
// Empty are both healthy - Empty simply means no node matches the node pool's labels -
// so the caller says which one it expects rather than the helper guessing.
func ForNodePoolPhase(
	ctx goctx.Context, k8sClient client.Client, name string, phase kaires.NodePoolPhase,
) *kaires.NodePool {
	nodePool := &kaires.NodePool{}

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, nodePool)).To(gomega.Succeed())
		g.Expect(nodePool.Status.Phase).To(gomega.Equal(phase),
			"nodepool %q: %s", name, nodePool.Status.Message)
	}).WithContext(ctx).WithTimeout(constant.Timeout).WithPolling(constant.Interval).Should(gomega.Succeed())

	return nodePool
}

// ForSchedulingShardReady waits for the shard nodepool-controller derives from a
// node pool to be reconciled by the KAI operator, not merely to exist. Ready is what
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

// ForServiceMonitor waits for a service monitor in the release namespace.
func ForServiceMonitor(
	ctx goctx.Context, k8sClient client.Client, name string,
) *monitoringv1.ServiceMonitor {
	monitor := &monitoringv1.ServiceMonitor{}
	key := types.NamespacedName{Namespace: constant.ReleaseNamespace, Name: name}
	getObject(ctx, k8sClient, key, monitor, "service monitor "+name)

	return monitor
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

// ForPodGroup returns the pod group the pod-grouper made for a pod.
//
// Resolved through the annotation the pod-grouper stamps on the pod rather than by listing
// the namespace: listing would tie the caller to there being exactly one pod group, which
// stops being true the moment a spec submits a second workload.
func ForPodGroup(
	ctx goctx.Context, k8sClient client.Client, namespace, podName string,
) *kaiv2alpha2.PodGroup {
	var podGroupName string

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		pod := &corev1.Pod{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, pod)).
			To(gomega.Succeed())

		podGroupName = pod.Annotations[kaiconstants.PodGroupAnnotationForPod]
		g.Expect(podGroupName).ToNot(gomega.BeEmpty(),
			"pod %s/%s is not grouped into a pod group yet", namespace, podName)
	}).WithContext(ctx).WithTimeout(constant.Timeout).WithPolling(constant.Interval).Should(gomega.Succeed())

	podGroup := &kaiv2alpha2.PodGroup{}
	key := types.NamespacedName{Namespace: namespace, Name: podGroupName}
	getObject(ctx, k8sClient, key, podGroup, "pod group "+podGroupName)

	return podGroup
}

// ForDeleted waits until the object is gone from the API server.
//
// Deletion is not immediate here: nodepool-controller and project-controller both hold
// finalizers, so a spec that returns as soon as Delete succeeds leaves the object
// Terminating for the next spec, or the next run's preflight, to trip over.
func ForDeleted(ctx goctx.Context, k8sClient client.Client, obj client.Object) {
	key := client.ObjectKeyFromObject(obj)
	var lastErr error

	gomega.EventuallyWithOffset(1, func() bool {
		lastErr = k8sClient.Get(ctx, key, obj)
		return apierrors.IsNotFound(lastErr)
	}).WithContext(ctx).WithTimeout(constant.Timeout).WithPolling(constant.Interval).
		Should(gomega.BeTrue(), func() string {
			return fmt.Sprintf("waiting for %T %s to be deleted (last error: %v)", obj, key.String(), lastErr)
		})
}

// ForNodePoolCondition waits for a nodePool to report conditionType as True and returns
// it, so the caller can assert on the reason and message it carries.
func ForNodePoolCondition(
	ctx goctx.Context, k8sClient client.Client, name string, conditionType kaires.NodePoolConditionType,
) kaires.NodePoolCondition {
	var reported kaires.NodePoolCondition

	gomega.EventuallyWithOffset(1, func(g gomega.Gomega) {
		nodePool := &kaires.NodePool{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, nodePool)).To(gomega.Succeed())

		condition := getNodePoolConditionOfType(nodePool, conditionType)
		g.Expect(condition).ToNot(gomega.BeNil(),
			"nodepool %q has no %q condition, only %v", name, conditionType, getReportedNodePoolConditionTypes(nodePool))
		g.Expect(condition.Status).To(gomega.Equal(corev1.ConditionTrue),
			"nodepool %q condition %q: %s", name, conditionType, condition.Message)

		reported = *condition
	}).WithContext(ctx).WithTimeout(constant.Timeout).WithPolling(constant.Interval).Should(gomega.Succeed())

	return reported
}

// getNodePoolConditionOfType finds a condition by type. NodePool carries its own condition type
// rather than metav1.Condition, so meta.FindStatusCondition does not apply.
func getNodePoolConditionOfType(
	nodePool *kaires.NodePool, conditionType kaires.NodePoolConditionType,
) *kaires.NodePoolCondition {
	for i := range nodePool.Status.Conditions {
		if nodePool.Status.Conditions[i].Type == conditionType {
			return &nodePool.Status.Conditions[i]
		}
	}

	return nil
}

// getReportedNodePoolConditionTypes lists what the nodePool does report, so a timeout on a missing
// condition says what was there instead.
func getReportedNodePoolConditionTypes(nodePool *kaires.NodePool) []kaires.NodePoolConditionType {
	reported := make([]kaires.NodePoolConditionType, 0, len(nodePool.Status.Conditions))
	for _, condition := range nodePool.Status.Conditions {
		reported = append(reported, condition.Type)
	}

	return reported
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
