package engines

import (
	"context"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRegistrySelection(t *testing.T) {
	registry := NewRegistry(NewVeleroEngine(fake.NewClientBuilder().Build()))
	if !registry.Has(opsv1alpha1.EngineVelero) {
		t.Fatal("expected velero engine to be registered")
	}
	if _, err := registry.Get(opsv1alpha1.EngineWorkflow); err == nil {
		t.Fatal("expected missing engine error")
	}
}

func TestVeleroEngineCreatesBackup(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = opsv1alpha1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	engine := NewVeleroEngine(c)

	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: "team-a"},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeBackup,
			Engine: opsv1alpha1.EngineVelero,
		},
	}
	target := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec:       appsv1alpha1.ApplicationInstanceSpec{Release: appsv1alpha1.ReleaseSpec{Namespace: "team-a"}},
	}

	if err := engine.Materialize(context.Background(), op, target); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	backup := &unstructured.Unstructured{}
	backup.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Backup"})
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "team-a", Name: "backup-1"}, backup); err != nil {
		t.Fatalf("expected backup CR: %v", err)
	}
	namespaces, _, _ := unstructured.NestedStringSlice(backup.Object, "spec", "includedNamespaces")
	if len(namespaces) != 1 || namespaces[0] != "team-a" {
		t.Fatalf("unexpected includedNamespaces: %v", namespaces)
	}
}
