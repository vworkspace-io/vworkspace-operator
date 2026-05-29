package webhook_test

import (
	"context"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/webhook"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOperationWebhookValidateCreateAcceptsBackup(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = opsv1alpha1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)

	hook, err := webhook.NewOperationWebhook(scheme)
	if err != nil {
		t.Fatalf("NewOperationWebhook: %v", err)
	}

	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: "team-a"},
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

	if _, err := hook.ValidateCreate(context.Background(), op); err != nil {
		t.Fatalf("ValidateCreate: %v", err)
	}
}

func TestOperationWebhookValidateCreateRejectsUnknownType(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = opsv1alpha1.AddToScheme(scheme)

	hook, err := webhook.NewOperationWebhook(scheme)
	if err != nil {
		t.Fatalf("NewOperationWebhook: %v", err)
	}

	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "team-a"},
		Spec: opsv1alpha1.OperationSpec{
			TargetRef: opsv1alpha1.TargetRef{
				APIVersion: appsv1alpha1.GroupVersion.String(),
				Kind:       "ApplicationInstance",
				Name:       "app",
			},
			Type:   opsv1alpha1.OperationType("snapshot"),
			Engine: opsv1alpha1.EngineVelero,
		},
	}

	if _, err := hook.ValidateCreate(context.Background(), op); err == nil {
		t.Fatal("expected rejection for unknown operation type")
	}
}

// TestOperationWebhookEnvtestPlaceholder documents envtest coverage planned in Phase 1d-c.
// Full admission integration requires webhook TLS manifests under config/webhook/.
func TestOperationWebhookEnvtestPlaceholder(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = opsv1alpha1.AddToScheme(scheme)
	_ = fake.NewClientBuilder().WithScheme(scheme).Build()
	t.Log("envtest webhook suite: TODO concurrency and inline-secret rejection")
}
