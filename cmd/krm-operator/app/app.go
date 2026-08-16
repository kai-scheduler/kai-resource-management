// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package app

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/kai-scheduler/kai-resource-management/cmd/krm-operator/config"
	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/controller"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(krmv1alpha1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(vpav1.AddToScheme(scheme))
}

type App struct {
	manager             manager.Manager
	krmConfigReconciler *controller.KRMConfigReconciler
}

func New() (*App, error) {
	opts, err := config.SetOptions()
	if err != nil {
		setupLog.Error(err, "unable to parse arguments")
		return nil, err
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts.ZapOptions)))

	clientConfig := ctrl.GetConfigOrDie()
	clientConfig.QPS = float32(opts.Qps)
	clientConfig.Burst = opts.Burst

	mgr, err := ctrl.NewManager(clientConfig, ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			// Managed fields are a large share of a cached object and nothing here
			// reads them.
			DefaultTransform: cache.TransformStripManagedFields(),
		},
		Metrics: server.Options{
			BindAddress: opts.MetricsAddr,
		},
		HealthProbeBindAddress: opts.ProbeAddr,
		LeaderElection:         opts.EnableLeaderElection,
		LeaderElectionID:       "krm-operator.kai.resources",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return nil, err
	}

	return &App{
		manager:             mgr,
		krmConfigReconciler: controller.NewKRMConfigReconciler(mgr.GetClient(), mgr.GetScheme()),
	}, nil
}

func (app *App) InitOperands(krmConfigOperands []operands.Operand) {
	app.krmConfigReconciler.SetOperands(krmConfigOperands)
}

func (app *App) Run() error {
	// One context for both: SetupWithManager registers the field indexes, which
	// must be in place before the caches it shares with the manager start.
	ctx := ctrl.SetupSignalHandler()

	if err := app.krmConfigReconciler.SetupWithManager(ctx, app.manager); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KRMConfig")
		return err
	}

	if err := app.manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := app.manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err := app.manager.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
