// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/kai-scheduler/kai-resource-management/cmd/nodepool-controller/app"
	"github.com/kai-scheduler/kai-resource-management/cmd/nodepool-controller/version"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/cachedclient"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/managed-nodes-config"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/nodepool_controller"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/nodepool_controller/metrics"
	scheme_init "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/scheme"
	"github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/topology_controller"
	nodepoolwebhook "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller/webhook"
)

var (
	scheme   = scheme_init.Scheme()
	setupLog = ctrl.Log.WithName("setup")
)

// bindZapFlags registers zap's flags before the single flag.Parse, so that the
// zap flags are accepted rather than rejected as undefined.
func bindZapFlags() *zap.Options {
	opts := &zap.Options{
		Development:     true,
		StacktraceLevel: zapcore.LevelEnabler(zapcore.FatalLevel),
		TimeEncoder:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")}
	opts.BindFlags(flag.CommandLine)

	return opts
}

func initLogging(debugLogLevel bool, zapOptions *zap.Options) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	if !debugLogLevel {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(zapOptions)))
}

func main() {
	ops, npConfig := app.BindFlags()
	zapOptions := bindZapFlags()
	flag.Parse()
	config.SetCurrent(npConfig)

	initLogging(ops.DebugLogLevel, zapOptions)

	log.Info().Msg("KAI NodePool Controller")

	nodepoolControllerParams, err := parseNodePoolControllerParams()
	if err != nil {
		setupLog.Error(err, "unable to parse args")
		os.Exit(1)
	}

	clientConfig := ctrl.GetConfigOrDie()
	clientConfig.QPS = float32(ops.K8sClientConfigQPS)
	clientConfig.Burst = ops.K8sClientConfigBurst

	managerOptions := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: fmt.Sprintf(":%s", ops.MetricsPort),
		},
		LeaderElection:   ops.EnableLeaderElection,
		LeaderElectionID: fmt.Sprintf("node-pool-controller.%s", config.Get().FinalizerDomain),
	}

	if ops.EnableNodepoolValidationWebhook {
		managerOptions.WebhookServer = &webhook.DefaultServer{
			Options: webhook.Options{
				Port:    ops.WebhookPort,
				CertDir: ops.WebhookCertDir,
			}}
	}

	mgr, err := ctrl.NewManager(clientConfig, managerOptions)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	stopper := make(chan struct{})
	defer close(stopper)
	managerContext := ctrl.SetupSignalHandler()

	nodePoolController := nodepool_controller.NewNodePoolController(
		mgr.GetClient(),
		mgr.GetScheme(),
		nodepoolControllerParams)
	err = nodePoolController.SetupWithManager(managerContext, mgr)
	if err != nil {
		setupLog.Error(err, "Error starting NodePoolController")
		os.Exit(1) //nolint:gocritic // exitAfterDefer
	}

	managedNodesConfigController := managed_nodes_config.NewManagedNodesConfigController(
		mgr.GetClient(),
		mgr.GetScheme(),
		nodePoolController)
	err = managedNodesConfigController.SetupWithManager(managerContext, mgr)
	if err != nil {
		setupLog.Error(err, "Error starting ManagedNodesConfigController")
		os.Exit(1)
	}

	if ops.EnableNodepoolValidationWebhook {
		if err = nodepoolwebhook.SetupNodePoolWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "Error setting up NodePool validating webhook")
			os.Exit(1)
		}
		setupLog.Info("NodePool validation webhook enabled",
			"port", ops.WebhookPort, "certDir", ops.WebhookCertDir)
	}

	go func() {
		<-mgr.Elected()
		setupLog.Info("Leader elected")
		metrics.Init()
		cachedClient := cachedclient.NewCachedClient(managerContext, mgr.GetClient(), ctrl.GetConfigOrDie(), scheme)
		metricsWatch, err := nodepool_controller.InitMetricsWatch(cachedClient)
		if err != nil {
			setupLog.Error(err, "Error starting NodePoolController metrics handler")
			os.Exit(1)
		}
		metricsWatch.Start(managerContext, stopper)

		topology_controller.StartWhenCRDReady(managerContext, mgr.GetAPIReader(), func() {
			tc := topology_controller.NewKaiTopologyController(mgr.GetClient())
			if err := tc.SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "Failed to start KAI topology controller")
				os.Exit(1)
			}
			setupLog.Info("KAI topology controller started successfully")
		})
	}()

	printVersion()
	log.Info().Msgf("Starting NodePoolController")
	if err := mgr.Start(managerContext); err != nil {
		setupLog.Error(err, "Error running manager")
		os.Exit(1)
	}
}

func printVersion() {
	versionInfo := version.GetVersion()
	setupLog.Info(fmt.Sprintf("Version: %s", versionInfo.Version))
	if strings.HasSuffix(versionInfo.Version, "-DEVELOPMENT") {
		return
	}
	if versionInfo.BuildDate != "" {
		setupLog.Info(fmt.Sprintf("BuildDate: %s", versionInfo.BuildDate))
	}
	if versionInfo.GitCommit != "" {
		setupLog.Info(fmt.Sprintf("Commit Hash: %s", versionInfo.GitCommit))
	}
	if versionInfo.GitTag != "" {
		setupLog.Info(fmt.Sprintf("Tag: %s", versionInfo.GitTag))
	}
	setupLog.Info(fmt.Sprintf("Go Version: %s", versionInfo.GoVersion))
	setupLog.Info(fmt.Sprintf("Platform: %s", versionInfo.Platform))
}

func parseNodePoolControllerParams() (*common.NodePoolControllerParams, error) {
	schedulingShardArgs, err := app.ParseSchedulingShardArgs(config.Get().SchedulingShardArgsStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse scheduler args: %w", err)
	}

	uninstallDetection, err := app.ParseUninstallDetectionRef(config.Get().UninstallDetectionRefStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse uninstall-detection-ref: %w", err)
	}

	nodePoolControllerParams := &common.NodePoolControllerParams{
		SchedulingShardArgs: schedulingShardArgs,
		UninstallDetection:  uninstallDetection,
	}
	return nodePoolControllerParams, nil
}
