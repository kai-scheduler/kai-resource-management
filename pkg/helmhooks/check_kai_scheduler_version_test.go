// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// releaseSecret is the Secret Helm writes for a release revision: its record as JSON,
// gzipped, then base64-encoded.
func releaseSecret(namespace, name, chartName, chartVersion, status string) *corev1.Secret {
	record, err := json.Marshal(map[string]any{
		"name":      name,
		"namespace": namespace,
		"chart":     map[string]any{"metadata": map[string]any{"name": chartName, "version": chartVersion}},
	})
	Expect(err).ToNot(HaveOccurred())
	var zipped bytes.Buffer
	writer := gzip.NewWriter(&zipped)
	_, err = writer.Write(record)
	Expect(err).ToNot(HaveOccurred())
	Expect(writer.Close()).To(Succeed())

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "sh.helm.release.v1." + name + ".v1",
			Labels:    map[string]string{"owner": "helm", "status": status, "name": name},
		},
		Type: helmReleaseSecretType,
		Data: map[string][]byte{
			helmReleaseDataKey: []byte(base64.StdEncoding.EncodeToString(zipped.Bytes())),
		},
	}
}

func newSecretsClientBuilder() *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme)
}

var _ = Describe("DetectKAISchedulerVersion", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	detect := func(objects ...client.Object) (string, string, error) {
		return DetectKAISchedulerVersion(ctx, newSecretsClientBuilder().WithObjects(objects...).Build())
	}

	It("reads the chart version of the deployed release", func() {
		version, source, err := detect(
			releaseSecret("kai-scheduler", "kai", "kai-scheduler", "v0.18.2", "deployed"))

		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal("v0.18.2"))
		Expect(source).To(Equal("Helm release kai-scheduler/kai"))
	})

	It("ignores other charts, superseded revisions, Secrets of another type and undecodable ones", func() {
		opaque := releaseSecret("kai-scheduler", "opaque", "kai-scheduler", "v0.17.0", "deployed")
		opaque.Type = corev1.SecretTypeOpaque
		broken := releaseSecret("elsewhere", "broken", "kai-scheduler", "v0.17.0", "deployed")
		broken.Data[helmReleaseDataKey] = []byte("not base64!")

		version, _, err := detect(
			releaseSecret("kai-scheduler", "kai", "kai-scheduler", "v0.18.2", "deployed"),
			releaseSecret("kai-scheduler", "old", "kai-scheduler", "v0.17.0", "superseded"),
			releaseSecret("monitoring", "prometheus", "kube-prometheus-stack", "65.0.0", "deployed"),
			opaque, broken)

		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal("v0.18.2"))
	})

	It("fails when there is no release", func() {
		_, _, err := detect()

		Expect(err).To(MatchError(ContainSubstring("no deployed Helm release of the kai-scheduler chart")))
	})

	It("fails, naming each, when there is more than one release", func() {
		_, _, err := detect(
			releaseSecret("kai-scheduler", "kai", "kai-scheduler", "v0.18.2", "deployed"),
			releaseSecret("other", "kai-two", "kai-scheduler", "v0.19.0", "deployed"))

		Expect(err).To(MatchError(And(ContainSubstring("kai-scheduler/kai"), ContainSubstring("other/kai-two"))))
	})

	It("reads every page of the list", func() {
		pages := map[string]corev1.SecretList{
			"":       {ListMeta: metav1.ListMeta{Continue: "page-2"}},
			"page-2": {Items: []corev1.Secret{*releaseSecret("kai", "kai", "kai-scheduler", "v0.18.2", "deployed")}},
		}
		reader := newSecretsClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				listOptions := &client.ListOptions{}
				listOptions.ApplyOptions(opts)
				*list.(*corev1.SecretList) = pages[listOptions.Continue]
				return nil
			},
		}).Build()

		version, _, err := DetectKAISchedulerVersion(ctx, reader)

		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal("v0.18.2"))
	})

	It("returns an error when the Secrets cannot be listed", func() {
		reader := newSecretsClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return errors.New("forbidden")
			},
		}).Build()

		_, _, err := DetectKAISchedulerVersion(ctx, reader)

		Expect(err).To(MatchError(ContainSubstring("forbidden")))
	})
})

var _ = Describe("CheckKAISchedulerVersion", func() {
	const (
		minimum = "v0.18.0"
		source  = "Helm release kai-scheduler/kai"
	)
	ctx := context.Background()

	DescribeTable("accepts a supported version",
		func(installed string) {
			Expect(CheckKAISchedulerVersion(ctx, installed, source, minimum)).To(Succeed())
		},
		Entry("the minimum itself", "v0.18.0"),
		Entry("a newer patch", "v0.18.2"),
		Entry("a newer minor", "v0.19.0"),
		Entry("a release candidate of a newer minor", "v0.19.0-rc.1"),
		Entry("a main branch build", "0.0.0-1db3d56"),
	)

	DescribeTable("rejects an older version, naming it, its source and the minimum",
		func(installed string) {
			Expect(CheckKAISchedulerVersion(ctx, installed, source, minimum)).To(MatchError(And(
				ContainSubstring(installed),
				ContainSubstring(source),
				ContainSubstring("older than the minimum supported "+minimum))))
		},
		Entry("an older minor", "v0.17.3"),
		Entry("a release candidate of the minimum", "v0.18.0-rc.1"),
	)

	It("rejects a version that is not a version", func() {
		Expect(CheckKAISchedulerVersion(ctx, "latest", source, minimum)).
			To(MatchError(ContainSubstring(`"latest"`)))
		Expect(CheckKAISchedulerVersion(ctx, "v0.18.2", source, "latest")).
			To(MatchError(ContainSubstring(`"latest"`)))
	})
})
