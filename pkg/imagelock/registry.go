// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package imagelock

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The registry is spoken to directly over the OCI distribution API rather than
// through a client library: the whole exchange is one manifest GET plus an
// anonymous token, and the standard library already ships everything it needs.
// Keeping it here means the release tooling adds no module to a repository whose
// NOTICE is generated from its dependency graph.

const (
	// manifestAccept lists both the OCI and the Docker media types, index types
	// first. A registry serves the first it can, so a multi-arch tag comes back as
	// the index rather than as the manifest it would otherwise resolve to.
	manifestAccept = "application/vnd.oci.image.index.v1+json," +
		"application/vnd.docker.distribution.manifest.list.v2+json," +
		"application/vnd.oci.image.manifest.v1+json," +
		"application/vnd.docker.distribution.manifest.v2+json"

	ociIndexMediaType   = "application/vnd.oci.image.index.v1+json"
	dockerListMediaType = "application/vnd.docker.distribution.manifest.list.v2+json"
	maxResponseBytes    = 8 << 20
	// maxStatusMessageBytes bounds how much of a registry's error body is quoted back.
	maxStatusMessageBytes = 200
	registryAttempts      = 3
	registryBackoff       = 2 * time.Second
	registryHTTPTimeout   = 60 * time.Second
	dockerHubRegistry     = "registry-1.docker.io"
	dockerHubIndexDomain  = "docker.io"
)

type Platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

func (p Platform) String() string { return p.OS + "/" + p.Architecture }

// resolved is one image's digests: the multi-arch index it was published as, and
// the manifest each requested Platform pulls out of that index.
type resolved struct {
	indexDigest string
	perPlatform map[Platform]string
}

type resolver struct {
	client *http.Client
	// scheme is https everywhere but in tests, which serve a registry over plain
	// http from an ephemeral port.
	scheme string
	// auths maps a registry host to its base64 "user:password", as written by
	// `docker login`. The published images need none; this repository's own are
	// private until a release goes out, and CI logs in before generating the lock.
	auths map[string]string
	// tokens caches one bearer token per repository scope, so resolving several
	// platforms of the same image costs a single authorization round trip.
	tokens map[string]string
	cache  map[string]*resolved
}

func newResolver() *resolver {
	return &resolver{
		client: &http.Client{Timeout: registryHTTPTimeout},
		scheme: "https",
		auths:  dockerConfigAuths(),
		tokens: map[string]string{},
		cache:  map[string]*resolved{},
	}
}

func (r *resolver) resolve(ctx context.Context, ref string, platforms []Platform) (*resolved, error) {
	if cached, ok := r.cache[ref]; ok {
		return cached, nil
	}

	host, repository, tag := parseReference(ref)
	body, digest, mediaType, err := r.manifest(ctx, host, repository, tag)
	if err != nil {
		return nil, err
	}

	if mediaType != ociIndexMediaType && mediaType != dockerListMediaType {
		// Every image is published with buildx for both platforms, and the lock
		// records an index digest for each one, which the tooling that composes
		// these locks requires. A tag that is a bare manifest is a build problem,
		// not something to paper over with the manifest's own digest.
		return nil, fmt.Errorf("%s is published as a single manifest rather than a multi-arch index", ref)
	}

	perPlatform, err := indexDigests(body, platforms)
	if err != nil {
		return nil, err
	}
	result := &resolved{indexDigest: digest, perPlatform: perPlatform}

	r.cache[ref] = result
	return result, nil
}

// manifest fetches a manifest and returns its content-addressed digest. The digest
// is computed from the bytes received rather than read out of the registry's header,
// so a lock entry can never name something the registry did not actually serve.
func (r *resolver) manifest(ctx context.Context, host, repository, tag string) (body []byte, digest, mediaType string, err error) {
	target := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", r.scheme, host, repository, url.PathEscape(tag))
	response, err := r.get(ctx, host, repository, target, manifestAccept)
	if err != nil {
		return nil, "", "", err
	}
	body = response.body

	sum := sha256.Sum256(body)
	digest = "sha256:" + hex.EncodeToString(sum[:])
	if served := response.header.Get("Docker-Content-Digest"); served != "" && served != digest {
		return nil, "", "", fmt.Errorf("registry reported digest %s but served %s", served, digest)
	}

	var envelope struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, "", "", fmt.Errorf("parse manifest: %w", err)
	}
	mediaType = envelope.MediaType
	if mediaType == "" {
		mediaType = response.header.Get("Content-Type")
	}
	return body, digest, mediaType, nil
}

// indexDigests picks each requested Platform's manifest out of a multi-arch index.
// A Platform that is missing, or that matches more than once, is an error: the lock
// must name exactly one digest per Platform or an air-gapped mirror is ambiguous.
func indexDigests(body []byte, platforms []Platform) (map[Platform]string, error) {
	var index struct {
		Manifests []struct {
			Digest   string    `json:"digest"`
			Platform *Platform `json:"Platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("parse image index: %w", err)
	}

	digests := make(map[Platform]string, len(platforms))
	for _, want := range platforms {
		var matches []string
		for _, entry := range index.Manifests {
			if entry.Platform != nil && *entry.Platform == want {
				matches = append(matches, entry.Digest)
			}
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("no %s manifest in index", want)
		case 1:
			if !isSHA256Digest(matches[0]) {
				return nil, fmt.Errorf("bad %s digest %q in index", want, matches[0])
			}
			digests[want] = matches[0]
		default:
			return nil, fmt.Errorf("ambiguous %s: %d manifests match", want, len(matches))
		}
	}
	return digests, nil
}

// registryResponse is an HTTP response whose body has already been read and closed,
// so nothing downstream holds a connection open.
type registryResponse struct {
	status int
	header http.Header
	body   []byte
}

// get performs an authenticated GET, retrying what a registry can fail transiently.
func (r *resolver) get(ctx context.Context, host, repository, target, accept string) (registryResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= registryAttempts; attempt++ {
		response, err := r.attempt(ctx, host, repository, target, accept)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if ctx.Err() != nil || !isRetryable(err) {
			return registryResponse{}, err
		}
		if attempt < registryAttempts {
			select {
			case <-ctx.Done():
				return registryResponse{}, ctx.Err()
			case <-time.After(registryBackoff):
			}
		}
	}
	return registryResponse{}, fmt.Errorf("after %d attempts: %w", registryAttempts, lastErr)
}

func (r *resolver) attempt(ctx context.Context, host, repository, target, accept string) (registryResponse, error) {
	response, err := r.do(ctx, target, accept, bearer(r.tokens[scopeKey(host, repository)]))
	if err != nil {
		return registryResponse{}, err
	}
	if response.status != http.StatusUnauthorized {
		return response, checkStatus(response)
	}

	// No token yet, or the cached one expired; the challenge says where to get one.
	token, err := r.authorize(ctx, host, repository, response.header.Get("WWW-Authenticate"))
	if err != nil {
		return registryResponse{}, err
	}
	r.tokens[scopeKey(host, repository)] = token

	response, err = r.do(ctx, target, accept, bearer(token))
	if err != nil {
		return registryResponse{}, err
	}
	return response, checkStatus(response)
}

func (r *resolver) do(ctx context.Context, target, accept, authorization string) (registryResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return registryResponse{}, err
	}
	request.Header.Set("Accept", accept)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}

	response, err := r.client.Do(request)
	if err != nil {
		return registryResponse{}, retryable{err}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return registryResponse{}, retryable{err}
	}
	return registryResponse{status: response.StatusCode, header: response.Header, body: body}, nil
}

// authorize completes the token exchange the registry asked for in its challenge.
// Anonymous pulls work for the published images; the credentials `docker login`
// stored are sent when the host has them, which is how CI reaches a private tag.
func (r *resolver) authorize(ctx context.Context, host, repository, challenge string) (string, error) {
	params := parseChallenge(challenge)
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("registry %s asked for authentication without naming a token realm", host)
	}

	query := url.Values{}
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + repository + ":pull"
	}
	query.Set("scope", scope)

	var authorization string
	if auth, ok := r.auths[host]; ok {
		authorization = "Basic " + auth
	}

	response, err := r.do(ctx, realm+"?"+query.Encode(), "application/json", authorization)
	if err != nil {
		return "", err
	}
	if err := checkStatus(response); err != nil {
		return "", fmt.Errorf("get a pull token for %s: %w", repository, err)
	}

	var token struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(response.body, &token); err != nil {
		return "", fmt.Errorf("parse the registry token: %w", err)
	}
	switch {
	case token.Token != "":
		return token.Token, nil
	case token.AccessToken != "":
		return token.AccessToken, nil
	default:
		return "", fmt.Errorf("registry %s returned an empty token", host)
	}
}

// parseChallenge reads the key="value" pairs out of a Bearer WWW-Authenticate header.
func parseChallenge(header string) map[string]string {
	params := map[string]string{}
	rest, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return params
	}
	for _, pair := range splitChallengePairs(rest) {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		params[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return params
}

// splitChallengePairs splits on commas outside quotes: a scope value legitimately
// contains commas when the registry asks for several actions at once.
func splitChallengePairs(header string) []string {
	var pairs []string
	var current strings.Builder
	quoted := false
	for _, char := range header {
		switch {
		case char == '"':
			quoted = !quoted
			current.WriteRune(char)
		case char == ',' && !quoted:
			pairs = append(pairs, current.String())
			current.Reset()
		default:
			current.WriteRune(char)
		}
	}
	if current.Len() > 0 {
		pairs = append(pairs, current.String())
	}
	return pairs
}

// dockerConfigAuths reads the basic credentials `docker login` writes. Credential
// helpers are deliberately not invoked: this runs in CI straight after
// docker/login-action, which stores the token inline.
func dockerConfigAuths() map[string]string {
	directory := os.Getenv("DOCKER_CONFIG")
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		directory = filepath.Join(home, ".docker")
	}
	// #nosec G703 -- this is the docker config location, resolved from DOCKER_CONFIG
	// or the home directory exactly as every docker client resolves it.
	raw, err := os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil {
		return nil
	}

	var config struct {
		Auths map[string]struct {
			Auth     string `json:"auth"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil
	}

	auths := map[string]string{}
	for host, entry := range config.Auths {
		switch {
		case entry.Auth != "":
			auths[authHost(host)] = entry.Auth
		case entry.Username != "":
			auths[authHost(host)] = base64.StdEncoding.EncodeToString([]byte(entry.Username + ":" + entry.Password))
		}
	}
	return auths
}

// authHost normalises a docker config key, which may be a bare host or a URL, to
// the host the registry requests go to.
func authHost(key string) string {
	if parsed, err := url.Parse(key); err == nil && parsed.Host != "" {
		key = parsed.Host
	}
	key = strings.TrimSuffix(key, "/")
	if key == dockerHubIndexDomain || key == "index."+dockerHubIndexDomain {
		return dockerHubRegistry
	}
	return key
}

// parseReference splits repo:tag into the host to call, the repository path within
// it, and the tag.
func parseReference(ref string) (host, repository, tag string) {
	repository, tag = splitReference(ref)
	first, rest, ok := strings.Cut(repository, "/")
	if ok && isRegistryHost(first) {
		if first == dockerHubIndexDomain {
			return dockerHubRegistry, rest, tag
		}
		return first, rest, tag
	}
	// No registry means Docker Hub, where an unqualified repository lives under library/.
	if !ok {
		repository = "library/" + repository
	}
	return dockerHubRegistry, repository, tag
}

// isRegistryHost distinguishes a registry from the first path element of a Docker
// Hub repository, which by convention can be neither dotted nor ported.
func isRegistryHost(candidate string) bool {
	return strings.ContainsAny(candidate, ".:") || candidate == "localhost"
}

func isSHA256Digest(digest string) bool {
	encoded, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(encoded) != 64 {
		return false
	}
	_, err := hex.DecodeString(encoded)
	return err == nil
}

func scopeKey(host, repository string) string { return host + "/" + repository }

// bearer renders a token as an Authorization header value, or nothing at all when
// there is no token yet: the first request is what draws the challenge.
func bearer(token string) string {
	if token == "" {
		return ""
	}
	return "Bearer " + token
}

// retryable marks a transport-level failure, which a second attempt may survive.
type retryable struct{ err error }

func (r retryable) Error() string { return r.err.Error() }
func (r retryable) Unwrap() error { return r.err }

func isRetryable(err error) bool {
	var transport retryable
	if errors.As(err, &transport) {
		return true
	}
	var status *statusError
	return errors.As(err, &status) && (status.code == http.StatusTooManyRequests || status.code >= http.StatusInternalServerError)
}

type statusError struct {
	code int
	body string
}

func (s *statusError) Error() string {
	if s.body == "" {
		return fmt.Sprintf("registry returned %d %s", s.code, http.StatusText(s.code))
	}
	return fmt.Sprintf("registry returned %d %s: %s", s.code, http.StatusText(s.code), s.body)
}

func checkStatus(response registryResponse) error {
	if response.status == http.StatusOK {
		return nil
	}
	message := strings.TrimSpace(string(response.body))
	if len(message) > maxStatusMessageBytes {
		message = message[:maxStatusMessageBytes]
	}
	return &statusError{code: response.status, body: message}
}
