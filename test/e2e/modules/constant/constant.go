// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package constant holds the values shared by every e2e suite: the labels that
// mark a resource as owned by the tests, and the polling budgets.
package constant

import "time"

const (
	// OwnerLabelKey marks a resource as created by the e2e suites. Preflight
	// matches on it to tell test objects apart from a real cluster's own.
	OwnerLabelKey = "kai.resources/e2e"

	// OwnerLabelValue is the only value OwnerLabelKey ever takes.
	OwnerLabelValue = "true"

	// RunLabelKey carries the id of the process that created the resource, so a
	// run only ever deletes its own objects even when two run at once.
	RunLabelKey = "kai.resources/e2e-run"

	// ReleaseNamespace is where the chart is installed, and so where the
	// controllers and the objects they create in their own namespace live.
	ReleaseNamespace = "kai-resource-management"
)

// Polling budgets. Controllers here reconcile in well under a second once their
// caches are warm; the generous ceilings absorb a cold start on a CI runner.
const (
	// Timeout bounds an ordinary wait for a controller to converge.
	Timeout = 2 * time.Minute

	// PodTimeout bounds waits that include pulling an image and scheduling.
	PodTimeout = 5 * time.Minute

	// Interval is how often a wait re-reads the object.
	Interval = time.Second
)
