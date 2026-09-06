// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// resolveTimeout bounds one image's round trip, retries included.
const resolveTimeout = 2 * time.Minute

// resolved is one image's digests: its multi-arch index, and the manifest each
// platform pulls out of that index.
type resolved struct {
	indexDigest string
	perPlatform map[platform]string
}

// resolve reads a tag's index and picks each platform's manifest digest out of it.
// This repository's images are private until a release goes out, so CI runs
// `docker login` first and the default keychain reads what it wrote.
func resolve(ctx context.Context, ref string, platforms []platform) (*resolved, error) {
	reference, err := name.ParseReference(ref)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	descriptor, err := remote.Get(reference,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, err
	}

	if !descriptor.MediaType.IsIndex() {
		// The composer requires an index digest per entry, and every image is
		// pushed with buildx for both platforms. A bare manifest is a build
		// problem, not something to paper over with the manifest's own digest.
		return nil, fmt.Errorf("%s is published as a single manifest rather than a multi-arch index", ref)
	}

	index, err := descriptor.ImageIndex()
	if err != nil {
		return nil, err
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		return nil, err
	}
	perPlatform, err := platformDigests(manifest.Manifests, platforms)
	if err != nil {
		return nil, err
	}
	return &resolved{indexDigest: descriptor.Digest.String(), perPlatform: perPlatform}, nil
}

// platformDigests errors on a platform that is missing or matches twice: the lock
// must name exactly one digest per platform or a mirror is ambiguous.
func platformDigests(entries []ggcrv1.Descriptor, platforms []platform) (map[platform]string, error) {
	digests := make(map[platform]string, len(platforms))
	for _, want := range platforms {
		var matches []string
		for _, entry := range entries {
			if entry.Platform == nil {
				continue
			}
			if entry.Platform.OS == want.OS && entry.Platform.Architecture == want.Architecture {
				matches = append(matches, entry.Digest.String())
			}
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("no %s manifest in the index", want)
		case 1:
			digests[want] = matches[0]
		default:
			return nil, fmt.Errorf("ambiguous %s: %d manifests match", want, len(matches))
		}
	}
	return digests, nil
}
