package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientFetchJobsAndAck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/agent/jobs" {
			if got := r.Header.Get("Accept"); got != mediaTypeV1 {
				t.Fatalf("unexpected accept header: %s", got)
			}
			_ = json.NewEncoder(w).Encode(JobsResponse{Jobs: []Job{{
				ID:   "j-1",
				Kind: "apply",
			}}})
			return
		}
		if r.URL.Path == "/api/agent/jobs/j-1/ack" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := NewHTTPClient(Config{
		BaseURL:   server.URL,
		ClusterID: "cluster-1",
		Token:     "token",
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	jobs, err := client.FetchJobs(context.Background(), 0)
	if err != nil {
		t.Fatalf("FetchJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "j-1" {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}
	if err := client.AckJob(context.Background(), "j-1"); err != nil {
		t.Fatalf("AckJob: %v", err)
	}
}

func TestHTTPClientReportResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/jobs/j-2/result" {
			http.NotFound(w, r)
			return
		}
		var payload JobResult
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode result: %v", err)
		}
		if payload.Outcome != "succeeded" {
			t.Fatalf("unexpected outcome: %s", payload.Outcome)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewHTTPClient(Config{BaseURL: server.URL, ClusterID: "cluster-1", Token: "token"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	if err := client.ReportResult(context.Background(), "j-2", JobResult{
		Outcome:   "succeeded",
		Timestamp: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ReportResult: %v", err)
	}
}
