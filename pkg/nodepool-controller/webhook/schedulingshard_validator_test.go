// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
	unmanaged_shards "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/unmanaged-shards"
)

var _ = Describe("SchedulingShard partitionLabelValue validation", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = webhookTestScheme()
		DeferCleanup(config.SetForTest(&config.NodePoolControllerConfig{DefaultNodepoolName: "default"}))
	})

	validatorWith := func(existing ...client.Object) *schedulingShardValidator {
		return &schedulingShardValidator{client: webhookTestClient(scheme, existing...).Build()}
	}

	Describe("ValidateCreate", func() {
		Context("when another shard already has the partitionLabelValue", func() {
			It("rejects the shard", func() {
				v := validatorWith(nodePool("pool-a", "gpu", "a100"), shard("first", "pool-a"))

				_, err := v.ValidateCreate(ctx, shard("second", "pool-a"))
				Expect(err).To(MatchError(And(
					ContainSubstring(`"second"`), ContainSubstring(`"first"`), ContainSubstring(`"pool-a"`))))
			})

			It("rejects the shard even when it is labelled unmanaged", func() {
				v := validatorWith(shard("first", "legacy"))

				_, err := v.ValidateCreate(ctx, unmanaged(shard("second", "legacy")))
				Expect(err).To(MatchError(And(ContainSubstring(`"first"`), ContainSubstring(`"legacy"`))))
			})

			It("rejects a second shard on the default partitionLabelValue", func() {
				v := validatorWith(nodePool("default", "", ""), shard("default", ""))

				_, err := v.ValidateCreate(ctx, shard("another-default", ""))
				Expect(err).To(MatchError(ContainSubstring(`"default"`)))
			})
		})

		Context("when no other shard has the partitionLabelValue", func() {
			It("allows a shard labelled unmanaged with no nodepool", func() {
				v := validatorWith()

				_, err := v.ValidateCreate(ctx, unmanaged(shard("legacy", "legacy")))
				Expect(err).ToNot(HaveOccurred())
			})

			It("allows a shard whose nodepool exists", func() {
				v := validatorWith(nodePool("pool-a", "gpu", "a100"))

				_, err := v.ValidateCreate(ctx, ownedBy(shard("pool-a", "pool-a"), "pool-a"))
				Expect(err).ToNot(HaveOccurred())
			})

			It("allows the default shard when the default nodepool exists", func() {
				v := validatorWith(nodePool("default", "", ""))

				_, err := v.ValidateCreate(ctx, shard("default", ""))
				Expect(err).ToNot(HaveOccurred())
			})

			It("rejects an unlabelled shard with no nodepool, naming the remedy", func() {
				v := validatorWith()

				_, err := v.ValidateCreate(ctx, shard("orphan", "pool-x"))
				Expect(err).To(MatchError(And(
					ContainSubstring(`"orphan"`), ContainSubstring(`"pool-x"`),
					ContainSubstring(unmanaged_shards.IgnoreShardLabelKey))))
			})

			It("rejects a default shard with no default nodepool, even when labelled unmanaged", func() {
				v := validatorWith()

				_, err := v.ValidateCreate(ctx, unmanaged(shard("default", "")))
				Expect(err).To(MatchError(SatisfyAll(
					ContainSubstring(`nodepool "default"`),
					Not(ContainSubstring(unmanaged_shards.IgnoreShardLabelKey)))),
					"the label has no effect on the default partitionLabelValue, so it must not be suggested")
			})
		})

		It("rejects the shard when the shards cannot be listed", func() {
			c := webhookTestClient(scheme).WithInterceptorFuncs(failingShardList()).Build()
			v := &schedulingShardValidator{client: c}

			_, err := v.ValidateCreate(ctx, unmanaged(shard("legacy", "legacy")))
			Expect(err).To(MatchError(ContainSubstring("could not list existing scheduling shards")))
		})
	})

	Describe("ValidateUpdate", func() {
		It("rejects a change of partitionLabelValue", func() {
			v := validatorWith()

			_, err := v.ValidateUpdate(ctx, shard("pool-a", "pool-a"), shard("pool-a", "pool-b"))
			Expect(err).To(MatchError(And(ContainSubstring(`"pool-a"`), ContainSubstring(`"pool-b"`))))
		})

		It("allows an update that keeps partitionLabelValue and the unmanaged label", func() {
			v := validatorWith()
			updated := unmanaged(shard("legacy", "legacy"))
			updated.Spec.Args = map[string]string{"verbosity": "4"}

			_, err := v.ValidateUpdate(ctx, unmanaged(shard("legacy", "legacy")), updated)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("on a non-default partitionLabelValue", func() {
			It("rejects adding the unmanaged label", func() {
				v := validatorWith()

				_, err := v.ValidateUpdate(ctx, shard("pool-a", "pool-a"), unmanaged(shard("pool-a", "pool-a")))
				Expect(err).To(MatchError(ContainSubstring(unmanaged_shards.IgnoreShardLabelKey)))
			})

			It("rejects removing the unmanaged label", func() {
				v := validatorWith()

				_, err := v.ValidateUpdate(ctx, unmanaged(shard("legacy", "legacy")), shard("legacy", "legacy"))
				Expect(err).To(MatchError(ContainSubstring(unmanaged_shards.IgnoreShardLabelKey)))
			})

			It("rejects changing the unmanaged label's value", func() {
				v := validatorWith()
				updated := unmanaged(shard("legacy", "legacy"))
				updated.Labels[unmanaged_shards.IgnoreShardLabelKey] = "false"

				_, err := v.ValidateUpdate(ctx, unmanaged(shard("legacy", "legacy")), updated)
				Expect(err).To(MatchError(ContainSubstring(unmanaged_shards.IgnoreShardLabelKey)))
			})
		})

		It("allows the migration hook to strip the label from the default shard", func() {
			v := validatorWith()

			_, err := v.ValidateUpdate(ctx, unmanaged(shard("default", "")), shard("default", ""))
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("ValidateDelete", func() {
		It("allows deletion", func() {
			v := validatorWith()

			_, err := v.ValidateDelete(ctx, shard("pool-a", "pool-a"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
