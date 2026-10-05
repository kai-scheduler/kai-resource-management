// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"
	"errors"
	"testing"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/nodepool_controller"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

func TestNodePoolWebhook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodePool Webhook Suite")
}

func webhookTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(kaiv1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

// webhookTestClient indexes shards the way the manager's cache does, since both
// validators look shards up by partitionLabelValue.
func webhookTestClient(scheme *runtime.Scheme, existing ...client.Object) *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing...).
		WithIndex(&kaiv1.SchedulingShard{}, common.SchedulingShardPartitionField,
			nodepool_controller.SchedulingShardPartitionIndexer)
}

var errListFailed = errors.New("list failed")

func failingShardList() interceptor.Funcs {
	return interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*kaiv1.SchedulingShardList); ok {
				return errListFailed
			}
			return c.List(ctx, list, opts...)
		},
	}
}

func shard(name, partitionLabelValue string) *kaiv1.SchedulingShard {
	return &kaiv1.SchedulingShard{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       kaiv1.SchedulingShardSpec{PartitionLabelValue: partitionLabelValue},
	}
}

func unmanaged(s *kaiv1.SchedulingShard) *kaiv1.SchedulingShard {
	if s.Labels == nil {
		s.Labels = map[string]string{}
	}
	s.Labels[unmanaged_shards.IgnoreShardLabelKey] = "true"
	return s
}

func ownedBy(s *kaiv1.SchedulingShard, nodePoolName string) *kaiv1.SchedulingShard {
	s.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: v1alpha1.GroupVersion.String(),
		Kind:       "NodePool",
		Name:       nodePoolName,
		UID:        types.UID(nodePoolName + "-uid"),
		Controller: ptr.To(true),
	}}
	return s
}

func nodePool(name, labelKey, labelValue string) *v1alpha1.NodePool {
	return &v1alpha1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.NodePoolSpec{
			LabelKey:   labelKey,
			LabelValue: labelValue,
		},
	}
}

var _ = Describe("NodePool duplicate-label validation", func() {
	var (
		ctx     context.Context
		scheme  *runtime.Scheme
		newPool *v1alpha1.NodePool
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = webhookTestScheme()
		DeferCleanup(config.SetForTest(&config.NodePoolControllerConfig{DefaultNodepoolName: "default"}))
	})

	validatorWith := func(existing ...client.Object) *nodePoolValidator {
		return &nodePoolValidator{client: webhookTestClient(scheme, existing...).Build()}
	}

	Describe("ValidateCreate", func() {
		It("allows a nodepool with a unique labelKey/labelValue pair", func() {
			v := validatorWith(nodePool("pool-a", "gpu", "a100"))
			newPool = nodePool("pool-b", "gpu2", "a1002")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).ToNot(HaveOccurred())
		})

		It("rejects a nodepool duplicating an existing labelKey/labelValue pair", func() {
			v := validatorWith(nodePool("pool-a", "gpu", "a100"))
			newPool = nodePool("pool-b", "gpu", "a100")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pool-a"))
			Expect(err.Error()).To(ContainSubstring("pool-b"))
		})

		It("allows a nodepool that shares only the labelKey but not the labelValue", func() {
			v := validatorWith(nodePool("pool-a", "gpu", "a100"))
			newPool = nodePool("pool-b", "gpu", "h100")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).ToNot(HaveOccurred())
		})

		It("allows the default nodepool with an empty labelKey/labelValue", func() {
			v := validatorWith()
			newPool = nodePool(config.Get().DefaultNodepoolName, "", "")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).ToNot(HaveOccurred())
		})

		It("rejects the default nodepool when it sets a labelKey/labelValue", func() {
			v := validatorWith()
			newPool = nodePool(config.Get().DefaultNodepoolName, "gpu", "a100")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must not set"))
		})

		It("rejects a non-default nodepool with an empty labelKey", func() {
			v := validatorWith()
			newPool = nodePool("pool-a", "", "a100")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("non-empty"))
		})

		It("rejects a non-default nodepool with an empty labelValue", func() {
			v := validatorWith()
			newPool = nodePool("pool-a", "gpu", "")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("non-empty"))
		})

		It("rejects a non-default nodepool with an empty labelKey and labelValue", func() {
			v := validatorWith()
			newPool = nodePool("pool-a", "", "")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("non-empty"))
		})

		It("rejects when it collides with any of several existing nodepools", func() {
			v := validatorWith(
				nodePool("pool-a", "gpu", "a100"),
				nodePool("pool-b", "gpu", "h100"),
				nodePool("pool-c", "region", "us-east"),
			)
			newPool = nodePool("pool-d", "gpu", "h100")

			_, err := v.ValidateCreate(ctx, newPool)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pool-b"))
		})
	})

	Describe("ValidateUpdate", func() {
		// ValidateUpdate accepts everything: the CRD enforces that the pair is
		// immutable, so there is nothing left for the webhook to reject.
		It("allows a no-op update that keeps a non-default pool's pair", func() {
			existing := nodePool("pool-a", "gpu", "a100")
			v := validatorWith(existing)
			updated := nodePool("pool-a", "gpu", "a100")

			_, err := v.ValidateUpdate(ctx, existing, updated)
			Expect(err).ToNot(HaveOccurred())
		})

		It("allows a no-op update of the default nodepool's empty pair", func() {
			existing := nodePool(config.Get().DefaultNodepoolName, "", "")
			v := validatorWith(existing)
			updated := nodePool(config.Get().DefaultNodepoolName, "", "")

			_, err := v.ValidateUpdate(ctx, existing, updated)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("ValidateDelete", func() {
		// This test is to verify the deletion is not blocked even though before the deletion, the nodepool still
		// exists and the labelKey/labelValue pair is not unique.
		It("does not block deletion of a non-default nodepool", func() {
			v := validatorWith(nodePool("pool-a", "gpu", "a100"))

			_, err := v.ValidateDelete(ctx, nodePool("pool-a", "gpu", "a100"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("refuses to delete the default nodepool", func() {
			defaultName := config.Get().DefaultNodepoolName
			v := validatorWith(nodePool(defaultName, "", ""))

			_, err := v.ValidateDelete(ctx, nodePool(defaultName, "", ""))
			Expect(err).To(MatchError(ContainSubstring(defaultName)))
		})

		It("follows the configured default nodepool name", func() {
			DeferCleanup(config.SetForTest(&config.NodePoolControllerConfig{DefaultNodepoolName: "all-nodes"}))
			v := validatorWith()

			_, err := v.ValidateDelete(ctx, nodePool("all-nodes", "", ""))
			Expect(err).To(HaveOccurred())

			_, err = v.ValidateDelete(ctx, nodePool("default", "gpu", "a100"))
			Expect(err).ToNot(HaveOccurred(), "the name is configurable, not hardcoded")
		})
	})

	Describe("ValidateCreate partitionLabelValue rules", func() {
		Context("when no shard has the nodepool's partitionLabelValue", func() {
			It("allows the nodepool", func() {
				v := validatorWith(shard("other", "other"))

				_, err := v.ValidateCreate(ctx, nodePool("pool-a", "gpu", "a100"))
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects the nodepool when another partitionLabelValue's shard already has its name", func() {
				v := validatorWith(shard("pool-a", "legacy"))

				_, err := v.ValidateCreate(ctx, nodePool("pool-a", "gpu", "a100"))
				Expect(err).To(MatchError(And(ContainSubstring(`"pool-a"`), ContainSubstring(`"legacy"`))))
			})
		})

		Context("when exactly one un-owned, unlabelled shard has it", func() {
			It("allows the nodepool, leaving the shard to the migration hook", func() {
				v := validatorWith(shard("legacy-a", "pool-a"))

				_, err := v.ValidateCreate(ctx, nodePool("pool-a", "gpu", "a100"))
				Expect(err).ToNot(HaveOccurred())
			})

			It("allows the default nodepool over the default shard", func() {
				v := validatorWith(shard("default", ""))

				_, err := v.ValidateCreate(ctx, nodePool(config.Get().DefaultNodepoolName, "", ""))
				Expect(err).ToNot(HaveOccurred())
			})

			It("ignores the unmanaged label on the default partitionLabelValue", func() {
				v := validatorWith(unmanaged(shard("default", "")))

				_, err := v.ValidateCreate(ctx, nodePool(config.Get().DefaultNodepoolName, "", ""))
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when a shard already serves it for someone else", func() {
			It("rejects the nodepool when a nodepool owns the shard", func() {
				v := validatorWith(ownedBy(shard("old-shard", "pool-a"), "old-pool"))

				_, err := v.ValidateCreate(ctx, nodePool("pool-a", "gpu", "a100"))
				Expect(err).To(MatchError(And(
					ContainSubstring(`"old-shard"`), ContainSubstring(`"old-pool"`), ContainSubstring(`"pool-a"`))))
			})

			It("rejects the nodepool when the shard is labelled unmanaged", func() {
				v := validatorWith(unmanaged(shard("legacy-a", "pool-a")))

				_, err := v.ValidateCreate(ctx, nodePool("pool-a", "gpu", "a100"))
				Expect(err).To(MatchError(And(
					ContainSubstring("legacy-a"), ContainSubstring(`"pool-a"`),
					ContainSubstring(unmanaged_shards.IgnoreShardLabelKey))))
			})

			It("rejects the nodepool when more than one shard has it", func() {
				v := validatorWith(shard("legacy-a", "pool-a"), shard("legacy-b", "pool-a"))

				_, err := v.ValidateCreate(ctx, nodePool("pool-a", "gpu", "a100"))
				Expect(err).To(MatchError(And(
					ContainSubstring("legacy-a"), ContainSubstring("legacy-b"), ContainSubstring(`"pool-a"`))))
			})
		})

		It("rejects the nodepool when the shards cannot be listed", func() {
			c := webhookTestClient(scheme).WithInterceptorFuncs(failingShardList()).Build()
			v := &nodePoolValidator{client: c}

			_, err := v.ValidateCreate(ctx, nodePool("pool-a", "gpu", "a100"))
			Expect(err).To(MatchError(errListFailed))
		})
	})
})
