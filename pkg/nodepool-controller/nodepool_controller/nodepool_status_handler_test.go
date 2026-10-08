// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	"context"
	"fmt"
	"time"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
)

func nodeForStatusTest(name string, ready bool, nrtConditionStatus ...corev1.ConditionStatus) *corev1.Node {
	readyStatus := corev1.ConditionFalse
	if ready {
		readyStatus = corev1.ConditionTrue
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
			Type:   corev1.NodeReady,
			Status: readyStatus,
		}}},
	}
	if len(nrtConditionStatus) > 0 {
		node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
			Type:   NrtHealthyConditionType,
			Status: nrtConditionStatus[0],
			Reason: NrtHealthReasonMissing,
		})
	}
	return node
}

var _ = Describe("NodePool NRT health status", func() {
	Describe("getNodePoolStatusByNodes", func() {
		It("publishes the degraded phase, appended warning, node status, and condition together", func() {
			restoreConfig := config.SetForTest(&config.NodePoolControllerConfig{
				NodePoolNameLabel:     "kai.scheduler/node-pool",
				DefaultNodepoolName:   "default",
				UnschedulableLabelKey: "kai.scheduler/unschedulable",
			})
			DeferCleanup(restoreConfig)

			scheme := runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			npc := &NodePoolController{
				Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			}
			nodePool := &v1alpha1.NodePool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-a"},
			}
			nodes := []*corev1.Node{
				nodeForStatusTest("node-a", true, corev1.ConditionTrue),
				nodeForStatusTest("node-b", false),
			}

			Expect(npc.getNodePoolStatusByNodes(context.Background(), nodePool, nodes)).To(Succeed())

			Expect(nodePool.Status.Phase).To(Equal(v1alpha1.NodePoolMissingPrerequisites))
			Expect(nodePool.Status.Message).To(Equal(
				fmt.Sprintf(common.NodesNotReadyMessage, "node-b") +
					"\nThe following prerequisites are not met on one or more nodes in the node pool: " +
					"NodeResourceTopology custom resource missing. " +
					"As a result, NUMA-aware scheduling may be impacted.",
			))
			Expect(nodePool.Status.Nodes).To(Equal([]v1alpha1.NodeInNodePool{
				{Name: "node-a", Status: v1alpha1.NodeMissingNrtHealthyPrerequisite},
				{Name: "node-b", Status: v1alpha1.NodeUnschedulable},
			}))
			Expect(nodePool.Status.Conditions).To(HaveLen(1))
			condition := nodePool.Status.Conditions[0]
			Expect(condition.Type).To(Equal(v1alpha1.NodePoolMissingNrtHealthyPrerequisite))
			Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			Expect(condition.Reason).To(Equal(v1alpha1.NodePoolMissingNrtHealthyPrerequisiteReason))
			Expect(condition.Message).To(Equal(
				"The following prerequisites are not met on one or more nodes in the node pool: " +
					"NodeResourceTopology custom resource missing. " +
					"As a result, NUMA-aware scheduling may be impacted.",
			))
		})
	})

	Describe("getNodesSituation", func() {
		It("marks ready nodes with a true NRT condition and deduplicates their prerequisite categories", func() {
			policyNode := nodeForStatusTest("node-b", true, corev1.ConditionTrue)
			findNodeCondition(policyNode, NrtHealthyConditionType).Reason = NrtHealthReasonPolicyNotEnforcing
			invalidNrtNode := nodeForStatusTest("node-d", true, corev1.ConditionTrue)
			findNodeCondition(invalidNrtNode, NrtHealthyConditionType).Reason = NrtHealthReasonNoNumaZones
			nodes := []*corev1.Node{
				policyNode,
				invalidNrtNode,
				nodeForStatusTest("node-a", true, corev1.ConditionTrue),
				nodeForStatusTest("node-c", true),
			}

			unschedulable, missingNrtItems, nodesInNodePool, atLeastOneReady,
				atLeastOneUnschedulable := getNodesSituation(nodes)

			Expect(unschedulable).To(BeEmpty())
			Expect(missingNrtItems).To(Equal([]string{
				NrtMissingItemPolicy,
				NrtMissingItemNoNumaZones,
				NrtMissingItemNoObject,
			}))
			Expect(nodesInNodePool).To(Equal([]v1alpha1.NodeInNodePool{
				{Name: "node-a", Status: v1alpha1.NodeMissingNrtHealthyPrerequisite},
				{Name: "node-b", Status: v1alpha1.NodeMissingNrtHealthyPrerequisite},
				{Name: "node-c", Status: v1alpha1.NodeReady},
				{Name: "node-d", Status: v1alpha1.NodeMissingNrtHealthyPrerequisite},
			}))
			Expect(atLeastOneReady).To(BeTrue())
			Expect(atLeastOneUnschedulable).To(BeFalse())
		})

		It("keeps node readiness precedence while still reporting its NRT prerequisite", func() {
			node := nodeForStatusTest("node-a", false, corev1.ConditionTrue)

			unschedulable, missingNrtItems, nodesInNodePool, atLeastOneReady,
				atLeastOneUnschedulable := getNodesSituation([]*corev1.Node{node})

			Expect(unschedulable).To(Equal([]string{"node-a"}))
			Expect(missingNrtItems).To(Equal([]string{NrtMissingItemNoObject}))
			Expect(nodesInNodePool).To(Equal([]v1alpha1.NodeInNodePool{{
				Name:   "node-a",
				Status: v1alpha1.NodeUnschedulable,
			}}))
			Expect(atLeastOneReady).To(BeFalse())
			Expect(atLeastOneUnschedulable).To(BeTrue())
		})

		It("ignores an NRT condition whose status is false", func() {
			node := nodeForStatusTest("node-a", true, corev1.ConditionFalse)

			_, missingNrtItems, nodesInNodePool, _, _ := getNodesSituation([]*corev1.Node{node})

			Expect(missingNrtItems).To(BeEmpty())
			Expect(nodesInNodePool).To(Equal([]v1alpha1.NodeInNodePool{{
				Name:   "node-a",
				Status: v1alpha1.NodeReady,
			}}))
		})
	})

	Describe("nodePoolPhaseForNodeSituation", func() {
		It("returns Empty when the nodepool has no nodes", func() {
			Expect(nodePoolPhaseForNodeSituation(false, false, false)).To(Equal(v1alpha1.NodePoolEmpty))
		})

		It("returns Ready when at least one node is ready and NRT is healthy", func() {
			Expect(nodePoolPhaseForNodeSituation(true, true, false)).To(Equal(v1alpha1.NodePoolReady))
		})

		It("returns MissingPrerequisites when NRT is unhealthy and the nodepool is otherwise ready", func() {
			Expect(nodePoolPhaseForNodeSituation(true, true, true)).To(Equal(v1alpha1.NodePoolMissingPrerequisites))
		})

		It("keeps Unschedulable precedence when no node is ready", func() {
			Expect(nodePoolPhaseForNodeSituation(false, true, true)).To(Equal(v1alpha1.NodePoolUnschedulable))
		})
	})

	Describe("nodepool NRT condition and message", func() {
		DescribeTable("maps node condition reasons to nodepool prerequisite categories",
			func(reason, expected string) {
				Expect(nrtNodePoolMissingItemForReason(reason)).To(Equal(expected))
			},
			Entry("missing NRT", NrtHealthReasonMissing, NrtMissingItemNoObject),
			Entry("invalid NRT", NrtHealthReasonNoNumaZones, NrtMissingItemNoNumaZones),
			Entry("Topology Manager policy", NrtHealthReasonPolicyNotEnforcing, NrtMissingItemPolicy),
			Entry("unknown reason", "Unknown", ""),
		)

		It("sets a true condition with the missing prerequisite categories and appends its warning", func() {
			nodePool := &v1alpha1.NodePool{}
			missingItems := []string{NrtMissingItemPolicy, NrtMissingItemNoNumaZones, NrtMissingItemNoObject}

			setNodePoolNrtHealthCondition(nodePool, missingItems)

			Expect(nodePool.Status.Conditions).To(HaveLen(1))
			condition := nodePool.Status.Conditions[0]
			Expect(condition.Type).To(Equal(v1alpha1.NodePoolMissingNrtHealthyPrerequisite))
			Expect(condition.Status).To(Equal(corev1.ConditionTrue))
			Expect(condition.Reason).To(Equal(v1alpha1.NodePoolMissingNrtHealthyPrerequisiteReason))
			Expect(condition.Message).To(Equal(
				"The following prerequisites are not met on one or more nodes in the node pool: " +
					"Kubelet Topology Manager Policy misconfigured, " +
					"NodeResourceTopology custom resource invalid, " +
					"NodeResourceTopology custom resource missing. " +
					"As a result, NUMA-aware scheduling may be impacted.",
			))
			Expect(joinNodePoolStatusMessages("Existing warning", condition.Message)).To(Equal(
				"Existing warning\nThe following prerequisites are not met on one or more nodes in the node pool: " +
					"Kubelet Topology Manager Policy misconfigured, " +
					"NodeResourceTopology custom resource invalid, " +
					"NodeResourceTopology custom resource missing. " +
					"As a result, NUMA-aware scheduling may be impacted.",
			))
		})

		It("does not add a false condition before NRT health has degraded", func() {
			nodePool := &v1alpha1.NodePool{}

			setNodePoolNrtHealthCondition(nodePool, nil)

			Expect(nodePool.Status.Conditions).To(BeEmpty())
			Expect(nrtNodePoolPrereqMessage(nil)).To(BeEmpty())
			Expect(joinNodePoolStatusMessages("", "")).To(BeEmpty())
		})

		It("transitions an existing condition to false after recovery", func() {
			previousTransition := metav1.NewTime(time.Now().Add(-time.Hour))
			nodePool := &v1alpha1.NodePool{Status: v1alpha1.NodePoolStatus{
				Conditions: []v1alpha1.NodePoolCondition{{
					Type:               v1alpha1.NodePoolMissingNrtHealthyPrerequisite,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: previousTransition,
					Reason:             v1alpha1.NodePoolMissingNrtHealthyPrerequisiteReason,
					Message:            "old warning",
				}},
			}}

			setNodePoolNrtHealthCondition(nodePool, nil)

			Expect(nodePool.Status.Conditions).To(HaveLen(1))
			condition := nodePool.Status.Conditions[0]
			Expect(condition.Status).To(Equal(corev1.ConditionFalse))
			Expect(condition.Message).To(BeEmpty())
			Expect(condition.LastTransitionTime.Time).To(BeTemporally(">", previousTransition.Time))
		})
	})
})

var _ = Describe("NodePool status when its partition's SchedulingShard is not its own", func() {
	It("leaves the shard alone, names it in the status message, and recovers once it is gone", func() {
		restoreConfig := config.SetForTest(&config.NodePoolControllerConfig{DefaultNodepoolName: "default"})
		DeferCleanup(restoreConfig)

		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(kaiv1.AddToScheme(scheme)).To(Succeed())
		Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

		nodePool := &v1alpha1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "pool-a", UID: "pool-a-uid"}}
		foreignShard := &kaiv1.SchedulingShard{
			ObjectMeta: metav1.ObjectMeta{Name: "admin-shard"},
			Spec: kaiv1.SchedulingShardSpec{
				PartitionLabelValue: "pool-a",
				Args:                map[string]string{"admin": "setting"},
			},
		}
		k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodePool, foreignShard).
			WithIndex(&kaiv1.SchedulingShard{}, common.SchedulingShardPartitionField, SchedulingShardPartitionIndexer).
			Build()
		npc := NewNodePoolController(k8sClient, scheme, &common.NodePoolControllerParams{})

		Expect(npc.reconcileScheduler(ctx, nodePool)).To(MatchError(ContainSubstring("admin-shard")))
		unchanged := &kaiv1.SchedulingShard{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "admin-shard"}, unchanged)).To(Succeed())
		Expect(unchanged.OwnerReferences).To(BeEmpty())
		Expect(unchanged.Spec).To(Equal(foreignShard.Spec))

		Expect(npc.calculateNodePoolStatus(ctx, nodePool, nil)).To(Succeed())
		Expect(nodePool.Status.Phase).To(Equal(v1alpha1.NodePoolUnschedulable))
		Expect(nodePool.Status.Message).To(And(
			HavePrefix(common.SchedulerNotReadyMessage+", reason: "),
			ContainSubstring("scheduling shard admin-shard"),
			ContainSubstring("re-run the KRM upgrade"),
		))

		Expect(k8sClient.Delete(ctx, unchanged)).To(Succeed())
		Expect(npc.reconcileScheduler(ctx, nodePool)).To(Succeed())
		ownShard := &kaiv1.SchedulingShard{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nodePool), ownShard)).To(Succeed())
		Expect(metav1.IsControlledBy(ownShard, nodePool)).To(BeTrue())

		Expect(npc.calculateNodePoolStatus(ctx, nodePool, nil)).To(Succeed())
		Expect(nodePool.Status.Message).NotTo(ContainSubstring("admin-shard"))
	})
})
