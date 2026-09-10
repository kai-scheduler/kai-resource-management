// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	"context"
	"errors"

	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
)

var _ = Describe("handleNodePoolDeletionOnOwnerUninstall", func() {
	var (
		ctx      context.Context
		scheme   *runtime.Scheme
		nodePool *v1alpha1.NodePool
		gets     []schema.GroupVersionKind

		ownerGVK = schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Cluster"}
		ownerRef = &common.UninstallDetectionRef{
			GVK: ownerGVK,
			Key: types.NamespacedName{Name: "my-cluster", Namespace: "my-namespace"},
		}
	)

	// recordingClient counts every Get so a spec can assert the controller
	// never reaches the API server when the check is disabled.
	recordingClient := func(objs ...client.Object) client.Client {
		return fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey,
					obj client.Object, opts ...client.GetOption) error {
					gets = append(gets, obj.GetObjectKind().GroupVersionKind())
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()
	}

	ownerObject := func(deleting bool) *unstructured.Unstructured {
		owner := &unstructured.Unstructured{}
		owner.SetGroupVersionKind(ownerGVK)
		owner.SetName(ownerRef.Key.Name)
		owner.SetNamespace(ownerRef.Key.Namespace)
		if deleting {
			now := metav1.Now()
			owner.SetDeletionTimestamp(&now)
			owner.SetFinalizers([]string{"example.com/keep"})
		}
		return owner
	}

	BeforeEach(func() {
		ctx = context.Background()
		gets = nil
		scheme = runtime.NewScheme()
		Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())
		nodePool = &v1alpha1.NodePool{ObjectMeta: metav1.ObjectMeta{Name: "np"}}
		DeferCleanup(config.SetForTest(&config.NodePoolControllerConfig{
			FinalizerDomain: "kai.scheduler",
		}))
	})

	Context("when no uninstall-detection GVK is configured", func() {
		It("does not delete the nodepool and never queries the API server", func() {
			npc := NewNodePoolController(recordingClient(), scheme, &common.NodePoolControllerParams{})

			Expect(npc.handleNodePoolDeletionOnOwnerUninstall(ctx, nodePool)).To(BeFalse())
			Expect(gets).To(BeEmpty())
		})
	})

	Context("when the configured CR is absent", func() {
		It("treats the install as gone and force-deletes the nodepool", func() {
			npc := NewNodePoolController(recordingClient(), scheme,
				&common.NodePoolControllerParams{UninstallDetection: ownerRef})

			Expect(npc.handleNodePoolDeletionOnOwnerUninstall(ctx, nodePool)).To(BeTrue())
			Expect(gets).To(ConsistOf(ownerGVK))
		})
	})

	Context("when reading the configured CR is forbidden", func() {
		It("does not delete the nodepool, unlike an absent CR", func() {
			denied := fake.NewClientBuilder().WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey,
						_ client.Object, _ ...client.GetOption) error {
						return apierrors.NewForbidden(
							schema.GroupResource{Group: ownerGVK.Group, Resource: "clusters"},
							ownerRef.Key.Name, errors.New("no grant"))
					},
				}).Build()
			npc := NewNodePoolController(denied, scheme,
				&common.NodePoolControllerParams{UninstallDetection: ownerRef})

			Expect(npc.handleNodePoolDeletionOnOwnerUninstall(ctx, nodePool)).To(BeFalse())
		})
	})

	Context("when the configured CR exists and is not being deleted", func() {
		It("does not delete the nodepool", func() {
			npc := NewNodePoolController(recordingClient(ownerObject(false)), scheme,
				&common.NodePoolControllerParams{UninstallDetection: ownerRef})

			Expect(npc.handleNodePoolDeletionOnOwnerUninstall(ctx, nodePool)).To(BeFalse())
		})
	})

	Context("when the configured CR is being deleted", func() {
		It("force-deletes the nodepool", func() {
			npc := NewNodePoolController(recordingClient(ownerObject(true)), scheme,
				&common.NodePoolControllerParams{UninstallDetection: ownerRef})

			Expect(npc.handleNodePoolDeletionOnOwnerUninstall(ctx, nodePool)).To(BeTrue())
		})
	})
})
