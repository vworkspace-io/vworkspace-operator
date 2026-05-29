package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestIdempotencyStoreReplayAfterRestart(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)

	app := sampleApplicationInstance()
	payload, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("marshal app: %v", err)
	}
	job := Job{
		ID:             "j-replay-1",
		Kind:           "apply",
		Payload:        payload,
		IdempotencyKey: "persist-key",
		ExpiresAt:      time.Now().Add(time.Hour),
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	store := &IdempotencyStore{
		Client:    cl,
		Namespace: "vworkspace-system",
		Name:      DefaultIdempotencyConfigMap,
	}
	applier := &Applier{Client: cl, Scheme: scheme, ClusterID: "cluster-1", Idempotency: store}

	outcome, err := applier.ApplyJob(context.Background(), job)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if outcome.Result.Outcome != OutcomeSucceeded {
		t.Fatalf("expected succeeded, got %s", outcome.Result.Outcome)
	}

	restartedStore := &IdempotencyStore{
		Client:    cl,
		Namespace: "vworkspace-system",
		Name:      DefaultIdempotencyConfigMap,
	}
	restartedApplier := &Applier{Client: cl, Scheme: scheme, ClusterID: "cluster-1", Idempotency: restartedStore}
	outcome, err = restartedApplier.ApplyJob(context.Background(), job)
	if err != nil {
		t.Fatalf("replay apply: %v", err)
	}
	if outcome.Result.Outcome != OutcomeNoop || !outcome.Idempotent {
		t.Fatalf("expected idempotent noop after restart, got %+v", outcome)
	}

	cm := &corev1.ConfigMap{}
	if err := cl.Get(context.Background(), client.ObjectKey{Namespace: "vworkspace-system", Name: DefaultIdempotencyConfigMap}, cm); err != nil {
		t.Fatalf("get configmap: %v", err)
	}
	if cm.Data[idempotencyDataKey] == "" {
		t.Fatal("expected idempotency keys persisted")
	}
}

func TestPersistCredentials(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	err := PersistCredentials(context.Background(), cl, "vworkspace-system", DefaultCredentialsSecret, Credentials{
		BaseURL:   "https://odoo.example.org",
		ClusterID: "cluster-1",
		Token:     "token",
	}, nil)
	if err != nil {
		t.Fatalf("PersistCredentials: %v", err)
	}

	secret := &corev1.Secret{}
	if err := cl.Get(context.Background(), client.ObjectKey{Namespace: "vworkspace-system", Name: DefaultCredentialsSecret}, secret); err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if string(secret.Data[SecretKeyToken]) != "token" {
		t.Fatalf("unexpected token: %q", secret.Data[SecretKeyToken])
	}
}
