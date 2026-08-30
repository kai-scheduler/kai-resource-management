// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	"time"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// blockedGracePeriod is how long the pool is watched for a deletion that should never
// happen. Long enough to outlast several reconciles, short enough to keep the suite quick.
const blockedGracePeriod = 7 * time.Second

var _ = Describe("Deleting a node pool a project references", Ordered, Label("nodepool-controller"), func() {
	var (
		nodePool *kaires.NodePool
		project  *kaires.Project
	)

	BeforeAll(func() {
		// Deliberately no node: the project reference is then the only thing holding the
		// pool, so a failure here cannot be confused with a node that did not leave.
		nodePool = resources.GeneratedNodePool("npc-referenced", nodePoolLabelKey)
		Expect(testClient.Create(ctx, nodePool)).To(Succeed())
		wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolEmpty)

		// Queues for both pools, but only the default one among the defaults: the webhook
		// wants a queue for every default pool, not the reverse, so this leaves the
		// referencing queue free to be dropped on its own later.
		project = resources.Project(
			utils.GenerateName("npc-proj"),
			[]string{testcontext.DefaultNodePoolName, nodePool.Name},
			resources.WithDefaultNodePools(testcontext.DefaultNodePoolName),
		)
		Expect(testClient.Create(ctx, project)).To(Succeed())
		wait.ForProjectReady(ctx, testClient, project.Name)

		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
			wait.ForDeleted(ctx, testClient, project)

			Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
			wait.ForDeleted(ctx, testClient, nodePool)
		})
	})

	It("holds the pool in Deleting and names the project blocking it", func() {
		// Wait for the controller's finalizer first: without it the delete below would
		// simply succeed, and the spec would fail for a reason that is not the one
		// under test.
		Eventually(func(g Gomega) {
			latest := &kaires.NodePool{}
			g.Expect(testClient.Get(ctx, types.NamespacedName{Name: nodePool.Name}, latest)).To(Succeed())
			g.Expect(latest.Finalizers).ToNot(BeEmpty())
		}).Should(Succeed())

		Expect(testClient.Delete(ctx, nodePool)).To(Succeed())

		blocked := wait.ForNodePoolPhase(ctx, testClient, nodePool.Name, kaires.NodePoolDeleting)
		Expect(blocked.DeletionTimestamp.IsZero()).To(BeFalse(), "the pool was never asked to delete")

		condition := wait.ForNodePoolCondition(ctx, testClient, nodePool.Name, kaires.ProjectReferencesExist)
		Expect(condition.Reason).To(Equal(kaires.ProjectReferencesExistReason))
		Expect(condition.Message).To(ContainSubstring(project.Name))
	})

	It("keeps the pool for as long as the reference stands", func() {
		// The point is not that deletion is slow but that it does not happen at all,
		// which only Consistently can say.
		Consistently(func() error {
			return testClient.Get(ctx, types.NamespacedName{Name: nodePool.Name}, &kaires.NodePool{})
		}).WithContext(ctx).WithTimeout(blockedGracePeriod).WithPolling(constant.Interval).
			Should(Succeed(), "the pool went while a project still referenced it")
	})

	It("lets the pool go once the project stops referencing it", func() {
		// Only spec.queues[].nodepool holds the pool, but the validating webhook wants a
		// queue for every default node pool, so both lists move to the default pool
		// together. Retried because project-controller writes the project's status.
		// Drop only the queue for this pool; the rest of the project is left as it was.
		// Retried because project-controller writes the project alongside this.
		Eventually(func(g Gomega) {
			latest := &kaires.Project{}
			g.Expect(testClient.Get(ctx, types.NamespacedName{Name: project.Name}, latest)).To(Succeed())

			remaining := make([]kaires.QueueConfig, 0, len(latest.Spec.Queues))
			for _, queue := range latest.Spec.Queues {
				if queue.Nodepool != nodePool.Name {
					remaining = append(remaining, queue)
				}
			}
			latest.Spec.Queues = remaining

			g.Expect(testClient.Update(ctx, latest)).To(Succeed())
		}).Should(Succeed())

		wait.ForDeleted(ctx, testClient, nodePool)
	})
})
