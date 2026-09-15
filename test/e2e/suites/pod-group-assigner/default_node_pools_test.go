// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package pod_group_assigner

import (
	kaiconstants "github.com/kai-scheduler/api/constants"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

var _ = Describe("The pod mutating webhook", Ordered, Label("pod-group-assigner"), func() {
	var (
		fallbackPool *kaires.NodePool
		otherPool    *kaires.NodePool
		project      *kaires.Project
		namespace    string
	)

	requirements := func(pod *corev1.Pod) []corev1.NodeSelectorRequirement {
		affinity := pod.Spec.Affinity
		if affinity == nil || affinity.NodeAffinity == nil ||
			affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			return nil
		}

		var all []corev1.NodeSelectorRequirement
		for _, term := range affinity.NodeAffinity.
			RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			all = append(all, term.MatchExpressions...)
		}

		return all
	}

	createAndFetchPod := func(options ...resources.PodOption) *corev1.Pod {
		pod := resources.Pod(utils.GenerateName("pga-mutation"), namespace, options...)
		Expect(testClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
		})

		stored := &corev1.Pod{}
		Expect(testClient.Get(ctx,
			types.NamespacedName{Namespace: namespace, Name: pod.Name}, stored)).To(Succeed())

		return stored
	}

	BeforeAll(func() {
		fallbackPool = resources.GeneratedNodePool("pga-fallback", nodePoolLabelKey)
		otherPool = resources.GeneratedNodePool("pga-notdefaulted", nodePoolLabelKey)
		for _, nodePool := range []*kaires.NodePool{fallbackPool, otherPool} {
			Expect(testClient.Create(ctx, nodePool)).To(Succeed())
			wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolEmpty)
		}

		// Only one pool among the defaults, so the other stays available to ask for.
		project = resources.Project(utils.GenerateName("pga-defaults-proj"),
			[]string{fallbackPool.Name, otherPool.Name},
			resources.WithDefaultNodePools(fallbackPool.Name),
			resources.WithEnforceScheduler(true))
		Expect(testClient.Create(ctx, project)).To(Succeed())
		namespace = wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			for _, nodePool := range []*kaires.NodePool{fallbackPool, otherPool} {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
				wait.ForDeleted(ctx, testClient, nodePool)
			}
		})
	})

	It("gives a pod that asks for nothing the project's default node pools", func() {
		pod := createAndFetchPod()

		Expect(requirements(pod)).To(ConsistOf(corev1.NodeSelectorRequirement{
			Key:      fallbackPool.Spec.LabelKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{fallbackPool.Spec.LabelValue},
		}))
	})

	It("lets a node pool label on the pod win over those defaults", func() {
		pod := createAndFetchPod(resources.WithNodePoolLabel(
			kaiconstants.DefaultNodePoolLabelKey, otherPool.Name))

		Expect(requirements(pod)).To(ConsistOf(corev1.NodeSelectorRequirement{
			Key:      otherPool.Spec.LabelKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{otherPool.Spec.LabelValue},
		}), "the defaults were applied on top of the pod's own node pool")
	})

	It("leaves a pod that already names a node pool in its affinity alone", func() {
		asked := resources.NodeSelectorForNodePool(otherPool)

		pod := createAndFetchPod(resources.WithNodeAffinity(asked))

		Expect(requirements(pod)).To(ConsistOf(corev1.NodeSelectorRequirement{
			Key:      asked.Key,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{asked.Value},
		}))
	})

	// The default nodepool is the absence of the label, not a value of it.
	It("expresses the default node pool as the label being absent", func() {
		pod := createAndFetchPod(resources.WithNodePoolLabel(
			kaiconstants.DefaultNodePoolLabelKey, testcontext.DefaultNodePoolName))

		Expect(requirements(pod)).To(ConsistOf(corev1.NodeSelectorRequirement{
			Key:      kaiconstants.DefaultNodePoolLabelKey,
			Operator: corev1.NodeSelectorOpDoesNotExist,
		}))
	})

	// Fail-open: a misconfigured nodepool must never block pod creation.
	It("admits a pod naming a deleting node pool, adding no affinity", func() {
		deletingPool := resources.GeneratedNodePool("pga-deleting", nodePoolLabelKey)
		Expect(testClient.Create(ctx, deletingPool)).To(Succeed())
		wait.ForNodePoolPhase(ctx, testClient, deletingPool.Name, kaires.NodePoolEmpty)

		// A project queue holds the pool in Deleting rather than letting it go.
		blocker := resources.Project(utils.GenerateName("pga-blocker-proj"),
			[]string{testcontext.DefaultNodePoolName, deletingPool.Name},
			resources.WithDefaultNodePools(testcontext.DefaultNodePoolName))
		Expect(testClient.Create(ctx, blocker)).To(Succeed())
		wait.ForProjectReady(ctx, testClient, blocker.Name)

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, blocker))).To(Succeed())
			wait.ForDeleted(ctx, testClient, blocker)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, deletingPool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, deletingPool)
		})

		// Without the finalizer the delete below just succeeds, leaving no pool at all.
		Eventually(func(g Gomega) {
			latest := &kaires.NodePool{}
			g.Expect(testClient.Get(ctx,
				types.NamespacedName{Name: deletingPool.Name}, latest)).To(Succeed())
			g.Expect(latest.Finalizers).ToNot(BeEmpty())
		}).Should(Succeed())

		Expect(testClient.Delete(ctx, deletingPool)).To(Succeed())
		wait.ForNodePoolPhase(ctx, testClient, deletingPool.Name, kaires.NodePoolDeleting)

		pod := createAndFetchPod(resources.WithNodePoolLabel(
			kaiconstants.DefaultNodePoolLabelKey, deletingPool.Name))

		Expect(requirements(pod)).To(BeEmpty())
	})
})
