package mockodoo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/test/mockodoo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMockOdooRegister(t *testing.T) {
	srv := mockodoo.NewServer()
	srv.AddRegistrationToken("one-time", "cluster-prod-1")
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	client := &agent.HTTPRegistrationClient{HTTP: server.Client()}
	resp, err := client.Register(context.Background(), server.URL, "one-time", "cluster-prod-1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.ClusterID != "cluster-prod-1" || resp.Token == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestMockOdooJobAckAndResult(t *testing.T) {
	srv := mockodoo.NewServer()
	srv.SetBootstrapToken("cluster-1", "token-1")

	app := &appsv1alpha1.ApplicationInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1alpha1.GroupVersion.String(),
			Kind:       "ApplicationInstance",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "app1", Namespace: "ns1"},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef:  appsv1alpha1.AppRef{CatalogID: "x"},
			Chart:   appsv1alpha1.ChartSpec{SourceType: appsv1alpha1.ChartSourceHelm, URL: "https://example.com", Name: "c", Version: "1.0.0"},
			Release: appsv1alpha1.ReleaseSpec{Name: "app1", Namespace: "ns1"},
			Values:  appsv1alpha1.ValuesSpec{Source: appsv1alpha1.ValuesSourceInline},
		},
	}
	payload, _ := json.Marshal(app)

	srv.EnqueueJob("cluster-1", agent.Job{
		ID:             "job-1",
		Kind:           "apply",
		Payload:        payload,
		IdempotencyKey: "once",
		ExpiresAt:      time.Now().Add(time.Hour),
	})

	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	httpClient, err := agent.NewHTTPClient(agent.Config{
		BaseURL:   server.URL,
		ClusterID: "cluster-1",
		Token:     "token-1",
		HTTP:      server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	jobs, err := httpClient.FetchJobs(context.Background(), 0)
	if err != nil {
		t.Fatalf("FetchJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job-1" {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}

	if err := httpClient.AckJob(context.Background(), "job-1"); err != nil {
		t.Fatalf("AckJob: %v", err)
	}
	if !srv.WasAcked("job-1") {
		t.Fatal("expected job to be acked on server")
	}

	result := agent.JobResult{
		Outcome:   agent.OutcomeSucceeded,
		Timestamp: time.Now().UTC(),
	}
	if err := httpClient.ReportResult(context.Background(), "job-1", result); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
	got, ok := srv.JobResult("job-1")
	if !ok || got.Outcome != agent.OutcomeSucceeded {
		t.Fatalf("unexpected stored result: %+v ok=%v", got, ok)
	}
}

func TestMockOdooPollerIntegration(t *testing.T) {
	srv := mockodoo.NewServer()
	srv.SetBootstrapToken("cluster-1", "token-1")

	app := &appsv1alpha1.ApplicationInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1alpha1.GroupVersion.String(),
			Kind:       "ApplicationInstance",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "app1", Namespace: "ns1"},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef:  appsv1alpha1.AppRef{CatalogID: "x"},
			Chart:   appsv1alpha1.ChartSpec{SourceType: appsv1alpha1.ChartSourceHelm, URL: "https://example.com", Name: "c", Version: "1.0.0"},
			Release: appsv1alpha1.ReleaseSpec{Name: "app1", Namespace: "ns1"},
			Values:  appsv1alpha1.ValuesSpec{Source: appsv1alpha1.ValuesSourceInline},
		},
	}
	payload, _ := json.Marshal(app)
	srv.EnqueueJob("cluster-1", agent.Job{
		ID:             "job-poller",
		Kind:           "apply",
		Payload:        payload,
		IdempotencyKey: "poller-once",
		ExpiresAt:      time.Now().Add(time.Hour),
	})

	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	httpClient, err := agent.NewHTTPClient(agent.Config{
		BaseURL:   server.URL,
		ClusterID: "cluster-1",
		Token:     "token-1",
		HTTP:      server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	poller := &agent.AgentPoller{
		Client:   httpClient,
		Applier:  &agent.Applier{Client: cl, Scheme: scheme, ClusterID: "cluster-1"},
		WaitSecs: 0,
	}
	go poller.Run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if srv.WasAcked("job-poller") {
			if res, ok := srv.JobResult("job-poller"); ok && res.Outcome == agent.OutcomeSucceeded {
				cancel()
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("poller did not ack and report success within timeout")
}

func TestMockOdooRejectsWrongCluster(t *testing.T) {
	srv := mockodoo.NewServer()
	srv.SetBootstrapToken("cluster-1", "token-1")
	server := httptest.NewServer(srv.Handler())
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/agent/jobs?cluster=other-cluster&wait=0", nil)
	req.Header.Set("Authorization", "Bearer token-1")
	req.Header.Set("Accept", agentMediaType())
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func agentMediaType() string {
	return "application/vnd.vworkspace.agent.v1+json"
}
