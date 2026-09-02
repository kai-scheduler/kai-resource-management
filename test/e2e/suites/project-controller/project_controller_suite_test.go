// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package project_controller

import (
	goctx "context"
	"testing"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	testcontext "github.com/kai-scheduler/kai-resource-management/test/e2e/modules/context"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

const projectControllerServiceAccount = "project-controller"

var (
	ctx        goctx.Context
	testClient client.Client
)

func TestProjectController(t *testing.T) {
	RegisterFailHandler(Fail)

	// Gomega's default is one second, shorter than a reconcile.
	SetDefaultEventuallyTimeout(constant.Timeout)
	SetDefaultEventuallyPollingInterval(constant.Interval)

	RunSpecs(t, "Project Controller Suite")
}

var _ = BeforeSuite(func() {
	ctx = goctx.Background()
	// Also the point at which preflight runs and can refuse.
	testClient = testcontext.GetConnectivity(ctx, Default).Client

	grantSecretsToProjectController()
})

// Delete blockers are runtime config, so whoever configures one grants its RBAC.
// hack/e2e-values.yaml configures the Secrets blocker, so this suite grants it.
// Generated names: two concurrent runs must not delete each other's grant.
func grantSecretsToProjectController() {
	clusterRole := resources.ClusterRole(utils.GenerateName("pc-blocker-secrets"),
		[]rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get", "list", "watch"},
		}})
	Expect(testClient.Create(ctx, clusterRole)).To(Succeed())
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, clusterRole))).To(Succeed())
	})

	binding := resources.ClusterRoleBinding(utils.GenerateName("pc-blocker-secrets"),
		clusterRole.Name, projectControllerServiceAccount, constant.ReleaseNamespace)
	Expect(testClient.Create(ctx, binding)).To(Succeed())
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, binding))).To(Succeed())
	})
}

// newProject creates a project on the default node pool and waits for it to be Ready,
// returning it with the namespace project-controller gave it.
//
// The default pool rather than one of the suite's own: no spec here schedules a workload,
// so a pool of its own would only add a scheduler deployment and a node to keep track of.
func newProject(options ...resources.ProjectOption) (*kaires.Project, string) {
	project := resources.Project(utils.GenerateName("pc-proj"),
		[]string{testcontext.DefaultNodePoolName}, options...)
	Expect(testClient.Create(ctx, project)).To(Succeed())

	// Registered before anything the project's namespace goes on to hold, so it runs
	// after: cleanup is last-in-first-out, and a project that blocks cannot finish
	// deleting until whatever blocks it has gone.
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, project))).To(Succeed())
		wait.ForDeleted(ctx, testClient, project)
	})

	return project, wait.ForProjectReady(ctx, testClient, project.Name).Status.Namespace
}
