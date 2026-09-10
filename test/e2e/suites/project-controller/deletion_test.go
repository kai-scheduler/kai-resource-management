// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package project_controller covers the project-controller: what holds a project back
// from being deleted, and what does not.
package project_controller

import (
	"fmt"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/resources"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/wait"
)

// The condition the configured Secrets blocker reports, derived from its display name in
// hack/e2e-values.yaml: "Secrets" gives "SecretsReady" and "SecretsDeletionHandlerFailed".
const (
	secretsReadyCondition = kaires.ProjectConditionType("SecretsReady")
	secretsBlockedReason  = "SecretsDeletionHandlerFailed"
)

// newBlockingSecret puts a secret carrying the ownership labels into the namespace, which
// is what the configured blocker selects on.
func newBlockingSecret(namespace string) *corev1.Secret {
	secret := resources.Secret(utils.GenerateName("pc-secret"), namespace)
	Expect(testClient.Create(ctx, secret)).To(Succeed())

	// A spec that fails before deleting the secret would otherwise leave its project
	// blocked forever, since the blocker keeps the finalizer on.
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, secret))).To(Succeed())
	})

	return secret
}

var _ = Describe("Deleting a project", Label("project-controller"), func() {
	Context("that blocks on what its namespace still holds", func() {
		It("goes when nothing in the namespace blocks it", func() {
			project, _ := newProject(resources.WithBlockingDeletion())

			Expect(testClient.Delete(ctx, project)).To(Succeed())

			wait.ForDeleted(ctx, testClient, project)
		})

		It("stays while an owned secret is there, and goes once the secret does", func() {
			project, namespace := newProject(resources.WithBlockingDeletion())
			secret := newBlockingSecret(namespace)

			// Wait for the controller's finalizer first: without it the delete below
			// would simply succeed, and the spec would fail for a reason that is not
			// the one under test.
			Eventually(func(g Gomega) {
				latest := &kaires.Project{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: project.Name}, latest)).To(Succeed())
				g.Expect(latest.Finalizers).ToNot(BeEmpty())
			}).Should(Succeed())

			Expect(testClient.Delete(ctx, project)).To(Succeed())

			blocked := wait.ForProjectCondition(ctx, testClient, project.Name,
				secretsReadyCondition, corev1.ConditionFalse)
			Expect(blocked.Reason).To(Equal(secretsBlockedReason))
			Expect(blocked.Message).To(Equal(fmt.Sprintf(
				"The project couldn't be deleted because the following needs to be deleted first:\nSecret %s\n",
				secret.Name)))

			Expect(testClient.Delete(ctx, secret)).To(Succeed())

			wait.ForDeleted(ctx, testClient, project)
		})
	})

	Context("that blocks but is forced", func() {
		It("goes even though an owned secret is there", func() {
			// The blocker still runs and still reports; force is what stops its error
			// from holding the finalizer. That is what separates this from the
			// non-blocking case below, where the blocker is never consulted at all.
			project, namespace := newProject(
				resources.WithBlockingDeletion(), resources.WithForceDelete())
			newBlockingSecret(namespace)

			Expect(testClient.Delete(ctx, project)).To(Succeed())

			wait.ForDeleted(ctx, testClient, project)
		})
	})

	Context("that does not block", func() {
		It("goes even though an owned secret is there", func() {
			// Same secret as the two above, with no deletionType at all, so the blocker
			// is never consulted rather than consulted and overridden.
			project, namespace := newProject()
			newBlockingSecret(namespace)

			Expect(testClient.Delete(ctx, project)).To(Succeed())

			wait.ForDeleted(ctx, testClient, project)
		})
	})
})
