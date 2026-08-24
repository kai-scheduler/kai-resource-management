// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package context

import (
	"fmt"
	"sync"

	kaischedulerv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
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
	connectErr       error
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
		clientgoscheme.AddToScheme, // Node, Pod, Namespace
		kaires.AddToScheme,         // Project, Department, NodePool, ManagedNodesConfig
		kaischedulerv1.AddToScheme, // SchedulingShard
		kaiv2.AddToScheme,          // Queue
		kaiv2alpha2.AddToScheme,    // PodGroup
		monitoringv1.AddToScheme,   // ServiceMonitor
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
		testScheme, connectErr = buildScheme()
		if connectErr != nil {
			connectErr = fmt.Errorf("building scheme: %w", connectErr)
			return
		}

		kubeConfig, connectErr = config.GetConfig()
		if connectErr != nil {
			connectErr = fmt.Errorf("loading kubeconfig: %w", connectErr)
			return
		}

		kubeClientset, connectErr = kubernetes.NewForConfig(kubeConfig)
		if connectErr != nil {
			connectErr = fmt.Errorf("building the clientset: %w", connectErr)
			return
		}

		controllerClient, connectErr = client.New(kubeConfig, client.Options{Scheme: testScheme})
		if connectErr != nil {
			connectErr = fmt.Errorf("building the controller-runtime client: %w", connectErr)
		}
	})

	return connectErr
}
