package helmengine

import (
	"context"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testSchemeWithCore() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(s)
	_ = clientgoscheme.AddToScheme(s)
	return s
}

func TestFluxEngineEnsureRelease(t *testing.T) {
	scheme := testSchemeWithCore()
	app := sampleApp()
	engine := NewFluxEngine(fake.NewClientBuilder().WithScheme(scheme).Build())

	if err := engine.EnsureRelease(context.Background(), app); err != nil {
		t.Fatalf("EnsureRelease failed: %v", err)
	}

	hr := &unstructured.Unstructured{}
	hr.SetGroupVersionKind(schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"})
	if err := engine.Client.Get(context.Background(), client.ObjectKey{Namespace: app.Namespace, Name: app.Spec.Release.Name}, hr); err != nil {
		t.Fatalf("expected helmrelease to exist: %v", err)
	}
	version, _, _ := unstructured.NestedString(hr.Object, "spec", "chart", "spec", "version")
	if version != "6.6.0" {
		t.Fatalf("expected chart version 6.6.0, got %q", version)
	}
}

func TestMapHelmReleaseConditions(t *testing.T) {
	reason, message, ready, reconciling, degraded := mapHelmReleaseConditions([]any{
		map[string]any{"type": "Ready", "status": "True", "message": "all good"},
		map[string]any{"type": "Reconciling", "status": "False"},
	})
	if !ready || reconciling || degraded {
		t.Fatalf("unexpected snapshot: ready=%v reconciling=%v degraded=%v", ready, reconciling, degraded)
	}
	if reason != "HelmReleaseReady" || message != "all good" {
		t.Fatalf("unexpected reason/message: %s / %s", reason, message)
	}
}

func TestFluxEngineValuesFromSecret(t *testing.T) {
	scheme := testSchemeWithCore()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "app-values", Namespace: "team-a"},
		Data:       map[string][]byte{"values.yaml": []byte(`{"replicaCount": 3}`)},
	}
	app := sampleApp()
	app.Spec.Values = appsv1alpha1.ValuesSpec{
		Source:    appsv1alpha1.ValuesSourceSecretRef,
		SecretRef: &appsv1alpha1.ObjectKeyRef{Name: "app-values", Key: "values.yaml"},
	}
	engine := NewFluxEngine(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build())

	if err := engine.EnsureRelease(context.Background(), app); err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	hr := &unstructured.Unstructured{}
	hr.SetGroupVersionKind(schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"})
	if err := engine.Client.Get(context.Background(), client.ObjectKey{Namespace: app.Namespace, Name: app.Spec.Release.Name}, hr); err != nil {
		t.Fatalf("get helmrelease: %v", err)
	}
	values, found, _ := unstructured.NestedMap(hr.Object, "spec", "values")
	if !found {
		t.Fatalf("expected spec.values on helmrelease, got %#v", hr.Object)
	}
	replicas, ok := numericValue(values["replicaCount"])
	if !ok || replicas != 3 {
		t.Fatalf("expected replicaCount 3 in values, got %#v", values)
	}
}

func TestFluxEngineValuesFromConfigMap(t *testing.T) {
	scheme := testSchemeWithCore()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "app-values", Namespace: "team-a"},
		Data:       map[string]string{"values.yaml": "ingress:\n  enabled: true\n"},
	}
	app := sampleApp()
	app.Spec.Values = appsv1alpha1.ValuesSpec{
		Source:       appsv1alpha1.ValuesSourceConfigMapRef,
		ConfigMapRef: &appsv1alpha1.ObjectKeyRef{Name: "app-values", Key: "values.yaml"},
	}
	engine := NewFluxEngine(fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build())

	if err := engine.EnsureRelease(context.Background(), app); err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	hr := &unstructured.Unstructured{}
	hr.SetGroupVersionKind(schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"})
	if err := engine.Client.Get(context.Background(), client.ObjectKey{Namespace: app.Namespace, Name: app.Spec.Release.Name}, hr); err != nil {
		t.Fatalf("get helmrelease: %v", err)
	}
	enabled, found, _ := unstructured.NestedBool(hr.Object, "spec", "values", "ingress", "enabled")
	if !found || !enabled {
		t.Fatalf("expected ingress.enabled true")
	}
}

func numericValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func sampleApp() *appsv1alpha1.ApplicationInstance {
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "nextcloud", Namespace: "team-a"},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "nextcloud"},
			Chart: appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceHelm,
				URL:        "https://charts.example.com",
				Name:       "nextcloud",
				Version:    "6.6.0",
			},
			Release: appsv1alpha1.ReleaseSpec{Name: "nextcloud", Namespace: "team-a"},
			Values: appsv1alpha1.ValuesSpec{
				Source: appsv1alpha1.ValuesSourceInline,
				Inline: &runtime.RawExtension{Raw: []byte(`{}`)},
			},
		},
	}
}
