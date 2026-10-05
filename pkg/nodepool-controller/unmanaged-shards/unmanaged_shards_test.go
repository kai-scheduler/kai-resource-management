// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package unmanaged_shards

import (
	"context"
	"testing"

	kaiv1 "github.com/kai-scheduler/api/kai/v1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUnmanagedShards(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Unmanaged Shards Suite")
}

func newShard(name, partition string, labels map[string]string, owners ...metav1.OwnerReference) *kaiv1.SchedulingShard {
	return &kaiv1.SchedulingShard{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels, OwnerReferences: owners},
		Spec:       kaiv1.SchedulingShardSpec{PartitionLabelValue: partition},
	}
}

func controllerOwner(apiVersion, kind string) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       "owner",
		UID:        "owner-uid",
		Controller: ptr.To(true),
	}
}

var ignored = map[string]string{IgnoreShardLabelKey: "true"}

var _ = Describe("Unmanaged shards", func() {
	Context("IsUnmanaged", func() {
		It("is true for a labelled, un-owned shard", func() {
			Expect(IsUnmanaged(newShard("legacy", "legacy", ignored))).To(BeTrue())
		})

		It("is false without the label", func() {
			Expect(IsUnmanaged(newShard("legacy", "legacy", nil))).To(BeFalse())
		})

		It("is false when the label is not \"true\"", func() {
			Expect(IsUnmanaged(newShard("legacy", "legacy", map[string]string{IgnoreShardLabelKey: "false"}))).To(BeFalse())
		})

		It("is false on the default partition", func() {
			Expect(IsUnmanaged(newShard("default", "", ignored))).To(BeFalse())
		})

		It("is false when a NodePool owns the shard", func() {
			owner := controllerOwner(v1alpha1.GroupVersion.String(), "NodePool")
			Expect(IsUnmanaged(newShard("legacy", "legacy", ignored, owner))).To(BeFalse())
		})

		It("is true when the shard is owned by something other than a NodePool", func() {
			owner := controllerOwner("apps/v1", "Deployment")
			Expect(IsUnmanaged(newShard("legacy", "legacy", ignored, owner))).To(BeTrue())
		})
	})

	Context("ListPartitionLabelValues", func() {
		It("returns only the partitions of unmanaged shards", func() {
			scheme := runtime.NewScheme()
			Expect(kaiv1.AddToScheme(scheme)).To(Succeed())

			nodePoolOwner := controllerOwner(v1alpha1.GroupVersion.String(), "NodePool")
			objects := []client.Object{
				newShard("legacy-a", "legacy-a", ignored),
				newShard("legacy-b-shard", "legacy-b", ignored),
				newShard("managed", "managed", nil),
				newShard("owned", "owned", ignored, nodePoolOwner),
				newShard("default", "", ignored),
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

			partitions, err := ListPartitionLabelValues(context.Background(), reader)
			Expect(err).NotTo(HaveOccurred())
			Expect(partitions).To(Equal(map[string]bool{"legacy-a": true, "legacy-b": true}))
		})

		It("returns an empty set when no shard is labelled", func() {
			scheme := runtime.NewScheme()
			Expect(kaiv1.AddToScheme(scheme)).To(Succeed())
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newShard("managed", "managed", nil)).Build()

			partitions, err := ListPartitionLabelValues(context.Background(), reader)
			Expect(err).NotTo(HaveOccurred())
			Expect(partitions).To(BeEmpty())
		})
	})
})
