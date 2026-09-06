// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Command imagelock writes the digest-pinned image list an air-gapped install of a
// KAI Resource Management release needs.
//
// It renders the chart once per profile, collects every container image the install
// would run - including the ones the KRM and KAI operators create from a Config
// custom resource rather than from a template - resolves each tag this repository
// publishes to its digest on every requested platform, and writes one ImageLock
// document per profile and platform. The bundled kai-scheduler subchart's images
// are recognised and left out: that project publishes and pins them itself.
//
// Run it through the Makefile: `make image-lock VERSION=vX.Y.Z`, or
// `make image-lock-check` for the offline coverage check that runs on every pull
// request. See README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
)

const (
	defaultChart    = "deployments/kai-resource-management-chart"
	defaultRegistry = "ghcr.io/kai-scheduler/kai-resource-management"
	defaultOutDir   = "bin/imagelocks"
)

// releaseVersion is what a published tag looks like. It exists to reject "latest"
// and other moving references, which would pin a lock to whatever happened to be
// tagged at generation time.
var releaseVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+`)

// options describes one generation run.
type options struct {
	chart string
	// version is the release tag. It is both the tag the chart's images are
	// rendered with and the version the lock records, so it has to be the tag that
	// was actually published.
	version string
	// registry is where this release publishes. Everything under it is locked;
	// values.yaml still holds the local build default at this point, so it is
	// passed in rather than read from the chart.
	registry  string
	platforms []platform
	profiles  []profile
	outDir    string
	// helmBin is the helm binary to render with. Empty means "helm".
	helmBin string
	// verifyOnly renders and classifies without reaching a registry or writing a
	// file, which is what runs on a pull request.
	verifyOnly bool
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("imagelock: ")

	// The signal handler is unregistered before log.Fatal exits the process.
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	opts, err := parseFlags(args)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return generate(ctx, opts)
}

func parseFlags(args []string) (options, error) {
	flags := flag.NewFlagSet("imagelock", flag.ContinueOnError)
	chart := flags.String("chart", defaultChart, "path to the Helm chart")
	version := flags.String("version", "", "release version, for example v1.2.3")
	registry := flags.String("registry", defaultRegistry, "registry the chart's own images are published to")
	outDir := flags.String("out-dir", defaultOutDir, "directory to write the locks to")
	helmBin := flags.String("helm", "helm", "helm binary to render with")
	verifyOnly := flags.Bool("verify-only", false, "render and classify only: no network, no files")

	var platformArgs, profileArgs []string
	flags.Func("platform", "os/arch to lock, repeatable (default linux/amd64, linux/arm64)", func(value string) error {
		platformArgs = append(platformArgs, value)
		return nil
	})
	flags.Func("profile", "build variant to lock, repeatable (default standard, fips)", func(value string) error {
		profileArgs = append(profileArgs, value)
		return nil
	})

	if err := flags.Parse(args); err != nil {
		return options{}, err
	}

	opts := options{
		chart:      *chart,
		version:    *version,
		registry:   strings.TrimSuffix(*registry, "/"),
		platforms:  defaultPlatforms(),
		profiles:   defaultProfiles(),
		outDir:     *outDir,
		helmBin:    *helmBin,
		verifyOnly: *verifyOnly,
	}

	var err error
	if len(platformArgs) > 0 {
		if opts.platforms, err = parsePlatforms(platformArgs); err != nil {
			return options{}, err
		}
	}
	if len(profileArgs) > 0 {
		if opts.profiles, err = parseProfiles(profileArgs); err != nil {
			return options{}, err
		}
	}
	if err := opts.validate(); err != nil {
		return options{}, err
	}
	return opts, nil
}

// validate reports whether these options describe a lock that can be generated.
func (o options) validate() error {
	if strings.TrimSpace(o.registry) == "" {
		return errors.New("a registry is required")
	}
	if len(o.platforms) == 0 {
		return errors.New("at least one platform is required")
	}
	if len(o.profiles) == 0 {
		return errors.New("at least one profile is required")
	}
	if o.verifyOnly {
		// Verification only classifies what the chart renders, so the release
		// version a lock file is named after is not needed.
		return nil
	}
	if o.version == "" {
		return errors.New("a version is required")
	}
	if !releaseVersion.MatchString(o.version) {
		return fmt.Errorf("version %q is not a release version like v1.2.3", o.version)
	}
	return nil
}

// generate renders, classifies, resolves and writes. It writes nothing until every
// digest is in hand, so a registry failure leaves no partial set of locks behind.
func generate(ctx context.Context, opts options) error {
	catalog := newImageCatalog(opts.registry)
	sets := make([]lockedImages, 0, len(opts.profiles))
	for _, prof := range opts.profiles {
		manifest, err := renderChart(ctx, opts, prof)
		if err != nil {
			return fmt.Errorf("render the %s profile: %w", prof, err)
		}
		images, skipped, err := imagesFromManifest(manifest, catalog)
		if err != nil {
			return fmt.Errorf("the %s profile: %w", prof, err)
		}
		if len(images) == 0 {
			return fmt.Errorf("the %s profile renders none of this repository's images", prof)
		}

		names := make([]string, len(images))
		for i, image := range images {
			names[i] = image.name
		}
		// #nosec G706 -- the names are traced back to `helm template` output and so
		// treated as tainted, but they come from this repository's own chart and are
		// the whole point of the line: on a pull request this log is what shows a
		// reviewer which images the release will lock.
		log.Printf("%s: locking %d image(s): %s (%d left to the kai-scheduler project)",
			prof, len(images), strings.Join(names, ", "), skipped)
		sets = append(sets, lockedImages{profile: prof, images: images})
	}

	if opts.verifyOnly {
		return nil
	}

	for i := range sets {
		sets[i].digests = map[string]*resolved{}
		for _, image := range sets[i].images {
			digests, err := resolve(ctx, image.reference(), opts.platforms)
			if err != nil {
				return fmt.Errorf("resolve %s: %w", image.reference(), err)
			}
			sets[i].digests[image.reference()] = digests
		}
	}

	locks := make([]imageLock, 0, len(sets)*len(opts.platforms))
	for _, set := range sets {
		for _, plat := range opts.platforms {
			locks = append(locks, buildLock(opts.version, plat, set))
		}
	}

	written, err := writeLocks(opts.outDir, locks)
	if err != nil {
		return err
	}
	for _, path := range written {
		log.Println("wrote", path)
	}
	return nil
}
