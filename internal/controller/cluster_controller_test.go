package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeRegistrationClient struct {
	resp  agent.RegisterResponse
	err   error
	calls int
}

func (f *fakeRegistrationClient) Register(_ context.Context, _, _, _ string) (agent.RegisterResponse, error) {
	f.calls++
	return f.resp, f.err
}

func newClusterTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := opsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add ops scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	return scheme
}

func newEventsServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/agent/events" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
}

func TestClusterReconcilerRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/register":
			_ = json.NewEncoder(w).Encode(agent.RegisterResponse{
				ClusterID: "cluster-prod-1",
				Token:     "bootstrap-token",
			})
		case "/api/agent/events":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	scheme := runtime.NewScheme()
	_ = opsv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	cluster := &opsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-prod-1",
		},
		Spec: opsv1alpha1.ClusterSpec{
			ClusterID:           "cluster-prod-1",
			ControlPlaneBaseURL: server.URL,
			RegistrationToken:   "one-time-token",
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	httpClient, err := agent.NewHTTPClient(agent.Config{
		BaseURL:   server.URL,
		ClusterID: "cluster-prod-1",
		Token:     "bootstrap-token",
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	reconciler := &ClusterReconciler{
		Client:             cl,
		Scheme:             scheme,
		AgentClient:        httpClient,
		RegistrationClient: &fakeRegistrationClient{resp: agent.RegisterResponse{ClusterID: "cluster-prod-1", Token: "bootstrap-token"}},
		CredentialsSecret:  agent.DefaultCredentialsSecret,
		OperatorNamespace:  "vworkspace-system",
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &opsv1alpha1.Cluster{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: cluster.Name}, updated); err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if updated.Spec.RegistrationToken != "" {
		t.Fatalf("expected registration token cleared, got %q", updated.Spec.RegistrationToken)
	}
	if updated.Status.CredentialStatus == nil || !updated.Status.CredentialStatus.RegistrationTokenConsumed {
		t.Fatal("expected registration token consumed status")
	}

	secret := &corev1.Secret{}
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "vworkspace-system", Name: agent.DefaultCredentialsSecret}, secret); err != nil {
		t.Fatalf("get credentials secret: %v", err)
	}
	if string(secret.Data[agent.SecretKeyToken]) != "bootstrap-token" {
		t.Fatalf("unexpected stored token")
	}
	if !conditions.IsTrue(updated.Status.Conditions, opsv1alpha1.ConditionAuthenticated) {
		t.Fatal("expected Authenticated=True after registration")
	}
	if !controllerContainsFinalizer(updated) {
		t.Fatal("expected cluster finalizer to be added")
	}
}

func controllerContainsFinalizer(c *opsv1alpha1.Cluster) bool {
	return slices.Contains(c.Finalizers, opsv1alpha1.ClusterFinalizer)
}

func TestClusterReconcilerRegistrationFromSecret(t *testing.T) {
	server := newEventsServer(t)
	defer server.Close()

	scheme := newClusterTestScheme(t)

	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-local-registration", Namespace: "vworkspace-system"},
		Data:       map[string][]byte{opsv1alpha1.DefaultRegistrationTokenKey: []byte("vwksp-reg-secret-token")},
	}
	cluster := &opsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-local"},
		Spec: opsv1alpha1.ClusterSpec{
			ControlPlaneEndpoint:       server.URL,
			RegistrationTokenSecretRef: &opsv1alpha1.SecretKeyRef{Name: "cluster-local-registration"},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, tokenSecret).WithStatusSubresource(cluster).Build()
	httpClient, err := agent.NewHTTPClient(agent.Config{BaseURL: server.URL, ClusterID: "cluster-local", Token: "bootstrap-token"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	regClient := &fakeRegistrationClient{resp: agent.RegisterResponse{ClusterID: "cluster-local", Token: "bootstrap-token"}}

	reconciler := &ClusterReconciler{
		Client:             cl,
		Scheme:             scheme,
		AgentClient:        httpClient,
		RegistrationClient: regClient,
		CredentialsSecret:  agent.DefaultCredentialsSecret,
		OperatorNamespace:  "vworkspace-system",
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &opsv1alpha1.Cluster{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: cluster.Name}, updated); err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if regClient.calls != 1 {
		t.Fatalf("expected exactly one registration call, got %d", regClient.calls)
	}
	if updated.Status.ObservedToken == "" {
		t.Fatal("expected observedToken fingerprint to be set")
	}
	if updated.Status.CredentialsSecretRef == nil || updated.Status.CredentialsSecretRef.Name != agent.DefaultCredentialsSecret {
		t.Fatalf("expected credentialsSecretRef to point at the credentials secret, got %+v", updated.Status.CredentialsSecretRef)
	}
	if updated.Status.Phase != opsv1alpha1.ClusterPhaseConnected {
		t.Fatalf("expected phase Connected, got %q", updated.Status.Phase)
	}
	if !conditions.IsTrue(updated.Status.Conditions, opsv1alpha1.ConditionConnected) {
		t.Fatal("expected Connected=True")
	}

	// The Secret-backed token must never be mutated out of the spec.
	if updated.Spec.RegistrationTokenSecretRef == nil {
		t.Fatal("expected registrationTokenSecretRef to be preserved")
	}
	tok := &corev1.Secret{}
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "vworkspace-system", Name: "cluster-local-registration"}, tok); err != nil {
		t.Fatalf("expected token secret to be preserved: %v", err)
	}

	// A second reconcile with an unchanged token fingerprint must not re-register.
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}}); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if regClient.calls != 1 {
		t.Fatalf("expected no re-registration on unchanged token, got %d calls", regClient.calls)
	}
}

func TestClusterReconcilerPendingWhenTokenSecretMissing(t *testing.T) {
	scheme := newClusterTestScheme(t)
	cluster := &opsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-local"},
		Spec: opsv1alpha1.ClusterSpec{
			ControlPlaneEndpoint:       "https://workspace.example.org",
			RegistrationTokenSecretRef: &opsv1alpha1.SecretKeyRef{Name: "missing-secret"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	reconciler := &ClusterReconciler{
		Client:            cl,
		Scheme:            scheme,
		CredentialsSecret: agent.DefaultCredentialsSecret,
		OperatorNamespace: "vworkspace-system",
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &opsv1alpha1.Cluster{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: cluster.Name}, updated); err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if updated.Status.Phase != opsv1alpha1.ClusterPhasePending {
		t.Fatalf("expected phase Pending, got %q", updated.Status.Phase)
	}
	cond, ok := conditions.Get(updated.Status.Conditions, opsv1alpha1.ConditionAuthenticated)
	if !ok || cond.Status != metav1.ConditionFalse || cond.Reason != "TokenSecretMissing" {
		t.Fatalf("expected Authenticated=False/TokenSecretMissing, got %+v", cond)
	}
	credSecret := &corev1.Secret{}
	if err := cl.Get(context.Background(), types.NamespacedName{Namespace: "vworkspace-system", Name: agent.DefaultCredentialsSecret}, credSecret); !apierrors.IsNotFound(err) {
		t.Fatalf("expected no credentials secret while pending, got %v", err)
	}
}

func TestClusterReconcilerRemovesFinalizerOnDelete(t *testing.T) {
	scheme := newClusterTestScheme(t)
	now := metav1.Now()
	cluster := &opsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cluster-local",
			DeletionTimestamp: &now,
			Finalizers:        []string{opsv1alpha1.ClusterFinalizer},
		},
		Spec: opsv1alpha1.ClusterSpec{ControlPlaneEndpoint: "https://workspace.example.org"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	reconciler := &ClusterReconciler{
		Client:            cl,
		Scheme:            scheme,
		CredentialsSecret: agent.DefaultCredentialsSecret,
		OperatorNamespace: "vworkspace-system",
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &opsv1alpha1.Cluster{}
	err := cl.Get(context.Background(), types.NamespacedName{Name: cluster.Name}, updated)
	if apierrors.IsNotFound(err) {
		return // object garbage-collected once the finalizer was removed
	}
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if controllerContainsFinalizer(updated) {
		t.Fatalf("expected cluster finalizer to be removed, got %v", updated.Finalizers)
	}
}
