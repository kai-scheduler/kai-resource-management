// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package imagelock

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

var (
	linuxAMD64 = Platform{OS: "linux", Architecture: "amd64"}
	linuxARM64 = Platform{OS: "linux", Architecture: "arm64"}
)

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// fakeRegistry is an in-memory OCI registry: it demands a bearer token, hands one
// out at its own token endpoint, and serves manifests by tag.
type fakeRegistry struct {
	server *httptest.Server
	// manifests maps "<repository>:<tag>" to the body served for it.
	manifests map[string][]byte
	// mediaTypes overrides the Content-Type served for a manifest.
	mediaTypes map[string]string
	// lyingDigests overrides the Docker-Content-Digest header served for a manifest.
	lyingDigests map[string]string
	// failuresLeft makes the next N manifest requests fail with a 500.
	failuresLeft atomic.Int32
	tokenGrants  atomic.Int32
	manifestGets atomic.Int32
	basicAuth    string
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	registry := &fakeRegistry{
		manifests:    map[string][]byte{},
		mediaTypes:   map[string]string{},
		lyingDigests: map[string]string{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		registry.tokenGrants.Add(1)
		registry.basicAuth = strings.TrimPrefix(r.Header.Get("Authorization"), "Basic ")
		if r.URL.Query().Get("scope") == "" {
			http.Error(w, "no scope", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "granted"})
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer granted" {
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Bearer realm="%s/token",service="fake",scope="repository:x:pull,push"`, registry.server.URL))
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		registry.serve(w, r)
	})

	registry.server = httptest.NewServer(mux)
	t.Cleanup(registry.server.Close)
	return registry
}

func (f *fakeRegistry) serve(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	repository, tag, ok := strings.Cut(path, "/manifests/")
	if !ok {
		http.Error(w, "not a manifest request", http.StatusNotFound)
		return
	}
	f.manifestGets.Add(1)
	if f.failuresLeft.Load() > 0 {
		f.failuresLeft.Add(-1)
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}

	key := repository + ":" + tag
	body, found := f.manifests[key]
	if !found {
		http.Error(w, "no such manifest", http.StatusNotFound)
		return
	}
	mediaType := f.mediaTypes[key]
	if mediaType == "" {
		mediaType = ociIndexMediaType
	}
	served := f.lyingDigests[key]
	if served == "" {
		served = digestOf(body)
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Docker-Content-Digest", served)
	_, _ = w.Write(body)
}

func (f *fakeRegistry) host() string {
	parsed, err := url.Parse(f.server.URL)
	if err != nil {
		panic(err)
	}
	return parsed.Host
}

func (f *fakeRegistry) resolver() *resolver {
	return &resolver{
		client: f.server.Client(),
		scheme: "http",
		auths:  map[string]string{},
		tokens: map[string]string{},
		cache:  map[string]*resolved{},
	}
}

// addIndex publishes a multi-arch tag and returns the digest of the index itself.
func (f *fakeRegistry) addIndex(repository, tag string, perPlatform map[Platform]string) string {
	entries := make([]map[string]any, 0, len(perPlatform))
	for plat, digest := range perPlatform {
		entries = append(entries, map[string]any{
			"mediaType": "application/vnd.oci.image.manifest.v1+json",
			"digest":    digest,
			"Platform":  map[string]string{"os": plat.OS, "architecture": plat.Architecture},
		})
	}
	body, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     ociIndexMediaType,
		"manifests":     entries,
	})
	if err != nil {
		panic(err)
	}
	f.manifests[repository+":"+tag] = body
	return digestOf(body)
}

func manifestDigest(seed byte) string {
	return "sha256:" + strings.Repeat(fmt.Sprintf("%02x", seed), 32)
}

func TestResolveMultiArchTag(t *testing.T) {
	registry := newFakeRegistry(t)
	amd64Digest, arm64Digest := manifestDigest(0xa1), manifestDigest(0xb2)
	indexDigest := registry.addIndex("kai/scheduler", "v1.0.0", map[Platform]string{
		linuxAMD64: amd64Digest,
		linuxARM64: arm64Digest,
	})

	ref := registry.host() + "/kai/scheduler:v1.0.0"
	got, err := registry.resolver().resolve(context.Background(), ref, []Platform{linuxAMD64, linuxARM64})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if got.indexDigest != indexDigest {
		t.Errorf("index digest: got %s, want %s", got.indexDigest, indexDigest)
	}
	if got.perPlatform[linuxAMD64] != amd64Digest {
		t.Errorf("amd64 digest: got %s, want %s", got.perPlatform[linuxAMD64], amd64Digest)
	}
	if got.perPlatform[linuxARM64] != arm64Digest {
		t.Errorf("arm64 digest: got %s, want %s", got.perPlatform[linuxARM64], arm64Digest)
	}
}

func TestResolveCachesByReference(t *testing.T) {
	registry := newFakeRegistry(t)
	registry.addIndex("kai/scheduler", "v1.0.0", map[Platform]string{linuxAMD64: manifestDigest(0xa1)})

	resolver := registry.resolver()
	ref := registry.host() + "/kai/scheduler:v1.0.0"
	for range 3 {
		if _, err := resolver.resolve(context.Background(), ref, []Platform{linuxAMD64}); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	}

	// One authorized manifest request; the two later resolves come from the cache.
	if got := registry.manifestGets.Load(); got != 1 {
		t.Errorf("got %d manifest requests, want 1", got)
	}
	if got := registry.tokenGrants.Load(); got != 1 {
		t.Errorf("got %d token grants, want 1", got)
	}
}

func TestResolveRejectsADigestTheRegistryDidNotServe(t *testing.T) {
	registry := newFakeRegistry(t)
	registry.addIndex("kai/scheduler", "v1.0.0", map[Platform]string{linuxAMD64: manifestDigest(0xa1)})
	registry.lyingDigests["kai/scheduler:v1.0.0"] = manifestDigest(0xcc)

	ref := registry.host() + "/kai/scheduler:v1.0.0"
	_, err := registry.resolver().resolve(context.Background(), ref, []Platform{linuxAMD64})
	if err == nil {
		t.Fatal("expected a manifest whose digest does not match its bytes to fail")
	}
	if !strings.Contains(err.Error(), "but served") {
		t.Errorf("error should report the mismatch, got: %v", err)
	}
}

func TestResolveRejectsAMissingPlatform(t *testing.T) {
	registry := newFakeRegistry(t)
	registry.addIndex("kai/scheduler", "v1.0.0", map[Platform]string{linuxAMD64: manifestDigest(0xa1)})

	ref := registry.host() + "/kai/scheduler:v1.0.0"
	_, err := registry.resolver().resolve(context.Background(), ref, []Platform{linuxAMD64, linuxARM64})
	if err == nil {
		t.Fatal("expected a Platform missing from the index to fail")
	}
	if !strings.Contains(err.Error(), "linux/arm64") {
		t.Errorf("error should name the missing Platform, got: %v", err)
	}
}

func TestResolveRejectsAnAmbiguousPlatform(t *testing.T) {
	registry := newFakeRegistry(t)
	body, err := json.Marshal(map[string]any{
		"mediaType": ociIndexMediaType,
		"manifests": []map[string]any{
			{"digest": manifestDigest(0xa1), "Platform": map[string]string{"os": "linux", "architecture": "arm64"}},
			{"digest": manifestDigest(0xa2), "Platform": map[string]string{"os": "linux", "architecture": "arm64"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.manifests["kai/scheduler:v1.0.0"] = body

	ref := registry.host() + "/kai/scheduler:v1.0.0"
	_, err = registry.resolver().resolve(context.Background(), ref, []Platform{linuxARM64})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected two matching manifests to be refused, got: %v", err)
	}
}

// Every published image is a multi-arch index, and the lock records that index's
// digest for each entry. A bare manifest cannot supply one, so it is refused
// rather than locked without it.
func TestResolveRefusesASingleManifest(t *testing.T) {
	registry := newFakeRegistry(t)
	manifest, err := json.Marshal(map[string]any{
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config":    map[string]string{"digest": manifestDigest(0x77)},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry.manifests["kai/tool:v1.0.0"] = manifest
	registry.mediaTypes["kai/tool:v1.0.0"] = "application/vnd.oci.image.manifest.v1+json"

	ref := registry.host() + "/kai/tool:v1.0.0"
	_, err = registry.resolver().resolve(context.Background(), ref, []Platform{linuxAMD64})
	if err == nil || !strings.Contains(err.Error(), "single manifest") {
		t.Fatalf("expected a bare manifest to be refused, got: %v", err)
	}
}

func TestResolveRetriesAServerError(t *testing.T) {
	registry := newFakeRegistry(t)
	registry.addIndex("kai/scheduler", "v1.0.0", map[Platform]string{linuxAMD64: manifestDigest(0xa1)})
	registry.failuresLeft.Store(1)

	ref := registry.host() + "/kai/scheduler:v1.0.0"
	if _, err := registry.resolver().resolve(context.Background(), ref, []Platform{linuxAMD64}); err != nil {
		t.Fatalf("a transient 500 should be retried, got: %v", err)
	}
}

func TestResolveDoesNotRetryANotFound(t *testing.T) {
	registry := newFakeRegistry(t)

	ref := registry.host() + "/kai/missing:v1.0.0"
	_, err := registry.resolver().resolve(context.Background(), ref, []Platform{linuxAMD64})
	if err == nil {
		t.Fatal("expected an absent tag to fail")
	}
	// A 404 is a real answer, so it is taken at face value rather than retried.
	if got := registry.manifestGets.Load(); got != 1 {
		t.Errorf("got %d manifest requests, want 1", got)
	}
}

func TestResolveSendsStoredCredentials(t *testing.T) {
	registry := newFakeRegistry(t)
	registry.addIndex("kai/scheduler", "v1.0.0", map[Platform]string{linuxAMD64: manifestDigest(0xa1)})

	auth := base64.StdEncoding.EncodeToString([]byte("kaibot:secret"))
	resolver := registry.resolver()
	resolver.auths[registry.host()] = auth

	ref := registry.host() + "/kai/scheduler:v1.0.0"
	if _, err := resolver.resolve(context.Background(), ref, []Platform{linuxAMD64}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if registry.basicAuth != auth {
		t.Errorf("token request carried %q, want %q", registry.basicAuth, auth)
	}
}

func TestParseChallenge(t *testing.T) {
	params := parseChallenge(`Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:org/name:pull,push"`)
	want := map[string]string{
		"realm":   "https://ghcr.io/token",
		"service": "ghcr.io",
		"scope":   "repository:org/name:pull,push",
	}
	for key, value := range want {
		if params[key] != value {
			t.Errorf("%s: got %q, want %q", key, params[key], value)
		}
	}

	if got := parseChallenge(`Basic realm="registry"`); len(got) != 0 {
		t.Errorf("a non-Bearer challenge yields nothing, got %v", got)
	}
}

func TestParseReference(t *testing.T) {
	tests := []struct {
		ref        string
		host       string
		repository string
		tag        string
	}{
		{
			ref:        "ghcr.io/kai-scheduler/kai-scheduler/scheduler:v0.17.0",
			host:       "ghcr.io",
			repository: "kai-scheduler/kai-scheduler/scheduler",
			tag:        "v0.17.0",
		},
		{ref: "localhost:5000/name:v1", host: "localhost:5000", repository: "name", tag: "v1"},
		{ref: "docker.io/library/busybox:1", host: dockerHubRegistry, repository: "library/busybox", tag: "1"},
		{ref: "library/busybox:1", host: dockerHubRegistry, repository: "library/busybox", tag: "1"},
		{ref: "busybox:1", host: dockerHubRegistry, repository: "library/busybox", tag: "1"},
	}
	for _, test := range tests {
		t.Run(test.ref, func(t *testing.T) {
			host, repository, tag := parseReference(test.ref)
			if host != test.host || repository != test.repository || tag != test.tag {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)",
					host, repository, tag, test.host, test.repository, test.tag)
			}
		})
	}
}

func TestAuthHost(t *testing.T) {
	tests := map[string]string{
		"https://index.docker.io/v1/": dockerHubRegistry,
		"docker.io":                   dockerHubRegistry,
		"ghcr.io":                     "ghcr.io",
		"https://ghcr.io":             "ghcr.io",
		"localhost:5000":              "localhost:5000",
	}
	for key, want := range tests {
		t.Run(key, func(t *testing.T) {
			if got := authHost(key); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestIsSHA256Digest(t *testing.T) {
	if !isSHA256Digest(manifestDigest(0x0a)) {
		t.Error("a 64-character hex digest should be accepted")
	}
	for _, digest := range []string{"", "sha256:", "sha256:abc", "md5:" + strings.Repeat("a", 64), strings.Repeat("z", 64)} {
		if isSHA256Digest(digest) {
			t.Errorf("%q should be rejected", digest)
		}
	}
}
