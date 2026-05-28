package agent

import (
	"context"
	"time"

	"github.com/go-logr/logr"
)

// Poller periodically fetches jobs from Odoo. Job application is deferred to Phase 1b.
type Poller struct {
	Client   Client
	Interval time.Duration
	Log      logr.Logger
}

func (p *Poller) Start(ctx context.Context) {
	if p.Client == nil {
		return
	}
	if p.Interval <= 0 {
		p.Interval = 30 * time.Second
	}
	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jobs, err := p.Client.FetchJobs(ctx, 0)
			if err != nil {
				if p.Log.GetSink() != nil {
					p.Log.Error(err, "fetch jobs failed")
				}
				continue
			}
			if len(jobs) > 0 && p.Log.GetSink() != nil {
				p.Log.Info("jobs fetched", "count", len(jobs))
			}
		}
	}
}
