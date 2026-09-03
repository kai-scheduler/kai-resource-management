// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package imagelock

import (
	"strings"
	"testing"
)

func validOptions() Options {
	return Options{
		Chart:     "chart",
		Version:   "v1.2.3",
		Registry:  "example.test/krm",
		Platforms: DefaultPlatforms(),
		Profiles:  DefaultProfiles(),
		OutDir:    "out",
	}
}

func TestDefaults(t *testing.T) {
	platforms := DefaultPlatforms()
	want := []Platform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64"}}
	if len(platforms) != len(want) {
		t.Fatalf("got %v, want %v", platforms, want)
	}
	for i := range want {
		if platforms[i] != want[i] {
			t.Errorf("platform %d: got %s, want %s", i, platforms[i], want[i])
		}
	}

	profiles := DefaultProfiles()
	if len(profiles) != 2 || profiles[0] != ProfileStandard || profiles[1] != ProfileFIPS {
		t.Errorf("got %v, want both standard and fips", profiles)
	}
}

func TestOptionsValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Options)
		wantErr string
	}{
		{name: "complete", mutate: func(*Options) {}},
		{
			name:    "moving tag",
			mutate:  func(o *Options) { o.Version = "latest" },
			wantErr: "not a release version",
		},
		{
			name:    "branch name",
			mutate:  func(o *Options) { o.Version = "main" },
			wantErr: "not a release version",
		},
		{
			name:    "no version",
			mutate:  func(o *Options) { o.Version = "" },
			wantErr: "a version is required",
		},
		{
			name:    "no registry",
			mutate:  func(o *Options) { o.Registry = "  " },
			wantErr: "a registry is required",
		},
		{
			name:    "no platforms",
			mutate:  func(o *Options) { o.Platforms = nil },
			wantErr: "at least one platform",
		},
		{
			name:    "no profiles",
			mutate:  func(o *Options) { o.Profiles = nil },
			wantErr: "at least one profile",
		},
		{
			// Verification only classifies what the chart renders, so it must run
			// without the release version a lock file is named after.
			name:   "verify-only needs no version",
			mutate: func(o *Options) { o.Version = ""; o.VerifyOnly = true },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := validOptions()
			test.mutate(&opts)
			err := opts.Validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("expected these options to be accepted, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %q to be rejected", test.name)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("got %q, want it to mention %q", err, test.wantErr)
			}
		})
	}
}

func TestParsePlatforms(t *testing.T) {
	platforms, err := ParsePlatforms([]string{"linux/arm64", "darwin/amd64"})
	if err != nil {
		t.Fatalf("ParsePlatforms: %v", err)
	}
	if len(platforms) != 2 || platforms[0].Architecture != "arm64" || platforms[1].OS != "darwin" {
		t.Errorf("got %v", platforms)
	}

	for _, bad := range []string{"linux", "", "/amd64", "linux/", "linux/amd64/v8"} {
		if _, err := ParsePlatforms([]string{bad}); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
	if _, err := ParsePlatforms(nil); err == nil {
		t.Error("expected an empty list to be rejected rather than silently defaulted")
	}
}

func TestParseProfiles(t *testing.T) {
	profiles, err := ParseProfiles([]string{"fips"})
	if err != nil {
		t.Fatalf("ParseProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0] != ProfileFIPS {
		t.Errorf("got %v, want only fips", profiles)
	}

	if _, err := ParseProfiles([]string{"hardened"}); err == nil {
		t.Error("expected an unknown profile to be rejected")
	}
	if _, err := ParseProfiles(nil); err == nil {
		t.Error("expected an empty list to be rejected rather than silently defaulted")
	}
}
