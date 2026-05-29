package webhook_test

import (
	"context"
	"strings"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/webhook"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := opsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("ops scheme: %v", err)
	}
	if err := appsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("apps scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1 scheme: %v", err)
	}
	return scheme
}

func TestOperationWebhookValidateCreateAcceptsBackup(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(sampleApplicationInstance("team-a", "app")).
		Build()

	hook, err := webhook.NewOperationWebhook(scheme, cl)
	if err != nil {
		t.Fatalf("NewOperationWebhook: %v", err)
	}

	op := sampleOperation("backup-1", "team-a", "app", opsv1alpha1.OperationTypeBackup)
	if _, err := hook.ValidateCreate(context.Background(), op); err != nil {
		t.Fatalf("ValidateCreate: %v", err)
	}
}

func TestOperationWebhookValidateCreateRejectsUnknownType(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(sampleApplicationInstance("team-a", "app")).
		Build()

	hook, err := webhook.NewOperationWebhook(scheme, cl)
	if err != nil {
		t.Fatalf("NewOperationWebhook: %v", err)
	}

	op := sampleOperation("bad", "team-a", "app", opsv1alpha1.OperationType("snapshot"))
	if _, err := hook.ValidateCreate(context.Background(), op); err == nil {
		t.Fatal("expected rejection for unknown operation type")
	}
}

func TestOperationWebhookValidateCreateRejectsMissingTarget(t *testing.T) {
	scheme := testScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	hook, err := webhook.NewOperationWebhook(scheme, cl)
	if err != nil {
		t.Fatalf("NewOperationWebhook: %v", err)
	}

	op := sampleOperation("backup-1", "team-a", "missing", opsv1alpha1.OperationTypeBackup)
	if _, err := hook.ValidateCreate(context.Background(), op); err == nil {
		t.Fatal("expected rejection for missing target")
	}
}

func TestOperationWebhookValidateCreateRejectsDisallowedNamespaceType(t *testing.T) {
	scheme := testScheme(t)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "team-a",
			Annotations: map[string]string{
				"ops.vworkspace.io/allowed-types": "Backup",
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(ns, sampleApplicationInstance("team-a", "app")).
		Build()

	hook, err := webhook.NewOperationWebhook(scheme, cl)
	if err != nil {
		t.Fatalf("NewOperationWebhook: %v", err)
	}

	op := sampleOperation("restore-1", "team-a", "app", opsv1alpha1.OperationTypeRestore)
	if _, err := hook.ValidateCreate(context.Background(), op); err == nil {
		t.Fatal("expected rejection for disallowed operation type in namespace")
	}
}

func TestOperationWebhookValidateCreateRejectsConcurrentUpgrade(t *testing.T) {
	scheme := testScheme(t)
	existing := sampleOperation("upgrade-1", "team-a", "app", opsv1alpha1.OperationTypeUpgrade)
	existing.Status.Phase = opsv1alpha1.PhaseRunning

	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(sampleApplicationInstance("team-a", "app"), existing).
		Build()

	hook, err := webhook.NewOperationWebhook(scheme, cl)
	if err != nil {
		t.Fatalf("NewOperationWebhook: %v", err)
	}

	op := sampleOperation("upgrade-2", "team-a", "app", opsv1alpha1.OperationTypeBackup)
	if _, err := hook.ValidateCreate(context.Background(), op); err == nil {
		t.Fatal("expected rejection for concurrent operation")
	}
}

func TestDetectInlineSecret(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name:    "non-secret config",
			raw:     `{"ingress":{"enabled":true,"host":"files.example.com"}}`,
			wantErr: false,
		},
		{
			name:    "empty password allowed",
			raw:     `{"postgresql":{"auth":{"password":""}}}`,
			wantErr: false,
		},
		{
			name:    "placeholder password allowed",
			raw:     `{"postgresql":{"auth":{"password":"<set via externalSecret>"}}}`,
			wantErr: false,
		},
		{
			name:    "non-empty password rejected",
			raw:     `{"postgresql":{"auth":{"password":"hunter2"}}}`,
			wantErr: true,
		},
		{
			name:    "clientSecret rejected",
			raw:     `{"oidc":{"clientSecret":"abc"}}`,
			wantErr: true,
		},
		{
			name:    "accessKey rejected",
			raw:     `{"s3":{"accessKey":"AKIAEXAMPLE"}}`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := webhook.DetectInlineSecret([]byte(tc.raw))
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && err != nil && !strings.Contains(err.Error(), "inline secret material rejected") {
				t.Fatalf("unexpected error message: %v", err)
			}
		})
	}
}

func sampleApplicationInstance(namespace, name string) *appsv1alpha1.ApplicationInstance { //nolint:unparam // shared fixture helper
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "nextcloud"},
			Chart: appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceOCI,
				URL:        "oci://registry.example.com/charts",
				Name:       "nextcloud",
				Version:    "6.6.0",
			},
			Release: appsv1alpha1.ReleaseSpec{Name: name, Namespace: namespace},
			Values: appsv1alpha1.ValuesSpec{
				Source: appsv1alpha1.ValuesSourceInline,
				Inline: &runtime.RawExtension{Raw: []byte(`{}`)},
			},
		},
	}
}

func sampleOperation(name, namespace, target string, opType opsv1alpha1.OperationType) *opsv1alpha1.Operation { //nolint:unparam // shared fixture helper
	return &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: opsv1alpha1.OperationSpec{
			TargetRef: opsv1alpha1.TargetRef{
				APIVersion: appsv1alpha1.GroupVersion.String(),
				Kind:       "ApplicationInstance",
				Name:       target,
			},
			Type:   opType,
			Engine: opsv1alpha1.EngineVelero,
		},
	}
}
