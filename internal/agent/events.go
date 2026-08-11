package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	defaultMaxBatch  = 100
	defaultFlushWait = time.Second
	defaultMaxBuffer = 1000
)

// EventBatcher coalesces outbound events and flushes them to the control plane.
type EventBatcher struct {
	Client Client
	Log    interface {
		Info(msg string, keysAndValues ...any)
		Error(err error, msg string, keysAndValues ...any)
	}
	// OnBeforePostEvents is invoked after a batch is extracted but before PostEvents.
	OnBeforePostEvents func(ctx context.Context, events []Event)
	// OnEventsPostFailed is invoked when PostEvents fails before events are requeued.
	OnEventsPostFailed func(ctx context.Context, events []Event)
	// OnEventsDelivered is invoked after a batch is accepted by PostEvents.
	OnEventsDelivered func(ctx context.Context, events []Event)

	mu              sync.Mutex
	events          []Event
	maxBatch        int
	maxBuffer       int
	flushWait       time.Duration
	overflowActive  bool
	overflowDropped int64
}

// NewEventBatcher returns a batcher with sensible defaults.
func NewEventBatcher(client Client) *EventBatcher {
	return &EventBatcher{
		Client:    client,
		maxBatch:  defaultMaxBatch,
		maxBuffer: defaultMaxBuffer,
		flushWait: defaultFlushWait,
	}
}

// Len returns the number of buffered events.
func (b *EventBatcher) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}

// HasEventKey reports whether an event with key is already buffered.
func (b *EventBatcher) HasEventKey(key string) bool {
	if key == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, event := range b.events {
		if event.EventKey == key {
			return true
		}
	}
	return false
}

// Enqueue adds an event to the buffer.
func (b *EventBatcher) Enqueue(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	maxBuffer := b.maxBuffer
	if maxBuffer <= 0 {
		maxBuffer = defaultMaxBuffer
	}
	if len(b.events) >= maxBuffer {
		drop := len(b.events) - maxBuffer + 1
		b.events = b.events[drop:]
		b.recordOverflowLocked(drop)
	}
	b.events = append(b.events, event)
	SetEventBufferOccupancy(len(b.events))
}

// OverflowState reports whether events were dropped and how many since the buffer last drained.
func (b *EventBatcher) OverflowState() (active bool, dropped int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.overflowActive, b.overflowDropped
}

// BufferOverflowMessage formats the Cluster BufferOverflow condition message.
func BufferOverflowMessage(dropped int64) string {
	return fmt.Sprintf(
		"Dropped %d outbound event(s) because the buffer is full; verify control plane connectivity and that events are being accepted",
		dropped,
	)
}

func (b *EventBatcher) recordOverflowLocked(dropped int) {
	if dropped <= 0 {
		return
	}
	b.overflowActive = true
	b.overflowDropped += int64(dropped)
}

func (b *EventBatcher) clearOverflowLocked() {
	b.overflowActive = false
	b.overflowDropped = 0
}

// Start runs the periodic flush loop until ctx is cancelled.
func (b *EventBatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(b.flushWait)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			b.Flush(context.Background())
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
		SetEventBufferOccupancy(0)
		return
	}
	maxBatch := b.maxBatch
	if maxBatch <= 0 {
		maxBatch = defaultMaxBatch
	}
	end := min(len(b.events), maxBatch)
	batch := make([]Event, end)
	copy(batch, b.events[:end])
	b.events = b.events[end:]
	SetEventBufferOccupancy(len(b.events))
	b.mu.Unlock()

	if b.Client == nil {
		return
	}
	if b.OnBeforePostEvents != nil {
		b.OnBeforePostEvents(ctx, batch)
	}
	if err := b.Client.PostEvents(ctx, EventsRequest{Events: batch}); err != nil {
		if b.OnEventsPostFailed != nil {
			b.OnEventsPostFailed(ctx, batch)
		}
		b.requeue(batch)
		SetConnectivityState("pull", 0)
		if b.Log != nil {
			type errorLogger interface {
				Error(err error, msg string, keysAndValues ...any)
			}
			if l, ok := b.Log.(errorLogger); ok {
				l.Error(err, "post events failed", "count", len(batch))
			}
		}
		return
	}
	if b.OnEventsDelivered != nil {
		b.OnEventsDelivered(ctx, batch)
	}
	b.mu.Lock()
	if len(b.events) == 0 {
		b.clearOverflowLocked()
	}
	b.mu.Unlock()
	SetConnectivityState("pull", 1)
}

func (b *EventBatcher) requeue(batch []Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	maxBuffer := b.maxBuffer
	if maxBuffer <= 0 {
		maxBuffer = defaultMaxBuffer
	}
	combined := append(batch, b.events...)
	if len(combined) > maxBuffer {
		drop := len(combined) - maxBuffer
		combined = combined[drop:]
		b.recordOverflowLocked(drop)
	}
	b.events = combined
	SetEventBufferOccupancy(len(b.events))
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
