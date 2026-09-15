// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"flag"
	"os"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/dependencies"
)

type Options struct {
	MetricsAddr             string
	ProbeAddr               string
	EnableLeaderElection    bool
	Qps                     int
	Burst                   int
	DependencyCheckInterval time.Duration
	MinimumSchedulerVersion string
	ZapOptions              zap.Options
}

func SetOptions() (*Options, error) {
	return parse(flag.CommandLine, os.Args[1:])
}

func parse(flagSet *flag.FlagSet, options []string) (*Options, error) {
	opts := &Options{}
	if err := opts.parseCommandLineArgs(flagSet, options); err != nil {
		return nil, err
	}
	return opts, nil
}

func (opts *Options) parseCommandLineArgs(flagSet *flag.FlagSet, options []string) error {
	flagSet.StringVar(&opts.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flagSet.StringVar(&opts.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flagSet.BoolVar(&opts.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flagSet.IntVar(&opts.Qps, "qps", 50, "Queries per second to the K8s API server")
	flagSet.IntVar(&opts.Burst, "burst", 300, "Burst to the K8s API server")
	flagSet.DurationVar(&opts.DependencyCheckInterval, "dependency-check-interval", time.Minute,
		"How often to re-check what the installation depends on and refresh the "+
			"DependenciesFulfilled condition. Nothing watches those components, so this "+
			"also bounds how long it takes to notice one coming back. Zero disables the "+
			"periodic re-check.")
	flagSet.StringVar(&opts.MinimumSchedulerVersion, "min-kai-scheduler-version",
		dependencies.DefaultMinimumSchedulerVersion,
		"Oldest KAI Scheduler this release supports. An older one is reported on the "+
			"KRMConfig DependenciesFulfilled condition. Set 0.0.0 to accept any version, "+
			"which is what an air-gapped installation with re-tagged images wants.")

	opts.ZapOptions = zap.Options{
		Development: true,
	}
	opts.ZapOptions.BindFlags(flagSet)
	return flagSet.Parse(options)
}
