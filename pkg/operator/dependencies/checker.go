// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Checker reports on something the installation needs and the operator does not
// install. An empty message means met; a non-empty one goes onto the
// DependenciesFulfilled condition as-is. An error means the cluster could not be
// asked, which is not the same as the dependency being absent.
//
// reader must be uncached: the cache cannot inform on a kind whose CRD is absent.
type Checker interface {
	Check(ctx context.Context, reader client.Reader) (string, error)
}
