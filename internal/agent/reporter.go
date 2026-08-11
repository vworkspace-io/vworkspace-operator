package agent

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// StatusReporter queues condition transition events for outbound delivery to the control plane.
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

// ConditionEventEnricher optionally adds endpoints / managedStorage to a condition event.
type ConditionEventEnricher func(condition metav1.Condition) EventExtras

// ReportConditionTransitions enqueues events for conditions that changed between prev and next.
func (r StatusReporter) ReportConditionTransitions(ref AppliedRef, prev, next []metav1.Condition, enrich ...ConditionEventEnricher) {
	if r.batcher == nil {
		return
	}
	var enricher ConditionEventEnricher
	if len(enrich) > 0 {
		enricher = enrich[0]
	}
	for _, condition := range changedConditions(prev, next) {
		event := ConditionTransitionEvent(ref, []metav1.Condition{condition})
		event.EventKey = ConditionEventKey(ref, condition)
		if enricher != nil {
			extra := enricher(condition)
			event.Endpoints = extra.Endpoints
			event.ManagedStorage = extra.ManagedStorage
		}
		r.batcher.Enqueue(event)
	}
}

const managedStorageEventKeyPrefix = "managedStorage/"
const endpointsEventKeyPrefix = "endpoints/"

// ManagedStorageEventKey builds the deduplication key for supplemental managed-storage events.
func ManagedStorageEventKey(ref AppliedRef, ready metav1.Condition, reportKey string) string {
	return fmt.Sprintf(
		"%s%s/%s/%s/%s/%s",
		managedStorageEventKeyPrefix,
		ref.Namespace,
		ref.Name,
		ready.Type,
		ready.Status,
		reportKey,
	)
}

// ManagedStorageReportKeyFromEventKey extracts the credential report key from a managed-storage event key.
func ManagedStorageReportKeyFromEventKey(eventKey string) (string, bool) {
	if !strings.HasPrefix(eventKey, managedStorageEventKeyPrefix) {
		return "", false
	}
	parts := strings.Split(eventKey, "/")
	if len(parts) != 6 {
		return "", false
	}
	return parts[5], true
}

// HasPendingEvent reports whether an event with the same key is already buffered for delivery.
func (r StatusReporter) HasPendingEvent(eventKey string) bool {
	if r.batcher == nil || eventKey == "" {
		return false
	}
	return r.batcher.HasEventKey(eventKey)
}

// EndpointsEventKey builds the deduplication key for supplemental endpoint-only events.
func EndpointsEventKey(ref AppliedRef, reportKey string) string {
	return fmt.Sprintf("%s%s/%s/%s", endpointsEventKeyPrefix, ref.Namespace, ref.Name, reportKey)
}

// ReportSeaweedEndpoints enqueues a supplemental Ready event carrying endpoints when they
// become available after the initial Ready transition without managed storage credentials.
func (r StatusReporter) ReportSeaweedEndpoints(ref AppliedRef, ready metav1.Condition, endpoints []EndpointPayload, reportKey string) bool {
	if r.batcher == nil || len(endpoints) == 0 {
		return false
	}
	event := ConditionTransitionEvent(ref, []metav1.Condition{ready})
	event.EventKey = EndpointsEventKey(ref, reportKey)
	event.Endpoints = endpoints
	r.batcher.Enqueue(event)
	return true
}

// ReportManagedStorageReady enqueues a supplemental Ready event when managed storage
// becomes available after the initial Ready transition (P10-T006). Returns false when
// the reporter is disabled and no event was enqueued.
func (r StatusReporter) ReportManagedStorageReady(ref AppliedRef, ready metav1.Condition, extras EventExtras, reportKey string) bool {
	if r.batcher == nil {
		return false
	}
	event := ConditionTransitionEvent(ref, []metav1.Condition{ready})
	event.EventKey = ManagedStorageEventKey(ref, ready, reportKey)
	event.Endpoints = extras.Endpoints
	event.ManagedStorage = extras.ManagedStorage
	r.batcher.Enqueue(event)
	return true
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
