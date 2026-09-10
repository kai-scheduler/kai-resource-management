// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	ggcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// testRegistry serves an in-memory registry over plain http: go-containerregistry
// speaks http to 127.0.0.1, so references resolve without a certificate.
func testRegistry(t *testing.T) string {
	t.Helper()
	// Discard the per-request log so a failing test shows only its own output.
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse the test registry URL: %v", err)
	}
	return parsed.Host
}

// pushIndex gives each platform different content, so the digests differ the way
// a real buildx push does.
func pushIndex(t *testing.T, ref string, platforms ...platform) {
	t.Helper()
	index := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	for i, plat := range platforms {
		image, err := random.Image(int64(64+i), 1)
		if err != nil {
			t.Fatalf("build a test image: %v", err)
		}
		index = mutate.AppendManifests(index, mutate.IndexAddendum{
			Add: image,
			Descriptor: ggcrv1.Descriptor{
				Platform: &ggcrv1.Platform{OS: plat.OS, Architecture: plat.Architecture},
			},
		})
	}

	reference, err := name.ParseReference(ref)
	if err != nil {
		t.Fatalf("parse %q: %v", ref, err)
	}
	if err := remote.WriteIndex(reference, index); err != nil {
		t.Fatalf("push %q: %v", ref, err)
	}
}

func TestResolveReadsAnIndexPerPlatform(t *testing.T) {
	ref := testRegistry(t) + "/krm/krm-operator:v1.2.3"
	pushIndex(t, ref, linuxAMD64, linuxARM64)

	got, err := resolve(context.Background(), ref, defaultPlatforms())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if !strings.HasPrefix(got.indexDigest, "sha256:") {
		t.Errorf("the index digest identifies the release, got %q", got.indexDigest)
	}
	amd64, arm64 := got.perPlatform[linuxAMD64], got.perPlatform[linuxARM64]
	if amd64 == "" || arm64 == "" {
		t.Fatalf("both platforms must resolve, got %+v", got.perPlatform)
	}
	if amd64 == arm64 {
		t.Error("the two platforms resolved to one digest; each has its own manifest")
	}
	for _, digest := range []string{amd64, arm64, got.indexDigest} {
		if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
			t.Errorf("%q is not a sha256 digest", digest)
		}
	}
}

func TestResolveRejectsAMissingPlatform(t *testing.T) {
	ref := testRegistry(t) + "/krm/krm-operator:v1.2.3"
	pushIndex(t, ref, linuxAMD64)

	_, err := resolve(context.Background(), ref, defaultPlatforms())
	if err == nil {
		t.Fatal("expected an index without linux/arm64 to be refused")
	}
	if !strings.Contains(err.Error(), "linux/arm64") {
		t.Errorf("the error should name the missing platform, got: %v", err)
	}
}

// A bare manifest carries no index digest, which the composer requires.
func TestResolveRejectsASingleManifest(t *testing.T) {
	ref := testRegistry(t) + "/krm/krm-operator:v1.2.3"
	image, err := random.Image(64, 1)
	if err != nil {
		t.Fatalf("build a test image: %v", err)
	}
	reference, err := name.ParseReference(ref)
	if err != nil {
		t.Fatalf("parse %q: %v", ref, err)
	}
	if err := remote.Write(reference, image); err != nil {
		t.Fatalf("push %q: %v", ref, err)
	}

	if _, err := resolve(context.Background(), ref, defaultPlatforms()); err == nil ||
		!strings.Contains(err.Error(), "single manifest") {
		t.Errorf("expected a bare manifest to be refused, got: %v", err)
	}
}

func TestResolveReportsAnUnknownTag(t *testing.T) {
	ref := testRegistry(t) + "/krm/krm-operator:v9.9.9"
	if _, err := resolve(context.Background(), ref, defaultPlatforms()); err == nil {
		t.Fatal("expected a tag that was never pushed to fail")
	}
}

func TestPlatformDigestsRejectsAnAmbiguousIndex(t *testing.T) {
	entry := ggcrv1.Descriptor{
		Platform: &ggcrv1.Platform{OS: "linux", Architecture: "amd64"},
	}
	_, err := platformDigests([]ggcrv1.Descriptor{entry, entry}, []platform{linuxAMD64})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("two manifests for one platform must be refused, got: %v", err)
	}
}
