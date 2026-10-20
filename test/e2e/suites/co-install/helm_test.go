// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package coinstall

import (
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/helm"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

const (
	kaiReleaseName = "kai-scheduler"
	kaiNamespace   = "kai-scheduler"

	krmReleaseName = "krm"

	// Not KAI's defaults, so a controller still on its own defaults fails to line up with
	// the scheduler instead of passing by coincidence.
	adminNodePoolLabelKey = "e2e.admin/node-pool"
	adminQueueLabelKey    = "e2e.admin/queue"

	// An admin's own label on their shard, which neither adoption nor release may drop.
	adminShardLabelKey   = "e2e.admin/team"
	adminShardLabelValue = "research"

	// Below the minimum KRM supports. The detected version cannot be used: CI installs a
	// 0.0.0-<commit> build of KAI, which the check accepts as unreleased.
	unsupportedKAIVersion = "v0.17.0"
)

// installKAIScheduler installs KAI the way an admin would without KRM. Only the label keys
// are set: KAI's chart never writes schedulerName to kai-config, and KRM must cope.
func installKAIScheduler() {
	output, err := helm.Run(ctx,
		"install", kaiReleaseName, env.kaiChart,
		"--version", env.kaiVersion,
		"--namespace", kaiNamespace, "--create-namespace",
		"--set", "global.nodePoolLabelKey="+adminNodePoolLabelKey,
		"--set", "podgrouper.queueLabelKey="+adminQueueLabelKey,
		// One scheduler per shard, and the suite runs three of them on a small cluster.
		"--set", "scheduler.resources.requests.cpu=50m",
		"--set", "scheduler.resources.requests.memory=128Mi",
		"--wait", "--timeout", helm.Timeout,
	)
	Expect(err).ToNot(HaveOccurred(), "installing KAI Scheduler failed: %s", output)
}

func uninstallKAIScheduler() {
	output, err := helm.Run(ctx, "uninstall", kaiReleaseName, "--namespace", kaiNamespace,
		"--ignore-not-found", "--wait", "--timeout", helm.Timeout)
	Expect(err).ToNot(HaveOccurred(), "uninstalling KAI Scheduler failed: %s", output)
}

// installKRM leaves the label keys and scheduler name unset, so the chart has to read them
// from the live kai-config.
func installKRM(release, namespace string, extraArgs ...string) (string, error) {
	args := []string{
		"install", release, env.chartPath,
		"--namespace", namespace, "--create-namespace",
		"--set", "kai-scheduler.enabled=false",
	}
	if env.valuesFile != "" {
		args = append(args, "--values", env.valuesFile)
	}
	if env.imageTag != "" {
		args = append(args, "--set", "image.tag="+env.imageTag)
	}
	args = append(args, extraArgs...)
	args = append(args, "--wait", "--timeout", helm.Timeout)

	return helm.Run(ctx, args...)
}

// uninstallKRM runs the post-delete cleanup, which is what releases the adopted shards.
func uninstallKRM(release, namespace string, extraArgs ...string) {
	args := append([]string{"uninstall", release, "--namespace", namespace,
		"--ignore-not-found", "--wait", "--timeout", helm.Timeout}, extraArgs...)

	output, err := helm.Run(ctx, args...)
	Expect(err).ToNot(HaveOccurred(), "uninstalling %s failed: %s", release, output)
}

// deleteNodePools removes the node pools the migration created for this run. Helm owns
// none of them, and they would fail the next run's preflight.
func deleteNodePools(names ...string) {
	for _, name := range names {
		nodePool := &kaires.NodePool{}
		err := testClient.Get(ctx, types.NamespacedName{Name: name}, nodePool)
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			continue
		}
		Expect(err).ToNot(HaveOccurred())

		Expect(client.IgnoreNotFound(testClient.Delete(ctx, nodePool))).To(Succeed())
		wait.ForDeleted(ctx, testClient, nodePool)
	}
}

// flagValue reads a flag the operator renders as two arguments, "--flag", "value".
func flagValue(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}

	return ""
}
