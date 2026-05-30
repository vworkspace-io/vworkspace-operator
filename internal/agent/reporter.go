package agent

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StatusReporter queues condition transition events for outbound delivery to Odoo.
type StatusReporter struct {
	batcher *EventBatcher
}

// NewStatusReporter returns a reporter that enqueues events on batcher.
func NewStatusReporter(batcher *EventBatcher) StatusReporter {
	return StatusReporter{batcher: batcher}
}

// NoopStatusReporter returns a reporter that discards events.
func NoopStatusReporter() StatusReporter {
	return StatusReporter{}
}

// ReportConditionTransitions enqueues events for conditions that changed between prev and next.
func (r StatusReporter) ReportConditionTransitions(ref AppliedRef, prev, next []metav1.Condition) {
	if r.batcher == nil {
		return
	}
	for _, condition := range changedConditions(prev, next) {
		event := ConditionTransitionEvent(ref, []metav1.Condition{condition})
		event.EventKey = ConditionEventKey(ref, condition)
		r.batcher.Enqueue(event)
	}
}

// ReportAudit enqueues a single audit-style event (e.g. credential rotation).
func (r StatusReporter) ReportAudit(ref AppliedRef, kind string, conditions []metav1.Condition) {
	if r.batcher == nil {
		return
	}
	event := Event{
		Kind:        kind,
		ResourceRef: ref,
		Conditions:  conditions,
		Timestamp:   time.Now().UTC(),
	}
	if len(conditions) > 0 {
		event.EventKey = fmt.Sprintf("%s/%s/%s", kind, ref.UID, conditions[0].Type)
	}
	r.batcher.Enqueue(event)
}

// ConditionEventKey builds a stable deduplication key for a condition transition.
func ConditionEventKey(ref AppliedRef, condition metav1.Condition) string {
	uid := strings.TrimSpace(ref.UID)
	if uid == "" {
		uid = "_"
	}
	namespace := strings.TrimSpace(ref.Namespace)
	if namespace == "" {
		namespace = "_"
	}
	return fmt.Sprintf(
		"condition/%s/%s/%s/%s/%s/%s/%s@%d",
		ref.APIVersion,
		ref.Kind,
		namespace,
		ref.Name,
		uid,
		condition.Type,
		condition.Status,
		ref.Generation,
	)
}

// ResourceRefFromMeta builds an AppliedRef from object metadata.
func ResourceRefFromMeta(gvk schema.GroupVersionKind, meta metav1.ObjectMeta) AppliedRef {
	return AppliedRef{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		Namespace:  meta.Namespace,
		Name:       meta.Name,
		UID:        string(meta.UID),
		Generation: meta.Generation,
	}
}

func changedConditions(prev, next []metav1.Condition) []metav1.Condition {
	prevByType := make(map[string]metav1.Condition, len(prev))
	for _, c := range prev {
		prevByType[c.Type] = c
	}
	var changed []metav1.Condition
	for _, nc := range next {
		pc, ok := prevByType[nc.Type]
		if !ok || pc.Status != nc.Status || pc.Reason != nc.Reason || pc.Message != nc.Message {
			changed = append(changed, nc)
		}
	}
	return changed
}
