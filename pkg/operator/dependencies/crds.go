// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package dependencies reports on what a KRM installation needs from components
// it does not install.
//
// KAI Scheduler can be installed, upgraded and removed independently of KRM, so the
// operator cannot assume the CRDs its services read are present, or that they
// still serve the API version those services talk to. Nothing here fails a
// reconcile: every check answers with a message for the DependenciesFulfilled
// condition, and reserves errors for a cluster that could not be asked.
package dependencies

import (
	"context"
	"fmt"
	"slices"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CRDRequirement is one CustomResourceDefinition a service needs, and the API
// version it reads it through.
type CRDRequirement struct {
	// Name is the CRD's own name, "<plural>.<group>".
	Name string

	// Version is the API version the service uses. A CRD that exists but has
	// stopped serving this version is as unusable to that service as an absent
	// one, which is why presence alone is not the check.
	Version string
}

func (r CRDRequirement) String() string {
	return r.Name + "/" + r.Version
}

// MissingCRDs reports which requirements the cluster does not meet, phrased to
// follow "<operand> is missing " — the sentence DeployableOperands builds. It
// returns an empty message when everything is present.
//
// The manager's cached client is the intended reader: the operator asks this on
// every reconcile, so one informer over CustomResourceDefinition costs less than
// re-reading each one from the API server every time. It has to be a client that
// sees deletions — the RESTMapper does not, since it caches positively and would
// keep reporting a deleted CRD as present.
//
// An error means the cluster could not be asked, and is deliberately distinct
// from a missing CRD: the caller reports the former as a failed check rather
// than as an unmet dependency.
func MissingCRDs(
	ctx context.Context, reader client.Reader, requirements ...CRDRequirement,
) (string, error) {
	var missing []string

	for _, requirement := range requirements {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := reader.Get(ctx, client.ObjectKey{Name: requirement.Name}, crd)

		// NoMatch rather than NotFound comes back when the apiextensions group
		// itself is unreachable through the RESTMapper.
		switch {
		case apierrors.IsNotFound(err) || meta.IsNoMatchError(err):
			missing = append(missing, requirement.String())
			continue
		case err != nil:
			return "", fmt.Errorf("reading CustomResourceDefinition %s: %w", requirement.Name, err)
		}

		if !servesVersion(crd, requirement.Version) {
			missing = append(missing, requirement.String())
		}
	}

	switch len(missing) {
	case 0:
		return "", nil
	case 1:
		return "CRD " + missing[0], nil
	default:
		return "CRDs " + strings.Join(missing, ", "), nil
	}
}

func servesVersion(crd *apiextensionsv1.CustomResourceDefinition, version string) bool {
	return slices.ContainsFunc(crd.Spec.Versions,
		func(crdVersion apiextensionsv1.CustomResourceDefinitionVersion) bool {
			return crdVersion.Name == version && crdVersion.Served
		})
}
