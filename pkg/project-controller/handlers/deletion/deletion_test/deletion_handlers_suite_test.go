// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deletion_test

import (
	"testing"

	kaiv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	kaiv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/project-controller/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// Test Globals
var scheme *runtime.Scheme

var _ = BeforeSuite(func() {
	scheme = runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).Should(Succeed())
	Expect(kaiv1alpha1.AddToScheme(scheme)).Should(Succeed())
	Expect(kaiv2.AddToScheme(scheme)).Should(Succeed())

	config.SetForTest(test.ConfigForTests())
})

func TestDeletionHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Project Resource Deletion Handlers Tests")
}

type TestDeletionHandler struct {
	HandlerCalled bool
}

func (handler *TestDeletionHandler) OnDelete(project *kaiv1alpha1.Project) ([]kaiv1alpha1.ProjectCondition, error) {
	handler.HandlerCalled = true
	return []kaiv1alpha1.ProjectCondition{}, nil
}
