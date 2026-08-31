// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"
	"fmt"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaiconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KAIScheduler reports on the scheduler the KRM services drive.
//
// It is one dependency of the installation as a whole rather than of any single
// operand: every service talks to the same scheduler, and the Config CR names
// the namespace it runs in. KAI Scheduler is installed, upgraded and removed on
// its own schedule, so none of this can be assumed from a successful install.
type KAIScheduler struct{}

func (k *KAIScheduler) Check(ctx context.Context, reader client.Reader) (string, error) {
	kaiConfig := &kaiv1.Config{}
	configName := kaiconstants.DefaultKAIConfigSingeltonInstanceName

	err := reader.Get(ctx, client.ObjectKey{Name: configName}, kaiConfig)
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

	return unreadyMessage(kaiConfig, configName), nil
}

// unreadyMessage repeats what KAI Scheduler says about itself rather than
// re-deriving it, so the two never disagree about whether the scheduler is up.
func unreadyMessage(kaiConfig *kaiv1.Config, configName string) string {
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
