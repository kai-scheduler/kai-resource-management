// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	lockAPIVersion = "artifacts.run.ai/v1alpha1"
	lockKind       = "ImageLock"
	lockName       = "kai-resource-management"
)

// profile is a build variant of the same release. global.fipsMode appends "-fips"
// to every image tag, so each profile resolves to different digests.
type profile string

const (
	profileStandard profile = "standard"
	profileFIPS     profile = "fips"
)

type platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

func (p platform) String() string { return p.OS + "/" + p.Architecture }

func defaultPlatforms() []platform {
	return []platform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64"}}
}

func defaultProfiles() []profile { return []profile{profileStandard, profileFIPS} }

func parsePlatforms(entries []string) ([]platform, error) {
	platforms := make([]platform, 0, len(entries))
	for _, entry := range entries {
		operatingSystem, architecture, ok := strings.Cut(entry, "/")
		if !ok || operatingSystem == "" || architecture == "" || strings.Contains(architecture, "/") {
			return nil, fmt.Errorf("bad platform %q, want os/arch", entry)
		}
		platforms = append(platforms, platform{OS: operatingSystem, Architecture: architecture})
	}
	if len(platforms) == 0 {
		return nil, errors.New("no platforms given")
	}
	return platforms, nil
}

func parseProfiles(entries []string) ([]profile, error) {
	profiles := make([]profile, 0, len(entries))
	for _, entry := range entries {
		switch profile(entry) {
		case profileStandard:
			profiles = append(profiles, profileStandard)
		case profileFIPS:
			profiles = append(profiles, profileFIPS)
		default:
			return nil, fmt.Errorf("unknown profile %q, want %s or %s", entry, profileStandard, profileFIPS)
		}
	}
	if len(profiles) == 0 {
		return nil, errors.New("no profiles given")
	}
	return profiles, nil
}

// imageLock is one profile on one platform, every tag resolved to the digest it
// pointed at when the release was published.
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
	Profile  profile       `json:"profile"`
	Platform platform      `json:"platform"`
	Images   []lockedImage `json:"images"`
}

type lockedImage struct {
	Name string `json:"name"`
	// Image is what to mirror: the repository at this platform's manifest digest.
	Image string `json:"image"`
	// Source is the tag the chart pulls, and so the tag the mirrored digest has to
	// be republished under.
	Source string `json:"source"`
	// IndexDigest identifies the index the platform manifest came from, so one
	// release is recognisable across platforms. The composer requires it.
	IndexDigest string `json:"indexDigest"`
}

// lockedImages is a resolved image set for one profile.
type lockedImages struct {
	profile profile
	images  []chartImage
	digests map[string]*resolved // keyed by image reference
}

func buildLock(version string, plat platform, set lockedImages) imageLock {
	lock := imageLock{
		APIVersion: lockAPIVersion,
		Kind:       lockKind,
		Metadata:   lockMetadata{Name: lockName, Version: version},
		Spec:       lockSpec{Profile: set.profile, Platform: plat},
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

// writeLocks marshals every lock before writing any, so a failure part way
// through leaves no half-written set behind.
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
