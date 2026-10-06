// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/dependencies"
)

// DetectKAISchedulerVersion returns the running KAI Scheduler's version and where it was
// read, the same way the krm-operator reads it at run time.
func DetectKAISchedulerVersion(ctx context.Context, reader client.Reader) (string, string, error) {
	tag, err := dependencies.KAISchedulerVersionTag(ctx, reader)
	if err != nil {
		return "", "", fmt.Errorf("cannot detect the KAI Scheduler version, "+
			"install KAI Scheduler first or pass its version with --kai-scheduler-version: %w", err)
	}
	return tag, "the kai-operator image tag", nil
}

// CheckKAISchedulerVersion fails when installed, read from source, is older than minimum.
func CheckKAISchedulerVersion(ctx context.Context, installed, source, minimum string) error {
	required, err := version.ParseSemantic(minimum)
	if err != nil {
		return fmt.Errorf("minimum KAI Scheduler version %q is not a version: %w", minimum, err)
	}
	if _, err := version.ParseSemantic(installed); err != nil {
		return fmt.Errorf("KAI Scheduler version %q from %s is not a version: %w", installed, source, err)
	}

	logger := logf.FromContext(ctx).WithValues("version", installed, "source", source)
	// A build of KAI's main branch is numbered 0.0.0-<commit> but is newer than every
	// release; the krm-operator's run-time check accepts it too.
	running := dependencies.ParseVersionTag(installed)
	if running == nil {
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
