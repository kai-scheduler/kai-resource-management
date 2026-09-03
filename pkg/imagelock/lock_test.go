// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package imagelock

import (
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"
)

func testLockedImages() lockedImages {
	return lockedImages{
		Profile: ProfileStandard,
		images: []chartImage{
			{name: "scheduler", repo: "ghcr.io/kai/scheduler", tag: "v0.17.0"},
			{name: "krm-operator", repo: "ghcr.io/krm/krm-operator", tag: "v1.2.3"},
		},
		digests: map[string]*resolved{
			"ghcr.io/kai/scheduler:v0.17.0": {
				indexDigest: manifestDigest(0x11),
				perPlatform: map[Platform]string{linuxAMD64: manifestDigest(0x22), linuxARM64: manifestDigest(0x33)},
			},
			"ghcr.io/krm/krm-operator:v1.2.3": {
				indexDigest: manifestDigest(0x66),
				perPlatform: map[Platform]string{linuxAMD64: manifestDigest(0x44), linuxARM64: manifestDigest(0x55)},
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
	if lock.Spec.Profile != ProfileStandard || lock.Spec.Platform != linuxAMD64 {
		t.Errorf("spec: got Profile %q Platform %s", lock.Spec.Profile, lock.Spec.Platform)
	}

	// Sorted by name, so krm-operator comes before scheduler whatever order the
	// chart rendered them in.
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

func TestBuildLockPicksThePlatformDigest(t *testing.T) {
	set := testLockedImages()
	amd64 := buildLock("v1.2.3", linuxAMD64, set)
	arm64 := buildLock("v1.2.3", linuxARM64, set)

	for i := range amd64.Spec.Images {
		if amd64.Spec.Images[i].Image == arm64.Spec.Images[i].Image {
			t.Errorf("%s: both platforms locked the same digest", amd64.Spec.Images[i].Name)
		}
		if amd64.Spec.Images[i].IndexDigest != arm64.Spec.Images[i].IndexDigest {
			t.Errorf("%s: the index digest identifies the release, so it must not differ by Platform",
				amd64.Spec.Images[i].Name)
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
	if roundTripped.Spec.Images[0].IndexDigest == "" {
		t.Error("every entry must carry an indexDigest; the consumer rejects one without")
	}
}
