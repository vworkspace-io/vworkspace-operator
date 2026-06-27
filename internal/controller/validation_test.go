package controller

import (
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestValidateApplicationInstanceSpec(t *testing.T) {
	valid := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "nextcloud"},
			Chart: &appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceOCI,
				URL:        "oci://registry.example.com/charts",
				Name:       "nextcloud",
				Version:    "6.6.0",
			},
			Release: &appsv1alpha1.ReleaseSpec{Name: "nextcloud", Namespace: "team-a"},
			Values: &appsv1alpha1.ValuesSpec{
				Source: appsv1alpha1.ValuesSourceInline,
				Inline: &runtime.RawExtension{Raw: []byte(`{"ingress":{"enabled":true}}`)},
			},
		},
	}
	if err := ValidateApplicationInstanceSpec(valid); err != nil {
		t.Fatalf("expected valid spec, got %v", err)
	}

	invalid := valid.DeepCopy()
	invalid.Spec.Release.Namespace = "other"
	if err := ValidateApplicationInstanceSpec(invalid); err == nil {
		t.Fatal("expected namespace mismatch error")
	}
}

func TestValidateApplicationInstanceSpecPlaceholder(t *testing.T) {
	placeholder := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-ops", Namespace: "vworkspace-clusterops"},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "cluster-ops"},
			Mode:   appsv1alpha1.InstanceModePlaceholder,
		},
	}
	if err := ValidateApplicationInstanceSpec(placeholder); err != nil {
		t.Fatalf("expected valid placeholder spec (no chart/release/values), got %v", err)
	}

	missingCatalog := placeholder.DeepCopy()
	missingCatalog.Spec.AppRef.CatalogID = ""
	if err := ValidateApplicationInstanceSpec(missingCatalog); err == nil {
		t.Fatal("expected catalogId required error for placeholder")
	}

	withChart := placeholder.DeepCopy()
	withChart.Spec.Chart = &appsv1alpha1.ChartSpec{SourceType: appsv1alpha1.ChartSourceHelm, URL: "https://charts.example.com", Name: "x", Version: "1.0.0"}
	if err := ValidateApplicationInstanceSpec(withChart); err == nil {
		t.Fatal("expected error when placeholder declares a chart")
	}

	releaseNamespaceMismatch := placeholder.DeepCopy()
	releaseNamespaceMismatch.Spec.Release = &appsv1alpha1.ReleaseSpec{Name: "cluster-ops", Namespace: "other"}
	if err := ValidateApplicationInstanceSpec(releaseNamespaceMismatch); err == nil {
		t.Fatal("expected release namespace mismatch error for placeholder with release set")
	}

	releaseNamespaceMatch := placeholder.DeepCopy()
	releaseNamespaceMatch.Spec.Release = &appsv1alpha1.ReleaseSpec{Name: "cluster-ops", Namespace: "vworkspace-clusterops"}
	if err := ValidateApplicationInstanceSpec(releaseNamespaceMatch); err != nil {
		t.Fatalf("expected placeholder with matching release namespace to be valid, got %v", err)
	}
}

func TestValidateOperationSpec(t *testing.T) {
	op := &opsv1alpha1.Operation{
		Spec: opsv1alpha1.OperationSpec{
			TargetRef: opsv1alpha1.TargetRef{
				APIVersion: appsv1alpha1.GroupVersion.String(),
				Kind:       "ApplicationInstance",
				Name:       "app",
			},
			Type:   opsv1alpha1.OperationTypeBackup,
			Engine: opsv1alpha1.EngineVelero,
		},
	}
	if err := ValidateOperationSpec(op); err != nil {
		t.Fatalf("expected valid operation, got %v", err)
	}
}
