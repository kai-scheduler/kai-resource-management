package handlers

import (
	kaiv1alpha1 "github.com/run-ai/runai/runai-cluster/cluster/sdk/apis/kai/v1alpha1"
)

type ProjectResourceHandler interface {
	HandleResource(project kaiv1alpha1.Project) ([]kaiv1alpha1.ProjectCondition, error)
}
