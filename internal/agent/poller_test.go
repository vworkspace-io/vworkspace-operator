package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAgentPollerProcessesJob(t *testing.T) {
	var mu sync.Mutex
	acked := false
	resulted := false
	jobsReturned := false

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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agent/jobs":
			mu.Lock()
			defer mu.Unlock()
			if jobsReturned {
				_ = json.NewEncoder(w).Encode(JobsResponse{Jobs: nil})
				return
			}
			jobsReturned = true
			_ = json.NewEncoder(w).Encode(JobsResponse{Jobs: []Job{{
				ID:             "job-1",
				Kind:           "apply",
				Payload:        payload,
				IdempotencyKey: "once",
				ExpiresAt:      time.Now().Add(time.Hour),
			}}})
		case "/api/agent/jobs/job-1/ack":
			mu.Lock()
			acked = true
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case "/api/agent/jobs/job-1/result":
			mu.Lock()
			resulted = true
			mu.Unlock()
			var res JobResult
			_ = json.NewDecoder(r.Body).Decode(&res)
			if res.Outcome != OutcomeSucceeded {
				t.Errorf("unexpected outcome %s", res.Outcome)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	httpClient, err := NewHTTPClient(Config{
		BaseURL:   server.URL,
		ClusterID: "cluster-1",
		Token:     "token",
		HTTP:      server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	poller := &AgentPoller{
		Client:   httpClient,
		Applier:  &Applier{Client: cl, Scheme: scheme, ClusterID: "cluster-1"},
		WaitSecs: 0,
	}
	go poller.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := acked && resulted
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if !acked || !resulted {
		t.Fatalf("expected ack and result, acked=%v resulted=%v", acked, resulted)
	}
}
