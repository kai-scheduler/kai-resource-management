// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package imagelock

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"sigs.k8s.io/yaml"
)

const (
	lockAPIVersion = "artifacts.run.ai/v1alpha1"
	lockKind       = "ImageLock"
	lockName       = "kai-resource-management"
)

// imageLock is one release's images for one Profile on one Platform, every tag
// resolved to the digest it pointed at when the release was published.
type imageLock struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Metadata   lockMetadata `json:"metadata"`
	Spec       lockSpec     `json:"spec"`
}

type lockMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type lockSpec struct {
	Profile  Profile       `json:"profile"`
	Platform Platform      `json:"platform"`
	Images   []lockedImage `json:"images"`
}

type lockedImage struct {
	Name string `json:"name"`
	// Image is what to mirror: the repository at this Platform's manifest digest.
	Image string `json:"image"`
	// Source is the tag the chart pulls, and so the tag the mirrored digest has to
	// be published under in the private registry.
	Source string `json:"source"`
	// IndexDigest identifies the multi-arch index the platform manifest came from,
	// so the same release can be recognised across platforms. Always set: the
	// tooling that composes these locks requires it.
	IndexDigest string `json:"indexDigest"`
}

// lockedImages is a resolved image set for one Profile, keyed the way buildLock
// consumes it.
type lockedImages struct {
	Profile Profile
	images  []chartImage
	digests map[string]*resolved // keyed by image reference
	// skipped counts the images another project locks, reported so a run says out
	// loud that it saw them and left them alone.
	skipped int
}

func buildLock(version string, plat Platform, set lockedImages) imageLock {
	lock := imageLock{
		APIVersion: lockAPIVersion,
		Kind:       lockKind,
		Metadata:   lockMetadata{Name: lockName, Version: version},
		Spec:       lockSpec{Profile: set.Profile, Platform: plat},
	}
	for _, image := range set.images {
		digests := set.digests[image.reference()]
		lock.Spec.Images = append(lock.Spec.Images, lockedImage{
			Name:        image.name,
			Image:       image.repo + "@" + digests.perPlatform[plat],
			Source:      image.reference(),
			IndexDigest: digests.indexDigest,
		})
	}
	sort.Slice(lock.Spec.Images, func(i, j int) bool {
		return lock.Spec.Images[i].Name < lock.Spec.Images[j].Name
	})
	return lock
}

func lockFileName(lock imageLock) string {
	return fmt.Sprintf("imagelock-%s-%s-%s-%s-%s.yaml",
		lock.Metadata.Name, lock.Metadata.Version, lock.Spec.Profile,
		lock.Spec.Platform.OS, lock.Spec.Platform.Architecture)
}

// writeLocks marshals every lock before it writes any of them, so a failure part
// way through leaves no half-written set behind for a release to pick up.
func writeLocks(outDir string, locks []imageLock) ([]string, error) {
	documents := make([][]byte, len(locks))
	for i, lock := range locks {
		document, err := yaml.Marshal(lock)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", lockFileName(lock), err)
		}
		documents[i] = document
	}

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", outDir, err)
	}

	written := make([]string, 0, len(locks))
	for i, lock := range locks {
		path := filepath.Join(outDir, lockFileName(lock))
		if err := os.WriteFile(path, documents[i], 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}
