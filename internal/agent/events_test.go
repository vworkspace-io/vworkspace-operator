package agent

import (
	"context"
	"testing"
	"time"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEventBatcherOverflowState(t *testing.T) {
	client := &recordingClient{}
	batcher := NewEventBatcher(client)
	batcher.maxBuffer = 3

	for i := range 5 {
		batcher.Enqueue(Event{Kind: "ConditionTransition", Timestamp: time.Now().UTC(), EventKey: "k" + string(rune('a'+i))})
	}

	active, dropped := batcher.OverflowState()
	if !active {
		t.Fatal("expected overflow to be active")
	}
	if dropped != 2 {
		t.Fatalf("expected 2 dropped events, got %d", dropped)
	}
	if batcher.Len() != 3 {
		t.Fatalf("expected buffer length 3, got %d", batcher.Len())
	}

	batcher.Flush(t.Context())
	if batcher.Len() != 0 {
		t.Fatalf("expected empty buffer after flush, got %d", batcher.Len())
	}

	active, dropped = batcher.OverflowState()
	if active {
		t.Fatalf("expected overflow cleared after successful drain, dropped=%d", dropped)
	}
	if dropped != 0 {
		t.Fatalf("expected dropped count reset, got %d", dropped)
	}
}

func TestSyncBufferOverflowCondition(t *testing.T) {
	batcher := NewEventBatcher(&recordingClient{})
	batcher.maxBuffer = 1
	batcher.Enqueue(Event{Kind: "ConditionTransition", Timestamp: time.Now().UTC(), EventKey: "a"})
	batcher.Enqueue(Event{Kind: "ConditionTransition", Timestamp: time.Now().UTC(), EventKey: "b"})

	cluster := &opsv1alpha1.Cluster{}
	syncBufferOverflowCondition(cluster, batcher)

	cond, ok := conditions.Get(cluster.Status.Conditions, opsv1alpha1.ConditionBufferOverflow)
	if !ok {
		t.Fatal("expected BufferOverflow condition")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Fatalf("expected True status, got %s", cond.Status)
	}
	if cond.Reason != "EventBufferFull" {
		t.Fatalf("unexpected reason: %s", cond.Reason)
	}
	if cond.Message == "" {
		t.Fatal("expected overflow message")
	}

	batcher.Flush(context.Background())
	syncBufferOverflowCondition(cluster, batcher)

	cond, ok = conditions.Get(cluster.Status.Conditions, opsv1alpha1.ConditionBufferOverflow)
	if !ok {
		t.Fatal("expected BufferOverflow condition after drain")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("expected False status after drain, got %s", cond.Status)
	}
	if cond.Reason != "BufferDrained" {
		t.Fatalf("unexpected reason after drain: %s", cond.Reason)
	}
}

func syncBufferOverflowCondition(cluster *opsv1alpha1.Cluster, batcher *EventBatcher) {
	active, dropped := batcher.OverflowState()
	if active && dropped > 0 {
		cluster.Status.Conditions = conditions.Set(
			cluster.Status.Conditions,
			opsv1alpha1.ConditionBufferOverflow,
			metav1.ConditionTrue,
			"EventBufferFull",
			BufferOverflowMessage(dropped),
		)
		return
	}
	cluster.Status.Conditions = conditions.Set(
		cluster.Status.Conditions,
		opsv1alpha1.ConditionBufferOverflow,
		metav1.ConditionFalse,
		"BufferDrained",
		"Outbound event buffer has drained successfully",
	)
}
