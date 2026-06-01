package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type fakeRegistrationClient struct {
	resp agent.RegisterResponse
	err  error
}

func (f *fakeRegistrationClient) Register(_ context.Context, _, _, _ string) (agent.RegisterResponse, error) {
	return f.resp, f.err
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
}
