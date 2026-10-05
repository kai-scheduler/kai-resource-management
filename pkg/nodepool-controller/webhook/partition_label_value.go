package webhook

import (
	"context"
	"fmt"
	"slices"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
)

func listShardsWithPartitionLabelValue(
	ctx context.Context, reader client.Reader, partitionLabelValue string,
) ([]kaiv1.SchedulingShard, error) {
	shards := &kaiv1.SchedulingShardList{}
	if err := reader.List(ctx, shards,
		client.MatchingFields{common.SchedulingShardPartitionField: partitionLabelValue}); err != nil {
		return nil, fmt.Errorf("listing scheduling shards with partitionLabelValue %q: %w", partitionLabelValue, err)
	}
	return shards.Items, nil
}

func nodePoolNameForPartitionLabelValue(partitionLabelValue string) string {
	if partitionLabelValue == "" {
		return config.Get().DefaultNodepoolName
	}
	return partitionLabelValue
}

func owningNodePoolName(shard *kaiv1.SchedulingShard) string {
	owner := metav1.GetControllerOf(shard)
	if owner == nil || owner.Kind != "NodePool" {
		return ""
	}
	ownerGroupVersion, err := schema.ParseGroupVersion(owner.APIVersion)
	if err != nil || ownerGroupVersion.Group != v1alpha1.GroupVersion.Group {
		return ""
	}
	return owner.Name
}

func sortedShardNames(shards []kaiv1.SchedulingShard) []string {
	names := make([]string, 0, len(shards))
	for i := range shards {
		names = append(names, shards[i].Name)
	}
	slices.Sort(names)
	return names
}
