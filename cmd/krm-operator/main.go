// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/kai-scheduler/kai-resource-management/cmd/krm-operator/app"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/controller"
)

func main() {
	operatorApp, err := app.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create app: %v\n", err)
		os.Exit(1)
	}

	operatorApp.InitOperands(controller.KRMConfigReconcilerOperands)

	if err := operatorApp.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error while running the app: %v\n", err)
		os.Exit(1)
	}
}
