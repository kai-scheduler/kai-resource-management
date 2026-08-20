// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package tests

import (
	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	. "github.com/onsi/gomega"

	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/podgroup/assigner"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// updatePodCondition sets the condition of its type on status, appending it when
// absent, and reports whether anything actually changed. It is
// k8s.io/kubernetes' podutil.UpdatePodCondition, reimplemented here because that
// single import would pull the entire Kubernetes tree into this module for one
// test helper.
func updatePodCondition(status *corev1.PodStatus, condition *corev1.PodCondition) bool {
	condition.LastTransitionTime = metav1.Now()

	for i := range status.Conditions {
		existing := &status.Conditions[i]
		if existing.Type != condition.Type {
			continue
		}
		// A condition that has not flipped keeps its original transition time,
		// so the timestamp reports when the state last changed, not when it was
		// last written.
		if condition.Status == existing.Status {
			condition.LastTransitionTime = existing.LastTransitionTime
		}
		unchanged := condition.Status == existing.Status &&
			condition.Reason == existing.Reason &&
			condition.Message == existing.Message &&
			condition.LastProbeTime.Equal(&existing.LastProbeTime) &&
			condition.LastTransitionTime.Equal(&existing.LastTransitionTime)

		status.Conditions[i] = *condition
		return !unchanged
	}

	status.Conditions = append(status.Conditions, *condition)
	return true
}

type TestSchedulerMock struct {
}

var SchedulerMock = &TestSchedulerMock{}

func (sm *TestSchedulerMock) UnschedulableOnNodePool(pg types.NamespacedName, assignmentParams *assigner.FullNodePoolAssignmentParams, k8sClient client.Client) {
	pgFromClient := getPodGroupFromClient(pg, k8sClient)
	pgFromClient.Status.SchedulingConditions = append(pgFromClient.Status.SchedulingConditions,
		getUnschedulableOnNodePoolCondition(assignmentParams.NodePoolName))
	expectUpdateResourceStatus(k8sClient, pgFromClient)

	EventuallyWithOffset(1, func() bool {
		pgFromClient = getPodGroupFromClient(pg, k8sClient)
		for _, condition := range pgFromClient.Status.SchedulingConditions {
			if condition.Type == kaiv2alpha2.UnschedulableOnNodePool &&
				condition.NodePool == assignmentParams.NodePoolName {
				return true
			}
		}

		return false
	}, timeout, interval).Should(BeTrue())

	if assignmentParams.MarkUnschedulable {
		markAsUnschedulable(pg, k8sClient)
	}
}

func (sm *TestSchedulerMock) UnschedulableOnNodePoolNoValidate(pg types.NamespacedName, assignmentParams *assigner.FullNodePoolAssignmentParams, k8sClient client.Client) {
	pgFromClient := getPodGroupFromClient(pg, k8sClient)
	pgFromClient.Status.SchedulingConditions = append(pgFromClient.Status.SchedulingConditions,
		getUnschedulableOnNodePoolCondition(assignmentParams.NodePoolName))
	expectUpdateResourceStatus(k8sClient, pgFromClient)

	if assignmentParams.MarkUnschedulable {
		markAsUnschedulable(pg, k8sClient)
	}
}

func (sm *TestSchedulerMock) Running(pg types.NamespacedName, k8sClient client.Client) {
	pods := getPodGroupPods(pg)
	Expect(pods).ToNot(BeNil())

	pod := pods.Items[len(pods.Items)-1]
	pod.Status.Phase = corev1.PodRunning
	expectUpdateResourceStatus(k8sClient, &pod)

	Eventually(func() bool {
		pods = getPodGroupPods(pg)
		pod = pods.Items[len(pods.Items)-1]

		return pod.Status.Phase == corev1.PodRunning
	}, timeout, interval).Should(BeTrue())
}

func markAsUnschedulable(pg types.NamespacedName, k8sClient client.Client) {
	var pods *corev1.PodList

	Eventually(func() bool {
		pods = getPodGroupPods(pg)
		return pods != nil && len(pods.Items) > 0
	}, timeout, interval).Should(BeTrue())

	condition := &corev1.PodCondition{
		Type:   corev1.PodScheduled,
		Status: corev1.ConditionFalse,
		Reason: corev1.PodReasonUnschedulable,
	}

	for i := range pods.Items {
		pod := pods.Items[i]
		if updatePodCondition(&pod.Status, condition) {
			Eventually(func() bool {
				apiPod := &corev1.Pod{}

				err := k8sClient.Get(apiCtx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, apiPod)
				if err != nil {
					return false
				}

				_ = updatePodCondition(&apiPod.Status, condition)
				err = k8sClient.Status().Update(apiCtx, apiPod)

				return err == nil
			}, timeout, interval).Should(BeTrue())
		}
	}

	Eventually(func() bool {
		pods = getPodGroupPods(pg)
		if pods == nil || len(pods.Items) == 0 {
			return false
		}

		for _, pod := range pods.Items {
			foundConditionForThisPod := false

			for _, pCondition := range pod.Status.Conditions {
				if pCondition.Type == corev1.PodScheduled ||
					pCondition.Status == corev1.ConditionFalse ||
					pCondition.Reason == corev1.PodReasonUnschedulable {
					foundConditionForThisPod = true
					break
				}
			}

			if !foundConditionForThisPod {
				return false
			}
		}

		return true
	}, timeout, interval).Should(BeTrue())
}

func getUnschedulableOnNodePoolCondition(nodePool string) kaiv2alpha2.SchedulingCondition {
	return kaiv2alpha2.SchedulingCondition{
		Type:     kaiv2alpha2.UnschedulableOnNodePool,
		NodePool: nodePool,
	}
}
