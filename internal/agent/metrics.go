package agent

import (
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
)

func init() {
	metrics.Registry.MustRegister(pullJobLagSeconds, connectivityState, appliedJobsTotal, eventBufferOccupancy)
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
