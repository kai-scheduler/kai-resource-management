// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package podgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	kaiv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/rs/zerolog/log"

	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/config"
	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/common"
	"github.com/kai-scheduler/kai-resource-management/pkg/pod-group-assigner/controllers/utils"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

//+kubebuilder:webhook:path=/mutate-pod-group,mutating=true,failurePolicy=fail,sideEffects=None,groups=scheduling.run.ai,resources=podgroups,verbs=create,versions=*,name=mpodgroup.kb.io,admissionReviewVersions=v1

type PodGroupMutator struct {
	client client.Client
}

func NewPodGroupMutator(k8sClient client.Client) *PodGroupMutator {
	return &PodGroupMutator{client: k8sClient}
}

func (pgm *PodGroupMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
	ctx = log.Logger.WithContext(ctx)

	podGroup := &kaiv2alpha2.PodGroup{}
	if err := json.Unmarshal(req.Object.Raw, podGroup); err != nil {
		webhookErr := fmt.Errorf("unable to unmarshal podgroup: %s", err.Error())
		log.Ctx(ctx).Error().Msgf("Failure in PodGroupMutator Handle, error: <%s>", webhookErr.Error())

		return admission.Errored(http.StatusInternalServerError, webhookErr)
	}

	// On CREATE the pod group is not yet persisted, so podGroup.Namespace may be empty.
	namespace := req.Namespace
	if namespace == "" {
		namespace = podGroup.Namespace
	}

	// Mutating a PodGroup outside every project would park it on the sentinel node pool, where
	// the scheduler that owns it never sees it again.
	external, err := utils.IsExternalNamespace(ctx, pgm.client, namespace)
	if err != nil {
		log.Ctx(ctx).Error().Msgf("PodGroupMutator: failed to resolve the project of pod group <%s/%s>, err: <%s>",
			namespace, podGroup.Name, err.Error())

		return admission.Errored(http.StatusInternalServerError, err)
	}
	if external {
		log.Ctx(ctx).Info().Msgf("PodGroupMutator: pod group <%s/%s> is outside every project, leaving it untouched",
			namespace, podGroup.Name)

		return admission.Allowed("pod group is not managed by the resource-management package")
	}

	log.Ctx(ctx).Info().Msgf("PodGroupMutator: handling mutation of pod group <%s/%s>",
		podGroup.Namespace, podGroup.Name)

	pgm.mutatePodGroup(podGroup)

	marshaledPod, err := json.Marshal(podGroup)
	if err != nil {
		log.Ctx(ctx).Error().Msgf("PodGroupMutator: failed to json marshal pod group <%s/%s>, err: <%s>",
			podGroup.Namespace, podGroup.Name, err.Error())

		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}

func (pgm *PodGroupMutator) mutatePodGroup(podGroup *kaiv2alpha2.PodGroup) {
	podGroup.Spec.MarkUnschedulable = ptr.To(false)
	podGroup.Spec.SchedulingBackoff = ptr.To(int32(common.SingleSchedulingBackoff))

	if podGroup.Labels == nil {
		podGroup.Labels = map[string]string{}
	}

	_, found := podGroup.Labels[config.Config().NodePoolLabelKey]
	if found {
		return
	}

	podGroup.Labels[config.Config().NodePoolLabelKey] = config.Config().UnexistingNodepoolSentinel
}
