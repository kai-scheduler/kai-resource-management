// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

var (
	linuxAMD64 = platform{OS: "linux", Architecture: "amd64"}
	linuxARM64 = platform{OS: "linux", Architecture: "arm64"}
)

func manifestDigest(fill byte) string {
	return "sha256:" + strings.Repeat(fmt.Sprintf("%02x", fill), 32)
}

func testLockedImages() lockedImages {
	return lockedImages{
		profile: profileStandard,
		images: []chartImage{
			{name: "scheduler", repo: "ghcr.io/kai/scheduler", tag: "v0.17.0"},
			{name: "krm-operator", repo: "ghcr.io/krm/krm-operator", tag: "v1.2.3"},
		},
		digests: map[string]*resolved{
			"ghcr.io/kai/scheduler:v0.17.0": {
				indexDigest: manifestDigest(0x11),
				perPlatform: map[platform]string{linuxAMD64: manifestDigest(0x22), linuxARM64: manifestDigest(0x33)},
			},
			"ghcr.io/krm/krm-operator:v1.2.3": {
				indexDigest: manifestDigest(0x66),
				perPlatform: map[platform]string{linuxAMD64: manifestDigest(0x44), linuxARM64: manifestDigest(0x55)},
			},
		},
	}
}

func TestBuildLock(t *testing.T) {
	lock := buildLock("v1.2.3", linuxAMD64, testLockedImages())

	if lock.APIVersion != lockAPIVersion || lock.Kind != lockKind {
		t.Errorf("got %s/%s, want %s/%s", lock.APIVersion, lock.Kind, lockAPIVersion, lockKind)
	}
	if lock.Metadata.Name != lockName || lock.Metadata.Version != "v1.2.3" {
		t.Errorf("metadata: got %+v", lock.Metadata)
	}
	if lock.Spec.Profile != profileStandard || lock.Spec.Platform != linuxAMD64 {
		t.Errorf("spec: got profile %q platform %s", lock.Spec.Profile, lock.Spec.Platform)
	}

	// Sorted by name, whatever order the chart rendered them in.
	want := []lockedImage{
		{
			Name:        "krm-operator",
			Image:       "ghcr.io/krm/krm-operator@" + manifestDigest(0x44),
			Source:      "ghcr.io/krm/krm-operator:v1.2.3",
			IndexDigest: manifestDigest(0x66),
		},
		{
			Name:        "scheduler",
			Image:       "ghcr.io/kai/scheduler@" + manifestDigest(0x22),
			Source:      "ghcr.io/kai/scheduler:v0.17.0",
			IndexDigest: manifestDigest(0x11),
		},
	}
	if len(lock.Spec.Images) != len(want) {
		t.Fatalf("got %d images, want %d", len(lock.Spec.Images), len(want))
	}
	for i := range want {
		if lock.Spec.Images[i] != want[i] {
			t.Errorf("image %d: got %+v, want %+v", i, lock.Spec.Images[i], want[i])
		}
	}
}

func TestLockFileName(t *testing.T) {
	lock := buildLock("v1.2.3", linuxARM64, testLockedImages())
	want := "imagelock-kai-resource-management-v1.2.3-standard-linux-arm64.yaml"
	if got := lockFileName(lock); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteLocks(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "locks")
	set := testLockedImages()
	locks := []imageLock{
		buildLock("v1.2.3", linuxAMD64, set),
		buildLock("v1.2.3", linuxARM64, set),
	}

	written, err := writeLocks(outDir, locks)
	if err != nil {
		t.Fatalf("writeLocks: %v", err)
	}
	if len(written) != len(locks) {
		t.Fatalf("got %d files, want %d", len(written), len(locks))
	}

	raw, err := os.ReadFile(written[0])
	if err != nil {
		t.Fatalf("read the lock back: %v", err)
	}
	var roundTripped imageLock
	if err := yaml.Unmarshal(raw, &roundTripped); err != nil {
		t.Fatalf("the lock is not valid YAML: %v", err)
	}
	if len(roundTripped.Spec.Images) != 2 || roundTripped.Spec.Images[0].Name != "krm-operator" {
		t.Errorf("round trip lost content: %+v", roundTripped.Spec)
	}
}

// The composer that reads these locks rejects the whole set on one bad entry, so
// its rules are restated here: nothing in this repository would otherwise notice a
// lock drifting out of shape.
func TestLockSatisfiesTheComposerContract(t *testing.T) {
	digestPattern := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	for _, plat := range defaultPlatforms() {
		lock := buildLock("v1.2.3", plat, testLockedImages())

		if lock.APIVersion != "artifacts.run.ai/v1alpha1" || lock.Kind != "ImageLock" {
			t.Errorf("got %s %s, want artifacts.run.ai/v1alpha1 ImageLock", lock.APIVersion, lock.Kind)
		}
		if lock.Metadata.Name == "" || lock.Metadata.Version == "" || lock.Metadata.Version == "latest" {
			t.Errorf("name and a non-moving version are required, got %+v", lock.Metadata)
		}
		if lock.Spec.Profile != profileStandard && lock.Spec.Profile != profileFIPS {
			t.Errorf("profile %q is not one the composer accepts", lock.Spec.Profile)
		}
		if lock.Spec.Platform.OS == "" || lock.Spec.Platform.Architecture == "" {
			t.Errorf("platform is incomplete: %+v", lock.Spec.Platform)
		}
		if len(lock.Spec.Images) == 0 {
			t.Fatal("a lock with no images is rejected")
		}

		names := map[string]struct{}{}
		for _, image := range lock.Spec.Images {
			if image.Name == "" || image.Source == "" {
				t.Errorf("name and source are required, got %+v", image)
			}
			if _, seen := names[image.Name]; seen {
				t.Errorf("duplicate image name %q", image.Name)
			}
			names[image.Name] = struct{}{}

			if !digestPattern.MatchString(image.IndexDigest) {
				t.Errorf("%s: indexDigest %q is not a sha256 digest", image.Name, image.IndexDigest)
			}
			at := strings.LastIndex(image.Image, "@")
			if at < 1 || !digestPattern.MatchString(image.Image[at+1:]) {
				t.Errorf("%s: image %q is not repository@sha256:...", image.Name, image.Image)
			}
		}
	}
}

// The composer decodes with KnownFields set, so an unknown key fails as surely as
// a missing one - and a misspelling would still round-trip cleanly through the
// struct that wrote it. So the keys are pinned against the encoded bytes.
func TestLockKeysMatchTheComposerSchema(t *testing.T) {
	wantKeys := map[string][]string{
		"":              {"apiVersion", "kind", "metadata", "spec"},
		"metadata":      {"name", "version"},
		"spec":          {"images", "platform", "profile"},
		"spec.platform": {"architecture", "os"},
		"spec.images[]": {"image", "indexDigest", "name", "source"},
	}

	encoded, err := yaml.Marshal(buildLock("v1.2.3", linuxAMD64, testLockedImages()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	spec, _ := document["spec"].(map[string]any)
	images, _ := spec["images"].([]any)
	if len(images) == 0 {
		t.Fatal("the lock encoded no images to check")
	}
	firstImage, _ := images[0].(map[string]any)

	got := map[string]map[string]any{
		"":              document,
		"metadata":      asMapping(document["metadata"]),
		"spec":          spec,
		"spec.platform": asMapping(spec["platform"]),
		"spec.images[]": firstImage,
	}
	for path, want := range wantKeys {
		if diff := keyDiff(got[path], want); diff != "" {
			if path == "" {
				path = "the lock document"
			}
			t.Errorf("%q: %s", path, diff)
		}
	}
}

// The composer keys images on name plus source and refuses two locks that give one
// source different digests, so only the platform digest may differ.
func TestLocksAgreeAcrossPlatformsExceptTheDigest(t *testing.T) {
	set := testLockedImages()
	amd64 := buildLock("v1.2.3", linuxAMD64, set)
	arm64 := buildLock("v1.2.3", linuxARM64, set)

	if len(amd64.Spec.Images) != len(arm64.Spec.Images) {
		t.Fatalf("platforms lock a different number of images: %d vs %d",
			len(amd64.Spec.Images), len(arm64.Spec.Images))
	}
	for i := range amd64.Spec.Images {
		a, b := amd64.Spec.Images[i], arm64.Spec.Images[i]
		if a.Name != b.Name || a.Source != b.Source || a.IndexDigest != b.IndexDigest {
			t.Errorf("%s: platforms disagree on more than the digest:\n  %+v\n  %+v", a.Name, a, b)
		}
		if a.Image == b.Image {
			t.Errorf("%s: both platforms locked the same digest", a.Name)
		}
	}
}

func asMapping(node any) map[string]any {
	mapping, _ := node.(map[string]any)
	return mapping
}

// keyDiff reports missing and unknown keys; either kind stops the composer.
func keyDiff(got map[string]any, want []string) string {
	known := map[string]struct{}{}
	var missing []string
	for _, key := range want {
		known[key] = struct{}{}
		if _, ok := got[key]; !ok {
			missing = append(missing, key)
		}
	}
	var unknown []string
	for key := range got {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)

	switch {
	case len(missing) > 0 && len(unknown) > 0:
		return fmt.Sprintf("missing %v, and %v is not a key the composer knows", missing, unknown)
	case len(missing) > 0:
		return fmt.Sprintf("missing %v", missing)
	case len(unknown) > 0:
		return fmt.Sprintf("%v is not a key the composer knows", unknown)
	}
	return ""
}
