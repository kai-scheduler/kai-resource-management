package validation

import (
	"context"

	kaiv1alpha1 "github.com/run-ai/runai/runai-cluster/cluster/sdk/apis/kai/v1alpha1"
)

// validateDepartment runs all create/update validations for a Department. A
// department only carries queues, so the single check is that every node pool
// referenced by a queue exists in the cluster (empty queues are allowed).
func (v *Validator) validateDepartment(ctx context.Context, department *kaiv1alpha1.Department) error {
	return v.validateQueueNodePoolsExist(ctx, department.Spec.Queues)
}
