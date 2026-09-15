// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package context

import (
	"fmt"
	"sync"

	kaischedulerv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	kaitopologyv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	connectOnce      sync.Once
	errConnect       error
	kubeConfig       *rest.Config
	kubeClientset    *kubernetes.Clientset
	controllerClient client.Client
	testScheme       *runtime.Scheme
)

// scheme registers every kind the suites read or write. An unregistered kind
// fails at call time with "no kind is registered", not at startup.
func buildScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,      // Node, Pod, Namespace
		kaires.AddToScheme,              // Project, Department, NodePool, ManagedNodesConfig
		kaischedulerv1.AddToScheme,      // SchedulingShard
		kaitopologyv1alpha1.AddToScheme, // Topology
		kaiv2.AddToScheme,               // Queue
		kaiv2alpha2.AddToScheme,         // PodGroup
		monitoringv1.AddToScheme,        // ServiceMonitor
	} {
		if err := add(scheme); err != nil {
			return nil, err
		}
	}

	return scheme, nil
}

// connect builds the clients once per process from the ambient KUBECONFIG.
func connect() error {
	connectOnce.Do(func() {
		testScheme, errConnect = buildScheme()
		if errConnect != nil {
			errConnect = fmt.Errorf("building scheme: %w", errConnect)
			return
		}

		kubeConfig, errConnect = config.GetConfig()
		if errConnect != nil {
			errConnect = fmt.Errorf("loading kubeconfig: %w", errConnect)
			return
		}

		kubeClientset, errConnect = kubernetes.NewForConfig(kubeConfig)
		if errConnect != nil {
			errConnect = fmt.Errorf("building the clientset: %w", errConnect)
			return
		}

		controllerClient, errConnect = client.New(kubeConfig, client.Options{Scheme: testScheme})
		if errConnect != nil {
			errConnect = fmt.Errorf("building the controller-runtime client: %w", errConnect)
		}
	})

	return errConnect
}
