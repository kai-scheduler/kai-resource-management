// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package nodes moves cluster nodes in and out of the node pools a suite owns.
package nodes

import (
	goctx "context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const controlPlaneLabel = "node-role.kubernetes.io/control-plane"

// Workers names the cluster's worker nodes, in a stable order.
//
// Workers only: test pods carry no control-plane toleration, so a node pool built on the
// control-plane node would accept pods that can never schedule.
func Workers(ctx goctx.Context, k8sClient client.Client) ([]string, error) {
	nodeList := &corev1.NodeList{}
	if err := k8sClient.List(ctx, nodeList); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var names []string
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if _, isControlPlane := node.Labels[controlPlaneLabel]; isControlPlane {
			continue
		}
		names = append(names, node.Name)
	}
	sort.Strings(names)

	return names, nil
}

// LabelWorker puts one worker node into a node pool and returns its name.
func LabelWorker(ctx goctx.Context, k8sClient client.Client, labelKey, labelValue string) (string, error) {
	workers, err := Workers(ctx, k8sClient)
	if err != nil {
		return "", err
	}
	if len(workers) == 0 {
		return "", fmt.Errorf("no worker node to put into the node pool; is the cluster single-node?")
	}

	return workers[0], SetLabel(ctx, k8sClient, workers[0], labelKey, labelValue)
}

// SetLabel adds or replaces one label on a node.
func SetLabel(ctx goctx.Context, k8sClient client.Client, nodeName, key, value string) error {
	return patchLabels(ctx, k8sClient, nodeName, fmt.Sprintf("{%q:%q}", key, value))
}

// RemoveLabel takes a node back out of a node pool.
//
// A node pool cannot finish deleting while it still owns a node, and a node
// cannot leave one while a pod assigned to it is running, so callers tear their
// workloads down first.
func RemoveLabel(ctx goctx.Context, k8sClient client.Client, nodeName, key string) error {
	return patchLabels(ctx, k8sClient, nodeName, fmt.Sprintf("{%q:null}", key))
}

// patchLabels merge-patches the node's labels, which needs no read and cannot
// conflict with the kubelet and nodepool-controller writing the same object.
func patchLabels(ctx goctx.Context, k8sClient client.Client, nodeName, labels string) error {
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	patch := client.RawPatch(types.MergePatchType,
		fmt.Appendf(nil, `{"metadata":{"labels":%s}}`, labels))

	if err := k8sClient.Patch(ctx, node, patch); err != nil {
		return fmt.Errorf("patching labels on node %q: %w", nodeName, err)
	}

	return nil
}
