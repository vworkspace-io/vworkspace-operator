package agent

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	return s
}

func TestCredentialsLoadFromSecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "system"},
		Data: map[string][]byte{
			SecretKeyControlPlaneBaseURL: []byte("https://control-plane.example.org"),
			SecretKeyClusterID:           []byte("cluster-1"),
			SecretKeyToken:               []byte("secret-token"),
		},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(secret).Build()

	creds, err := CredentialsConfig{
		SecretNamespace: "system",
		SecretName:      "creds",
		K8s:             cl,
	}.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if creds.BaseURL != "https://control-plane.example.org" || creds.ClusterID != "cluster-1" || creds.Token != "secret-token" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}

func TestCredentialsFlagsOverrideSecret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "system"},
		Data: map[string][]byte{
			SecretKeyControlPlaneBaseURL: []byte("https://from-secret.example.org"),
			SecretKeyClusterID:           []byte("cluster-secret"),
			SecretKeyToken:               []byte("secret-token"),
		},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(secret).Build()

	creds, err := CredentialsConfig{
		BaseURL:         "https://from-flag.example.org",
		ClusterID:       "cluster-flag",
		Token:           "flag-token",
		SecretNamespace: "system",
		SecretName:      "creds",
		K8s:             cl,
	}.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if creds.BaseURL != "https://from-flag.example.org" {
		t.Fatalf("expected flag base URL")
	}
}
