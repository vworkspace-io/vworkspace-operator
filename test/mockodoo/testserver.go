package mockodoo

import (
	"net/http"
	"net/http/httptest"

	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
)

// TestServer wraps a mock Odoo Server with an httptest listener for integration tests.
type TestServer struct {
	*Server
	HTTPServer *httptest.Server
	URL        string
}

// NewTestServer starts an in-memory mock Odoo API on a local httptest server.
// Call Close when the test finishes.
func NewTestServer() *TestServer {
	srv := NewServer()
	hs := httptest.NewServer(srv.Handler())
	return &TestServer{
		Server:     srv,
		HTTPServer: hs,
		URL:        hs.URL,
	}
}

// Close shuts down the httptest server.
func (ts *TestServer) Close() {
	ts.HTTPServer.Close()
}

// Client returns an http.Client that talks to the test server.
func (ts *TestServer) Client() *http.Client {
	return ts.HTTPServer.Client()
}

// NewAgentClient builds a Pull-mode HTTP client configured for this test server.
func (ts *TestServer) NewAgentClient(clusterID, token string) (*agent.HTTPClient, error) {
	return agent.NewHTTPClient(agent.Config{
		BaseURL:   ts.URL,
		ClusterID: clusterID,
		Token:     token,
		HTTP:      ts.HTTPServer.Client(),
	})
}
