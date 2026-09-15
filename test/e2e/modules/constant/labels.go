// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package constant

import (
	"crypto/rand"
	"encoding/hex"
	"maps"
)

// runID identifies this process. It is generated once at package load so every
// resource a single `go test` invocation creates carries the same value.
var runID = newRunID()

func newRunID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand only fails if the system entropy source is broken, in
		// which case failing loudly beats sharing a run id with another run.
		panic("e2e: cannot generate a run id: " + err.Error())
	}
	return hex.EncodeToString(buf)
}

// RunID reports this process's run id, the value of RunLabelKey on everything
// it creates.
func RunID() string { return runID }

// OwnerLabels are stamped on every resource the suites create: the static pair
// preflight looks for, plus this run's id for scoped cleanup.
func OwnerLabels() map[string]string {
	return map[string]string{
		OwnerLabelKey: OwnerLabelValue,
		RunLabelKey:   runID,
	}
}

// WithOwnerLabels merges the ownership labels into a resource's own labels,
// leaving the caller's map untouched. The ownership labels are applied last and
// so always win: a caller that overwrote them would hide the resource from
// cleanup and make preflight treat it as a real cluster's own.
func WithOwnerLabels(labels map[string]string) map[string]string {
	merged := map[string]string{}
	maps.Copy(merged, labels)
	maps.Copy(merged, OwnerLabels())

	return merged
}
