// Copyright 2026 NVIDIA CORPORATION
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
	// DefaultMinimumSchedulerVersion is the oldest KAI Scheduler this release is
	// built against. Keep it in step with the kai-scheduler dependency pinned in
	// deployments/kai-resource-management-chart/Chart.yaml.
	DefaultMinimumSchedulerVersion = "v0.17.0"

	// The Deployment the KAI chart installs, and the container inside it whose
	// image tag is the only place the running version is written down.
	kaiOperatorDeploymentName = "kai-operator"
	kaiOperatorContainerName  = "operator"

	// msTagEnvVar is set from the same chart value as the image tag, and is read
	// as a fallback for a Deployment whose image is pinned by digest.
	msTagEnvVar = "MS_TAG"

	// fipsTagSuffix marks the FIPS build of a release. It has to come off before
	// the tag is parsed: semver orders a prerelease *below* the release it
	// qualifies, so v0.17.0-fips would otherwise read as older than v0.17.0.
	fipsTagSuffix = "-fips"
)

// KAIScheduler reports on the scheduler the KRM services drive.
//
// It is one dependency of the installation as a whole rather than of any single
// operand: every service talks to the same scheduler, and the Config CR names
// the namespace it runs in. KAI Scheduler is installed, upgraded and removed on
// its own schedule, so none of this can be assumed from a successful install.
type KAIScheduler struct {
	MinimumVersion string
}

func (k *KAIScheduler) Check(ctx context.Context, uncachedReader client.Reader) (string, error) {
	kaiConfig := &kaiv1.Config{}
	configName := kaiconstants.DefaultKAIConfigSingeltonInstanceName

	err := uncachedReader.Get(ctx, client.ObjectKey{Name: configName}, kaiConfig)
	switch {
	case meta.IsNoMatchError(err):
		return "KAI Scheduler is not installed: no kai.scheduler/v1 Config API", nil
	case apierrors.IsNotFound(err):
		// KAI's own deployer hook was disabled, or the CR was deleted out from
		// under it.
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

// unsupportedVersionMessage is best effort throughout, and never returns an
// error. The running version is only written down as an image tag, and a tag is
// not required to be a version at all — air-gapped mirrors re-tag, and images
// can be pinned by digest. An unreadable tag is therefore not an unmet
// dependency: guessing wrong would hold back an installation that is fine.
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

	// Reported as the tag rather than the parsed version, because that is what
	// is written on the Deployment and what someone will go looking for.
	// A floor only: anything at or above the minimum is accepted.
	if !running.AtLeast(minimum) {
		return fmt.Sprintf("KAI Scheduler %s is older than the minimum supported %s",
			tag, k.MinimumVersion)
	}
	return ""
}

// runningVersion reads the version off the KAI operator Deployment, returning it
// alongside the tag it was read from, or nil when it cannot be determined.
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
		// Its Config reports ready, so something is running it another way. Not
		// ours to fail the installation over, whatever went wrong reading it.
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

// parseVersionTag answers with nil rather than an error for a tag that is not a
// version — "latest", or an air-gapped mirror's own. Not knowing the version is
// an ordinary outcome here, not a failure.
func parseVersionTag(tag string) *version.Version {
	parsed, err := version.ParseSemantic(strings.TrimSuffix(tag, fipsTagSuffix))
	if err != nil {
		return nil
	}
	return parsed
}

// versionTag prefers the image tag and falls back to the MS_TAG the KAI chart
// sets from the same value, which survives an image pinned by digest.
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

// imageTag returns the tag of a container image reference, or empty when it
// carries none. A colon before the last slash is a registry port, not a tag.
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

// readinessMessage is empty when KAI Scheduler reports itself ready, and
// otherwise repeats its own verdict rather than re-deriving it, so the two never
// disagree about whether the scheduler is up.
func readinessMessage(kaiConfig *kaiv1.Config, configName string) string {
	ready := meta.FindStatusCondition(kaiConfig.Status.Conditions, string(kaiv1.ConditionTypeReady))

	switch {
	case ready == nil:
		// Normal for a few seconds after KAI is installed, and reported rather
		// than ignored because it is indistinguishable from an operator that
		// never got as far as reconciling it.
		return fmt.Sprintf("KAI Scheduler Config %q has not reported readiness", configName)
	case ready.Status == metav1.ConditionTrue:
		return ""
	case ready.Message != "":
		return fmt.Sprintf("KAI Scheduler Config %q is not ready: %s", configName, ready.Message)
	default:
		return fmt.Sprintf("KAI Scheduler Config %q is not ready", configName)
	}
}
