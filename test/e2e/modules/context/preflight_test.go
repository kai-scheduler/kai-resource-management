// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package context

import (
	"context"
	"testing"

	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
)

// Runs against no cluster, so `go test ./test/e2e/modules/...` is enough. It sits
// outside `make test` with the rest of test/e2e, as it does in KAI-scheduler. The
// guard is worth testing because it must be seen to refuse, not only to pass.
func TestPreflight(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Preflight Suite")
}

func preflightScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(kaires.AddToScheme(scheme)).To(Succeed())
	Expect(kaiv2.AddToScheme(scheme)).To(Succeed())

	return scheme
}

func owned() map[string]string {
	return constant.OwnerLabels()
}

var _ = Describe("Preflight", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	clusterWith := func(objects ...client.Object) client.Client {
		return fake.NewClientBuilder().WithScheme(preflightScheme()).WithObjects(objects...).Build()
	}

	Context("on a cluster the tests own", func() {
		It("accepts an empty cluster", func() {
			Expect(checkForeignObjects(ctx, clusterWith())).To(Succeed())
		})

		It("accepts the chart's own default nodepool", func() {
			nodePool := &kaires.NodePool{ObjectMeta: metav1.ObjectMeta{Name: DefaultNodePoolName}}

			Expect(checkForeignObjects(ctx, clusterWith(nodePool))).To(Succeed())
		})

		It("accepts objects carrying the ownership label", func() {
			objects := []client.Object{
				&kaires.Project{ObjectMeta: metav1.ObjectMeta{Name: "p", Labels: owned()}},
				&kaires.Department{ObjectMeta: metav1.ObjectMeta{Name: "d", Labels: owned()}},
				&kaires.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "np", Labels: owned()}},
			}

			Expect(checkForeignObjects(ctx, clusterWith(objects...))).To(Succeed())
		})

		// A Queue is written by project-controller and never carries the test
		// labels, so it is judged by its owner instead.
		It("accepts a Queue owned by a Project", func() {
			queue := &kaiv2.Queue{ObjectMeta: metav1.ObjectMeta{
				Name:            "p-default",
				OwnerReferences: []metav1.OwnerReference{{Kind: "Project", Name: "p"}},
			}}

			Expect(checkForeignObjects(ctx, clusterWith(queue))).To(Succeed())
		})
	})

	Context("on a cluster holding objects the tests did not create", func() {
		It("refuses on a foreign Project", func() {
			project := &kaires.Project{ObjectMeta: metav1.ObjectMeta{Name: "someone-elses"}}

			err := checkForeignObjects(ctx, clusterWith(project))

			Expect(err).To(MatchError(ContainSubstring("Project: someone-elses")))
			Expect(err).To(MatchError(ContainSubstring("no override")))
		})

		It("refuses on a foreign Department", func() {
			department := &kaires.Department{ObjectMeta: metav1.ObjectMeta{Name: "research"}}

			Expect(checkForeignObjects(ctx, clusterWith(department))).
				To(MatchError(ContainSubstring("Department: research")))
		})

		It("refuses on a nodepool that is neither the default nor ours", func() {
			nodePool := &kaires.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "production-gpu"}}

			Expect(checkForeignObjects(ctx, clusterWith(nodePool))).
				To(MatchError(ContainSubstring("NodePool: production-gpu")))
		})

		It("refuses on a Queue that nothing owns", func() {
			queue := &kaiv2.Queue{ObjectMeta: metav1.ObjectMeta{Name: "hand-made"}}

			Expect(checkForeignObjects(ctx, clusterWith(queue))).
				To(MatchError(ContainSubstring("Queue: hand-made")))
		})

		It("reports every offending kind at once, so one run fixes them all", func() {
			objects := []client.Object{
				&kaires.Project{ObjectMeta: metav1.ObjectMeta{Name: "prod-project"}},
				&kaires.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "prod-pool"}},
			}

			err := checkForeignObjects(ctx, clusterWith(objects...))

			Expect(err).To(MatchError(ContainSubstring("prod-project")))
			Expect(err).To(MatchError(ContainSubstring("prod-pool")))
		})
	})
})
