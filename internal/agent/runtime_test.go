package agent

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const runtimeTestNamespace = "vworkspace-system"

func runtimeTestScheme(t *testing.T) *kruntime.Scheme {
	t.Helper()
	scheme := kruntime.NewScheme()
	if err := opsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add ops scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	return scheme
}

func TestRuntimeResolveCredentialsTargetWithoutCluster(t *testing.T) {
	scheme := runtimeTestScheme(t)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	agentRuntime := NewRuntime(RuntimeConfig{
		K8s:             cl,
		Scheme:          scheme,
		SecretNamespace: runtimeTestNamespace,
		SecretName:      DefaultCredentialsSecret,
	})

	ns, name, present, err := agentRuntime.resolveCredentialsTarget(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentialsTarget: %v", err)
	}
	if present {
		t.Fatal("expected no cluster to be present")
	}
	if ns != runtimeTestNamespace || name != DefaultCredentialsSecret {
		t.Fatalf("unexpected default target %s/%s", ns, name)
	}
}

func TestRuntimeResolveCredentialsTargetFromClusterStatus(t *testing.T) {
	scheme := runtimeTestScheme(t)
	cluster := &opsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-local"},
		Status: opsv1alpha1.ClusterStatus{
			CredentialsSecretRef: &opsv1alpha1.SecretReference{
				Name:      "custom-creds",
				Namespace: runtimeTestNamespace,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	agentRuntime := NewRuntime(RuntimeConfig{
		K8s:             cl,
		Scheme:          scheme,
		SecretNamespace: runtimeTestNamespace,
		SecretName:      DefaultCredentialsSecret,
	})

	ns, name, present, err := agentRuntime.resolveCredentialsTarget(context.Background())
	if err != nil {
		t.Fatalf("resolveCredentialsTarget: %v", err)
	}
	if !present {
		t.Fatal("expected cluster to be present")
	}
	if ns != runtimeTestNamespace || name != "custom-creds" {
		t.Fatalf("unexpected target %s/%s", ns, name)
	}
}

func TestRuntimeIdlesWithoutCredentialsSecret(t *testing.T) {
	scheme := runtimeTestScheme(t)
	cluster := &opsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-local"},
		Spec:       opsv1alpha1.ClusterSpec{ControlPlaneEndpoint: "https://workspace.example.org"},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	agentRuntime := NewRuntime(RuntimeConfig{
		K8s:             cl,
		Scheme:          scheme,
		SecretNamespace: runtimeTestNamespace,
		SecretName:      DefaultCredentialsSecret,
		Log:             logr.Discard(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	agentRuntime.Start(ctx)

	agentRuntime.mu.Lock()
	running := agentRuntime.running
	agentRuntime.mu.Unlock()
	if running {
		t.Fatal("expected poller to remain idle without credentials secret")
	}
}

func TestRuntimeStartsWhenCredentialsExist(t *testing.T) {
	scheme := runtimeTestScheme(t)
	credsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: DefaultCredentialsSecret, Namespace: runtimeTestNamespace},
		Data: map[string][]byte{
			SecretKeyControlPlaneBaseURL: []byte("https://workspace.example.org"),
			SecretKeyClusterID:           []byte("cluster-local"),
			SecretKeyToken:               []byte("bootstrap-token"),
		},
	}
	cluster := &opsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-local"},
		Status: opsv1alpha1.ClusterStatus{
			CredentialsSecretRef: &opsv1alpha1.SecretReference{
				Name:      DefaultCredentialsSecret,
				Namespace: runtimeTestNamespace,
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, credsSecret).Build()
	batcher := NewEventBatcher(nil)
	agentRuntime := NewRuntime(RuntimeConfig{
		K8s:             cl,
		Scheme:          scheme,
		SecretNamespace: runtimeTestNamespace,
		SecretName:      DefaultCredentialsSecret,
		EventBatcher:    batcher,
		Log:             logr.Discard(),
	})

	if err := agentRuntime.ensurePoller(context.Background()); err != nil {
		t.Fatalf("ensurePoller: %v", err)
	}

	agentRuntime.mu.Lock()
	running := agentRuntime.running
	clusterID := agentRuntime.clusterID
	agentRuntime.mu.Unlock()
	if !running {
		t.Fatal("expected poller to start when credentials exist")
	}
	if clusterID != "cluster-local" {
		t.Fatalf("expected cluster-local, got %q", clusterID)
	}
	if batcher.Client == nil {
		t.Fatal("expected shared event batcher client to be wired")
	}
}
