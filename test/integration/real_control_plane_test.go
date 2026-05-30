//go:build integration

package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
)

// TestRealControlPlaneRegisterPollHeartbeat exercises a live vWorkspace Server agent API.
//
// Required environment (skipped when unset):
//   - CONTROL_PLANE_BASE_URL — e.g. http://127.0.0.1:8069
//   - VWORKSPACE_REGISTRATION_TOKEN — one-time token from Cluster Registry
//
// Optional:
//   - VWORKSPACE_CLUSTER_ID — cluster id bound to the token (default: cluster-integration-real)
//
// Run:
//
//	go test -tags=integration ./test/integration/... -run TestRealControlPlane -count=1 -v
func TestRealControlPlaneRegisterPollHeartbeat(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("CONTROL_PLANE_BASE_URL"))
	regToken := strings.TrimSpace(os.Getenv("VWORKSPACE_REGISTRATION_TOKEN"))
	if baseURL == "" || regToken == "" {
		t.Skip("set CONTROL_PLANE_BASE_URL and VWORKSPACE_REGISTRATION_TOKEN to run against a live control plane")
	}

	clusterID := strings.TrimSpace(os.Getenv("VWORKSPACE_CLUSTER_ID"))
	if clusterID == "" {
		clusterID = "cluster-integration-real"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	reg, err := agent.Register(ctx, baseURL, regToken, clusterID, nil)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if reg.ClusterID == "" || reg.Token == "" {
		t.Fatal("register response missing clusterId or token")
	}
	t.Logf("registered cluster %s", reg.ClusterID)

	client, err := agent.NewHTTPClient(agent.Config{
		BaseURL:   baseURL,
		ClusterID: reg.ClusterID,
		Token:     reg.Token,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	jobs, err := client.FetchJobs(ctx, 1)
	if err != nil {
		t.Fatalf("FetchJobs: %v", err)
	}
	t.Logf("FetchJobs returned %d job(s) (empty is OK for smoke test)", len(jobs))

	if err := client.Heartbeat(ctx); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}
