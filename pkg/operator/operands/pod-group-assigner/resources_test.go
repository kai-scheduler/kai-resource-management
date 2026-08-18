// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgroupassigner

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("buildArgsList", func() {
	// Pinned as one list so an unintended extra argument fails the test.
	It("passes exactly these flags for a stock installation", func() {
		Expect(buildArgsList(newKRMConfig())).To(Equal([]string{
			"--webhook-port", "8443",
			"--enable-pod-webhook=true",
			"--default-nodepool-name", "default",
			"--qps", "50",
			"--burst", "300",
		}))
	})

	// The scheduler vocabulary is unset until resolved, and an unset flag must not
	// be passed at all: an empty value would override the binary's own default.
	It("omits the vocabulary while it is unset", func() {
		args := buildArgsList(newKRMConfig())

		Expect(args).ToNot(ContainElement("--nodepool-label-key"))
		Expect(args).ToNot(ContainElement("--queue-label-key"))
		Expect(args).ToNot(ContainElement("--scheduler-name"))
		Expect(args).ToNot(ContainElement("--project-label-key"))
	})

	// Unlike project-controller, this service does take the scheduler name.
	It("passes the shared vocabulary once it is set", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.SchedulerName = ptr.To("my-scheduler")
		krmConfig.Spec.Global.NodePoolLabelKey = ptr.To("acme/node-pool")
		krmConfig.Spec.Global.QueueLabelKey = ptr.To("acme/queue")
		krmConfig.Spec.Global.ProjectLabelKey = ptr.To("acme/project")
		krmConfig.Spec.Global.NamespaceProjectLabelKey = ptr.To("acme/ns-project")
		krmConfig.Spec.Global.EnforceSchedulerAnnotationKey = ptr.To("acme/enforce")

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElements("--scheduler-name", "my-scheduler"))
		Expect(args).To(ContainElements("--nodepool-label-key", "acme/node-pool"))
		Expect(args).To(ContainElements("--queue-label-key", "acme/queue"))
		Expect(args).To(ContainElements("--project-label-key", "acme/project"))
		Expect(args).To(ContainElements("--namespace-project-label-key", "acme/ns-project"))
		Expect(args).To(ContainElements("--enforce-scheduler-annotation-key", "acme/enforce"))
	})

	// This binary has no --finalizer-domain, so the shared setting must not reach it.
	It("never passes the finalizer domain", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.FinalizerDomain = ptr.To("acme.io")

		Expect(buildArgsList(krmConfig)).ToNot(ContainElement("--finalizer-domain"))
	})

	It("passes its own label and annotation keys", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.PodGroupAssigner.Args.UnexistingNodepoolSentinel = ptr.To("acme-none")
		krmConfig.Spec.PodGroupAssigner.Args.AnnotationNodepoolsKey = ptr.To("acme/node-pools")

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElements("--unexisting-nodepool-sentinel", "acme-none"))
		Expect(args).To(ContainElements("--annotation-nodepools-key", "acme/node-pools"))
	})

	// Off is expressed as =false rather than by omission, because the flag decides
	// whether the Pod handler is registered at all.
	It("spells the pod webhook toggle out either way", func() {
		Expect(buildArgsList(newKRMConfig())).To(ContainElement("--enable-pod-webhook=true"))

		krmConfig := newKRMConfig()
		krmConfig.Spec.PodGroupAssigner.Webhooks.EnablePodWebhook = ptr.To(false)

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElement("--enable-pod-webhook=false"))
		// The server still runs, so the port is still needed.
		Expect(args).To(ContainElements("--webhook-port", "8443"))
	})

	// Verbosity is a switch, not a level: --log-level is not a flag this binary has.
	It("passes --debug only when asked, and never --log-level", func() {
		Expect(buildArgsList(newKRMConfig())).ToNot(ContainElement("--debug"))

		krmConfig := newKRMConfig()
		krmConfig.Spec.PodGroupAssigner.Args.Debug = ptr.To(true)

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElement("--debug"))
		Expect(args).ToNot(ContainElement("--log-level"))
	})

	It("passes qps and burst from the client config", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.PodGroupAssigner.Service.K8sClientConfig.QPS = ptr.To(100)
		krmConfig.Spec.PodGroupAssigner.Service.K8sClientConfig.Burst = ptr.To(200)

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElements("--qps", "100"))
		Expect(args).To(ContainElements("--burst", "200"))
	})

	It("appends extraArgs last so they win", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.PodGroupAssigner.ExtraArgs = []string{"--qps", "500"}

		args := buildArgsList(krmConfig)

		Expect(args[len(args)-2:]).To(Equal([]string{"--qps", "500"}))
	})

	It("switches to JSON logging with the global setting", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.JSONLog = ptr.To(true)

		Expect(buildArgsList(krmConfig)).To(ContainElement("--zap-devel=false"))
	})
})

var _ = Describe("leader election", func() {
	It("is off for a single replica by default", func() {
		Expect(buildArgsList(newKRMConfig())).ToNot(ContainElement("--leader-elect"))
	})

	It("follows the global setting", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.LeaderElection = ptr.To(true)

		Expect(buildArgsList(krmConfig)).To(ContainElement("--leader-elect"))
	})

	// Two replicas acting at once would fight, so replicas force it on regardless.
	It("is forced on by more than one replica", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.PodGroupAssigner.Replicas = ptr.To(int32(2))

		Expect(buildArgsList(krmConfig)).To(ContainElement("--leader-elect"))
	})

	// A per-service false beats a global true, so one service can opt out.
	It("lets the service override the global setting", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.LeaderElection = ptr.To(true)
		krmConfig.Spec.PodGroupAssigner.Args.LeaderElect = ptr.To(false)

		Expect(buildArgsList(krmConfig)).ToNot(ContainElement("--leader-elect"))
	})
})

var _ = Describe("the operand contract", func() {
	It("reports its name", func() {
		Expect((&PodGroupAssigner{}).Name()).To(Equal("PodGroupAssigner"))
	})
})
