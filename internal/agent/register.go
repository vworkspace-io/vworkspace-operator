package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RegisterRequest is posted to POST /api/agent/register.
type RegisterRequest struct {
	RegistrationToken string `json:"registrationToken"`
	ClusterID         string `json:"clusterId,omitempty"`
	DisplayName       string `json:"displayName,omitempty"`
}

// RegisterResponse is returned after a successful registration exchange.
type RegisterResponse struct {
	ClusterID string     `json:"clusterId"`
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// RegistrationClient exchanges one-time registration tokens for bootstrap credentials.
type RegistrationClient interface {
	Register(ctx context.Context, baseURL, registrationToken, clusterID string) (RegisterResponse, error)
}

// HTTPRegistrationClient implements RegistrationClient over HTTPS.
type HTTPRegistrationClient struct {
	HTTP *http.Client
}

func (c *HTTPRegistrationClient) Register(ctx context.Context, baseURL, registrationToken, clusterID string) (RegisterResponse, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	registrationToken = strings.TrimSpace(registrationToken)
	if baseURL == "" {
		return RegisterResponse{}, fmt.Errorf("control plane base URL is required")
	}
	if registrationToken == "" {
		return RegisterResponse{}, fmt.Errorf("registration token is required")
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}

	body, err := json.Marshal(RegisterRequest{
		RegistrationToken: registrationToken,
		ClusterID:         strings.TrimSpace(clusterID),
	})
	if err != nil {
		return RegisterResponse{}, fmt.Errorf("marshal register request: %w", err)
	}

	endpoint := baseURL + "/api/agent/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return RegisterResponse{}, fmt.Errorf("create register request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+registrationToken)
	req.Header.Set("Accept", mediaTypeV1)
	req.Header.Set("Content-Type", mediaTypeV1)

	resp, err := httpClient.Do(req)
	if err != nil {
		return RegisterResponse{}, fmt.Errorf("execute register request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return RegisterResponse{}, decodeRegisterError(resp)
	}

	var payload RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return RegisterResponse{}, fmt.Errorf("decode register response: %w", err)
	}
	payload.ClusterID = strings.TrimSpace(payload.ClusterID)
	payload.Token = strings.TrimSpace(payload.Token)
	if payload.ClusterID == "" || payload.Token == "" {
		return RegisterResponse{}, fmt.Errorf("register response missing clusterId or token")
	}
	return payload, nil
}

func decodeRegisterError(resp *http.Response) error {
	var payload apiError
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	if payload.Message == "" {
		payload.Message = resp.Status
	}
	return fmt.Errorf("register API error (%d): %s: %s", resp.StatusCode, payload.Code, payload.Message)
}

// Register is a convenience wrapper for HTTPRegistrationClient.
func Register(ctx context.Context, baseURL, registrationToken, clusterID string, httpClient *http.Client) (RegisterResponse, error) {
	return (&HTTPRegistrationClient{HTTP: httpClient}).Register(ctx, baseURL, registrationToken, clusterID)
}
