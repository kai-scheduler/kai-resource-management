// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	kaiSchedulerChartName = "kai-scheduler"

	// What Helm's Secret storage driver writes for every release revision.
	helmReleaseSecretType = corev1.SecretType("helm.sh/release.v1")
	helmReleaseDataKey    = "release"

	// Release payloads run to megabytes, so the Secrets are read a page at a time.
	releaseListPageSize = 50
)

var (
	deployedHelmReleaseLabels = client.MatchingLabels{"owner": "helm", "status": "deployed"}
	gzipMagic                 = []byte{0x1f, 0x8b, 0x08}
)

// helmRelease is the part of Helm's stored release record read here. Decoding it
// directly keeps the Helm SDK, and everything it pulls in, out of the hook image.
type helmRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Chart     struct {
		Metadata struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"metadata"`
	} `json:"chart"`
}

// DetectKAISchedulerVersion returns the chart version of the deployed kai-scheduler Helm
// release, and the release it came from. It fails rather than guesses when there is not
// exactly one.
func DetectKAISchedulerVersion(ctx context.Context, reader client.Reader) (string, string, error) {
	releases, err := listDeployedReleases(ctx, reader, kaiSchedulerChartName)
	if err != nil {
		return "", "", err
	}

	switch len(releases) {
	case 0:
		return "", "", fmt.Errorf("no deployed Helm release of the %s chart in any namespace: "+
			"install KAI Scheduler first, or pass its version with --kai-scheduler-version",
			kaiSchedulerChartName)
	case 1:
		release := releases[0]
		return release.Chart.Metadata.Version,
			fmt.Sprintf("Helm release %s/%s", release.Namespace, release.Name), nil
	default:
		names := make([]string, 0, len(releases))
		for _, release := range releases {
			names = append(names, release.Namespace+"/"+release.Name)
		}
		return "", "", fmt.Errorf("found %d deployed Helm releases of the %s chart (%s): "+
			"pass the version of the one to use with --kai-scheduler-version",
			len(releases), kaiSchedulerChartName, strings.Join(names, ", "))
	}
}

// CheckKAISchedulerVersion fails when installed, read from source, is older than minimum.
func CheckKAISchedulerVersion(ctx context.Context, installed, source, minimum string) error {
	required, err := version.ParseSemantic(minimum)
	if err != nil {
		return fmt.Errorf("minimum KAI Scheduler version %q is not a version: %w", minimum, err)
	}
	running, err := version.ParseSemantic(installed)
	if err != nil {
		return fmt.Errorf("KAI Scheduler version %q from %s is not a version: %w", installed, source, err)
	}

	logger := logf.FromContext(ctx).WithValues("version", installed, "source", source)
	// A build of KAI's main branch is numbered 0.0.0-<commit> but is newer than every
	// release; the krm-operator's run-time check accepts it too.
	if running.Major() == 0 && running.Minor() == 0 && running.Patch() == 0 {
		logger.Info("KAI Scheduler is a main branch build; accepting it")
		return nil
	}
	if !running.AtLeast(required) {
		return fmt.Errorf("KAI Scheduler %s, from %s, is older than the minimum supported %s",
			installed, source, minimum)
	}
	logger.Info("KAI Scheduler version is supported", "minimum", minimum)
	return nil
}

// listDeployedReleases returns the deployed revision of every release of chartName. A
// Secret that does not decode is skipped: a broken release elsewhere in the cluster must
// not decide this one.
func listDeployedReleases(ctx context.Context, reader client.Reader, chartName string) ([]helmRelease, error) {
	logger := logf.FromContext(ctx)

	var releases []helmRelease
	secrets := &corev1.SecretList{}
	for {
		if err := reader.List(ctx, secrets, deployedHelmReleaseLabels,
			client.Limit(releaseListPageSize), client.Continue(secrets.Continue)); err != nil {
			return nil, fmt.Errorf("failed to list Helm release Secrets: %w", err)
		}
		for i := range secrets.Items {
			secret := &secrets.Items[i]
			if secret.Type != helmReleaseSecretType {
				continue
			}
			release, err := decodeHelmRelease(secret.Data[helmReleaseDataKey])
			if err != nil {
				logger.Info("Skipping a Helm release Secret that does not decode",
					"namespace", secret.Namespace, "name", secret.Name, "reason", err.Error())
				continue
			}
			if release.Chart.Metadata.Name == chartName {
				releases = append(releases, *release)
			}
		}
		if secrets.Continue == "" {
			return releases, nil
		}
	}
}

// decodeHelmRelease reverses Helm's encoding: JSON, gzipped, then base64 on top of the
// base64 the Secret API itself applies.
func decodeHelmRelease(data []byte) (*helmRelease, error) {
	raw, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode the release: %w", err)
	}

	var payload io.Reader = bytes.NewReader(raw)
	if bytes.HasPrefix(raw, gzipMagic) {
		unzipped, err := gzip.NewReader(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress the release: %w", err)
		}
		defer unzipped.Close()
		payload = unzipped
	}

	release := &helmRelease{}
	if err := json.NewDecoder(payload).Decode(release); err != nil {
		return nil, fmt.Errorf("failed to parse the release: %w", err)
	}
	return release, nil
}
