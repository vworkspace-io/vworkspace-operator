package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	mediaTypeV1 = "application/vnd.vworkspace.agent.v1+json"

	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
	OutcomeNoop      = "noop"
	OutcomeConflict  = "conflict"
)

// Job represents a Pull-mode work item from the control plane.
type Job struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotencyKey"`
	CreatedAt      time.Time       `json:"createdAt"`
	ExpiresAt      time.Time       `json:"expiresAt"`
	Signature      string          `json:"signature,omitempty"`
}

// JobsResponse is returned by GET /api/agent/jobs.
type JobsResponse struct {
	Jobs []Job `json:"jobs"`
}

// StatusUpdate is posted to /api/agent/jobs/{id}/status.
type StatusUpdate struct {
	Phase      string             `json:"phase"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	Message    string             `json:"message,omitempty"`
	Timestamp  time.Time          `json:"timestamp"`
}

// AppliedRef identifies a resource applied by the operator.
type AppliedRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	UID        string `json:"uid,omitempty"`
	Generation int64  `json:"generation,omitempty"`
}

// JobResult is posted to /api/agent/jobs/{id}/result.
type JobResult struct {
	Outcome    string      `json:"outcome"`
	Error      string      `json:"error,omitempty"`
	AppliedRef *AppliedRef `json:"appliedRef,omitempty"`
	Timestamp  time.Time   `json:"timestamp"`
}

// EndpointPayload reports a reachable service URL on Ready ApplicationInstance events.
type EndpointPayload struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ManagedStoragePayload carries inline S3 credentials for managed SeaweedFS registry sync.
type ManagedStoragePayload struct {
	AccessKeyID     string `json:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	BucketName      string `json:"bucketName,omitempty"`
}

// EventExtras optional fields merged into selected ConditionTransition events.
type EventExtras struct {
	Endpoints      []EndpointPayload
	ManagedStorage *ManagedStoragePayload
}

// Event represents a batched status event.
type Event struct {
	// EventKey is a stable idempotency key for control-plane-side deduplication.
	EventKey    string             `json:"eventKey,omitempty"`
	Kind        string             `json:"kind"`
	ResourceRef AppliedRef         `json:"resourceRef"`
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
	Endpoints      []EndpointPayload      `json:"endpoints,omitempty"`
	ManagedStorage *ManagedStoragePayload `json:"managedStorage,omitempty"`
	Timestamp   time.Time          `json:"timestamp"`
}

// EventsRequest is posted to /api/agent/events.
type EventsRequest struct {
	Events []Event `json:"events"`
}

// Config configures the Pull-mode HTTP client.
type Config struct {
	BaseURL   string
	ClusterID string
	Token     string
	HTTP      *http.Client
}

// RotateCredentialsResponse is returned by POST /api/agent/credentials/rotate.
type RotateCredentialsResponse struct {
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// Client communicates with the control plane using the Pull-mode job protocol.
type Client interface {
	FetchJobs(ctx context.Context, waitSeconds int) ([]Job, error)
	AckJob(ctx context.Context, jobID string) error
	ReportStatus(ctx context.Context, jobID string, update StatusUpdate) error
	ReportResult(ctx context.Context, jobID string, result JobResult) error
	PostEvents(ctx context.Context, events EventsRequest) error
	Heartbeat(ctx context.Context) error
	RotateCredentials(ctx context.Context) (RotateCredentialsResponse, error)
}

// HTTPClient implements Client over HTTPS.
type HTTPClient struct {
	baseURL   string
	clusterID string
	token     string
	http      *http.Client
}

func NewHTTPClient(cfg Config) (*HTTPClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if strings.TrimSpace(cfg.ClusterID) == "" {
		return nil, fmt.Errorf("cluster ID is required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("token is required")
	}
	httpClient := cfg.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	return &HTTPClient{
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		clusterID: cfg.ClusterID,
		token:     cfg.Token,
		http:      httpClient,
	}, nil
}

func (c *HTTPClient) FetchJobs(ctx context.Context, waitSeconds int) ([]Job, error) {
	query := url.Values{}
	query.Set("cluster", c.clusterID)
	query.Set("wait", strconv.Itoa(waitSeconds))
	endpoint := fmt.Sprintf("%s/api/agent/jobs?%s", c.baseURL, query.Encode())
	resp, err := c.do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}
	var payload JobsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode jobs response: %w", err)
	}
	return payload.Jobs, nil
}

func (c *HTTPClient) AckJob(ctx context.Context, jobID string) error {
	endpoint := fmt.Sprintf("%s/api/agent/jobs/%s/ack", c.baseURL, url.PathEscape(jobID))
	resp, err := c.do(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusConflict {
		return nil
	}
	return c.decodeError(resp)
}

func (c *HTTPClient) ReportStatus(ctx context.Context, jobID string, update StatusUpdate) error {
	endpoint := fmt.Sprintf("%s/api/agent/jobs/%s/status", c.baseURL, url.PathEscape(jobID))
	resp, err := c.do(ctx, http.MethodPost, endpoint, update)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return c.decodeError(resp)
}

func (c *HTTPClient) ReportResult(ctx context.Context, jobID string, result JobResult) error {
	endpoint := fmt.Sprintf("%s/api/agent/jobs/%s/result", c.baseURL, url.PathEscape(jobID))
	resp, err := c.do(ctx, http.MethodPost, endpoint, result)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusConflict {
		return nil
	}
	return c.decodeError(resp)
}

func (c *HTTPClient) PostEvents(ctx context.Context, events EventsRequest) error {
	endpoint := fmt.Sprintf("%s/api/agent/events", c.baseURL)
	resp, err := c.do(ctx, http.MethodPost, endpoint, events)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return c.decodeError(resp)
}

func (c *HTTPClient) Heartbeat(ctx context.Context) error {
	return c.PostEvents(ctx, EventsRequest{
		Events: []Event{{
			Kind: "ClusterHeartbeat",
			ResourceRef: AppliedRef{
				APIVersion: "ops.vworkspace.io/v1alpha1",
				Kind:       "Cluster",
				Name:       c.clusterID,
			},
			Timestamp: time.Now().UTC(),
		}},
	})
}

func (c *HTTPClient) RotateCredentials(ctx context.Context) (RotateCredentialsResponse, error) {
	endpoint := fmt.Sprintf("%s/api/agent/credentials/rotate", c.baseURL)
	resp, err := c.do(ctx, http.MethodPost, endpoint, struct{}{})
	if err != nil {
		return RotateCredentialsResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return RotateCredentialsResponse{}, c.decodeError(resp)
	}
	var payload RotateCredentialsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return RotateCredentialsResponse{}, fmt.Errorf("decode rotate response: %w", err)
	}
	payload.Token = strings.TrimSpace(payload.Token)
	if payload.Token == "" {
		return RotateCredentialsResponse{}, fmt.Errorf("rotate response missing token")
	}
	return payload, nil
}

func (c *HTTPClient) do(ctx context.Context, method, endpoint string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reader = strings.NewReader(string(payload))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", mediaTypeV1)
	if body != nil {
		req.Header.Set("Content-Type", mediaTypeV1)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	return resp, nil
}

type apiError struct {
	Code      string `json:"error"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

func (c *HTTPClient) decodeError(resp *http.Response) error {
	var payload apiError
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload.Message == "" {
		payload.Message = resp.Status
	}
	return fmt.Errorf("agent API error (%d): %s: %s", resp.StatusCode, payload.Code, payload.Message)
}

var _ Client = (*HTTPClient)(nil)
