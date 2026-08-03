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

	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	nodepoolcontroller "github.com/kai-scheduler/kai-resource-management/pkg/nodepool-controller"
)

func main() {
	logOptions := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	logOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&logOptions), zap.WriteTo(os.Stderr)))

	ctx := ctrl.SetupSignalHandler()
	if err := nodepoolcontroller.New().Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error while running the app: %v\n", err)
		os.Exit(1)
	}
}
