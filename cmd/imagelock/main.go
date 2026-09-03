// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Command imagelock writes the digest-pinned image list an air-gapped install of a
// KAI Resource Management release needs.
//
// Run it through the Makefile: `make image-lock VERSION=vX.Y.Z`, or
// `make image-lock-check` for the offline coverage check that runs on every pull
// request. The generator itself is pkg/imagelock; this is flags and reporting.
// See cmd/imagelock/README.md.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kai-scheduler/kai-resource-management/pkg/imagelock"
)

const (
	defaultChart    = "deployments/kai-resource-management-chart"
	defaultRegistry = "ghcr.io/kai-scheduler/kai-resource-management"
	defaultOutDir   = "bin/imagelocks"
)

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

	result, err := imagelock.Generate(ctx, opts)
	if err != nil {
		return err
	}

	for _, profile := range result.Profiles {
		log.Printf("%s: locking %d image(s): %s (%d left to the kai-scheduler project)",
			profile.Profile, len(profile.Locked), strings.Join(profile.Locked, ", "), profile.Skipped)
	}
	for _, path := range result.Written {
		log.Println("wrote", path)
	}
	return nil
}

func parseFlags(args []string) (imagelock.Options, error) {
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
		return imagelock.Options{}, err
	}

	platforms := imagelock.DefaultPlatforms()
	if len(platformArgs) > 0 {
		parsed, err := imagelock.ParsePlatforms(platformArgs)
		if err != nil {
			return imagelock.Options{}, err
		}
		platforms = parsed
	}

	profiles := imagelock.DefaultProfiles()
	if len(profileArgs) > 0 {
		parsed, err := imagelock.ParseProfiles(profileArgs)
		if err != nil {
			return imagelock.Options{}, err
		}
		profiles = parsed
	}

	return imagelock.Options{
		Chart:      *chart,
		Version:    *version,
		Registry:   strings.TrimSuffix(*registry, "/"),
		Platforms:  platforms,
		Profiles:   profiles,
		OutDir:     *outDir,
		HelmBin:    *helmBin,
		VerifyOnly: *verifyOnly,
	}, nil
}
