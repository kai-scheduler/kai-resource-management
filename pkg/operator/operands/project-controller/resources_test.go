// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package projectcontroller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

var _ = Describe("buildArgsList", func() {
	// Pinned as one list so an unintended extra argument fails the test.
	It("passes exactly these flags for a stock installation", func() {
		Expect(buildArgsList(newKRMConfig())).To(Equal([]string{
			"--metrics-port", "9400",
			"--profiler-api-port", "8182",
			"--rolebindings-configmap-name", "project-controller-rolebindings-plugin",
			"--rolebindings-configmap-namespace", testNamespace,
			"--project-delete-blockers-namespace", testNamespace,
			"--install-namespace", testNamespace,
			"--default-nodepool-name", "default",
			"--enable-project-validation-webhook=true",
			"--enable-department-validation-webhook=true",
			"--webhook-port", "8443",
			"--namespaces",
			"--role-bindings",
			"--cluster-wide-secrets",
			"--cluster-wide-pvcs",
			"--cluster-wide-config-maps",
			"--qps", "50",
			"--burst", "300",
		}))
	})

	// The scheduler vocabulary is unset until resolved, and an unset flag must not
	// be passed at all: an empty value would override the binary's own default.
	It("omits the scheduler vocabulary while it is unset", func() {
		args := buildArgsList(newKRMConfig())

		Expect(args).ToNot(ContainElement("--nodepool-label-key"))
		Expect(args).ToNot(ContainElement("--queue-label-key"))
		Expect(args).ToNot(ContainElement("--project-label-key"))
	})

	It("passes the shared vocabulary once it is set", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.NodePoolLabelKey = ptr.To("acme/node-pool")
		krmConfig.Spec.Global.QueueLabelKey = ptr.To("acme/queue")
		krmConfig.Spec.Global.ProjectLabelKey = ptr.To("acme/project")
		krmConfig.Spec.Global.FinalizerDomain = ptr.To("acme.io")

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElements("--nodepool-label-key", "acme/node-pool"))
		Expect(args).To(ContainElements("--queue-label-key", "acme/queue"))
		Expect(args).To(ContainElements("--project-label-key", "acme/project"))
		Expect(args).To(ContainElements("--finalizer-domain", "acme.io"))
	})

	// project-controller's binary has no --scheduler-name flag, so the global
	// setting must not reach it even though other services take it.
	It("never passes the scheduler name", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.SchedulerName = ptr.To("my-scheduler")

		Expect(buildArgsList(krmConfig)).ToNot(ContainElement("--scheduler-name"))
	})

	// The controller takes --install-namespace; --scheduler-namespace is another
	// service's flag and this binary would reject it.
	It("names its own namespace flag, not the scheduler's", func() {
		args := buildArgsList(newKRMConfig())

		Expect(args).To(ContainElements("--install-namespace", testNamespace))
		Expect(args).ToNot(ContainElement("--scheduler-namespace"))
	})

	// The controller reads both ConfigMaps out of the namespace it is installed in.
	It("points the ConfigMap flags at the install namespace", func() {
		args := buildArgsList(newKRMConfig())

		Expect(args).To(ContainElements("--rolebindings-configmap-namespace", testNamespace))
		Expect(args).To(ContainElements("--project-delete-blockers-namespace", testNamespace))
		Expect(args).To(ContainElements("--rolebindings-configmap-name", roleBindingsConfigMapName))
	})

	It("passes qps and burst from the client config", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Service.K8sClientConfig.QPS = ptr.To(100)
		krmConfig.Spec.ProjectController.Service.K8sClientConfig.Burst = ptr.To(200)

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElements("--qps", "100"))
		Expect(args).To(ContainElements("--burst", "200"))
	})

	It("drops the feature flags that are turned off", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Features.ClusterWideSecret = ptr.To(false)
		krmConfig.Spec.ProjectController.Features.LimitRange = ptr.To(true)

		args := buildArgsList(krmConfig)

		Expect(args).ToNot(ContainElement("--cluster-wide-secrets"))
		Expect(args).To(ContainElement("--limit-range"))
	})

	// Off is expressed as =false rather than by omission, because the flag also
	// tells the binary not to start its TLS server.
	It("spells the webhook toggles out either way", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Webhooks.EnableDepartmentValidation = ptr.To(false)

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElement("--enable-project-validation-webhook=true"))
		Expect(args).To(ContainElement("--enable-department-validation-webhook=false"))
		Expect(args).To(ContainElement("--webhook-port"))
	})

	It("omits the webhook port when neither webhook is served", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Webhooks.EnableProjectValidation = ptr.To(false)
		krmConfig.Spec.ProjectController.Webhooks.EnableDepartmentValidation = ptr.To(false)

		Expect(buildArgsList(krmConfig)).ToNot(ContainElement("--webhook-port"))
	})

	It("switches the profiler on with its port", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Profiling.Enabled = ptr.To(true)

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElement("--enable-profiling"))
		Expect(args).To(ContainElements("--profiler-api-port", "8182"))
	})

	// Verbosity is a switch, not a level: --log-level is not a flag this binary has.
	It("passes --debug only when asked, and never --log-level", func() {
		Expect(buildArgsList(newKRMConfig())).ToNot(ContainElement("--debug"))
		Expect(buildArgsList(newKRMConfig())).ToNot(ContainElement("--log-level"))

		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Args.Debug = ptr.To(true)

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElement("--debug"))
		Expect(args).ToNot(ContainElement("--log-level"))
	})

	It("passes the controller's own label keys", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.Args.ProjectNamePrefix = ptr.To("acme")
		krmConfig.Spec.ProjectController.Args.LimitRangeName = ptr.To("acme-limits")

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElements("--project-name-prefix", "acme"))
		Expect(args).To(ContainElements("--limit-range-name", "acme-limits"))
	})

	It("appends extraArgs last so they win", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.ProjectController.ExtraArgs = []string{"--qps", "500"}

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
		krmConfig.Spec.ProjectController.Replicas = ptr.To(int32(2))

		Expect(buildArgsList(krmConfig)).To(ContainElement("--leader-elect"))
	})

	// A per-service false beats a global true, so one service can opt out.
	It("lets the service override the global setting", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.LeaderElection = ptr.To(true)
		krmConfig.Spec.ProjectController.Args.LeaderElect = ptr.To(false)

		Expect(buildArgsList(krmConfig)).ToNot(ContainElement("--leader-elect"))
	})
})

var _ = Describe("ports", func() {
	It("has the container listen on the ports the Service targets", func() {
		config := newKRMConfig().Spec.ProjectController

		containers := containerPorts(config)
		services := servicePorts(config)

		Expect(containers).To(HaveLen(2))
		Expect(services).To(HaveLen(2))
		for index := range services {
			Expect(services[index].TargetPort.IntVal).To(Equal(containers[index].ContainerPort),
				"Service port %s targets a port the container does not listen on", services[index].Name)
		}
	})

	It("publishes 443 for a webhook served on 8443", func() {
		services := servicePorts(newKRMConfig().Spec.ProjectController)

		Expect(services[1].Port).To(Equal(int32(443)))
		Expect(services[1].TargetPort.IntVal).To(Equal(int32(8443)))
	})
})

var _ = Describe("the operand contract", func() {
	It("reports its name", func() {
		Expect((&ProjectController{}).Name()).To(Equal("ProjectController"))
	})

	It("has no dependencies and nothing to monitor", func() {
		operand := &ProjectController{}
		krmConfig := newKRMConfig()

		missing, err := operand.HasMissingDependencies(context.Background(), newClient(), krmConfig)
		Expect(err).ToNot(HaveOccurred())
		Expect(missing).To(BeEmpty())
		Expect(operand.Monitor(context.Background(), newClient(), krmConfig)).To(Succeed())
	})

	It("reports nothing deployed before DesiredState has run", func() {
		deployed, err := (&ProjectController{}).IsDeployed(context.Background(), newClient())

		Expect(err).ToNot(HaveOccurred())
		Expect(deployed).To(BeTrue(), "an empty desired state is vacuously deployed")
	})
})

var _ = Describe("DeleteBlocker wire contract", func() {
	// The controller parses these names out of the ConfigMap; renaming a json tag
	// here silently stops it recognising the blocker.
	It("keeps the field names the controller reads", func() {
		blocker := krmv1alpha1.DeleteBlocker{
			DisplayName: "Workloads", Group: "run.ai", Version: "v2alpha1", Kind: "TrainingWorkload",
		}

		encoded, err := yaml.Marshal(blocker)

		Expect(err).ToNot(HaveOccurred())
		Expect(string(encoded)).To(Equal(
			"displayName: Workloads\ngroup: run.ai\nkind: TrainingWorkload\nversion: v2alpha1\n"))
	})
})
