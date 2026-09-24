// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"
	"fmt"
	"strings"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Oldest KAI Scheduler this release is built against. Keep in step with the
	// kai-scheduler dependency pinned in the chart's Chart.yaml.
	DefaultMinimumSchedulerVersion = "v0.18.0"

	// The container whose image tag is the only record of the running version.
	kaiOperatorDeploymentName = "kai-operator"
	kaiOperatorContainerName  = "operator"

	// Same chart value as the image tag; the fallback when the image is a digest.
	msTagEnvVar = "MS_TAG"

	// Stripped before parsing: semver orders a prerelease below its release, so
	// v0.18.0-fips would otherwise read as older than v0.18.0.
	fipsTagSuffix = "-fips"
)

// KAIScheduler reports on the scheduler every KRM service drives. It belongs to
// the installation rather than to one operand, and is upgraded on its own
// schedule, so nothing about it follows from a successful install.
type KAIScheduler struct {
	MinimumVersion string
}

func (k *KAIScheduler) Check(
	ctx context.Context, cachedReader, uncachedReader client.Reader,
) (string, error) {
	kaiConfig := &kaiv1.Config{}
	configName := kaiconstants.DefaultKAIConfigSingeltonInstanceName

	err := uncachedReader.Get(ctx, client.ObjectKey{Name: configName}, kaiConfig)
	switch {
	case meta.IsNoMatchError(err):
		return "KAI Scheduler is not installed: no kai.scheduler/v1 Config API", nil
	case apierrors.IsNotFound(err):
		// KAI's deployer hook was disabled, or the CR was deleted under it.
		return fmt.Sprintf("KAI Scheduler Config %q does not exist", configName), nil
	case err != nil:
		return "", fmt.Errorf("reading KAI Scheduler Config %s: %w", configName, err)
	}

	var problems []string
	if unsupported := k.unsupportedVersionMessage(ctx, uncachedReader, kaiConfig); unsupported != "" {
		problems = append(problems, unsupported)
	}
	if unready := readinessMessage(kaiConfig, configName); unready != "" {
		problems = append(problems, unready)
	}
	return strings.Join(problems, "; "), nil
}

// unsupportedVersionMessage never errors. A tag need not be a version at all, so
// an unreadable one is skipped: guessing wrong would hold back a fine install.
func (k *KAIScheduler) unsupportedVersionMessage(
	ctx context.Context, reader client.Reader, kaiConfig *kaiv1.Config,
) string {
	logger := log.FromContext(ctx)

	if k.MinimumVersion == "" {
		return ""
	}
	minimum, err := version.ParseSemantic(k.MinimumVersion)
	if err != nil {
		logger.Error(err, "Minimum supported KAI Scheduler version is not a version, skipping the check",
			"minimumVersion", k.MinimumVersion)
		return ""
	}

	running, tag := k.runningVersion(ctx, reader, kaiConfig.Spec.Namespace)
	if running == nil {
		return ""
	}

	// A floor only, and reported as the tag rather than the parsed version, since
	// the tag is what is written on the Deployment.
	if !running.AtLeast(minimum) {
		return fmt.Sprintf("KAI Scheduler %s is older than the minimum supported %s",
			tag, k.MinimumVersion)
	}
	return ""
}

// runningVersion returns the version and the tag it came from, or nil.
func (k *KAIScheduler) runningVersion(
	ctx context.Context, reader client.Reader, namespace string,
) (*version.Version, string) {
	logger := log.FromContext(ctx)

	if namespace == "" {
		logger.V(1).Info("KAI Scheduler Config names no namespace, skipping the version check")
		return nil, ""
	}

	deployment := &appsv1.Deployment{}
	err := reader.Get(ctx,
		client.ObjectKey{Namespace: namespace, Name: kaiOperatorDeploymentName}, deployment)
	if err != nil {
		// Its Config reports ready, so something runs it another way.
		logger.V(1).Info("Cannot read the KAI Scheduler operator, skipping the version check",
			"namespace", namespace, "name", kaiOperatorDeploymentName, "reason", err.Error())
		return nil, ""
	}

	tag := versionTag(deployment)
	if tag == "" {
		logger.V(1).Info("KAI Scheduler operator carries no version tag, skipping the version check")
		return nil, ""
	}

	running := parseVersionTag(tag)
	if running == nil {
		logger.V(1).Info("KAI Scheduler version tag is not a version, skipping the version check",
			"tag", tag)
	}
	return running, tag
}

// parseVersionTag returns nil for a tag that is not a version — "latest", or a
// mirror's own. Not knowing is an ordinary outcome here, not a failure.
func parseVersionTag(tag string) *version.Version {
	parsed, err := version.ParseSemantic(strings.TrimSuffix(tag, fipsTagSuffix))
	if err != nil {
		return nil
	}
	return parsed
}

// versionTag prefers the image tag, falling back to MS_TAG for a digest pin.
func versionTag(deployment *appsv1.Deployment) string {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != kaiOperatorContainerName {
			continue
		}
		if tag := imageTag(container.Image); tag != "" {
			return tag
		}
		for _, env := range container.Env {
			if env.Name == msTagEnvVar {
				return env.Value
			}
		}
	}
	return ""
}

// imageTag returns the tag, or empty. A colon before the last slash is a port.
func imageTag(image string) string {
	if digest := strings.Index(image, "@"); digest >= 0 {
		image = image[:digest]
	}
	colon := strings.LastIndex(image, ":")
	if colon <= strings.LastIndex(image, "/") {
		return ""
	}
	return image[colon+1:]
}

// readinessMessage repeats KAI's own verdict rather than re-deriving it.
func readinessMessage(kaiConfig *kaiv1.Config, configName string) string {
	ready := meta.FindStatusCondition(kaiConfig.Status.Conditions, string(kaiv1.ConditionTypeReady))

	switch {
	case ready == nil:
		// Normal briefly after install, but indistinguishable from a stuck operator.
		return fmt.Sprintf("KAI Scheduler Config %q has not reported readiness", configName)
	case ready.Status == metav1.ConditionTrue:
		return ""
	case ready.Message != "":
		return fmt.Sprintf("KAI Scheduler Config %q is not ready: %s", configName, ready.Message)
	default:
		return fmt.Sprintf("KAI Scheduler Config %q is not ready", configName)
	}
}
