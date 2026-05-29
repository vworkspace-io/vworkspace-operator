package agent

import (
	"context"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EventBatcher coalesces outbound events and flushes them to Odoo.
type EventBatcher struct {
	Client Client
	Log    interface {
		Info(msg string, keysAndValues ...any)
		Error(err error, msg string, keysAndValues ...any)
	}

	mu        sync.Mutex
	events    []Event
	maxBatch  int
	flushWait time.Duration
}

// NewEventBatcher returns a batcher with sensible defaults.
func NewEventBatcher(client Client) *EventBatcher {
	return &EventBatcher{
		Client:    client,
		maxBatch:  100,
		flushWait: time.Second,
	}
}

// Enqueue adds an event to the buffer.
func (b *EventBatcher) Enqueue(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	b.events = append(b.events, event)
}

// Start runs the periodic flush loop until ctx is cancelled.
func (b *EventBatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(b.flushWait)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			b.Flush(ctx)
			return
		case <-ticker.C:
			b.Flush(ctx)
		}
	}
}

// Flush sends buffered events.
func (b *EventBatcher) Flush(ctx context.Context) {
	b.mu.Lock()
	if len(b.events) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.events
	b.events = nil
	b.mu.Unlock()

	if b.Client == nil {
		return
	}
	if err := b.Client.PostEvents(ctx, EventsRequest{Events: batch}); err != nil {
		if b.Log != nil {
			type errorLogger interface {
				Error(err error, msg string, keysAndValues ...any)
			}
			if l, ok := b.Log.(errorLogger); ok {
				l.Error(err, "post events failed", "count", len(batch))
			}
		}
	}
}

// ConditionTransitionEvent builds a standard condition transition event.
func ConditionTransitionEvent(ref AppliedRef, conditions []metav1.Condition) Event {
	return Event{
		Kind:        "ConditionTransition",
		ResourceRef: ref,
		Conditions:  conditions,
		Timestamp:   time.Now().UTC(),
	}
}
