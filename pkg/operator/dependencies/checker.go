// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package dependencies

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Checker reports on one thing the installation needs and the operator does not
// install, for components that belong to no single operand.
//
// An empty message means the requirement is met. A non-empty one goes onto the
// DependenciesFulfilled condition as-is, so it has to read on its own. An error
// means the cluster could not be asked, which is deliberately a different answer
// from the dependency not being there.
//
// reader must be uncached: what these read may include a kind whose CRD is not
// installed, and the cache cannot start an informer for a kind the API server
// does not serve.
type Checker interface {
	Check(ctx context.Context, reader client.Reader) (string, error)
}
