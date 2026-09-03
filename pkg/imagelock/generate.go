// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package imagelock produces the digest-pinned image list an air-gapped install of
// a KAI Resource Management release needs.
//
// It renders the chart once per profile, collects every container image the install
// would run - including the ones the KRM and KAI operators create from a Config
// custom resource rather than from a template - resolves each tag this repository
// publishes to its digest on every requested platform, and writes one ImageLock
// document per profile and platform.
//
// The bundled kai-scheduler subchart's images are recognised and left out: that
// project publishes and pins them itself. See cmd/imagelock/README.md.
package imagelock

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// releaseVersion is what a published tag looks like. It exists to reject "latest"
// and other moving references, which would pin a lock to whatever happened to be
// tagged at generation time.
var releaseVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+`)

// Options describes one generation run.
type Options struct {
	// Chart is the path to the Helm chart to render.
	Chart string
	// Version is the release tag. It is both the tag the chart's images are
	// rendered with and the version the lock records, so it has to be the tag that
	// was actually published.
	Version string
	// Registry is where this release publishes. Everything under it is locked;
	// values.yaml still holds the local build default at this point, so it is
	// passed in rather than read from the chart.
	Registry  string
	Platforms []Platform
	Profiles  []Profile
	OutDir    string
	// HelmBin is the helm binary to render with. Empty means "helm".
	HelmBin string
	// VerifyOnly renders and classifies without reaching a registry or writing a
	// file, which is what runs on a pull request.
	VerifyOnly bool
}

// Validate reports whether these options describe a lock that can be generated.
func (o Options) Validate() error {
	if strings.TrimSpace(o.Registry) == "" {
		return errors.New("a registry is required")
	}
	if len(o.Platforms) == 0 {
		return errors.New("at least one platform is required")
	}
	if len(o.Profiles) == 0 {
		return errors.New("at least one profile is required")
	}
	if o.VerifyOnly {
		// Verification only classifies what the chart renders, so the release
		// version a lock file is named after is not needed.
		return nil
	}
	if o.Version == "" {
		return errors.New("a version is required")
	}
	if !releaseVersion.MatchString(o.Version) {
		return fmt.Errorf("version %q is not a release version like v1.2.3", o.Version)
	}
	return nil
}

// ProfileResult is what one profile contributed to a run.
type ProfileResult struct {
	Profile Profile
	// Locked names the images this release locks, in the order the lock lists them.
	Locked []string
	// Skipped counts the images recognised as another project's to lock.
	Skipped int
}

// Result is what a run produced. Reporting is left to the caller so this package
// writes nothing but the locks themselves.
type Result struct {
	Profiles []ProfileResult
	// Written lists the lock files, and is empty when Options.VerifyOnly is set.
	Written []string
}

// Generate renders, classifies, resolves and writes. It writes nothing until every
// digest is in hand, so a registry failure leaves no partial set of locks behind.
func Generate(ctx context.Context, opts Options) (Result, error) {
	if err := opts.Validate(); err != nil {
		return Result{}, err
	}

	var result Result
	sets := make([]lockedImages, 0, len(opts.Profiles))
	catalog := newImageCatalog(opts.Registry)
	for _, prof := range opts.Profiles {
		manifest, err := renderChart(ctx, opts, prof)
		if err != nil {
			return Result{}, fmt.Errorf("render the %s profile: %w", prof, err)
		}
		images, skipped, err := imagesFromManifest(manifest, catalog)
		if err != nil {
			return Result{}, fmt.Errorf("the %s profile: %w", prof, err)
		}
		if len(images) == 0 {
			return Result{}, fmt.Errorf("the %s profile renders none of this repository's images", prof)
		}
		sets = append(sets, lockedImages{Profile: prof, images: images, skipped: skipped})

		locked := make([]string, 0, len(images))
		for _, image := range images {
			locked = append(locked, image.name)
		}
		result.Profiles = append(result.Profiles, ProfileResult{Profile: prof, Locked: locked, Skipped: skipped})
	}

	if opts.VerifyOnly {
		return result, nil
	}

	registry := newResolver()
	for i := range sets {
		sets[i].digests = map[string]*resolved{}
		for _, image := range sets[i].images {
			digests, err := registry.resolve(ctx, image.reference(), opts.Platforms)
			if err != nil {
				return Result{}, fmt.Errorf("resolve %s: %w", image.reference(), err)
			}
			sets[i].digests[image.reference()] = digests
		}
	}

	locks := make([]imageLock, 0, len(sets)*len(opts.Platforms))
	for _, set := range sets {
		for _, plat := range opts.Platforms {
			locks = append(locks, buildLock(opts.Version, plat, set))
		}
	}

	written, err := writeLocks(opts.OutDir, locks)
	if err != nil {
		return Result{}, err
	}
	result.Written = written
	return result, nil
}

// DefaultPlatforms are the platforms every release publishes images for.
func DefaultPlatforms() []Platform {
	return []Platform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64"}}
}

// DefaultProfiles are the build variants every release publishes.
func DefaultProfiles() []Profile { return []Profile{ProfileStandard, ProfileFIPS} }

func ParsePlatforms(entries []string) ([]Platform, error) {
	platforms := make([]Platform, 0, len(entries))
	for _, entry := range entries {
		operatingSystem, architecture, ok := strings.Cut(entry, "/")
		if !ok || operatingSystem == "" || architecture == "" || strings.Contains(architecture, "/") {
			return nil, fmt.Errorf("bad platform %q, want os/arch", entry)
		}
		platforms = append(platforms, Platform{OS: operatingSystem, Architecture: architecture})
	}
	if len(platforms) == 0 {
		return nil, errors.New("no platforms given")
	}
	return platforms, nil
}

func ParseProfiles(entries []string) ([]Profile, error) {
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		switch Profile(entry) {
		case ProfileStandard:
			profiles = append(profiles, ProfileStandard)
		case ProfileFIPS:
			profiles = append(profiles, ProfileFIPS)
		default:
			return nil, fmt.Errorf("unknown profile %q, want %s or %s", entry, ProfileStandard, ProfileFIPS)
		}
	}
	if len(profiles) == 0 {
		return nil, errors.New("no profiles given")
	}
	return profiles, nil
}
