package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RuntimeConfig configures the Pull-mode agent loop with credential reload.
type RuntimeConfig struct {
	K8s              client.Client
	Scheme           *runtime.Scheme
	SecretNamespace  string
	SecretName       string
	PollInterval     time.Duration
	Idempotency      *IdempotencyStore
	EventBatcher     *EventBatcher
	Log              logr.Logger
	InitialClusterID string
}

// Runtime watches credentials and runs the Pull-mode poller when material is available.
type Runtime struct {
	cfg RuntimeConfig

	mu        sync.Mutex
	cancel    context.CancelFunc
	running   bool
	clusterID string
}

// NewRuntime builds a Pull-mode runtime from config.
func NewRuntime(cfg RuntimeConfig) *Runtime {
	return &Runtime{cfg: cfg}
}

// Start blocks until ctx is cancelled, reloading credentials when the Secret changes.
func (r *Runtime) Start(ctx context.Context) {
	if r.cfg.K8s == nil || r.cfg.Scheme == nil {
		return
	}
	if r.cfg.SecretName == "" {
		r.cfg.SecretName = DefaultCredentialsSecret
	}
	if r.cfg.PollInterval <= 0 {
		r.cfg.PollInterval = 30 * time.Second
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		if err := r.ensurePoller(ctx); err != nil && r.cfg.Log.GetSink() != nil {
			SetConnectivityState("pull", 0)
			r.cfg.Log.Info("waiting for agent credentials", "reason", err.Error())
		}
		select {
		case <-ctx.Done():
			r.stopPoller()
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) ensurePoller(ctx context.Context) error {
	secret, found, err := GetCredentialsSecret(ctx, r.cfg.K8s, r.cfg.SecretNamespace, r.cfg.SecretName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("credentials secret %s/%s not found", r.cfg.SecretNamespace, r.cfg.SecretName)
	}
	creds, err := CredentialsFromSecret(secret)
	if err != nil {
		return err
	}
	UpdateCredentialAgeFromSecret(secret)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running && r.clusterID == creds.ClusterID {
		return nil
	}
	r.stopPollerLocked()

	httpClient, err := NewHTTPClient(Config{
		BaseURL:   creds.BaseURL,
		ClusterID: creds.ClusterID,
		Token:     creds.Token,
	})
	if err != nil {
		return fmt.Errorf("configure agent client: %w", err)
	}

	pollerCtx, cancel := context.WithCancel(ctx)
	applier := &Applier{
		Client:      r.cfg.K8s,
		Scheme:      r.cfg.Scheme,
		ClusterID:   creds.ClusterID,
		Idempotency: r.cfg.Idempotency,
	}
	batcher := r.cfg.EventBatcher
	if batcher == nil {
		batcher = NewEventBatcher(httpClient)
		if r.cfg.Log.GetSink() != nil {
			batcher.Log = r.cfg.Log.WithName("events")
		}
		go batcher.Start(pollerCtx)
	}
	poller := &AgentPoller{
		Client:   httpClient,
		Applier:  applier,
		Events:   batcher,
		Log:      r.cfg.Log.WithName("poller"),
		WaitSecs: int(r.cfg.PollInterval.Seconds()),
	}

	go poller.Run(pollerCtx)

	r.cancel = cancel
	r.running = true
	r.clusterID = creds.ClusterID
	if r.cfg.Log.GetSink() != nil {
		r.cfg.Log.Info("pull-mode agent started", "cluster_id", creds.ClusterID)
	}
	return nil
}

func (r *Runtime) stopPoller() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopPollerLocked()
}

func (r *Runtime) stopPollerLocked() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.running = false
	r.clusterID = ""
}
