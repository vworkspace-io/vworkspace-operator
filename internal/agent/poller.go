package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

const defaultLongPollWait = 30

// AgentPoller long-polls Odoo for jobs, applies them, and reports results.
type AgentPoller struct {
	Client   Client
	Applier  *Applier
	Events   *EventBatcher
	Log      logr.Logger
	WaitSecs int
}

// Run blocks until ctx is cancelled.
func (p *AgentPoller) Run(ctx context.Context) {
	if p.Client == nil || p.Applier == nil {
		return
	}
	wait := p.WaitSecs
	if wait <= 0 {
		wait = defaultLongPollWait
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		jobs, err := p.Client.FetchJobs(ctx, wait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if p.Log.GetSink() != nil {
				p.Log.Error(err, "fetch jobs failed")
			}
			p.backoff(ctx, 2*time.Second)
			continue
		}

		for _, job := range jobs {
			if err := p.processJob(ctx, job); err != nil && p.Log.GetSink() != nil {
				p.Log.Error(err, "process job failed", "jobID", job.ID, "kind", job.Kind)
			}
		}
	}
}

func (p *AgentPoller) processJob(ctx context.Context, job Job) error {
	if err := p.Client.AckJob(ctx, job.ID); err != nil {
		return fmt.Errorf("ack job %s: %w", job.ID, err)
	}

	outcome, err := p.Applier.ApplyJob(ctx, job)
	if err != nil {
		result := JobResult{
			Outcome:   OutcomeFailed,
			Error:     err.Error(),
			Timestamp: time.Now().UTC(),
		}
		if IsConflict(err) {
			result = ConflictResult(err)
			result.Outcome = OutcomeConflict
		}
		if reportErr := p.Client.ReportResult(ctx, job.ID, result); reportErr != nil {
			return fmt.Errorf("report failed result: %w", reportErr)
		}
		return err
	}

	if reportErr := p.Client.ReportResult(ctx, job.ID, outcome.Result); reportErr != nil {
		return fmt.Errorf("report result: %w", reportErr)
	}
	if outcome.Idempotent && p.Log.GetSink() != nil {
		p.Log.Info("job idempotent replay", "jobID", job.ID, "key", job.IdempotencyKey)
	}
	return nil
}

func (p *AgentPoller) backoff(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// Poller is deprecated; use AgentPoller.
type Poller = AgentPoller

// Start runs the poller until ctx is done (compat wrapper).
func (p *AgentPoller) Start(ctx context.Context) {
	p.Run(ctx)
}
