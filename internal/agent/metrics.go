package agent

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	pullJobLagSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "vworkspace_operator_pull_job_lag_seconds",
		Help: "Age in seconds of the oldest fetched Pull-mode job not yet applied.",
	})
	connectivityState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "vworkspace_operator_connectivity_state",
		Help: "Pull-mode connectivity state: 1 connected, 0 reconnecting, -1 disconnected.",
	}, []string{"mode"})
	appliedJobsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "vworkspace_operator_applied_jobs_total",
		Help: "Total number of Pull-mode jobs applied successfully.",
	})
	eventBufferOccupancy = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "vworkspace_operator_event_buffer_occupancy",
		Help: "Current depth of the outbound Pull-mode event buffer.",
	})
	credentialUpdatedAtUnix atomic.Int64
)

func init() {
	metrics.Registry.MustRegister(
		pullJobLagSeconds,
		connectivityState,
		appliedJobsTotal,
		eventBufferOccupancy,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "vworkspace_operator_credential_age_seconds",
			Help: "Seconds since the bootstrap credentials Secret was last updated or rotated.",
		}, credentialAgeSeconds),
	)
}

// SetPullJobLagSeconds updates the oldest unapplied job lag gauge.
func SetPullJobLagSeconds(seconds float64) {
	pullJobLagSeconds.Set(seconds)
}

// SetConnectivityState sets connectivity for the given mode (pull, push, gitops).
func SetConnectivityState(mode string, state float64) {
	connectivityState.WithLabelValues(mode).Set(state)
}

// IncAppliedJobs increments the applied jobs counter.
func IncAppliedJobs() {
	appliedJobsTotal.Inc()
}

// SetEventBufferOccupancy updates the outbound event buffer depth gauge.
func SetEventBufferOccupancy(count int) {
	eventBufferOccupancy.Set(float64(count))
}

// SetCredentialUpdatedAt records when bootstrap credentials were last loaded or persisted.
func SetCredentialUpdatedAt(t time.Time) {
	if t.IsZero() {
		credentialUpdatedAtUnix.Store(0)
		return
	}
	credentialUpdatedAtUnix.Store(t.Unix())
}

func credentialAgeSeconds() float64 {
	ts := credentialUpdatedAtUnix.Load()
	if ts == 0 {
		return 0
	}
	age := time.Since(time.Unix(ts, 0)).Seconds()
	if age < 0 {
		return 0
	}
	return age
}
