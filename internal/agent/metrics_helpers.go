package agent

import (
	"time"
)

// TrackJobLag updates pull job lag from the oldest fetched job timestamp.
func TrackJobLag(jobs []Job) {
	if len(jobs) == 0 {
		SetPullJobLagSeconds(0)
		return
	}
	oldest := time.Now().UTC()
	for _, job := range jobs {
		if !job.CreatedAt.IsZero() && job.CreatedAt.Before(oldest) {
			oldest = job.CreatedAt
		}
	}
	SetPullJobLagSeconds(time.Since(oldest).Seconds())
}
