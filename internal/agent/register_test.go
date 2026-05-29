package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRegistrationClientRegister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/register" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer one-time-token" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.RegistrationToken != "one-time-token" {
			t.Fatalf("unexpected token: %s", req.RegistrationToken)
		}
		_ = json.NewEncoder(w).Encode(RegisterResponse{
			ClusterID: "cluster-prod-1",
			Token:     "long-lived-token",
		})
	}))
	defer server.Close()

	client := &HTTPRegistrationClient{}
	resp, err := client.Register(context.Background(), server.URL, "one-time-token", "cluster-prod-1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.ClusterID != "cluster-prod-1" || resp.Token != "long-lived-token" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHTTPRegistrationClientRegisterError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(apiError{Code: "RegistrationTokenInvalid", Message: "expired"})
	}))
	defer server.Close()

	_, err := (&HTTPRegistrationClient{}).Register(context.Background(), server.URL, "bad-token", "")
	if err == nil {
		t.Fatal("expected error")
	}
}
