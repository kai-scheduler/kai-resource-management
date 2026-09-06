// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestParseFlagsDefaults(t *testing.T) {
	opts, err := parseFlags([]string{"--version", "v1.2.3"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if opts.chart != defaultChart || opts.registry != defaultRegistry || opts.outDir != defaultOutDir {
		t.Errorf("unexpected defaults: %+v", opts)
	}
	if len(opts.platforms) != 2 || len(opts.profiles) != 2 {
		t.Errorf("got %v and %v, want both release platforms and both profiles", opts.platforms, opts.profiles)
	}
}

func TestParseFlagsOverrides(t *testing.T) {
	opts, err := parseFlags([]string{
		"--version", "1.2.3",
		"--platform", "linux/arm64",
		"--profile", "fips",
		"--registry", "example.test/krm/",
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(opts.platforms) != 1 || opts.platforms[0] != linuxARM64 {
		t.Errorf("got platforms %v, want only linux/arm64", opts.platforms)
	}
	if len(opts.profiles) != 1 || opts.profiles[0] != profileFIPS {
		t.Errorf("got profiles %v, want only fips", opts.profiles)
	}
	if opts.registry != "example.test/krm" {
		t.Errorf("a trailing slash should be trimmed, got %q", opts.registry)
	}
}

func TestParseFlagsRejections(t *testing.T) {
	tests := map[string]struct {
		args   []string
		wantIn string
	}{
		"bad platform": {args: []string{"--version", "v1.2.3", "--platform", "linux"}, wantIn: "want os/arch"},
		"bad profile":  {args: []string{"--version", "v1.2.3", "--profile", "hardened"}, wantIn: "unknown profile"},
		"moving tag":   {args: []string{"--version", "latest"}, wantIn: "not a release version"},
		"branch name":  {args: []string{"--version", "main"}, wantIn: "not a release version"},
		"no version":   {args: nil, wantIn: "a version is required"},
		"no registry":  {args: []string{"--version", "v1.2.3", "--registry", " "}, wantIn: "a registry is required"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseFlags(test.args)
			if err == nil {
				t.Fatalf("expected %v to be rejected", test.args)
			}
			if !strings.Contains(err.Error(), test.wantIn) {
				t.Errorf("got %q, want it to mention %q", err, test.wantIn)
			}
		})
	}
}

// Verification only classifies what the chart renders, so it must run without the
// release version a lock file is named after.
func TestVerifyOnlyNeedsNoVersion(t *testing.T) {
	opts, err := parseFlags([]string{"--verify-only"})
	if err != nil {
		t.Fatalf("--verify-only should not require a version, got: %v", err)
	}
	if !opts.verifyOnly {
		t.Error("verifyOnly was not set")
	}
}

func TestParsePlatformsAndProfiles(t *testing.T) {
	platforms, err := parsePlatforms([]string{"linux/arm64", "darwin/amd64"})
	if err != nil {
		t.Fatalf("parsePlatforms: %v", err)
	}
	if len(platforms) != 2 || platforms[0].Architecture != "arm64" || platforms[1].OS != "darwin" {
		t.Errorf("got %v", platforms)
	}
	for _, bad := range []string{"linux", "", "/amd64", "linux/", "linux/amd64/v8"} {
		if _, err := parsePlatforms([]string{bad}); err == nil {
			t.Errorf("expected platform %q to be rejected", bad)
		}
	}

	if _, err := parsePlatforms(nil); err == nil {
		t.Error("expected an empty platform list to be rejected rather than silently defaulted")
	}
	if _, err := parseProfiles(nil); err == nil {
		t.Error("expected an empty profile list to be rejected rather than silently defaulted")
	}
}
