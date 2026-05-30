package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/labels"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestApplierApplyApplicationInstance(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	_ = opsv1alpha1.AddToScheme(scheme)

	app := sampleApplicationInstance()
	payload, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("marshal app: %v", err)
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	store := &IdempotencyStore{
		Client:    cl,
		Namespace: "vworkspace-system",
		Name:      "test-idempotency",
	}
	applier := &Applier{Client: cl, Scheme: scheme, ClusterID: "cluster-1", Idempotency: store}

	outcome, err := applier.ApplyJob(context.Background(), Job{
		ID:             "j-apply-1",
		Kind:           "apply",
		Payload:        payload,
		IdempotencyKey: "key-1",
		ExpiresAt:      time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ApplyJob: %v", err)
	}
	if outcome.Result.Outcome != OutcomeSucceeded {
		t.Fatalf("expected succeeded, got %s", outcome.Result.Outcome)
	}

	got := &appsv1alpha1.ApplicationInstance{}
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: app.Namespace, Name: app.Name}, got); err != nil {
		t.Fatalf("get applied app: %v", err)
	}
	if got.Labels[labels.ManagedByKey] != labels.ManagedByControlPlane {
		t.Fatalf("expected managed-by control-plane, got %q", got.Labels[labels.ManagedByKey])
	}
	if got.Labels[labels.ClusterIDKey] != "cluster-1" {
		t.Fatalf("expected cluster-id label")
	}
}

func TestApplierIdempotentReplay(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	app := sampleApplicationInstance()
	payload, _ := json.Marshal(app)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	store := &IdempotencyStore{
		Client:    cl,
		Namespace: "vworkspace-system",
		Name:      "test-idempotency",
	}
	applier := &Applier{Client: cl, Scheme: scheme, ClusterID: "cluster-1", Idempotency: store}

	job := Job{
		ID:             "j-apply-2",
		Kind:           "apply",
		Payload:        payload,
		IdempotencyKey: "same-key",
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	if _, err := applier.ApplyJob(context.Background(), job); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	outcome, err := applier.ApplyJob(context.Background(), job)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if outcome.Result.Outcome != OutcomeNoop || !outcome.Idempotent {
		t.Fatalf("expected idempotent noop, got %+v", outcome)
	}
}

func TestApplierDeleteJob(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	app := sampleApplicationInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).Build()
	applier := &Applier{Client: cl, Scheme: scheme, ClusterID: "cluster-1"}

	payload, _ := json.Marshal(map[string]string{
		"apiVersion": "apps.vworkspace.io/v1alpha1",
		"kind":       "ApplicationInstance",
		"name":       app.Name,
		"namespace":  app.Namespace,
	})
	outcome, err := applier.ApplyJob(context.Background(), Job{
		ID:        "j-del-1",
		Kind:      "delete",
		Payload:   payload,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("delete job: %v", err)
	}
	if outcome.Result.Outcome != OutcomeSucceeded {
		t.Fatalf("expected succeeded, got %s", outcome.Result.Outcome)
	}
	err = cl.Get(context.Background(), client.ObjectKeyFromObject(app), &appsv1alpha1.ApplicationInstance{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestApplierEnsureIntent(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	app := sampleApplicationInstance()
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	store := &IdempotencyStore{
		Client:    cl,
		Namespace: "vworkspace-system",
		Name:      "test-idempotency",
	}
	applier := &Applier{Client: cl, Scheme: scheme, ClusterID: "cluster-1", Idempotency: store}

	payload, _ := json.Marshal(intentPayload{
		Intent:              "ensure-application-instance",
		ApplicationInstance: app,
	})
	outcome, err := applier.ApplyJob(context.Background(), Job{
		ID:        "j-intent-1",
		Kind:      "intent",
		Payload:   payload,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("intent job: %v", err)
	}
	if outcome.Result.Outcome != OutcomeSucceeded {
		t.Fatalf("expected succeeded, got %s", outcome.Result.Outcome)
	}
}

func sampleApplicationInstance() *appsv1alpha1.ApplicationInstance {
	return &appsv1alpha1.ApplicationInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1alpha1.GroupVersion.String(),
			Kind:       "ApplicationInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nextcloud",
			Namespace: "team-a",
		},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "nextcloud"},
			Chart: appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceHelm,
				URL:        "https://charts.example.com",
				Name:       "nextcloud",
				Version:    "6.6.0",
			},
			Release: appsv1alpha1.ReleaseSpec{Name: "nextcloud", Namespace: "team-a"},
			Values:  appsv1alpha1.ValuesSpec{Source: appsv1alpha1.ValuesSourceInline},
		},
	}
}
