// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"go.uber.org/zap/zapcore"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kai-scheduler/kai-resource-management/pkg/helmhooks"
)

const (
	applyCRDsCommand   = "apply-crds"
	applyConfigCommand = "apply-config"
	cleanupCommand     = "cleanup"
)

// clientFactory defers connecting to the cluster until the subcommand arguments are valid.
type clientFactory func() (client.Client, error)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(kaires.AddToScheme(scheme))
}

// Run executes the subcommand named by args[0] with the remaining args as its flags.
func Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("subcommand required")
	}
	command, flags := args[0], args[1:]

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{TimeEncoder: zapcore.ISO8601TimeEncoder})))
	ctx := ctrl.SetupSignalHandler()

	switch command {
	case applyCRDsCommand:
		return applyCRDs(ctx, newClusterClient, flags)
	case applyConfigCommand:
		return applyConfig(ctx, newClusterClient, flags)
	case cleanupCommand:
		return cleanup(ctx, newClusterClient, flags)
	default:
		printUsage()
		return fmt.Errorf("unknown subcommand %q", command)
	}
}

func newClusterClient() (client.Client, error) {
	k8sClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	return k8sClient, nil
}

func applyCRDs(ctx context.Context, newClient clientFactory, flags []string) error {
	if err := parseFlags(flag.NewFlagSet(applyCRDsCommand, flag.ContinueOnError), flags); err != nil {
		return err
	}
	k8sClient, err := newClient()
	if err != nil {
		return err
	}
	return helmhooks.ApplyCRDs(ctx, k8sClient)
}

func applyConfig(ctx context.Context, newClient clientFactory, flags []string) error {
	flagSet := flag.NewFlagSet(applyConfigCommand, flag.ContinueOnError)
	file := flagSet.String("file", "", "path to the KRMConfig manifest to apply")
	if err := parseFlags(flagSet, flags); err != nil {
		return err
	}
	if *file == "" {
		return missingFlagError(applyConfigCommand, "file")
	}
	k8sClient, err := newClient()
	if err != nil {
		return err
	}
	return helmhooks.ApplyConfig(ctx, k8sClient, *file)
}

func cleanup(ctx context.Context, newClient clientFactory, flags []string) error {
	flagSet := flag.NewFlagSet(cleanupCommand, flag.ContinueOnError)
	namespace := flagSet.String("namespace", "", "namespace holding the operator-managed deployments")
	deleteConfig := flagSet.String("delete-config", "", "name of the KRMConfig to delete; skipped when empty")
	if err := parseFlags(flagSet, flags); err != nil {
		return err
	}
	if *namespace == "" {
		return missingFlagError(cleanupCommand, "namespace")
	}
	k8sClient, err := newClient()
	if err != nil {
		return err
	}
	return helmhooks.Cleanup(ctx, k8sClient, *namespace, *deleteConfig)
}

// parseFlags also rejects leftover positional arguments, which the flag package otherwise ignores.
func parseFlags(flagSet *flag.FlagSet, flags []string) error {
	if err := flagSet.Parse(flags); err != nil {
		return err
	}
	if flagSet.NArg() > 0 {
		return fmt.Errorf("%s: unexpected arguments %q", flagSet.Name(), flagSet.Args())
	}
	return nil
}

func missingFlagError(command, flagName string) error {
	return fmt.Errorf("%s: --%s is required", command, flagName)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: helm-hooks <subcommand> [flags]

Subcommands:
  %s                        server-side apply the CRDs bundled in this binary
  %s --file=<path>       server-side apply the KRMConfig manifest at <path>
  %s --namespace=<ns> [--delete-config=<name>]
                                    delete the operator-managed deployments, and
                                    optionally the named KRMConfig
`, applyCRDsCommand, applyConfigCommand, cleanupCommand)
}
