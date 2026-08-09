// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// TEMPORARY: placeholder entry point. It exists only so the build, image and
// release infrastructure has a binary to produce. Replace it with the real
// nodepool-controller wiring.
package main

import (
	"flag"
	"fmt"
	"os"

	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	nodepoolcontroller "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(kaires.AddToScheme(scheme))
}

func main() {
	logOptions := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOptions), zap.WriteTo(os.Stderr)))

	ctx := ctrl.SetupSignalHandler()
	if err := nodepoolcontroller.New(scheme).Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error while running the app: %v\n", err)
		os.Exit(1)
	}
}
