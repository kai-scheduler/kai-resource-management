// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package context

import (
	goctx "context"
	"fmt"
	"sort"
	"strings"
	"sync"

	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
)

// DefaultNodePoolName is the one NodePool the chart creates for itself. It is
// the catch-all for nodes no other node pool claims and is expected on any install.
const DefaultNodePoolName = "default"

var (
	preflightOnce sync.Once
	errPreflight  error
)

// runPreflight refuses to touch a cluster that holds resource-management objects
// the suites did not create. Such an object means the kubeconfig points at a
// real cluster rather than a throwaway one, and these suites create and delete
// cluster-scoped state that would not survive the mistake.
//
// There is deliberately no override: an escape hatch is what turns a guard into
// a formality. Clean the objects up or switch contexts.
func runPreflight(ctx goctx.Context, k8sClient client.Client) error {
	preflightOnce.Do(func() {
		errPreflight = checkForeignObjects(ctx, k8sClient)
	})

	return errPreflight
}

// foreign reports the names of objects in list that the suites do not own.
type foreignFinder func(ctx goctx.Context, k8sClient client.Client) (kind string, names []string, err error)

func checkForeignObjects(ctx goctx.Context, k8sClient client.Client) error {
	var problems []string

	for _, find := range []foreignFinder{
		foreignProjects, foreignDepartments, foreignNodePools, foreignQueues,
	} {
		kind, names, err := find(ctx, k8sClient)
		if err != nil {
			return err
		}
		if len(names) > 0 {
			sort.Strings(names)
			problems = append(problems, fmt.Sprintf("%s: %s", kind, strings.Join(names, ", ")))
		}
	}

	if len(problems) == 0 {
		return nil
	}

	return fmt.Errorf(
		"preflight: refusing to run e2e tests against this cluster. It already holds "+
			"resource-management objects these tests did not create (%s). That usually means "+
			"the kubeconfig points at a real cluster rather than a throwaway one. Delete those "+
			"objects, or switch context, and re-run. There is no override",
		strings.Join(problems, "; "))
}

// ownedByTests reports whether the suites created this object.
func ownedByTests(labels map[string]string) bool {
	return labels[constant.OwnerLabelKey] == constant.OwnerLabelValue
}

// listErr distinguishes a missing CRD, which is fine, from a real failure. A
// cluster without the CRD cannot hold a foreign object of that kind.
func listErr(kind string, err error) error {
	if err == nil || meta.IsNoMatchError(err) {
		return nil
	}

	return fmt.Errorf("preflight: listing %s: %w", kind, err)
}

func foreignProjects(ctx goctx.Context, k8sClient client.Client) (string, []string, error) {
	list := &kaires.ProjectList{}
	if err := listErr("Projects", k8sClient.List(ctx, list)); err != nil {
		return "", nil, err
	}

	var names []string
	for i := range list.Items {
		if !ownedByTests(list.Items[i].Labels) {
			names = append(names, list.Items[i].Name)
		}
	}

	return "Project", names, nil
}

func foreignDepartments(ctx goctx.Context, k8sClient client.Client) (string, []string, error) {
	list := &kaires.DepartmentList{}
	if err := listErr("Departments", k8sClient.List(ctx, list)); err != nil {
		return "", nil, err
	}

	var names []string
	for i := range list.Items {
		if !ownedByTests(list.Items[i].Labels) {
			names = append(names, list.Items[i].Name)
		}
	}

	return "Department", names, nil
}

// foreignNodePools tolerates the chart's own default nodePool, which every install
// has and no test creates.
func foreignNodePools(ctx goctx.Context, k8sClient client.Client) (string, []string, error) {
	list := &kaires.NodePoolList{}
	if err := listErr("NodePools", k8sClient.List(ctx, list)); err != nil {
		return "", nil, err
	}

	var names []string
	for i := range list.Items {
		nodePool := &list.Items[i]
		if nodePool.Name == DefaultNodePoolName || ownedByTests(nodePool.Labels) {
			continue
		}
		names = append(names, nodePool.Name)
	}

	return "NodePool", names, nil
}

// foreignQueues flags only Queues that no Project or Department owns.
//
// A Queue is created by project-controller, not by a test, so it never carries
// the ownership labels - it carries an ownerReference to the Project or
// Department it was derived from instead. Those owners are checked above, so a
// derived Queue is already governed by its owner's verdict and flagging it here
// would reject every cluster mid-run. What is left, an ownerless Queue, was
// written by hand or by something else, and is exactly what should stop a run.
func foreignQueues(ctx goctx.Context, k8sClient client.Client) (string, []string, error) {
	list := &kaiv2.QueueList{}
	if err := listErr("Queues", k8sClient.List(ctx, list)); err != nil {
		return "", nil, err
	}

	var names []string
	for i := range list.Items {
		queue := &list.Items[i]
		if ownedByTests(queue.Labels) || hasResourceManagementOwner(queue.OwnerReferences) {
			continue
		}
		names = append(names, queue.Name)
	}

	return "Queue", names, nil
}

// hasResourceManagementOwner reports whether one of the owners is a kind this
// preflight checks in its own right.
func hasResourceManagementOwner(owners []metav1.OwnerReference) bool {
	for _, owner := range owners {
		if owner.Kind == "Project" || owner.Kind == "Department" {
			return true
		}
	}

	return false
}
