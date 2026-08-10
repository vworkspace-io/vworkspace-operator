package seaweedengine

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

func TestSeaweedEngineEnsureSeaweed(t *testing.T) {
	scheme := testSchemeWithCore()
	app := sampleSeaweedApp()
	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme).Build())

	if err := engine.EnsureSeaweed(context.Background(), app); err != nil {
		t.Fatalf("EnsureSeaweed failed: %v", err)
	}

	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(schema.GroupVersionKind{Group: "seaweed.seaweedfs.com", Version: "v1", Kind: "Seaweed"})
	if err := engine.Client.Get(context.Background(), client.ObjectKey{Namespace: app.Namespace, Name: app.Spec.Release.Name}, sw); err != nil {
		t.Fatalf("expected Seaweed to exist: %v", err)
	}
	replicas, found, _ := unstructured.NestedInt64(sw.Object, "spec", "s3", "replicas")
	if !found || replicas != 1 {
		t.Fatalf("expected s3.replicas 1, got found=%v replicas=%d", found, replicas)
	}
	storage, found, _ := unstructured.NestedString(sw.Object, "spec", "volume", "requests", "storage")
	if !found || storage != "10Gi" {
		t.Fatalf("expected volume.requests.storage 10Gi, got %q (found=%v)", storage, found)
	}
}

func TestMapSeaweedConditions(t *testing.T) {
	reason, message, ready, reconciling, degraded := mapSeaweedConditions([]any{
		map[string]any{"type": "Ready", "status": "True", "message": "cluster healthy"},
		map[string]any{"type": "Progressing", "status": "False"},
	})
	if !ready || reconciling || degraded {
		t.Fatalf("unexpected snapshot: ready=%v reconciling=%v degraded=%v", ready, reconciling, degraded)
	}
	if reason != "SeaweedReady" || message != "cluster healthy" {
		t.Fatalf("unexpected reason/message: %s / %s", reason, message)
	}
}

func TestSeaweedEngineSyncStatus(t *testing.T) {
	app := sampleSeaweedApp()
	sw := MaterializeSeaweedForTest(app, map[string]any{
		"master": map[string]any{"replicas": int64(1)},
		"s3":     map[string]any{"replicas": int64(1)},
	})
	_ = unstructured.SetNestedSlice(sw.Object, []any{
		map[string]any{"type": "Ready", "status": "True", "message": "all components ready"},
	}, "status", "conditions")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: app.Spec.Release.Name + "-s3", Namespace: app.Namespace},
	}

	scheme := testSchemeWithCore()
	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme).WithObjects(sw, svc).Build())
	snapshot, err := engine.SyncStatus(context.Background(), app)
	if err != nil {
		t.Fatalf("SyncStatus: %v", err)
	}
	if !snapshot.Ready {
		t.Fatal("expected Ready=true")
	}
	wantEndpoint := S3Endpoint(app.Spec.Release.Name, app.Namespace)
	if snapshot.S3Endpoint != wantEndpoint {
		t.Fatalf("expected endpoint %q, got %q", wantEndpoint, snapshot.S3Endpoint)
	}
}

func TestSeaweedEngineValuesFromSecret(t *testing.T) {
	scheme := testSchemeWithCore()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "seaweed-values", Namespace: "seaweedfs"},
		Data: map[string][]byte{
			"values.yaml": []byte(`{"master":{"replicas":1},"volume":{"replicas":1,"requests":{"storage":"5Gi"}},"filer":{"replicas":1},"s3":{"replicas":1}}`),
		},
	}
	app := sampleSeaweedApp()
	app.Spec.Values = &appsv1alpha1.ValuesSpec{
		Source:    appsv1alpha1.ValuesSourceSecretRef,
		SecretRef: &appsv1alpha1.ObjectKeyRef{Name: "seaweed-values", Key: "values.yaml"},
	}
	engine := NewSeaweedEngine(fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build())
	if err := engine.EnsureSeaweed(context.Background(), app); err != nil {
		t.Fatalf("EnsureSeaweed: %v", err)
	}
	sw := &unstructured.Unstructured{}
	sw.SetGroupVersionKind(schema.GroupVersionKind{Group: "seaweed.seaweedfs.com", Version: "v1", Kind: "Seaweed"})
	if err := engine.Client.Get(context.Background(), client.ObjectKey{Namespace: app.Namespace, Name: app.Spec.Release.Name}, sw); err != nil {
		t.Fatalf("get Seaweed: %v", err)
	}
	storage, _, _ := unstructured.NestedString(sw.Object, "spec", "volume", "requests", "storage")
	if storage != "5Gi" {
		t.Fatalf("expected storage 5Gi, got %q", storage)
	}
}

func TestIsSeaweedWorkload(t *testing.T) {
	if !IsSeaweedWorkload(sampleSeaweedApp()) {
		t.Fatal("expected seaweedfs catalog to match")
	}
	app := sampleSeaweedApp()
	app.Spec.AppRef.CatalogID = "nextcloud"
	if IsSeaweedWorkload(app) {
		t.Fatal("expected nextcloud catalog not to match")
	}
}

func TestS3Endpoint(t *testing.T) {
	got := S3Endpoint("seaweedfs-dev", "seaweedfs")
	want := "http://seaweedfs-dev-s3.seaweedfs.svc:8333"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestS3EnabledInSpec(t *testing.T) {
	if s3EnabledInSpec(map[string]any{"s3": map[string]any{}}) {
		t.Fatal("empty s3 section should not enable endpoint wait")
	}
	if s3EnabledInSpec(map[string]any{"s3": map[string]any{"replicas": int64(0)}}) {
		t.Fatal("s3 replicas 0 should not enable endpoint wait")
	}
	if !s3EnabledInSpec(map[string]any{"s3": map[string]any{"replicas": int64(1)}}) {
		t.Fatal("s3 replicas 1 should enable endpoint wait")
	}
}

func TestNormalizeSeaweedValues(t *testing.T) {
	wrapped, err := normalizeSeaweedValues(map[string]any{
		"seaweedfs": map[string]any{
			"master": map[string]any{"replicas": int64(1)},
		},
	})
	if err != nil {
		t.Fatalf("normalize wrapped values: %v", err)
	}
	if _, ok := wrapped["master"]; !ok {
		t.Fatal("expected seaweedfs wrapper to be unwrapped")
	}
	_, err = normalizeSeaweedValues(map[string]any{
		"a": map[string]any{"master": map[string]any{"replicas": int64(1)}},
		"b": map[string]any{"filer": map[string]any{"replicas": int64(1)}},
	})
	if err == nil {
		t.Fatal("expected ambiguous wrapper values to fail")
	}
}

func sampleSeaweedApp() *appsv1alpha1.ApplicationInstance {
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "seaweedfs-dev", Namespace: "seaweedfs"},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: CatalogIDSeaweedFS},
			Chart: &appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceHelm,
				URL:        "https://vworkspace-io.github.io/vworkspace-server/charts/",
				Name:       "seaweedfs",
				Version:    "0.1.0",
			},
			Release: &appsv1alpha1.ReleaseSpec{Name: "seaweedfs-dev", Namespace: "seaweedfs"},
			Values: &appsv1alpha1.ValuesSpec{
				Source: appsv1alpha1.ValuesSourceInline,
				Inline: &runtime.RawExtension{Raw: []byte(`{"master":{"replicas":1},"volume":{"replicas":1,"requests":{"storage":"10Gi"}},"filer":{"replicas":1},"s3":{"replicas":1}}`)},
			},
		},
	}
}
