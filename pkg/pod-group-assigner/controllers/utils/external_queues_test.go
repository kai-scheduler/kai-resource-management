// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"testing"

	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/config"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pod Group Assigner Utils Unit Tests")
}

const (
	testNamespaceProjectLabelKey = "test/project"
	testProjectLabelKey          = "project"
	testNodePoolLabelKey         = "test/node-pool"
	testDefaultNodePoolName      = "default"
)

var _ = Describe("IsExternalNamespace", func() {
	DescribeTable("decides from the namespace's project label",
		func(allowed bool, objects []client.Object, expected bool) {
			DeferCleanup(config.SetForTest(externalQueuesConfig(allowed)))

			external, err := IsExternalNamespace(context.Background(), newFakeClient(objects...), "ns")

			Expect(err).NotTo(HaveOccurred())
			Expect(external).To(Equal(expected))
		},
		Entry("no namespace is external while external queues are not allowed",
			false, []client.Object{namespace("ns", "")}, false),
		Entry("a namespace whose project label names an existing project is that project's",
			true, []client.Object{namespace("ns", "research"), project("research")}, false),
		Entry("a namespace whose project label names a project that is gone is external",
			true, []client.Object{namespace("ns", "research")}, true),
		Entry("a namespace without the project label is external",
			true, []client.Object{namespace("ns", "")}, true),
		Entry("a namespace that is gone is external",
			true, []client.Object{}, true),
	)
})

var _ = Describe("GetQueueNameOfNodePool", func() {
	It("skips a queue no Project owns while external queues are allowed", func() {
		DeferCleanup(config.SetForTest(externalQueuesConfig(true)))
		k8sClient := newFakeClient(projectQueue("admin", "research", false), projectQueue("research", "research", true))

		queueName, err := GetQueueNameOfNodePool(context.Background(), k8sClient, "research", testDefaultNodePoolName)

		Expect(err).NotTo(HaveOccurred())
		Expect(queueName).To(Equal("research"))
	})

	It("finds no queue when only an unowned one matches and external queues are allowed", func() {
		DeferCleanup(config.SetForTest(externalQueuesConfig(true)))
		k8sClient := newFakeClient(projectQueue("admin", "research", false))

		_, err := GetQueueNameOfNodePool(context.Background(), k8sClient, "research", testDefaultNodePoolName)

		Expect(errors.IsNotFound(err)).To(BeTrue())
	})

	It("takes an unowned queue while external queues are not allowed", func() {
		DeferCleanup(config.SetForTest(externalQueuesConfig(false)))
		k8sClient := newFakeClient(projectQueue("admin", "research", false))

		queueName, err := GetQueueNameOfNodePool(context.Background(), k8sClient, "research", testDefaultNodePoolName)

		Expect(err).NotTo(HaveOccurred())
		Expect(queueName).To(Equal("admin"))
	})
})

func externalQueuesConfig(allowed bool) config.PodGroupAssignerConfig {
	return config.PodGroupAssignerConfig{
		NamespaceProjectLabelKey: testNamespaceProjectLabelKey,
		ProjectLabelKey:          testProjectLabelKey,
		NodePoolLabelKey:         testNodePoolLabelKey,
		DefaultNodepoolName:      testDefaultNodePoolName,
		AllowExternalQueues:      allowed,
	}
}

func newFakeClient(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(kaiv2.AddToScheme(scheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func project(name string) *v1alpha1.Project {
	return &v1alpha1.Project{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

// namespace builds a namespace fixture; an empty project leaves the project label off.
func namespace(name, project string) *corev1.Namespace {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if project != "" {
		ns.Labels = map[string]string{testNamespaceProjectLabelKey: project}
	}

	return ns
}

// projectQueue builds a default-pool queue labelled for project, owned by it when owned is set.
func projectQueue(name, project string, owned bool) *kaiv2.Queue {
	queue := &kaiv2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{testProjectLabelKey: project},
		},
	}
	if owned {
		queue.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: "kai.resources/v1alpha1",
			Kind:       projectKind,
			Name:       project,
			UID:        "project-uid",
		}}
	}

	return queue
}
