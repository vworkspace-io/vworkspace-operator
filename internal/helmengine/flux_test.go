package helmengine

import (
	"context"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFluxEngineEnsureRelease(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
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
	reason, message, ready, reconciling, degraded := mapHelmReleaseConditions([]interface{}{
		map[string]interface{}{"type": "Ready", "status": "True", "message": "all good"},
		map[string]interface{}{"type": "Reconciling", "status": "False"},
	})
	if !ready || reconciling || degraded {
		t.Fatalf("unexpected snapshot: ready=%v reconciling=%v degraded=%v", ready, reconciling, degraded)
	}
	if reason != "HelmReleaseReady" || message != "all good" {
		t.Fatalf("unexpected reason/message: %s / %s", reason, message)
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
