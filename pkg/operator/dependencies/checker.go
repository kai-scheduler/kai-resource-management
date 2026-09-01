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
// Read through cachedReader by default. uncachedReader is for a kind whose CRD
// may be absent, which the cache cannot start an informer for.
type Checker interface {
	Check(ctx context.Context, cachedReader, uncachedReader client.Reader) (string, error)
}
