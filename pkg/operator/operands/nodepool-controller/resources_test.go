// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package nodepoolcontroller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
)

// Matched on type as well as name: every object this operand builds shares the
// base name, so a name alone would just as happily find the ServiceAccount.
func monitorNamed(objects []client.Object, name string) *monitoringv1.ServiceMonitor {
	for _, object := range objects {
		if monitor, matches := object.(*monitoringv1.ServiceMonitor); matches && monitor.Name == name {
			return monitor
		}
	}
	return nil
}

var _ = Describe("Deployment", func() {
	// Pinned as one list rather than flag by flag: that is what catches an argument
	// nobody meant to add.
	It("passes exactly these flags with nothing configured", func() {
		deployment := findType[*appsv1.Deployment](desiredState(newKRMConfig()))

		Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{
			"--metrics-port", "9400",
			"--scheduler-namespace", testNamespace,
			"--enable-nodepool-validation-webhook=true",
			"--default-nodepool-name", "default",
			"--webhook-port", "8443",
			"--qps", "50",
			"--burst", "300",
		}))
	})

	It("passes the shared vocabulary only once it is set", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.NodePoolLabelKey = ptr.To("kai.scheduler/node-pool")
		krmConfig.Spec.Global.SchedulerName = ptr.To("kai-scheduler")
		krmConfig.Spec.Global.FinalizerDomain = ptr.To("kai.resources")

		args := findType[*appsv1.Deployment](desiredState(krmConfig)).Spec.Template.Spec.Containers[0].Args

		Expect(args).To(ContainElements(
			"--nodepool-label-key", "kai.scheduler/node-pool",
			"--scheduler-name", "kai-scheduler",
			"--finalizer-domain", "kai.resources"))
	})

	// Verbosity is a switch, not a level: --log-level is not a flag this binary has.
	It("passes --debug only when asked, and never --log-level", func() {
		Expect(buildArgsList(newKRMConfig())).ToNot(ContainElement("--debug"))
		Expect(buildArgsList(newKRMConfig())).ToNot(ContainElement("--log-level"))

		krmConfig := newKRMConfig()
		krmConfig.Spec.NodePoolController.Args.Debug = ptr.To(true)

		args := buildArgsList(krmConfig)

		Expect(args).To(ContainElement("--debug"))
		Expect(args).ToNot(ContainElement("--log-level"))
	})

	// The shared vocabulary on spec.global is offered to every service, but this
	// binary defines flags for only some of it. Passing one it has no flag for would
	// make it exit on an unknown argument.
	It("passes only the shared vocabulary this binary has a flag for", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.QueueLabelKey = ptr.To("kai.scheduler/queue")
		krmConfig.Spec.Global.ProjectLabelKey = ptr.To("kai.scheduler/project")
		krmConfig.Spec.Global.NamespaceProjectLabelKey = ptr.To("kai/project")
		krmConfig.Spec.Global.EnforceSchedulerAnnotationKey = ptr.To("kai/enforce")

		args := findType[*appsv1.Deployment](desiredState(krmConfig)).Spec.Template.Spec.Containers[0].Args

		Expect(args).ToNot(ContainElement("--queue-label-key"))
		Expect(args).ToNot(ContainElement("--project-label-key"))
		Expect(args).ToNot(ContainElement("--namespace-project-label-key"))
		Expect(args).ToNot(ContainElement("--enforce-scheduler-annotation-key"))
	})

	// An empty value would override the binary's own default rather than leave it alone.
	It("omits a flag whose value is set to the empty string", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.SchedulerName = ptr.To("")
		krmConfig.Spec.NodePoolController.Args.ExcludedNodepoolName = ptr.To("")

		args := findType[*appsv1.Deployment](desiredState(krmConfig)).Spec.Template.Spec.Containers[0].Args

		Expect(args).ToNot(ContainElement("--scheduler-name"))
		Expect(args).ToNot(ContainElement("--excluded-nodepool-name"))
	})

	It("passes every modelled argument", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.NodePoolController.Args = &krmv1alpha1.NodePoolControllerArgs{
			Debug:                                  ptr.To(true),
			MetricsNamespace:                       ptr.To("kai"),
			DcgmExporterNamespace:                  ptr.To("gpu-operator"),
			SchedulingShardArgs:                    ptr.To(`{"verbosity":"4"}`),
			ExcludedNodepoolName:                   ptr.To("excluded"),
			ManagedNodesConfigName:                 ptr.To("managed-nodes"),
			ToExcludeLabel:                         ptr.To("to-exclude"),
			UnschedulableLabel:                     ptr.To("unschedulable"),
			GroveTopologyAnnotation:                ptr.To("grove-topology"),
			GroveTopologyResourceVersionAnnotation: ptr.To("grove-topology-rv"),
		}

		args := findType[*appsv1.Deployment](desiredState(krmConfig)).Spec.Template.Spec.Containers[0].Args

		Expect(args).To(ContainElements(
			"--debug",
			"--metrics-namespace", "kai",
			"--dcgm-exporter-namespace", "gpu-operator",
			"--scheduling-shard-args", `{"verbosity":"4"}`,
			"--excluded-nodepool-name", "excluded",
			"--managed-nodes-config-name", "managed-nodes",
			"--to-exclude-label", "to-exclude",
			"--unschedulable-label", "unschedulable",
			"--grove-topology-annotation", "grove-topology",
			"--grove-topology-resource-version-annotation", "grove-topology-rv"))
	})

	It("appends extraArgs last, so a repeat there wins", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.NodePoolController.ExtraArgs = []string{"--metrics-port", "9500"}

		args := findType[*appsv1.Deployment](desiredState(krmConfig)).Spec.Template.Spec.Containers[0].Args

		Expect(args[len(args)-2:]).To(Equal([]string{"--metrics-port", "9500"}))
	})

	DescribeTable("leader election",
		func(mutate func(*krmv1alpha1.KRMConfig), expected bool) {
			krmConfig := newKRMConfig()
			mutate(krmConfig)

			args := findType[*appsv1.Deployment](desiredState(krmConfig)).Spec.Template.Spec.Containers[0].Args

			if expected {
				Expect(args).To(ContainElement("--leader-elect"))
				return
			}
			Expect(args).ToNot(ContainElement("--leader-elect"))
		},
		Entry("off by default", func(*krmv1alpha1.KRMConfig) {}, false),
		Entry("on from the global setting", func(c *krmv1alpha1.KRMConfig) {
			c.Spec.Global.LeaderElection = ptr.To(true)
		}, true),
		Entry("on from a replica count above one", func(c *krmv1alpha1.KRMConfig) {
			c.Spec.NodePoolController.Replicas = ptr.To(int32(2))
		}, true),
		// The per-service override is the only way back off with more than one replica.
		Entry("off from an explicit override that beats the global setting", func(c *krmv1alpha1.KRMConfig) {
			c.Spec.Global.LeaderElection = ptr.To(true)
			c.Spec.NodePoolController.Args.LeaderElect = ptr.To(false)
		}, false),
	)

	It("declares the metrics and webhook container ports", func() {
		deployment := findType[*appsv1.Deployment](desiredState(newKRMConfig()))

		Expect(deployment.Spec.Template.Spec.Containers[0].Ports).To(Equal([]corev1.ContainerPort{
			{Name: "metrics", ContainerPort: 9400},
			{Name: "webhook", ContainerPort: 8443},
		}))
	})

	It("mounts the TLS Secret the chart mints", func() {
		deployment := findType[*appsv1.Deployment](desiredState(newKRMConfig()))

		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(Equal([]corev1.VolumeMount{
			{Name: certVolumeName, MountPath: certMountPath, ReadOnly: true},
		}))
		Expect(deployment.Spec.Template.Spec.Volumes[0].Secret.SecretName).
			To(Equal("nodepool-controller-tls-secret"))
	})
})

var _ = Describe("the webhook turned off", func() {
	var objects []client.Object

	BeforeEach(func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.NodePoolController.Webhooks.EnableNodePoolValidation = ptr.To(false)
		objects = desiredState(krmConfig)
	})

	// Omitting the flag is not the same as passing false: the binary defaults it to
	// true and would start a TLS server with no certificate mounted.
	It("still spells the flag out, as false", func() {
		args := findType[*appsv1.Deployment](objects).Spec.Template.Spec.Containers[0].Args

		Expect(args).To(ContainElement("--enable-nodepool-validation-webhook=false"))
		Expect(args).ToNot(ContainElement("--webhook-port"))
	})

	It("drops the certificate the controller no longer serves with", func() {
		deployment := findType[*appsv1.Deployment](objects)

		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Volumes).To(BeEmpty())
	})

	It("drops the webhook port from the container and the Service", func() {
		Expect(findType[*appsv1.Deployment](objects).Spec.Template.Spec.Containers[0].Ports).
			To(HaveLen(1))
		Expect(findType[*corev1.Service](objects).Spec.Ports).To(HaveLen(2))
	})
})

var _ = Describe("Service", func() {
	It("publishes the node-pool metrics, metrics and webhook ports", func() {
		service := findType[*corev1.Service](desiredState(newKRMConfig()))

		Expect(service.Spec.Ports).To(Equal([]corev1.ServicePort{
			{Name: "np-metrics", Protocol: corev1.ProtocolTCP, Port: 8080, TargetPort: intstrFromInt32(8080)},
			{Name: "metrics", Protocol: corev1.ProtocolTCP, Port: 9400, TargetPort: intstrFromInt32(9400)},
			{Name: "webhook", Protocol: corev1.ProtocolTCP, Port: 443, TargetPort: intstrFromInt32(8443)},
		}))
	})

	It("carries no OpenShift serving-cert annotation off OpenShift", func() {
		service := findType[*corev1.Service](desiredState(newKRMConfig()))

		Expect(service.GetAnnotations()).ToNot(HaveKey(openshiftServingCertAnnotation))
	})

	// Nothing else asks the service-CA operator for that certificate, so without the
	// annotation the Secret is never minted and the pod never starts.
	It("asks the service-CA operator for the certificate on OpenShift", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.Openshift = ptr.To(true)

		service := findType[*corev1.Service](desiredState(krmConfig))

		Expect(service.GetAnnotations()).To(HaveKeyWithValue(
			openshiftServingCertAnnotation, "nodepool-controller-tls-secret"))
	})

	It("wants no certificate on OpenShift once the webhook is off", func() {
		krmConfig := newKRMConfig()
		krmConfig.Spec.Global.Openshift = ptr.To(true)
		krmConfig.Spec.NodePoolController.Webhooks.EnableNodePoolValidation = ptr.To(false)

		service := findType[*corev1.Service](desiredState(krmConfig))

		Expect(service.GetAnnotations()).ToNot(HaveKey(openshiftServingCertAnnotation))
	})
})

var _ = Describe("ServiceMonitors", func() {
	// The accounting label routes a monitor to exactly one Prometheus, so the primary
	// one must not carry it or the metrics reach only the accounting side.
	It("labels the accounting monitor and leaves the primary one unlabeled", func() {
		objects := desiredState(newKRMConfig())

		primary := monitorNamed(objects, defaultResourceName)
		accounting := monitorNamed(objects, defaultResourceName+accountingMonitorSuffix)

		Expect(primary.Labels).ToNot(HaveKey(accountingLabelKey))
		Expect(accounting.Labels).To(HaveKeyWithValue(accountingLabelKey, accountingLabelValue))
	})

	// The app label names the scraped Service, whatever the monitor is called.
	It("points both monitors at the same Service and endpoint", func() {
		objects := desiredState(newKRMConfig())

		for _, monitor := range []*monitoringv1.ServiceMonitor{
			monitorNamed(objects, defaultResourceName),
			monitorNamed(objects, defaultResourceName+accountingMonitorSuffix),
		} {
			Expect(monitor.Labels).To(HaveKeyWithValue("app", defaultResourceName))
			Expect(monitor.Spec.Selector.MatchLabels).To(HaveKeyWithValue("app", defaultResourceName))
			Expect(monitor.Spec.Endpoints).To(HaveLen(1))
			Expect(monitor.Spec.Endpoints[0].Port).To(Equal("metrics"))
		}
	})
})

func intstrFromInt32(value int32) intstr.IntOrString {
	return intstr.FromInt32(value)
}
