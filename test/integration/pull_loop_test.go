package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/controller"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"github.com/vworkspace-io/vworkspace-operator/internal/labels"
	"github.com/vworkspace-io/vworkspace-operator/test/mockcontrolplane"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	testClusterID = "cluster-integration-1"
	testToken     = "bootstrap-integration"
)

func TestPullLoopApplyReconcileAndReport(t *testing.T) {
	ts := mockcontrolplane.NewTestServer()
	defer ts.Close()
	ts.SetBootstrapToken(testClusterID, testToken)

	app := sampleApplicationInstance("pull-loop-app", "team-a")
	payload, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("marshal app: %v", err)
	}
	ts.EnqueueJob(testClusterID, agent.Job{
		ID:             "job-pull-1",
		Kind:           "apply",
		Payload:        payload,
		IdempotencyKey: "pull-loop-key-1",
		ExpiresAt:      time.Now().Add(time.Hour),
	})

	cl, scheme := newPullLoopClient(t)
	httpClient, err := ts.NewAgentClient(testClusterID, testToken)
	if err != nil {
		t.Fatalf("NewAgentClient: %v", err)
	}

	store := &agent.IdempotencyStore{
		Client:    cl,
		Namespace: "vworkspace-system",
		Name:      "test-idempotency",
	}
	poller := &agent.AgentPoller{
		Client: httpClient,
		Applier: &agent.Applier{
			Client:      cl,
			Scheme:      scheme,
			ClusterID:   testClusterID,
			Idempotency: store,
		},
	}

	if err := poller.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	if !ts.WasAcked("job-pull-1") {
		t.Fatal("expected job to be acked on mock control plane")
	}
	res, ok := ts.JobResult("job-pull-1")
	if !ok || res.Outcome != agent.OutcomeSucceeded {
		t.Fatalf("expected succeeded result on mock control plane, got %+v ok=%v", res, ok)
	}

	got := &appsv1alpha1.ApplicationInstance{}
	key := types.NamespacedName{Namespace: app.Namespace, Name: app.Name}
	if err := cl.Get(context.Background(), key, got); err != nil {
		t.Fatalf("get applied ApplicationInstance: %v", err)
	}
	if got.Labels[labels.ManagedByKey] != labels.ManagedByControlPlane {
		t.Fatalf("expected managed-by control-plane, got %q", got.Labels[labels.ManagedByKey])
	}

	engine := helmengine.NewFluxEngine(cl)
	reconciler := &controller.ApplicationInstanceReconciler{
		Client: cl,
		Scheme: scheme,
		Engine: engine,
	}
	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	hr := &unstructured.Unstructured{}
	hr.SetGroupVersionKind(schema.GroupVersionKind{Group: "helm.toolkit.fluxcd.io", Version: "v2", Kind: "HelmRelease"})
	if err := cl.Get(ctx, client.ObjectKey{Namespace: app.Namespace, Name: app.Spec.Release.Name}, hr); err != nil {
		t.Fatalf("expected HelmRelease from reconciler: %v", err)
	}
}

func TestPullLoopIdempotentReplayNoop(t *testing.T) {
	ts := mockcontrolplane.NewTestServer()
	defer ts.Close()
	ts.SetBootstrapToken(testClusterID, testToken)

	app := sampleApplicationInstance("pull-loop-idem", "team-a")
	payload, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("marshal app: %v", err)
	}
	const idemKey = "pull-loop-idem-key"
	ts.EnqueueJob(testClusterID, agent.Job{
		ID:             "job-pull-idem",
		Kind:           "apply",
		Payload:        payload,
		IdempotencyKey: idemKey,
		ExpiresAt:      time.Now().Add(time.Hour),
	})

	cl, scheme := newPullLoopClient(t)
	store := &agent.IdempotencyStore{
		Client:    cl,
		Namespace: "vworkspace-system",
		Name:      "test-idempotency-idem",
	}
	if err := store.Record(context.Background(), idemKey); err != nil {
		t.Fatalf("seed idempotency key: %v", err)
	}

	httpClient, err := ts.NewAgentClient(testClusterID, testToken)
	if err != nil {
		t.Fatalf("NewAgentClient: %v", err)
	}
	poller := &agent.AgentPoller{
		Client: httpClient,
		Applier: &agent.Applier{
			Client:      cl,
			Scheme:      scheme,
			ClusterID:   testClusterID,
			Idempotency: store,
		},
	}
	if err := poller.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	res, ok := ts.JobResult("job-pull-idem")
	if !ok {
		t.Fatal("expected result posted to mock control plane")
	}
	if res.Outcome != agent.OutcomeNoop {
		t.Fatalf("expected noop outcome for idempotent replay, got %s", res.Outcome)
	}
}

func newPullLoopClient(t *testing.T) (client.Client, *runtime.Scheme) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := appsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&appsv1alpha1.ApplicationInstance{}).
		Build()
	return cl, scheme
}

func sampleApplicationInstance(name, namespace string) *appsv1alpha1.ApplicationInstance {
	return &appsv1alpha1.ApplicationInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1alpha1.GroupVersion.String(),
			Kind:       "ApplicationInstance",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "nextcloud"},
			Chart: appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceHelm,
				URL:        "https://charts.example.com",
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
