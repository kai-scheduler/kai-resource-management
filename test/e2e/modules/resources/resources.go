// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package resources builds the objects the e2e suites create. Every builder
// stamps the ownership labels, so a resource cannot reach the cluster without
// being visible to preflight and to cleanup.
package resources

import (
	"fmt"
	"strings"

	kaitopologyv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	kaires "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	projectcommon "github.com/kai-scheduler/kai-resource-management/pkg/project-controller/common"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/constant"
	"github.com/kai-scheduler/kai-resource-management/test/e2e/modules/utils"
)

// PauseImage is a container that starts and does nothing, which is all these
// suites need: they assert on placement and admission, never on workload output.
const PauseImage = "registry.k8s.io/pause:3.9"

func objectMeta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:   name,
		Labels: constant.OwnerLabels(),
	}
}

// NodePoolOption customises a NodePool before it is created.
type NodePoolOption func(*kaires.NodePool)

// WithPreferredNetworkTopology names the Topology the pod-group-assigner stamps onto the
// pod groups it assigns to this node pool.
func WithPreferredNetworkTopology(topology string) NodePoolOption {
	return func(nodePool *kaires.NodePool) { nodePool.Spec.PreferredNetworkTopologyName = topology }
}

// NodePool builds a nodePool selecting nodes by labelKey=labelValue. The webhook
// requires a non-empty pair on every node pool but the default one.
func NodePool(name, labelKey, labelValue string, options ...NodePoolOption) *kaires.NodePool {
	nodePool := &kaires.NodePool{
		ObjectMeta: objectMeta(name),
		Spec: kaires.NodePoolSpec{
			LabelKey:   labelKey,
			LabelValue: labelValue,
		},
	}
	for _, apply := range options {
		apply(nodePool)
	}

	return nodePool
}

// GeneratedNodePool builds a nodePool whose name and node-label value are unique to
// this call, so parallel specs never collide on the webhook's duplicate-pair rule.
func GeneratedNodePool(prefix, labelKey string, options ...NodePoolOption) *kaires.NodePool {
	name := utils.GenerateName(prefix)

	return NodePool(name, labelKey, name, options...)
}

// Topology builds a kai.scheduler Topology from its node labels, ordered highest level
// first. The pod-group-assigner stamps the last one, the lowest, onto a pod group; a node
// missing any of them is reported as a topology mismatch by nodepool-controller.
func Topology(name string, nodeLabels ...string) *kaitopologyv1alpha1.Topology {
	levels := make([]kaitopologyv1alpha1.TopologyLevel, 0, len(nodeLabels))
	for _, nodeLabel := range nodeLabels {
		levels = append(levels, kaitopologyv1alpha1.TopologyLevel{NodeLabel: nodeLabel})
	}

	return &kaitopologyv1alpha1.Topology{
		ObjectMeta: objectMeta(name),
		Spec:       kaitopologyv1alpha1.TopologySpec{Levels: levels},
	}
}

// ProjectOption customises a Project before it is created.
type ProjectOption func(*kaires.Project)

// WithParentDepartment sets the department whose queues parent this project's.
func WithParentDepartment(department string) ProjectOption {
	return func(project *kaires.Project) { project.Spec.Parent = department }
}

// WithEnforceScheduler controls whether every pod in the project's namespace is
// forced onto the KAI scheduler, or only those that ask for it by name.
func WithEnforceScheduler(enforce bool) ProjectOption {
	return func(project *kaires.Project) { project.Spec.EnforceKaiScheduler = enforce }
}

// WithBlockingDeletion makes the project refuse to finish deleting while its namespace
// still holds anything the projectController.deleteBlockers chart value names.
func WithBlockingDeletion() ProjectOption {
	return func(project *kaires.Project) {
		project.Spec.DeletionType = ptr.To(kaires.Blocking)
	}
}

// WithForceDelete lets the project finish deleting even when a blocker reports its
// namespace is not empty. Meaningful only with WithBlockingDeletion configured.
func WithForceDelete() ProjectOption {
	return func(project *kaires.Project) {
		if project.Annotations == nil {
			project.Annotations = map[string]string{}
		}
		project.Annotations[projectcommon.ForceDeleteAnnotation] = "true"
	}
}

// WithDefaultNodePools narrows the project's defaults to a subset of the pools it has
// queues for. The webhook only requires the reverse - a queue for every default pool -
// so a spec that needs to drop one queue can keep that pool out of the defaults and
// leave the rest of the project alone.
func WithDefaultNodePools(nodePools ...string) ProjectOption {
	return func(project *kaires.Project) { project.Spec.DefaultNodePools = nodePools }
}

// Project builds a project with one queue per node pool, and those same pools as
// its defaults. The validating webhook requires every default node pool to have a
// queue in the same spec, so the two lists are derived together rather than
// passed separately; WithDefaultNodePools narrows the defaults afterwards.
func Project(name string, nodePools []string, options ...ProjectOption) *kaires.Project {
	project := &kaires.Project{
		ObjectMeta: objectMeta(name),
		Spec: kaires.ProjectSpec{
			DefaultNodePools: nodePools,
			Queues:           queueConfigs(name, nodePools),
		},
	}
	for _, apply := range options {
		apply(project)
	}

	return project
}

// Department builds a department with one queue per node pool. A project naming
// it as parent inherits those queues as the parents of its own.
func Department(name string, nodePools []string) *kaires.Department {
	return &kaires.Department{
		ObjectMeta: objectMeta(name),
		Spec: kaires.DepartmentSpec{
			Queues: queueConfigs(name, nodePools),
		},
	}
}

// queueConfigs names one queue per node pool as "<owner>-<node pool>", which keeps the
// queue a spec expects derivable from what it created.
func queueConfigs(owner string, nodePools []string) []kaires.QueueConfig {
	queues := make([]kaires.QueueConfig, 0, len(nodePools))
	for _, nodePool := range nodePools {
		queues = append(queues, kaires.QueueConfig{
			Name:     QueueName(owner, nodePool),
			Nodepool: nodePool,
		})
	}

	return queues
}

// QueueName is the name the controller gives the queue an owner has for a node pool.
func QueueName(owner, nodePool string) string {
	return fmt.Sprintf("%s-%s", owner, nodePool)
}

// PodOption customises a Pod before it is created.
type PodOption func(*corev1.Pod)

// WithSchedulerName asks for a specific scheduler by name. A pod that names the
// KAI scheduler is mutated even in a project that does not enforce it.
func WithSchedulerName(scheduler string) PodOption {
	return func(pod *corev1.Pod) { pod.Spec.SchedulerName = scheduler }
}

// WithNodePoolAnnotation requests node pools explicitly, the most direct of the
// three ways a pod can ask for one.
func WithNodePoolAnnotation(key string, nodePools ...string) PodOption {
	return func(pod *corev1.Pod) {
		if pod.Annotations == nil {
			pod.Annotations = map[string]string{}
		}
		pod.Annotations[key] = strings.Join(nodePools, " ")
	}
}

// NodeSelectorPair is one node pool expressed the way a node carries it: the
// label key the node pool selects on, and the value it selects.
type NodeSelectorPair struct {
	Key   string
	Value string
}

// NodeSelectorForNodePool returns the node label a nodePool selects its nodes by, so
// a pod can require that node pool's nodes without restating the pair.
func NodeSelectorForNodePool(nodePool *kaires.NodePool) NodeSelectorPair {
	return NodeSelectorPair{Key: nodePool.Spec.LabelKey, Value: nodePool.Spec.LabelValue}
}

// WithNodeAffinity requires nodes matching any one of the given pools, so the
// node pool is derived from the affinity rather than requested by name.
//
// Each pair becomes its own nodeSelectorTerm. Terms are OR-ed by Kubernetes
// while expressions within one term are AND-ed, so several pools have to be
// separate terms - putting them in one term would demand a single node carry
// every node pool's label at once, which no node does.
func WithNodeAffinity(pairs ...NodeSelectorPair) PodOption {
	return func(pod *corev1.Pod) {
		terms := make([]corev1.NodeSelectorTerm, 0, len(pairs))
		for _, pair := range pairs {
			terms = append(terms, corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{{
					Key:      pair.Key,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{pair.Value},
				}},
			})
		}

		pod.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: terms,
				},
			},
		}
	}
}

// Secret builds an empty secret; the ownership labels are what the configured blocker
// selects it by.
func Secret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    constant.OwnerLabels(),
		},
	}
}

// Pod builds a pod that runs on a worker node. It carries no control-plane
// toleration, so test workloads and the controllers under test stay apart.
func Pod(name, namespace string, options ...PodOption) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    constant.OwnerLabels(),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "main",
				Image: PauseImage,
			}},
		},
	}
	for _, apply := range options {
		apply(pod)
	}

	return pod
}
