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

// resolveTimeout bounds one image's round trip to the registry. remote retries a
// transient failure underneath this, so it covers the retries too.
const resolveTimeout = 2 * time.Minute

// resolved is one image's digests: the multi-arch index it was published as, and
// the manifest each requested platform pulls out of that index.
type resolved struct {
	indexDigest string
	perPlatform map[platform]string
}

// resolve reads a tag's index and picks each platform's manifest digest out of it.
// The published images need no credentials, but this repository's own are private
// until a release goes out; CI runs `docker login` first and the default keychain
// reads what it wrote.
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
		// Every image is published with buildx for both platforms, and the lock
		// records an index digest for each one, which the tooling that composes
		// these locks requires. A tag that is a bare manifest is a build problem,
		// not something to paper over with the manifest's own digest.
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

// platformDigests picks each requested platform's manifest out of an index. A
// platform that is missing, or that matches more than once, is an error: the lock
// must name exactly one digest per platform or an air-gapped mirror is ambiguous.
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
