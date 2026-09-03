// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/kai-scheduler/kai-resource-management/pkg/imagelock"
)

func TestParseFlagsDefaults(t *testing.T) {
	opts, err := parseFlags([]string{"--version", "v1.2.3"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if opts.Chart != defaultChart || opts.Registry != defaultRegistry || opts.OutDir != defaultOutDir {
		t.Errorf("unexpected defaults: %+v", opts)
	}
	if len(opts.Platforms) != len(imagelock.DefaultPlatforms()) {
		t.Errorf("got platforms %v, want the release defaults", opts.Platforms)
	}
	if len(opts.Profiles) != len(imagelock.DefaultProfiles()) {
		t.Errorf("got profiles %v, want the release defaults", opts.Profiles)
	}
	if err := opts.Validate(); err != nil {
		t.Errorf("the defaults should describe a valid run, got: %v", err)
	}
}

func TestParseFlagsRepeatable(t *testing.T) {
	opts, err := parseFlags([]string{
		"--version", "1.2.3",
		"--platform", "linux/arm64",
		"--profile", "fips",
		"--registry", "example.test/krm/",
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(opts.Platforms) != 1 || opts.Platforms[0].Architecture != "arm64" {
		t.Errorf("got platforms %v, want only linux/arm64", opts.Platforms)
	}
	if len(opts.Profiles) != 1 || opts.Profiles[0] != imagelock.ProfileFIPS {
		t.Errorf("got profiles %v, want only fips", opts.Profiles)
	}
	if opts.Registry != "example.test/krm" {
		t.Errorf("a trailing slash should be trimmed, got %q", opts.Registry)
	}
}

func TestParseFlagsRejectsBadValues(t *testing.T) {
	for name, args := range map[string][]string{
		"bad platform": {"--platform", "linux"},
		"bad profile":  {"--profile", "hardened"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseFlags(append([]string{"--version", "v1.2.3"}, args...)); err == nil {
				t.Errorf("expected %v to be rejected", args)
			}
		})
	}

	// Version validation belongs to the package, and reaches the command through it.
	opts, err := parseFlags([]string{"--version", "latest"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if err := opts.Validate(); err == nil || !strings.Contains(err.Error(), "not a release version") {
		t.Errorf("expected a moving tag to be refused, got: %v", err)
	}
}
