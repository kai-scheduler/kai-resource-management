// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package nodepool_controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/rs/zerolog/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/utils"
)

func (npc *NodePoolController) addControllerAsFinalizerIfNeeded(ctx context.Context, nodePool *v1alpha1.NodePool) (err error) {
	finalizerName := config.FinalizerName()
	if utils.IsItemInList(nodePool.Finalizers, finalizerName) {
		return nil
	}

	log.Info().Msgf("Adding controller to finalizers list of node pool <%v>", nodePool.Name)
	nodePool.Finalizers = append(nodePool.Finalizers, finalizerName)

	if err = npc.updateNodePoolFinalizers(ctx, nodePool); err != nil {
		log.Error().Msgf("Error adding controller to finalizers list of node pool <%v>, err: %v", nodePool.Name, err.Error())
		return err
	}
	return nil
}

func (npc *NodePoolController) finalize(ctx context.Context, nodePool *v1alpha1.NodePool) (err error) {
	log.Info().Msgf("Finalizing deletion of nodepool <%v>", nodePool.Name)

	if !utils.IsItemInList(nodePool.Finalizers, config.FinalizerName()) {
		log.Info().Msgf("Got a finalization event for NodePool <%v>, but this controller is not in its finalizers list. Skipping finalization.", nodePool.Name)
		return
	}

	isNodePoolDeleted := npc.handleNodePoolDeletionOnOwnerUninstall(ctx, nodePool)
	if isNodePoolDeleted {
		return
	}

	resultNodes, err := npc.handleNodePoolDeletion(ctx, nodePool)
	if err != nil {
		innerErr := npc.updateNodePoolStatusOnDeletionError(ctx, nodePool, resultNodes)
		err = utils.AppendErrIfNotNil(err, innerErr)
		return fmt.Errorf("errors encountered during deletion of nodepool <%v>, error: %w", nodePool.Name, err)
	}

	phase, err := npc.updateNodePoolStatusOnDeletion(ctx, nodePool, resultNodes)
	if err != nil {
		return fmt.Errorf("errors encountered during deletion of nodepool <%v>, error: %v", nodePool.Name, err.Error())
	}

	nodePoolDeletingMsg := ""
	if phase != v1alpha1.NodePoolDeleting {
		// removing the finalizer from the nodepool object, will cause the resource to be deleted by Kubernetes
		if err = npc.deleteFinalizer(ctx, nodePool); err != nil {
			return
		}
	} else {
		nodePoolDeletingMsg = "; nodepool is in 'Deleting' phase"
		unschedulableErr := fmt.Errorf("NodePool <%v> is in 'Deleting' phase", nodePool.Name)
		err = utils.AppendErrIfNotNil(err, unschedulableErr)
	}

	log.Info().Msgf("Done handling deletion of nodepool <%v>%v", nodePool.Name, nodePoolDeletingMsg)
	return err
}

// handleNodePoolDeletionOnOwnerUninstall force-deletes the nodepool, ignoring pods
// still running on its nodes, when the CR named by --uninstall-detection-ref is
// being deleted. Returns true if the nodepool was deleted.
func (npc *NodePoolController) handleNodePoolDeletionOnOwnerUninstall(ctx context.Context, nodePool *v1alpha1.NodePool) bool {
	if npc.params.UninstallDetection == nil {
		return false
	}
	ref := npc.params.UninstallDetection

	ownerResource, err := npc.getOwnerResource(ctx, ref)
	// A missing CR counts as uninstalled and falls through to the force-delete
	// below. Being denied the read does not: without the grant we cannot tell,
	// so leave the nodepool to the normal deletion path.
	if err != nil && !apierrors.IsNotFound(err) {
		if apierrors.IsForbidden(err) {
			log.Debug().Msgf("Not permitted to read %v <%v>; skipping uninstall detection",
				ref.GVK.Kind, ref.Key)
		} else {
			log.Error().Msgf("Failed getting %v <%v>, err: %s", ref.GVK.Kind, ref.Key, err.Error())
		}
		return false
	}

	if ownerResource != nil && ownerResource.GetDeletionTimestamp().IsZero() {
		return false
	}

	log.Info().Msgf("Deleting NodePool <%v>; %v <%v> is being deleted, ignoring running pods on nodepool's nodes",
		nodePool.Name, ref.GVK.Kind, ref.Key)
	_ = npc.deleteFinalizer(ctx, nodePool)
	_ = npc.cleanupDeletedNodePool(ctx, nodePool)
	return true
}

func (npc *NodePoolController) deleteFinalizer(ctx context.Context, nodePool *v1alpha1.NodePool) (err error) {
	finalizerName := config.FinalizerName()
	log.Info().Msgf("Removing finalizer <%v> from nodepool's <%v> finalizers list",
		finalizerName, nodePool.Name)
	nodePool.Finalizers = utils.DeleteFromList(nodePool.Finalizers, finalizerName)
	if err = npc.updateNodePoolFinalizers(ctx, nodePool); err != nil && !apierrors.IsNotFound(err) {
		log.Error().Msgf("Error removing controller from finalizer list for nodepool <%v>; err: %v",
			nodePool.Name, err.Error())
		return err
	}

	return nil
}

func (npc *NodePoolController) updateNodePoolFinalizers(ctx context.Context, nodePool *v1alpha1.NodePool) (err error) {
	patchBytes, err := updateNodePoolFinalizersPatchBytes(nodePool.Finalizers)
	if err != nil {
		log.Error().Msgf("Failed to json.Marshal patch - update finalizers of nodepool <%v>, err: %v", nodePool.Name, err)
		return err
	}

	patch := client.RawPatch(types.MergePatchType, patchBytes)
	err = npc.Client.Patch(ctx, nodePool, patch)
	if err != nil {
		return err
	}
	return nil
}

// The CR belongs to the installing distribution, so it is read as unstructured
// to keep this module free of that distribution's API types.
func (npc *NodePoolController) getOwnerResource(
	ctx context.Context, ref *common.UninstallDetectionRef,
) (*unstructured.Unstructured, error) {
	owner := &unstructured.Unstructured{}
	owner.SetGroupVersionKind(ref.GVK)
	if err := npc.Client.Get(ctx, ref.Key, owner); err != nil {
		return nil, err
	}
	return owner, nil
}

func updateNodePoolFinalizersPatchBytes(finalizers []string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{"metadata": map[string]interface{}{
		"finalizers": finalizers}})
}
