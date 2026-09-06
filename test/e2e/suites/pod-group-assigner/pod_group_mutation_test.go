// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pod_group_assigner

import (
	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	kaiconstants "github.com/kai-scheduler/api/constants"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgaconfig "github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/config"
	assignercommon "github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/common"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// No pods on purpose: the assigner gives up on a pod group it can find none for.
var _ = Describe("The pod group mutating webhook", Ordered, Label("pod-group-assigner"), func() {
	var (
		nodePool  *kaires.NodePool
		project   *kaires.Project
		namespace string
	)

	createAndFetchPodGroup := func(labels map[string]string, spec kaiv2alpha2.PodGroupSpec) *kaiv2alpha2.PodGroup {
		if labels == nil {
			labels = map[string]string{}
		}
		for key, value := range constant.OwnerLabels() {
			labels[key] = value
		}

		podGroup := &kaiv2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      utils.GenerateName("pga-pg"),
				Namespace: namespace,
				Labels:    labels,
			},
			Spec: spec,
		}
		Expect(testClient.Create(ctx, podGroup)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, podGroup))).To(Succeed())
		})

		stored := &kaiv2alpha2.PodGroup{}
		Expect(testClient.Get(ctx,
			types.NamespacedName{Namespace: namespace, Name: podGroup.Name}, stored)).To(Succeed())

		return stored
	}

	BeforeAll(func() {
		nodePool = resources.GeneratedNodePool("pga-pgmutation", nodePoolLabelKey)
		Expect(testClient.Create(ctx, nodePool)).To(Succeed())
		wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolEmpty)

		project = resources.Project(utils.GenerateName("pga-pgmutation-proj"),
			[]string{nodePool.Name})
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, nodePool)
		})
	})

	It("marks a pod group that names no node pool with the sentinel", func() {
		podGroup := createAndFetchPodGroup(nil, kaiv2alpha2.PodGroupSpec{MinMember: ptr.To(int32(1))})

		Expect(podGroup.Labels).To(HaveKeyWithValue(
			kaiconstants.DefaultNodePoolLabelKey, pgaconfig.DefaultUnexistingNodepoolSentinel))
	})

	It("leaves a pod group that already names one as it is", func() {
		podGroup := createAndFetchPodGroup(
			map[string]string{kaiconstants.DefaultNodePoolLabelKey: nodePool.Name},
			kaiv2alpha2.PodGroupSpec{MinMember: ptr.To(int32(1))})

		Expect(podGroup.Labels).To(HaveKeyWithValue(
			kaiconstants.DefaultNodePoolLabelKey, nodePool.Name))
	})

	It("takes over the scheduling backoff and unschedulable mark it was given", func() {
		podGroup := createAndFetchPodGroup(nil, kaiv2alpha2.PodGroupSpec{
			MinMember:         ptr.To(int32(1)),
			MarkUnschedulable: ptr.To(true),
			SchedulingBackoff: ptr.To(int32(assignercommon.NoSchedulingBackoff)),
		})

		Expect(podGroup.Spec.MarkUnschedulable).To(HaveValue(BeFalse()))
		Expect(podGroup.Spec.SchedulingBackoff).To(HaveValue(
			Equal(int32(assignercommon.SingleSchedulingBackoff))))
	})
})
