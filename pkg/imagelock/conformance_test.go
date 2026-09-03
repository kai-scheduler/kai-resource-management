// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package imagelock

import (
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// The locks this package writes are read back by the tooling that composes a
// platform-wide artifact lock out of every project's. That reader validates what it
// parses and rejects the whole set on a single bad entry, so the rules it enforces
// are restated here: they are a contract with another repository, and nothing in
// this one would otherwise notice a lock drifting out of shape.
//
// Kept deliberately close to the reader's own checks:
//   - apiVersion and kind must match exactly
//   - metadata name and version are required, and "latest" is refused
//   - the profile is one of standard or fips
//   - platform os and architecture are required
//   - there is at least one image
//   - image names are unique within a lock
//   - indexDigest is a sha256 digest - not optional
//   - image is repository@sha256:...
func TestLockSatisfiesTheComposerContract(t *testing.T) {
	digestPattern := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	for _, plat := range DefaultPlatforms() {
		lock := buildLock("v1.2.3", plat, testLockedImages())

		// Round-trip through YAML: the contract applies to the bytes on disk, not
		// to the struct that produced them.
		encoded, err := yaml.Marshal(lock)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got imageLock
		if err := yaml.Unmarshal(encoded, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if got.APIVersion != "artifacts.run.ai/v1alpha1" || got.Kind != "ImageLock" {
			t.Errorf("got %s %s, want artifacts.run.ai/v1alpha1 ImageLock", got.APIVersion, got.Kind)
		}
		if got.Metadata.Name == "" || got.Metadata.Version == "" {
			t.Errorf("name and version are required, got %+v", got.Metadata)
		}
		if got.Metadata.Version == "latest" {
			t.Error("a moving version is refused by the composer")
		}
		if got.Spec.Profile != ProfileStandard && got.Spec.Profile != ProfileFIPS {
			t.Errorf("profile %q is not one the composer accepts", got.Spec.Profile)
		}
		if got.Spec.Platform.OS == "" || got.Spec.Platform.Architecture == "" {
			t.Errorf("platform is incomplete: %+v", got.Spec.Platform)
		}
		if len(got.Spec.Images) == 0 {
			t.Fatal("a lock with no images is rejected")
		}

		names := map[string]struct{}{}
		for _, image := range got.Spec.Images {
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

// The composer refuses a set in which two locks give one source different digests,
// and it keys images on name plus source. Both platforms therefore have to agree on
// everything except the platform digest itself.
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
