// Package mockodoo implements an in-memory HTTP server that mimics the Pull-mode
// Odoo agent API for local development and integration tests.
package mockodoo

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
)

const mediaTypeV1 = "application/vnd.vworkspace.agent.v1+json"

// Server is an in-memory mock of the Odoo Pull-mode agent API.
type Server struct {
	mu sync.Mutex

	// registrationTokens maps one-time registration tokens to cluster IDs.
	registrationTokens map[string]string
	// clusters holds per-cluster state keyed by cluster ID.
	clusters map[string]*clusterState
	// tokenIndex maps bootstrap tokens to cluster IDs for auth lookup.
	tokenIndex map[string]string
}

type clusterState struct {
	bootstrapToken string
	pending        []agent.Job
	acked          map[string]bool
	statuses       map[string][]agent.StatusUpdate
	results        map[string]agent.JobResult
	events         []agent.Event
}

// NewServer returns an empty mock Odoo server.
func NewServer() *Server {
	s := &Server{
		registrationTokens: make(map[string]string),
		clusters:           make(map[string]*clusterState),
		tokenIndex:         make(map[string]string),
	}
	return s
}

// AddRegistrationToken registers a one-time token that POST /api/agent/register accepts.
func (s *Server) AddRegistrationToken(registrationToken, clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrationTokens[strings.TrimSpace(registrationToken)] = strings.TrimSpace(clusterID)
}

// SetBootstrapToken sets or replaces the long-lived token for a cluster (test helper).
func (s *Server) SetBootstrapToken(clusterID, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	clusterID = strings.TrimSpace(clusterID)
	token = strings.TrimSpace(token)
	cs := s.ensureClusterLocked(clusterID)
	if cs.bootstrapToken != "" {
		delete(s.tokenIndex, cs.bootstrapToken)
	}
	cs.bootstrapToken = token
	s.tokenIndex[token] = clusterID
}

// EnqueueJob adds a job to the cluster queue; long-polling clients are notified.
func (s *Server) EnqueueJob(clusterID string, job agent.Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs := s.ensureClusterLocked(clusterID)
	cs.pending = append(cs.pending, job)
}

// WasAcked reports whether a job was acknowledged.
func (s *Server) WasAcked(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cs := range s.clusters {
		if cs.acked[jobID] {
			return true
		}
	}
	return false
}

// JobResult returns the terminal result posted for a job, if any.
func (s *Server) JobResult(jobID string) (agent.JobResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cs := range s.clusters {
		if res, ok := cs.results[jobID]; ok {
			return res, true
		}
	}
	return agent.JobResult{}, false
}

// Events returns events posted to /api/agent/events for a cluster.
func (s *Server) Events(clusterID string) []agent.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs := s.clusters[clusterID]
	if cs == nil {
		return nil
	}
	out := make([]agent.Event, len(cs.events))
	copy(out, cs.events)
	return out
}

// Handler returns the HTTP handler for the mock API.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) ensureClusterLocked(clusterID string) *clusterState {
	cs, ok := s.clusters[clusterID]
	if !ok {
		cs = &clusterState{
			acked:    make(map[string]bool),
			statuses: make(map[string][]agent.StatusUpdate),
			results:  make(map[string]agent.JobResult),
		}
		s.clusters[clusterID] = cs
	}
	return cs
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/api/agent/register":
		s.handleRegister(w, r)
	case r.Method == http.MethodGet && path == "/api/agent/jobs":
		s.handleJobs(w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/agent/jobs/") && strings.HasSuffix(path, "/ack"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/agent/jobs/"), "/ack")
		s.handleAck(w, r, jobID)
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/agent/jobs/") && strings.HasSuffix(path, "/status"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/agent/jobs/"), "/status")
		s.handleStatus(w, r, jobID)
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/api/agent/jobs/") && strings.HasSuffix(path, "/result"):
		jobID := strings.TrimSuffix(strings.TrimPrefix(path, "/api/agent/jobs/"), "/result")
		s.handleResult(w, r, jobID)
	case r.Method == http.MethodPost && path == "/api/agent/events":
		s.handleEvents(w, r)
	default:
		writeError(w, http.StatusNotFound, "NotFound", "unknown endpoint")
	}
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !acceptsV1(r) {
		writeError(w, http.StatusBadRequest, "UnsupportedMediaType", "Accept header must include v1 media type")
		return
	}
	regToken := bearerToken(r)
	if regToken == "" {
		writeError(w, http.StatusUnauthorized, "Unauthorized", "missing bearer token")
		return
	}

	var req agent.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidJSON", err.Error())
		return
	}
	if strings.TrimSpace(req.RegistrationToken) == "" {
		req.RegistrationToken = regToken
	}

	s.mu.Lock()
	clusterID, ok := s.registrationTokens[req.RegistrationToken]
	if !ok {
		clusterID = strings.TrimSpace(req.ClusterID)
		if clusterID == "" {
			clusterID = "cluster-" + req.RegistrationToken[:min(8, len(req.RegistrationToken))]
		}
	}
	cs := s.ensureClusterLocked(clusterID)
	if cs.bootstrapToken == "" {
		cs.bootstrapToken = "bootstrap-" + clusterID
		s.tokenIndex[cs.bootstrapToken] = clusterID
	}
	delete(s.registrationTokens, req.RegistrationToken)
	token := cs.bootstrapToken
	s.mu.Unlock()

	w.Header().Set("Content-Type", mediaTypeV1)
	_ = json.NewEncoder(w).Encode(agent.RegisterResponse{
		ClusterID: clusterID,
		Token:     token,
	})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	clusterID := strings.TrimSpace(r.URL.Query().Get("cluster"))
	if clusterID == "" {
		writeError(w, http.StatusBadRequest, "MissingCluster", "cluster query parameter is required")
		return
	}
	wait := parseWait(r.URL.Query().Get("wait"))

	clusterID, err := s.authorize(r, clusterID)
	if err != nil {
		writeError(w, http.StatusForbidden, "Forbidden", err.Error())
		return
	}

	jobs := s.pollJobs(r.Context(), clusterID, wait)
	w.Header().Set("Content-Type", mediaTypeV1)
	_ = json.NewEncoder(w).Encode(agent.JobsResponse{Jobs: jobs})
}

func (s *Server) pollJobs(ctx context.Context, clusterID string, wait time.Duration) []agent.Job {
	deadline := time.Now().Add(wait)
	tick := 25 * time.Millisecond
	if wait <= 0 {
		tick = 0
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		s.mu.Lock()
		cs := s.clusters[clusterID]
		if cs != nil && len(cs.pending) > 0 {
			jobs := cs.pending
			cs.pending = nil
			s.mu.Unlock()
			return jobs
		}
		if wait <= 0 || time.Now().After(deadline) {
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()

		sleepFor := tick
		if remaining := time.Until(deadline); remaining < sleepFor {
			sleepFor = remaining
		}
		if sleepFor > 0 {
			timer := time.NewTimer(sleepFor)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	}
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request, jobID string) {
	clusterID, err := s.authorize(r, "")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cs := s.ensureClusterLocked(clusterID)
	if _, closed := cs.results[jobID]; closed {
		w.WriteHeader(http.StatusConflict)
		return
	}
	cs.acked[jobID] = true
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, jobID string) {
	clusterID, err := s.authorize(r, "")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized", err.Error())
		return
	}
	var update agent.StatusUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidJSON", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cs := s.ensureClusterLocked(clusterID)
	cs.statuses[jobID] = append(cs.statuses[jobID], update)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResult(w http.ResponseWriter, r *http.Request, jobID string) {
	clusterID, err := s.authorize(r, "")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized", err.Error())
		return
	}
	var result agent.JobResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidJSON", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cs := s.ensureClusterLocked(clusterID)
	if _, exists := cs.results[jobID]; exists {
		w.WriteHeader(http.StatusConflict)
		return
	}
	cs.results[jobID] = result
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	clusterID, err := s.authorize(r, "")
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Unauthorized", err.Error())
		return
	}
	var req agent.EventsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "InvalidJSON", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cs := s.ensureClusterLocked(clusterID)
	cs.events = append(cs.events, req.Events...)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorize(r *http.Request, clusterID string) (string, error) {
	token := bearerToken(r)
	if token == "" {
		return "", errUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	boundCluster, ok := s.tokenIndex[token]
	if !ok {
		return "", errUnauthorized
	}
	if clusterID != "" && boundCluster != clusterID {
		return "", errClusterMismatch
	}
	return boundCluster, nil
}

var (
	errUnauthorized    = errString("invalid or missing bearer token")
	errClusterMismatch = errString("cluster query parameter does not match token")
)

type errString string

func (e errString) Error() string { return string(e) }

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

func acceptsV1(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "vworkspace.agent.v1")
}

func parseWait(raw string) time.Duration {
	if raw == "" {
		raw = "30"
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs < 0 {
		secs = 30
	}
	if secs > 300 {
		secs = 300
	}
	return time.Duration(secs) * time.Second
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", mediaTypeV1)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
