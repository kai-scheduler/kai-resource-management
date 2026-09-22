// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
)

const cleanupNamespace = "kai-resource-management"

func managedDeployment(name, namespace string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels:    map[string]string{common.OperatorManagedByLabelKey: common.OperatorManagedByLabelValue},
	}}
}

var _ = Describe("Cleanup", func() {
	var ctx context.Context

	newClient := func(objects ...client.Object) client.Client {
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(kaires.AddToScheme(scheme))
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	}

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("deletes the operator-managed deployments in the namespace", func() {
		fakeClient := newClient(managedDeployment("nodepool-controller", cleanupNamespace))

		Expect(Cleanup(ctx, fakeClient, cleanupNamespace, "")).To(Succeed())

		err := fakeClient.Get(ctx,
			client.ObjectKey{Namespace: cleanupNamespace, Name: "nodepool-controller"}, &appsv1.Deployment{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("leaves a deployment the operator does not manage", func() {
		foreign := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name: "somebody-elses", Namespace: cleanupNamespace}}
		fakeClient := newClient(foreign)

		Expect(Cleanup(ctx, fakeClient, cleanupNamespace, "")).To(Succeed())

		Expect(fakeClient.Get(ctx,
			client.ObjectKey{Namespace: cleanupNamespace, Name: "somebody-elses"}, &appsv1.Deployment{})).To(Succeed())
	})

	It("leaves a managed deployment in another namespace", func() {
		fakeClient := newClient(managedDeployment("nodepool-controller", "other"))

		Expect(Cleanup(ctx, fakeClient, cleanupNamespace, "")).To(Succeed())

		Expect(fakeClient.Get(ctx,
			client.ObjectKey{Namespace: "other", Name: "nodepool-controller"}, &appsv1.Deployment{})).To(Succeed())
	})

	It("deletes the named KRMConfig", func() {
		config := &kaires.KRMConfig{ObjectMeta: metav1.ObjectMeta{Name: "krm-config"}}
		fakeClient := newClient(config)

		Expect(Cleanup(ctx, fakeClient, cleanupNamespace, "krm-config")).To(Succeed())

		err := fakeClient.Get(ctx, client.ObjectKey{Name: "krm-config"}, &kaires.KRMConfig{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("leaves the KRMConfig alone when no name is given", func() {
		config := &kaires.KRMConfig{ObjectMeta: metav1.ObjectMeta{Name: "krm-config"}}
		fakeClient := newClient(config)

		Expect(Cleanup(ctx, fakeClient, cleanupNamespace, "")).To(Succeed())

		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "krm-config"}, &kaires.KRMConfig{})).To(Succeed())
	})

	It("succeeds when the KRMConfig is already gone", func() {
		Expect(Cleanup(ctx, newClient(), cleanupNamespace, "krm-config")).To(Succeed())
	})

	It("succeeds when the KRMConfig CRD itself is already gone", func() {
		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(kaires.AddToScheme(scheme))
		unmapped := fake.NewClientBuilder().WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
					return &meta.NoKindMatchError{}
				},
			}).Build()

		Expect(Cleanup(ctx, unmapped, cleanupNamespace, "krm-config")).To(Succeed())
	})
})
