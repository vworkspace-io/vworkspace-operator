package webhook_test

import (
	"context"
	"strings"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/webhook"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func newAppInstanceWebhook(t *testing.T) *webhook.ApplicationInstanceWebhook {
	t.Helper()
	hook, err := webhook.NewApplicationInstanceWebhook(testScheme(t))
	if err != nil {
		t.Fatalf("new application instance webhook: %v", err)
	}
	return hook
}

func placeholderApplicationInstance(namespace, name string) *appsv1alpha1.ApplicationInstance { //nolint:unparam // shared fixture helper
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "cluster-ops"},
			Mode:   appsv1alpha1.InstanceModePlaceholder,
		},
	}
}

func TestApplicationInstanceWebhookAdmitsBarePlaceholder(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	app := placeholderApplicationInstance("team-a", "cluster-ops")
	if _, err := hook.ValidateCreate(context.Background(), app); err != nil {
		t.Fatalf("expected bare placeholder to be admitted: %v", err)
	}
}

func TestApplicationInstanceWebhookRejectsPlaceholderWithChart(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	app := placeholderApplicationInstance("team-a", "cluster-ops")
	app.Spec.Chart = &appsv1alpha1.ChartSpec{
		SourceType: appsv1alpha1.ChartSourceOCI,
		URL:        "oci://registry.example.com/charts",
		Name:       "nextcloud",
		Version:    "6.6.0",
	}
	_, err := hook.ValidateCreate(context.Background(), app)
	if err == nil {
		t.Fatal("expected placeholder with chart to be rejected at admission")
	}
	if !strings.Contains(err.Error(), "spec.chart must not be set in placeholder mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplicationInstanceWebhookRejectsPlaceholderWithValues(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	app := placeholderApplicationInstance("team-a", "cluster-ops")
	app.Spec.Values = &appsv1alpha1.ValuesSpec{
		Source: appsv1alpha1.ValuesSourceInline,
		Inline: &runtime.RawExtension{Raw: []byte(`{"postgresql":{"auth":{"password":"hunter2"}}}`)},
	}
	_, err := hook.ValidateCreate(context.Background(), app)
	if err == nil {
		t.Fatal("expected placeholder with values to be rejected at admission")
	}
	if !strings.Contains(err.Error(), "spec.values must not be set in placeholder mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplicationInstanceWebhookRejectsPlaceholderReleaseNamespaceMismatch(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	app := placeholderApplicationInstance("team-a", "cluster-ops")
	app.Spec.Release = &appsv1alpha1.ReleaseSpec{Name: "cluster-ops", Namespace: "other-ns"}
	_, err := hook.ValidateCreate(context.Background(), app)
	if err == nil {
		t.Fatal("expected placeholder with mismatched release namespace to be rejected")
	}
	if !strings.Contains(err.Error(), "spec.release.namespace must match metadata.namespace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplicationInstanceWebhookAdmitsPlaceholderReleaseSameNamespace(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	app := placeholderApplicationInstance("team-a", "cluster-ops")
	app.Spec.Release = &appsv1alpha1.ReleaseSpec{Name: "cluster-ops", Namespace: "team-a"}
	if _, err := hook.ValidateCreate(context.Background(), app); err != nil {
		t.Fatalf("expected placeholder with matching release namespace to be admitted: %v", err)
	}
}

func TestApplicationInstanceWebhookAdmitsManagedInstance(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	app := sampleApplicationInstance("team-a", "app")
	if _, err := hook.ValidateCreate(context.Background(), app); err != nil {
		t.Fatalf("expected managed instance to be admitted: %v", err)
	}
}

func TestApplicationInstanceWebhookRejectsManagedToPlaceholderWithRelease(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	oldApp := sampleApplicationInstance("team-a", "app")
	// Authoritative status lives on the stored (old) object; a spec-only update
	// may submit an object whose status is empty.
	oldApp.Status.HelmReleaseRef = &appsv1alpha1.HelmReleaseRef{Name: "app", Namespace: "team-a"}
	newApp := placeholderApplicationInstance("team-a", "app")
	_, err := hook.ValidateUpdate(context.Background(), oldApp, newApp)
	if err == nil {
		t.Fatal("expected managed->placeholder transition with existing release to be rejected")
	}
	if !strings.Contains(err.Error(), "delete and recreate as a placeholder") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplicationInstanceWebhookRejectsConfiguredManagedToPlaceholder(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	// A configured managed instance (chart/release set) may already own a release
	// even before status.helmReleaseRef is written, so the transition is rejected.
	oldApp := sampleApplicationInstance("team-a", "app")
	newApp := placeholderApplicationInstance("team-a", "app")
	_, err := hook.ValidateUpdate(context.Background(), oldApp, newApp)
	if err == nil {
		t.Fatal("expected configured managed->placeholder transition to be rejected")
	}
	if !strings.Contains(err.Error(), "delete and recreate as a placeholder") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplicationInstanceWebhookAllowsBareManagedToPlaceholder(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	// An unconfigured instance (no chart/release/values, no status) cannot own a
	// release, so converting it to a placeholder is safe.
	oldApp := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec:       appsv1alpha1.ApplicationInstanceSpec{AppRef: appsv1alpha1.AppRef{CatalogID: "cluster-ops"}},
	}
	newApp := placeholderApplicationInstance("team-a", "app")
	if _, err := hook.ValidateUpdate(context.Background(), oldApp, newApp); err != nil {
		t.Fatalf("expected bare managed->placeholder transition to be admitted: %v", err)
	}
}

func TestApplicationInstanceWebhookAllowsPlaceholderUpdate(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	oldApp := placeholderApplicationInstance("team-a", "app")
	newApp := placeholderApplicationInstance("team-a", "app")
	if _, err := hook.ValidateUpdate(context.Background(), oldApp, newApp); err != nil {
		t.Fatalf("expected placeholder update to be admitted: %v", err)
	}
}

func TestApplicationInstanceWebhookAllowsManagedUpdate(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	oldApp := sampleApplicationInstance("team-a", "app")
	newApp := sampleApplicationInstance("team-a", "app")
	newApp.Status.HelmReleaseRef = &appsv1alpha1.HelmReleaseRef{Name: "app", Namespace: "team-a"}
	if _, err := hook.ValidateUpdate(context.Background(), oldApp, newApp); err != nil {
		t.Fatalf("expected managed update to be admitted: %v", err)
	}
}

func TestApplicationInstanceWebhookRejectsManagedInlineSecret(t *testing.T) {
	hook := newAppInstanceWebhook(t)
	app := sampleApplicationInstance("team-a", "app")
	app.Spec.Values = &appsv1alpha1.ValuesSpec{
		Source: appsv1alpha1.ValuesSourceInline,
		Inline: &runtime.RawExtension{Raw: []byte(`{"postgresql":{"auth":{"password":"hunter2"}}}`)},
	}
	_, err := hook.ValidateCreate(context.Background(), app)
	if err == nil {
		t.Fatal("expected managed instance with inline secret to be rejected")
	}
	if !strings.Contains(err.Error(), "inline secret material rejected") {
		t.Fatalf("unexpected error: %v", err)
	}
}
