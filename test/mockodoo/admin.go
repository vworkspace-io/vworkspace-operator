package mockodoo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
)

const adminTokenHeader = "X-Mock-Odoo-Admin-Token"

// AdminEnqueueRequest enqueues a Pull-mode job for e2e and dev tooling.
type AdminEnqueueRequest struct {
	ClusterID string    `json:"clusterId"`
	Job       agent.Job `json:"job"`
}

// AdminClient talks to mock Odoo admin endpoints from outside the cluster.
type AdminClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewAdminClient returns an admin client for the given mock Odoo base URL.
func NewAdminClient(baseURL, token string, httpClient *http.Client) *AdminClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &AdminClient{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Token:   strings.TrimSpace(token),
		HTTP:    httpClient,
	}
}

// EnqueueJob posts a job to the mock Odoo admin API.
func (c *AdminClient) EnqueueJob(clusterID string, job agent.Job) error {
	body, err := json.Marshal(AdminEnqueueRequest{
		ClusterID: strings.TrimSpace(clusterID),
		Job:       job,
	})
	if err != nil {
		return fmt.Errorf("marshal enqueue request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/api/admin/enqueue", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build enqueue request: %w", err)
	}
	req.Header.Set("Content-Type", mediaTypeV1)
	req.Header.Set("Accept", mediaTypeV1)
	if c.Token != "" {
		req.Header.Set(adminTokenHeader, c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("enqueue job: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("enqueue job: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// JobResult fetches the terminal result for a job via the admin API.
func (c *AdminClient) JobResult(jobID string) (agent.JobResult, bool, error) {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/api/admin/jobs/"+jobID+"/result", nil)
	if err != nil {
		return agent.JobResult{}, false, fmt.Errorf("build result request: %w", err)
	}
	req.Header.Set("Accept", mediaTypeV1)
	if c.Token != "" {
		req.Header.Set(adminTokenHeader, c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return agent.JobResult{}, false, fmt.Errorf("get job result: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return agent.JobResult{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return agent.JobResult{}, false, fmt.Errorf(
			"get job result: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var result agent.JobResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agent.JobResult{}, false, fmt.Errorf("decode job result: %w", err)
	}
	return result, true, nil
}

// WaitForJobResult polls the admin API until a terminal result appears or timeout elapses.
func (c *AdminClient) WaitForJobResult(jobID string, timeout time.Duration) (agent.JobResult, error) {
	deadline := time.Now().Add(timeout)
	for {
		result, ok, err := c.JobResult(jobID)
		if err != nil {
			return agent.JobResult{}, err
		}
		if ok {
			return result, nil
		}
		if time.Now().After(deadline) {
			return agent.JobResult{}, fmt.Errorf("timed out waiting for job %q result", jobID)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *Server) authorizeAdmin(r *http.Request) bool {
	if s.AdminToken == "" {
		return true
	}
	return strings.TrimSpace(r.Header.Get(adminTokenHeader)) == s.AdminToken
}

func (s *Server) handleAdminEnqueue(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeAdmin(r) {
		writeError(w, http.StatusUnauthorized, "Unauthorized", "invalid admin token")
		return
	}
	var req AdminEnqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidJSON", err.Error())
		return
	}
	if strings.TrimSpace(req.ClusterID) == "" {
		writeError(w, http.StatusBadRequest, "MissingCluster", "clusterId is required")
		return
	}
	if strings.TrimSpace(req.Job.ID) == "" {
		writeError(w, http.StatusBadRequest, "MissingJobID", "job.id is required")
		return
	}
	s.EnqueueJob(req.ClusterID, req.Job)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminJobResult(w http.ResponseWriter, r *http.Request, jobID string) {
	if !s.authorizeAdmin(r) {
		writeError(w, http.StatusUnauthorized, "Unauthorized", "invalid admin token")
		return
	}
	result, ok := s.JobResult(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "NotFound", "job result not found")
		return
	}
	w.Header().Set("Content-Type", mediaTypeV1)
	_ = json.NewEncoder(w).Encode(result)
}
