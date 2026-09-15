// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package managed_nodes_config

import (
	"context"
	"testing"

	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/nodepool_controller"
)

func TestManagedNodesConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ManagedNodesConfig Suite")
}

func mncTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

var _ = Describe("ManagedNodesConfig status reconciliation", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = mncTestScheme()
		DeferCleanup(config.SetForTest(&config.NodePoolControllerConfig{
			ManagedNodesConfigName:   "kai-managed-nodes-config",
			ShouldBeExcludedLabelKey: "kai.resources/should-be-excluded",
		}))
	})

	controllerWith := func(existing ...client.Object) *ManagedNodesConfigController {
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&v1alpha1.ManagedNodesConfig{}).
			WithObjects(existing...).
			Build()
		return NewManagedNodesConfigController(c, scheme,
			nodepool_controller.NewNodePoolController(c, scheme, &common.NodePoolControllerParams{}))
	}

	nodeRequest := func(name string) MNCReconcileRequest {
		return MNCReconcileRequest{NamespacedName: types.NamespacedName{Name: name}, isNode: true}
	}

	Context("when no ManagedNodesConfig exists in the cluster", func() {
		// getManagedNodesConfig substitutes a nameless placeholder, and a status
		// write to that is rejected with "resource name may not be empty".
		It("skips the status write instead of erroring on every node event", func() {
			mncc := controllerWith()

			err := mncc.reconcileStatus(ctx, nodeRequest("some-node"), &v1alpha1.ManagedNodesConfig{}, nil)

			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("when a ManagedNodesConfig exists", func() {
		It("writes the status", func() {
			mnc := &v1alpha1.ManagedNodesConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "kai-managed-nodes-config"},
			}
			mncc := controllerWith(mnc)

			err := mncc.reconcileStatus(ctx, nodeRequest("some-node"), mnc, nil)

			Expect(err).ToNot(HaveOccurred())
		})
	})
})
