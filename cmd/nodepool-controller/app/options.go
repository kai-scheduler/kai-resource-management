// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
)

const (
	defaultDcgmExporterNamespace = "gpu-operator"
	defaultWebhookCertDir        = "/etc/webhook/certs"
	defaultWebhookPort           = 8443
)

// Options is the main context object for the controller manager.
type Options struct {
	DebugLogLevel                   bool
	DcgmExporterNamespace           string
	RestrictNodeScheduling          bool
	MetricsPort                     string
	K8sClientConfigQPS              int
	K8sClientConfigBurst            int
	EnableLeaderElection            bool
	EnableNodepoolValidationWebhook bool
	WebhookPort                     int
	WebhookCertDir                  string
}

func BindFlags() (*Options, *config.NodePoolControllerConfig) {
	options := &Options{}
	flag.BoolVar(&options.DebugLogLevel, "debug", false, "Should use debug log level")
	flag.StringVar(&options.DcgmExporterNamespace, "dcgm-exporter-namespace", defaultDcgmExporterNamespace, "Namespace of dcgm-exporter")
	flag.BoolVar(&options.RestrictNodeScheduling, "restrict-node-scheduling", false, "deprecated- the scheduler will allocate jobs only to restricted nodes")
	flag.StringVar(&options.MetricsPort, "metrics-port", "9400", "The port to serve metrics from")
	flag.IntVar(&options.K8sClientConfigQPS, "qps", 50, "Queries per second to the K8s API server")
	flag.IntVar(&options.K8sClientConfigBurst, "burst", 300, "Burst to the K8s API server")
	flag.BoolVar(&options.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&options.EnableNodepoolValidationWebhook, "enable-nodepool-validation-webhook", true,
		"Enable the NodePool validating webhook that enforces the labelKey/labelValue rules. "+
			"When enabled, serving certs must be present in --cert-dir.")
	flag.IntVar(&options.WebhookPort, "webhook-port", defaultWebhookPort, "The port the validating webhook server serves on")
	flag.StringVar(&options.WebhookCertDir, "cert-dir", defaultWebhookCertDir,
		"Directory that contains the server key and certificate for the validating webhook")

	npConfig := &config.NodePoolControllerConfig{}
	config.AddLabelFlags(flag.CommandLine, npConfig)

	return options, npConfig
}

// ParseSchedulingShardArgs decodes the --scheduling-shard-args flag: a JSON map of KAI
// scheduling-shard args merged into every SchedulingShard. An empty flag (e.g. an
// open-source install that sets nothing) returns an empty map, so the scheduler
// runs on its built-in defaults.
func ParseSchedulingShardArgs(schedulingShardArgsStr string) (map[string]string, error) {
	args := map[string]string{}
	if schedulingShardArgsStr == "" {
		return args, nil
	}
	err := json.Unmarshal([]byte(schedulingShardArgsStr), &args)
	return args, err
}

// ParseUninstallDetectionRef decodes the --uninstall-detection-ref flag,
// "group/version/Kind/namespace/name". An empty flag returns nil, disabling the
// check; an empty namespace segment means a cluster-scoped resource.
func ParseUninstallDetectionRef(refStr string) (*common.UninstallDetectionRef, error) {
	if refStr == "" {
		return nil, nil
	}

	group, version, kind, namespace, name, ok := splitRef(refStr)
	if !ok {
		return nil, fmt.Errorf("expected group/version/Kind/namespace/name, got %q", refStr)
	}

	return &common.UninstallDetectionRef{
		GVK: schema.GroupVersionKind{Group: group, Version: version, Kind: kind},
		Key: types.NamespacedName{Name: name, Namespace: namespace},
	}, nil
}

// splitRef reports ok only when all five segments are present and every one but
// the namespace is non-empty.
func splitRef(refStr string) (group, version, kind, namespace, name string, ok bool) {
	parts := strings.Split(refStr, "/")
	if len(parts) != 5 {
		return "", "", "", "", "", false
	}
	group, version, kind, namespace, name = parts[0], parts[1], parts[2], parts[3], parts[4]
	if slices.Contains([]string{group, version, kind, name}, "") {
		return "", "", "", "", "", false
	}
	return group, version, kind, namespace, name, true
}
