// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package deployable

import (
	"context"
	"errors"
	"testing"

	krmv1alpha1 "github.com/kai-scheduler/kai-resource-management-api/kai/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands"
	"github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/common"
	knowntypes "github.com/kai-scheduler/kai-resource-management/pkg/operator/operands/known-types"
)

func TestDeployable(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Deployable suite")
}

const testNamespace = "kai-resource-management"

// fakeOperand drives the engine through every action without standing up a real
// service. It builds its objects the way a real operand does — through
// ObjectForKRMConfig, which reads the live object first — because an operand that
// builds from scratch drops the owner references and resourceVersion the engine
// compares against, and so rewrites its objects on every resync.
type fakeOperand struct {
	configMaps map[string]map[string]string
	extra      []client.Object
	missing    string
}

func (o *fakeOperand) DesiredState(
	ctx context.Context, reader client.Reader, krmConfig *krmv1alpha1.KRMConfig,
) ([]client.Object, error) {
	desired := append([]client.Object{}, o.extra...)
	for name, data := range o.configMaps {
		object, err := common.ObjectForKRMConfig(ctx, reader, &corev1.ConfigMap{}, name, krmConfig.Spec.Namespace)
		if err != nil {
			return nil, err
		}
		configMap := object.(*corev1.ConfigMap)
		configMap.TypeMeta = metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"}
		configMap.Data = data
		desired = append(desired, configMap)
	}
	return desired, nil
}

func (o *fakeOperand) IsDeployed(_ context.Context, _ client.Reader) (bool, error)  { return true, nil }
func (o *fakeOperand) IsAvailable(_ context.Context, _ client.Reader) (bool, error) { return true, nil }

func (o *fakeOperand) Monitor(_ context.Context, _ client.Reader, _ *krmv1alpha1.KRMConfig) error {
	return nil
}

func (o *fakeOperand) HasMissingDependencies(
	_ context.Context, _ client.Reader, _ *krmv1alpha1.KRMConfig,
) (string, error) {
	return o.missing, nil
}

func (o *fakeOperand) Name() string { return "FakeOperand" }

func newKRMConfig() *krmv1alpha1.KRMConfig {
	krmConfig := &krmv1alpha1.KRMConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: krmv1alpha1.KRMConfigSingletonName,
			UID:  types.UID("11111111-2222-3333-4444-555555555555"),
		},
		Spec: krmv1alpha1.KRMConfigSpec{Namespace: testNamespace},
	}
	// The reconciler stamps this after Get; the owner reference and the index key
	// are both built from the Kind.
	krmConfig.SetGroupVersionKind(krmv1alpha1.GroupVersion.WithKind(krmv1alpha1.KRMConfigKind))
	return krmConfig
}

func newScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(krmv1alpha1.AddToScheme(scheme)).To(Succeed())
	// Registered so the optional collectables can install their indexes; the fake
	// client builder panics on an index for a kind it cannot recognise.
	Expect(monitoringv1.AddToScheme(scheme)).To(Succeed())
	Expect(vpav1.AddToScheme(scheme)).To(Succeed())
	return scheme
}

// newClient registers the same field indexes the manager does, so Collect exercises
// the real lookup rather than a simplified one.
func newClient(scheme *runtime.Scheme, objects ...client.Object) client.Client {
	return newClientBuilder(scheme).WithObjects(objects...).Build()
}

func newClientBuilder(scheme *runtime.Scheme) *fake.ClientBuilder {
	clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
	for _, collectable := range knowntypes.KRMConfigOwned {
		collectable.InitWithFakeClientBuilder(clientBuilder)
	}
	return clientBuilder
}

var _ = Describe("DeployableOperands", func() {
	var (
		ctx           context.Context
		scheme        *runtime.Scheme
		runtimeClient client.Client
		operand       *fakeOperand
		engine        *DeployableOperands
		krmConfig     *krmv1alpha1.KRMConfig
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = newScheme()
		runtimeClient = newClient(scheme)
		operand = &fakeOperand{}
		engine = New([]operands.Operand{operand}, knowntypes.KRMConfigOwned)
		krmConfig = newKRMConfig()
	})

	deploy := func() error { return engine.Deploy(ctx, runtimeClient, krmConfig, krmConfig) }

	getConfigMap := func(name string) *corev1.ConfigMap {
		configMap := &corev1.ConfigMap{}
		Expect(runtimeClient.Get(ctx,
			types.NamespacedName{Namespace: testNamespace, Name: name}, configMap)).To(Succeed())
		return configMap
	}

	Context("creating", func() {
		It("creates a desired object and makes the KRMConfig its controller", func() {
			operand.configMaps = map[string]map[string]string{"settings": {"key": "value"}}

			Expect(deploy()).To(Succeed())

			created := getConfigMap("settings")
			Expect(created.Data).To(HaveKeyWithValue("key", "value"))
			Expect(created.OwnerReferences).To(HaveLen(1))
			Expect(created.OwnerReferences[0].Kind).To(Equal(krmv1alpha1.KRMConfigKind))
			Expect(created.OwnerReferences[0].Name).To(Equal(krmv1alpha1.KRMConfigSingletonName))
			Expect(created.OwnerReferences[0].Controller).To(Equal(ptr.To(true)))
		})

		It("takes the apiVersion from the owner rather than assuming one", func() {
			operand.configMaps = map[string]map[string]string{"settings": {"key": "value"}}
			foreignOwner := &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{Name: "some-owner", Namespace: testNamespace},
			}
			foreignOwner.SetGroupVersionKind(
				monitoringv1.SchemeGroupVersion.WithKind(monitoringv1.ServiceMonitorsKind))

			Expect(engine.Deploy(ctx, runtimeClient, krmConfig, foreignOwner)).To(Succeed())

			created := getConfigMap("settings")
			Expect(created.OwnerReferences).To(HaveLen(1))
			Expect(created.OwnerReferences[0].APIVersion).To(
				Equal(monitoringv1.SchemeGroupVersion.String()))
			Expect(created.OwnerReferences[0].Kind).To(Equal(monitoringv1.ServiceMonitorsKind))
			Expect(created.OwnerReferences[0].Name).To(Equal("some-owner"))
		})

		It("rejects an object with no GroupVersionKind, which could never be matched again", func() {
			operand.extra = []client.Object{&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "no-gvk", Namespace: testNamespace},
			}}

			Expect(deploy()).To(MatchError(ContainSubstring("no GroupVersionKind set")))
		})
	})

	Context("failing to create", func() {
		// Anything other than an existing object would fail the update the same
		// way, and retrying hides the real error behind a second one.
		It("does not try to take ownership when the create was refused", func() {
			updateAttempted := false
			runtimeClient = newClientBuilder(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(
						_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption,
					) error {
						return apierrors.NewForbidden(
							schema.GroupResource{Resource: "configmaps"}, "settings", errors.New("denied"))
					},
					Update: func(
						_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption,
					) error {
						updateAttempted = true
						return nil
					},
				}).
				Build()
			operand.configMaps = map[string]map[string]string{"settings": nil}

			err := engine.Deploy(ctx, runtimeClient, krmConfig, krmConfig)

			Expect(err).To(MatchError(ContainSubstring("forbidden")))
			Expect(updateAttempted).To(BeFalse())
		})

		It("reports the update failure when taking ownership fails", func() {
			runtimeClient = newClientBuilder(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Create: func(
						_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption,
					) error {
						return apierrors.NewAlreadyExists(
							schema.GroupResource{Resource: "configmaps"}, obj.GetName())
					},
					Update: func(
						_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption,
					) error {
						return apierrors.NewConflict(
							schema.GroupResource{Resource: "configmaps"}, "settings", errors.New("stale"))
					},
				}).
				Build()
			operand.configMaps = map[string]map[string]string{"settings": nil}

			err := engine.Deploy(ctx, runtimeClient, krmConfig, krmConfig)

			// The conflict is the real failure; "already exists" is not.
			Expect(err).To(MatchError(ContainSubstring("failed taking ownership")))
			Expect(err).To(MatchError(ContainSubstring("stale")))
			Expect(err).ToNot(MatchError(ContainSubstring("already exists")))
		})
	})

	Context("updating", func() {
		It("writes nothing when the desired state is unchanged", func() {
			operand.configMaps = map[string]map[string]string{"settings": {"key": "value"}}
			Expect(deploy()).To(Succeed())
			afterCreate := getConfigMap("settings").ResourceVersion

			// A steady state must issue no writes, or the operator rewrites every
			// owned object on every resync.
			operand.configMaps = map[string]map[string]string{"settings": {"key": "value"}}
			Expect(deploy()).To(Succeed())

			Expect(getConfigMap("settings").ResourceVersion).To(Equal(afterCreate))
		})

		It("updates an object whose desired state changed", func() {
			operand.configMaps = map[string]map[string]string{"settings": {"key": "value"}}
			Expect(deploy()).To(Succeed())

			operand.configMaps = map[string]map[string]string{"settings": {"key": "changed"}}
			Expect(deploy()).To(Succeed())

			Expect(getConfigMap("settings").Data).To(HaveKeyWithValue("key", "changed"))
		})
	})

	Context("deleting", func() {
		It("deletes an owned object that is no longer desired", func() {
			operand.configMaps = map[string]map[string]string{"kept": nil, "dropped": nil}
			Expect(deploy()).To(Succeed())

			operand.configMaps = map[string]map[string]string{"kept": nil}
			Expect(deploy()).To(Succeed())

			Expect(runtimeClient.Get(ctx,
				types.NamespacedName{Namespace: testNamespace, Name: "dropped"},
				&corev1.ConfigMap{})).To(MatchError(ContainSubstring("not found")))
			Expect(getConfigMap("kept")).ToNot(BeNil())
		})

		It("leaves an object it does not own alone", func() {
			foreign := &corev1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "foreign", Namespace: testNamespace},
				Data:       map[string]string{"owner": "somebody-else"},
			}
			runtimeClient = newClient(scheme, foreign)

			operand.configMaps = map[string]map[string]string{"ours": nil}
			Expect(deploy()).To(Succeed())

			Expect(getConfigMap("foreign").Data).To(HaveKeyWithValue("owner", "somebody-else"))
		})
	})

	Context("creation order", func() {
		It("creates a ServiceAccount before the Deployment that names it", func() {
			objects := []client.Object{
				&appsv1.Deployment{
					TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: testNamespace},
				},
				&corev1.ServiceAccount{
					TypeMeta:   metav1.TypeMeta{Kind: "ServiceAccount", APIVersion: "v1"},
					ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: testNamespace},
				},
			}

			sortObjectByCreationOrder(objects, objectsCreationOrder)

			Expect(objects[0].GetObjectKind().GroupVersionKind().Kind).To(Equal("ServiceAccount"))
		})
	})

	Context("reporting", func() {
		It("names the operand that is missing a dependency", func() {
			operand.missing = "the prometheus operator"

			missing, err := engine.HasMissingDependencies(ctx, runtimeClient, krmConfig)

			Expect(err).ToNot(HaveOccurred())
			Expect(missing).To(Equal("FakeOperand is missing the prometheus operator"))
		})

		It("reports nothing missing when every operand is satisfied", func() {
			missing, err := engine.HasMissingDependencies(ctx, runtimeClient, krmConfig)

			Expect(err).ToNot(HaveOccurred())
			Expect(missing).To(BeEmpty())
		})
	})
})
