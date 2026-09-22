// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"os"
	"path/filepath"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const krmConfigManifest = `apiVersion: kai.resources/v1alpha1
kind: KRMConfig
metadata:
  name: krm-config
spec: {}
`

var _ = Describe("ApplyConfig", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		dir        string
	)

	writeManifest := func(content string) string {
		path := filepath.Join(dir, "krm-config.yaml")
		Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
		return path
	}

	BeforeEach(func() {
		ctx = context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(kaires.AddToScheme(scheme))
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		dir = GinkgoT().TempDir()
	})

	It("applies the manifest", func() {
		Expect(ApplyConfig(ctx, fakeClient, writeManifest(krmConfigManifest))).To(Succeed())

		applied := &kaires.KRMConfig{}
		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "krm-config"}, applied)).To(Succeed())
	})

	It("applies twice without error, so a re-run of the hook is safe", func() {
		path := writeManifest(krmConfigManifest)
		Expect(ApplyConfig(ctx, fakeClient, path)).To(Succeed())
		Expect(ApplyConfig(ctx, fakeClient, path)).To(Succeed())
	})

	It("rejects a manifest of another kind", func() {
		path := writeManifest("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: krm-config\n")

		err := ApplyConfig(ctx, fakeClient, path)
		Expect(err).To(MatchError(ContainSubstring("expected \"KRMConfig\"")))
	})

	It("rejects a manifest without a name", func() {
		path := writeManifest("apiVersion: kai.resources/v1alpha1\nkind: KRMConfig\nspec: {}\n")

		err := ApplyConfig(ctx, fakeClient, path)
		Expect(err).To(MatchError(ContainSubstring("no metadata.name")))
	})

	It("reports a missing manifest file", func() {
		err := ApplyConfig(ctx, fakeClient, filepath.Join(dir, "absent.yaml"))
		Expect(err).To(MatchError(ContainSubstring("failed to read manifest")))
	})
})
