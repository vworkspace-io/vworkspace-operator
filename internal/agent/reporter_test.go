package agent

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConditionEventKeyStable(t *testing.T) {
	ref := AppliedRef{
		APIVersion: "apps.vworkspace.io/v1alpha1",
		Kind:       "ApplicationInstance",
		Namespace:  "team-a",
		Name:       "demo",
		UID:        "uid-1",
		Generation: 3,
	}
	condition := metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionTrue,
		Reason: "HelmReleaseReady",
	}
	key := ConditionEventKey(ref, condition)
	if key == "" {
		t.Fatal("expected non-empty event key")
	}
	if key2 := ConditionEventKey(ref, condition); key2 != key {
		t.Fatalf("event key not stable: %q vs %q", key, key2)
	}
}

func TestStatusReporterEnqueuesChangedConditions(t *testing.T) {
	var posted []Event
	client := &recordingClient{postEvents: func(events []Event) error {
		posted = append(posted, events...)
		return nil
	}}
	batcher := NewEventBatcher(client)
	reporter := NewStatusReporter(batcher)

	ref := AppliedRef{
		APIVersion: "apps.vworkspace.io/v1alpha1",
		Kind:       "ApplicationInstance",
		Namespace:  "team-a",
		Name:       "demo",
		UID:        "uid-1",
		Generation: 1,
	}
	prev := []metav1.Condition{}
	next := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "HelmReleaseReady",
		Message:            "ok",
		LastTransitionTime: metav1.Now(),
	}}

	reporter.ReportConditionTransitions(ref, prev, next)
	batcher.Flush(t.Context())

	if len(posted) != 1 {
		t.Fatalf("expected 1 event, got %d", len(posted))
	}
	if posted[0].Kind != "ConditionTransition" {
		t.Fatalf("unexpected kind: %s", posted[0].Kind)
	}
	if posted[0].EventKey == "" {
		t.Fatal("expected event key on posted event")
	}
	if len(posted[0].Conditions) != 1 || posted[0].Conditions[0].Type != "Ready" {
		t.Fatalf("unexpected conditions: %+v", posted[0].Conditions)
	}
}

func TestStatusReporterEnrichesConditionEvents(t *testing.T) {
	var posted []Event
	client := &recordingClient{postEvents: func(events []Event) error {
		posted = append(posted, events...)
		return nil
	}}
	batcher := NewEventBatcher(client)
	reporter := NewStatusReporter(batcher)

	ref := AppliedRef{
		APIVersion: "apps.vworkspace.io/v1alpha1",
		Kind:       "ApplicationInstance",
		Namespace:  "seaweedfs",
		Name:       "seaweedfs-dev",
		UID:        "uid-1",
		Generation: 1,
	}
	prev := []metav1.Condition{}
	next := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "SeaweedReady",
		LastTransitionTime: metav1.Now(),
	}}
	enrich := func(condition metav1.Condition) EventExtras {
		if condition.Type != "Ready" {
			return EventExtras{}
		}
		return EventExtras{
			Endpoints: []EndpointPayload{{Name: "s3", URL: "http://seaweedfs-dev-s3.seaweedfs.svc:8333"}},
			ManagedStorage: &ManagedStoragePayload{
				AccessKeyID:     "admin",
				SecretAccessKey: "secret",
				BucketName:      "seaweedfs-dev",
			},
		}
	}

	reporter.ReportConditionTransitions(ref, prev, next, enrich)
	batcher.Flush(t.Context())

	if len(posted) != 1 {
		t.Fatalf("expected 1 event, got %d", len(posted))
	}
	if len(posted[0].Endpoints) != 1 || posted[0].Endpoints[0].URL == "" {
		t.Fatalf("expected endpoints on event: %+v", posted[0].Endpoints)
	}
	if posted[0].ManagedStorage == nil || posted[0].ManagedStorage.AccessKeyID != "admin" {
		t.Fatalf("expected managedStorage on event: %+v", posted[0].ManagedStorage)
	}
}

func TestStatusReporterSkipsUnchangedConditions(t *testing.T) {
	var posted []Event
	client := &recordingClient{postEvents: func(events []Event) error {
		posted = append(posted, events...)
		return nil
	}}
	batcher := NewEventBatcher(client)
	reporter := NewStatusReporter(batcher)

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "HelmReleaseReady",
		Message:            "ok",
		LastTransitionTime: metav1.Now(),
	}
	ref := AppliedRef{APIVersion: "apps.vworkspace.io/v1alpha1", Kind: "ApplicationInstance", Name: "demo", UID: "uid-1", Generation: 1}
	prev := []metav1.Condition{condition}
	next := []metav1.Condition{condition}

	reporter.ReportConditionTransitions(ref, prev, next)
	batcher.Flush(t.Context())
	if len(posted) != 0 {
		t.Fatalf("expected no events, got %d", len(posted))
	}
}

func TestEventBatcherRequeuesOnFailure(t *testing.T) {
	fail := true
	client := &recordingClient{postEvents: func(events []Event) error {
		if fail {
			fail = false
			return errPostFailed
		}
		return nil
	}}
	batcher := NewEventBatcher(client)
	batcher.Enqueue(Event{Kind: "ConditionTransition", Timestamp: time.Now().UTC()})

	batcher.Flush(t.Context())
	if batcher.Len() != 1 {
		t.Fatalf("expected requeued event, buffer len=%d", batcher.Len())
	}

	batcher.Flush(t.Context())
	if batcher.Len() != 0 {
		t.Fatalf("expected empty buffer after successful flush, len=%d", batcher.Len())
	}
}

type recordingClient struct {
	postEvents func(events []Event) error
}

func (c *recordingClient) FetchJobs(context.Context, int) ([]Job, error) { return nil, nil }
func (c *recordingClient) AckJob(context.Context, string) error          { return nil }
func (c *recordingClient) ReportStatus(context.Context, string, StatusUpdate) error {
	return nil
}
func (c *recordingClient) ReportResult(context.Context, string, JobResult) error { return nil }
func (c *recordingClient) Heartbeat(context.Context) error                       { return nil }
func (c *recordingClient) RotateCredentials(context.Context) (RotateCredentialsResponse, error) {
	return RotateCredentialsResponse{}, nil
}

func (c *recordingClient) PostEvents(_ context.Context, req EventsRequest) error {
	if c.postEvents == nil {
		return nil
	}
	return c.postEvents(req.Events)
}

var errPostFailed = errString("post failed")

type errString string

func (e errString) Error() string { return string(e) }
