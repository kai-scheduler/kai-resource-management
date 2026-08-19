// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deployable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management/pkg/operator/apis/kai/v1alpha1"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands"
	knowntypes "github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/known-types"
)

// A Deployment referencing a ServiceAccount that does not exist yet is admitted
// but never starts a pod.
var objectsCreationOrder = []string{"ServiceAccount", "ConfigMap"}

type Deployable interface {
	Deploy(ctx context.Context, runtimeClient client.Client, krmConfig *krmv1alpha1.KRMConfig, owner client.Object) error
	IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error)
	IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error)
	Monitor(ctx context.Context, readerClient client.Reader, krmConfig *krmv1alpha1.KRMConfig) error
	HasMissingDependencies(ctx context.Context, readerClient client.Reader, krmConfig *krmv1alpha1.KRMConfig) (string, error)
}

type DeployableOperands struct {
	operands               []operands.Operand
	collectables           []*knowntypes.Collectable
	dataFieldsInheritFuncs map[reflect.Type]knowntypes.FieldInherit
}

func New(operandsToDeploy []operands.Operand, collectables []*knowntypes.Collectable) *DeployableOperands {
	return &DeployableOperands{
		operands:               operandsToDeploy,
		collectables:           collectables,
		dataFieldsInheritFuncs: knowntypes.DefaultFieldInherits(),
	}
}

func (d *DeployableOperands) RegisterFieldsInheritFromClusterObjects(
	objectType reflect.Type, fieldInheritFunc knowntypes.FieldInherit,
) {
	d.dataFieldsInheritFuncs[objectType] = fieldInheritFunc
}

// Deploy creates before deleting, so a rename never leaves the service absent in
// between.
func (d *DeployableOperands) Deploy(
	ctx context.Context,
	runtimeClient client.Client,
	krmConfig *krmv1alpha1.KRMConfig,
	owner client.Object,
) error {
	desiredState, err := d.getDesiredState(ctx, runtimeClient, krmConfig)
	if err != nil {
		return fmt.Errorf("failed calculating desired state: %w", err)
	}

	currentState, err := d.getCurrentState(ctx, runtimeClient, owner)
	if err != nil {
		return fmt.Errorf("failed collecting current state: %w", err)
	}

	objectsToCreate, objectsToDelete, objectsToUpdate := d.calculateActionsOnObjects(desiredState, currentState)

	ownerGVK := owner.GetObjectKind().GroupVersionKind()
	reconcilerAsOwnerReference := metav1.OwnerReference{
		APIVersion: ownerGVK.GroupVersion().String(),
		Kind:       ownerGVK.Kind,
		Name:       owner.GetName(),
		UID:        owner.GetUID(),
		Controller: ptr.To(true),
	}

	if err := createObjectsInCluster(ctx, runtimeClient, reconcilerAsOwnerReference, objectsToCreate); err != nil {
		return err
	}
	if err := deleteObjectsInCluster(ctx, runtimeClient, objectsToDelete); err != nil {
		return err
	}
	return updateObjectsInCluster(ctx, runtimeClient, reconcilerAsOwnerReference, objectsToUpdate)
}

func (d *DeployableOperands) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	return d.aggregate(ctx, readerClient, "not deployed", operands.Operand.IsDeployed)
}

func (d *DeployableOperands) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	return d.aggregate(ctx, readerClient, "not available", operands.Operand.IsAvailable)
}

func (d *DeployableOperands) Monitor(
	ctx context.Context, readerClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) error {
	var errs []error
	for _, operand := range d.operands {
		if err := operand.Monitor(ctx, readerClient, krmConfig); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", operand.Name(), err))
		}
	}
	return errors.Join(errs...)
}

func (d *DeployableOperands) HasMissingDependencies(
	ctx context.Context, readerClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (string, error) {
	var missingDependencies []string
	var errs []error
	for _, operand := range d.operands {
		message, err := operand.HasMissingDependencies(ctx, readerClient, krmConfig)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", operand.Name(), err))
			continue
		}
		if message != "" {
			missingDependencies = append(missingDependencies,
				fmt.Sprintf("%s is missing %s", operand.Name(), message))
		}
	}
	return strings.Join(missingDependencies, "; "), errors.Join(errs...)
}

// The joined error describes why the answer is false; callers report it as a
// condition message rather than failing the reconcile.
func (d *DeployableOperands) aggregate(
	ctx context.Context,
	readerClient client.Reader,
	unmetDescription string,
	check func(operands.Operand, context.Context, client.Reader) (bool, error),
) (bool, error) {
	conditionMet := true
	var errs []error
	for _, operand := range d.operands {
		met, err := check(operand, ctx, readerClient)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", operand.Name(), err))
			continue
		}
		if !met {
			conditionMet = false
			errs = append(errs, fmt.Errorf("%s: %s", operand.Name(), unmetDescription))
		}
	}
	return conditionMet, errors.Join(errs...)
}

func (d *DeployableOperands) getDesiredState(
	ctx context.Context, readerClient client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) (map[string]client.Object, error) {
	desiredState := map[string]client.Object{}
	for _, operand := range d.operands {
		objects, err := operand.DesiredState(ctx, readerClient, krmConfig)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", operand.Name(), err)
		}
		for _, obj := range objects {
			groupVersionKind := obj.GetObjectKind().GroupVersionKind()
			if groupVersionKind.Empty() {
				// Without a GVK the object cannot be matched against current
				// state, and would be recreated on every reconcile.
				return nil, fmt.Errorf("%s: object %s/%s has no GroupVersionKind set",
					operand.Name(), obj.GetNamespace(), obj.GetName())
			}
			desiredState[knowntypes.GetKey(groupVersionKind, obj.GetNamespace(), obj.GetName())] = obj
		}
	}
	return desiredState, nil
}

func (d *DeployableOperands) getCurrentState(
	ctx context.Context, runtimeClient client.Client, owner client.Object,
) (map[string]client.Object, error) {
	currentState := map[string]client.Object{}
	for _, collectable := range d.collectables {
		collected, err := collectable.Collect(ctx, runtimeClient, owner)
		if err != nil {
			return nil, err
		}
		for key, obj := range collected {
			currentState[key] = obj
		}
	}
	return currentState, nil
}

func (d *DeployableOperands) calculateActionsOnObjects(
	desiredState map[string]client.Object, currentState map[string]client.Object) (
	objectsToCreate []client.Object, objectsToDelete []client.Object, objectsToUpdate map[string]client.Object) {
	objectsToCreate = []client.Object{}
	objectsToDelete = []client.Object{}
	objectsToUpdate = map[string]client.Object{}
	objectsNotToChange := map[string]client.Object{}

	for key, obj := range desiredState {
		if _, found := currentState[key]; found {
			d.inheritFieldsFromCurrent(currentState[key], obj)
			if reflect.DeepEqual(currentState[key], obj) {
				objectsNotToChange[key] = obj
			} else {
				objectsToUpdate[key] = obj
			}
		} else {
			objectsToCreate = append(objectsToCreate, obj)
		}
	}

	for key, obj := range currentState {
		if _, found := objectsNotToChange[key]; !found {
			if _, found := objectsToUpdate[key]; !found {
				objectsToDelete = append(objectsToDelete, obj)
			}
		}
	}
	return objectsToCreate, objectsToDelete, objectsToUpdate
}

func (d *DeployableOperands) inheritFieldsFromCurrent(currentObj, desiredObj client.Object) {
	if fieldInheritFunc, found := d.dataFieldsInheritFuncs[reflect.TypeOf(desiredObj)]; found {
		fieldInheritFunc(currentObj, desiredObj)
	}
}

func createObjectsInCluster(
	ctx context.Context, runtimeClient client.Client, reconcilerAsOwnerReference metav1.OwnerReference,
	objectsToCreate []client.Object) error {

	sortObjectByCreationOrder(objectsToCreate, objectsCreationOrder)

	for _, obj := range objectsToCreate {
		if err := createObjectForKRMConfig(ctx, runtimeClient, reconcilerAsOwnerReference, obj); err != nil {
			return err
		}
	}
	return nil
}

func sortObjectByCreationOrder(objectsToCreate []client.Object, kindsOrder []string) {
	customCreationOrderSort := func(firstIndex, secondIndex int) bool {
		firstOrderIndex := slices.Index(kindsOrder, objectsToCreate[firstIndex].GetObjectKind().GroupVersionKind().Kind)
		secondOrderIndex := slices.Index(kindsOrder, objectsToCreate[secondIndex].GetObjectKind().GroupVersionKind().Kind)
		if firstOrderIndex == -1 {
			return false
		}
		if secondOrderIndex == -1 {
			return true
		}
		return firstOrderIndex < secondOrderIndex
	}
	sort.Slice(objectsToCreate, customCreationOrderSort)
}

func createObjectForKRMConfig(
	ctx context.Context, runtimeClient client.Client, reconcilerAsOwnerReference metav1.OwnerReference,
	obj client.Object) error {
	obj.SetOwnerReferences([]metav1.OwnerReference{reconcilerAsOwnerReference})

	err := runtimeClient.Create(ctx, obj)
	if err == nil {
		return nil
	}

	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed creating %s %s/%s: %w", obj.GetObjectKind().GroupVersionKind(),
			obj.GetNamespace(), obj.GetName(), err)
	}

	logger := log.FromContext(ctx)
	logger.Info("Object already exists, updating to take ownership",
		"GroupVersionKind", obj.GetObjectKind().GroupVersionKind(), "Name", obj.GetName())

	if updateErr := runtimeClient.Update(ctx, obj); updateErr != nil {
		logger.Error(updateErr, "failed taking ownership on object",
			"GroupVersionKind", obj.GetObjectKind().GroupVersionKind(),
			"Name", obj.GetName(), "Namespace", obj.GetNamespace())

		return fmt.Errorf("failed taking ownership on %s %s/%s: %w",
			obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName(), updateErr)
	}

	logger.Info("Took ownership on obj",
		"GroupVersionKind", obj.GetObjectKind().GroupVersionKind(), "Name", obj.GetName())
	return nil
}

func updateObjectsInCluster(
	ctx context.Context, runtimeClient client.Client, reconcilerAsOwnerReference metav1.OwnerReference,
	objectsToUpdate map[string]client.Object) error {
	for _, obj := range objectsToUpdate {
		obj.SetOwnerReferences([]metav1.OwnerReference{reconcilerAsOwnerReference})

		if err := runtimeClient.Update(ctx, obj); err != nil {
			return fmt.Errorf("failed updating %s %s/%s: %w", obj.GetObjectKind().GroupVersionKind(),
				obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
}

func deleteObjectsInCluster(
	ctx context.Context, runtimeClient client.Client, objectsToDelete []client.Object) error {
	for _, obj := range objectsToDelete {
		if err := client.IgnoreNotFound(runtimeClient.Delete(ctx, obj)); err != nil {
			return fmt.Errorf("failed deleting %s %s/%s: %w", obj.GetObjectKind().GroupVersionKind(),
				obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
}
