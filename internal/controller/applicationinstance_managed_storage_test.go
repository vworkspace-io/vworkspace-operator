package controller

import (
	"context"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/seaweedengine"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type stagedManagedStorageEngine struct {
	recordingSeaweedEngine
	states []managedStorageState
	calls  int
}

type managedStorageState struct {
	snapshot *seaweedengine.ManagedStorageSnapshot
	pending  bool
	err      error
}

func (e *stagedManagedStorageEngine) ResolveManagedStorageState(context.Context, *appsv1alpha1.ApplicationInstance) (*seaweedengine.ManagedStorageSnapshot, bool, error) {
	idx := e.calls
	if idx >= len(e.states) {
		idx = len(e.states) - 1
	}
	e.calls++
	state := e.states[idx]
	return state.snapshot, state.pending, state.err
}

func TestReconcileSeaweedManagedStorageQuietWhenNoCredentialsExist(t *testing.T) {
	t.Parallel()

	app := sampleSeaweedApplicationInstance()
	app.Status.Conditions = []metav1.Condition{{
		Type:               appsv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SeaweedReady",
		LastTransitionTime: metav1.Now(),
	}}

	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(app).WithObjects(app).Build()

	reconciler := &ApplicationInstanceReconciler{
		Client: cl,
		SeaweedEngine: &stagedManagedStorageEngine{
			states: []managedStorageState{{snapshot: nil, pending: false}},
		},
	}

	result, err := reconciler.reconcileSeaweedManagedStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("reconcileSeaweedManagedStorage: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected quiet steady state when no S3Credentials exist, got %+v", result)
	}
}

func TestReconcileSeaweedManagedStorageRequeuesWhileCredentialsPending(t *testing.T) {
	t.Parallel()

	app := sampleSeaweedApplicationInstance()
	app.Status.Conditions = []metav1.Condition{{
		Type:               appsv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SeaweedReady",
		LastTransitionTime: metav1.Now(),
	}}

	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(app).WithObjects(app).Build()

	reconciler := &ApplicationInstanceReconciler{
		Client: cl,
		SeaweedEngine: &stagedManagedStorageEngine{
			states: []managedStorageState{{snapshot: nil, pending: true}},
		},
	}

	result, err := reconciler.reconcileSeaweedManagedStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("reconcileSeaweedManagedStorage: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Fatalf("expected requeue while credentials pending, got %+v", result)
	}
}

func TestReconcileSeaweedManagedStorageEmitsSingleSupplementalEvent(t *testing.T) {
	t.Parallel()

	app := sampleSeaweedApplicationInstance()
	app.Status.Conditions = []metav1.Condition{{
		Type:               appsv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SeaweedReady",
		LastTransitionTime: metav1.Now(),
	}}
	app.Status.Endpoints = []appsv1alpha1.EndpointStatus{{
		Name: "s3",
		URL:  "http://seaweedfs-smoke-s3.seaweedfs.svc:8333",
	}}

	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(app).WithObjects(app).Build()

	var posted []agent.Event
	batcher := agent.NewEventBatcher(&managedStorageRecordingClient{postEvents: func(events []agent.Event) error {
		posted = append(posted, events...)
		return nil
	}})

	reconciler := &ApplicationInstanceReconciler{
		Client: cl,
		SeaweedEngine: &stagedManagedStorageEngine{
			states: []managedStorageState{{
				snapshot: &seaweedengine.ManagedStorageSnapshot{
					AccessKeyID:     "admin",
					SecretAccessKey: "secret",
					BucketName:      testSeaweedRelease,
				},
			}},
		},
		Reporter: agent.NewStatusReporter(batcher),
	}
	batcher.OnEventsDelivered = reconciler.AckManagedStorageDelivered

	result, err := reconciler.reconcileSeaweedManagedStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("reconcileSeaweedManagedStorage: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected no requeue after reporting, got %+v", result)
	}
	if app.Annotations[reportedManagedStorageAccessKeyAnnotation] != "" {
		t.Fatalf("expected no annotation before delivery, got %#v", app.Annotations)
	}
	batcher.Flush(t.Context())
	if len(posted) != 1 {
		t.Fatalf("expected one supplemental event, got %d", len(posted))
	}
	if posted[0].ManagedStorage == nil || posted[0].ManagedStorage.AccessKeyID != "admin" {
		t.Fatalf("unexpected event payload: %+v", posted[0])
	}

	updated := &appsv1alpha1.ApplicationInstance{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(app), updated); err != nil {
		t.Fatalf("get app: %v", err)
	}
	if updated.Annotations[reportedManagedStorageAccessKeyAnnotation] == "" {
		t.Fatalf("expected reported annotation, got %#v", updated.Annotations)
	}

	posted = nil
	result, err = reconciler.reconcileSeaweedManagedStorage(context.Background(), updated)
	if err != nil {
		t.Fatalf("second reconcileSeaweedManagedStorage: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected no requeue after annotation set, got %+v", result)
	}
	batcher.Flush(t.Context())
	if len(posted) != 0 {
		t.Fatalf("expected no duplicate event, got %d", len(posted))
	}
}

func TestReconcileSeaweedManagedStorageRetriesAckWithoutRepost(t *testing.T) {
	t.Parallel()

	app := sampleSeaweedApplicationInstance()
	app.Status.Conditions = []metav1.Condition{{
		Type:               appsv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SeaweedReady",
		LastTransitionTime: metav1.Now(),
	}}

	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(app).WithObjects(app).Build()

	var posted []agent.Event
	batcher := agent.NewEventBatcher(&managedStorageRecordingClient{postEvents: func(events []agent.Event) error {
		posted = append(posted, events...)
		return nil
	}})

	reconciler := &ApplicationInstanceReconciler{
		Client: cl,
		SeaweedEngine: &stagedManagedStorageEngine{
			states: []managedStorageState{{
				snapshot: &seaweedengine.ManagedStorageSnapshot{
					AccessKeyID:     "admin",
					SecretAccessKey: "secret",
					BucketName:      testSeaweedRelease,
				},
			}},
		},
		Reporter: agent.NewStatusReporter(batcher),
	}
	batcher.OnEventsDelivered = reconciler.AckManagedStorageDelivered

	reportKey := managedStorageReportKey(&seaweedengine.ManagedStorageSnapshot{
		AccessKeyID:     "admin",
		SecretAccessKey: "secret",
		BucketName:      testSeaweedRelease,
	})
	reconciler.markStorageAckPending(app.Namespace, app.Name, reportKey)

	result, err := reconciler.reconcileSeaweedManagedStorage(context.Background(), app)
	if err != nil {
		t.Fatalf("reconcileSeaweedManagedStorage: %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("expected quiet reconcile after ack retry, got %+v", result)
	}
	batcher.Flush(t.Context())
	if len(posted) != 0 {
		t.Fatalf("expected no repost while ack pending, got %d events", len(posted))
	}

	updated := &appsv1alpha1.ApplicationInstance{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(app), updated); err != nil {
		t.Fatalf("get app: %v", err)
	}
	if updated.Annotations[reportedManagedStorageAccessKeyAnnotation] != reportKey {
		t.Fatalf("expected ack annotation %q, got %#v", reportKey, updated.Annotations)
	}
}

func TestReconcileSeaweedManagedStorageInFlightBlocksRepost(t *testing.T) {
	t.Parallel()

	app := sampleSeaweedApplicationInstance()
	app.Status.Conditions = []metav1.Condition{{
		Type:               appsv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SeaweedReady",
		LastTransitionTime: metav1.Now(),
	}}

	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(app).WithObjects(app).Build()

	var posted []agent.Event
	batcher := agent.NewEventBatcher(&managedStorageRecordingClient{postEvents: func(events []agent.Event) error {
		posted = append(posted, events...)
		return nil
	}})

	reconciler := &ApplicationInstanceReconciler{
		Client: cl,
		SeaweedEngine: &stagedManagedStorageEngine{
			states: []managedStorageState{{
				snapshot: &seaweedengine.ManagedStorageSnapshot{
					AccessKeyID:     "admin",
					SecretAccessKey: "secret",
					BucketName:      testSeaweedRelease,
				},
			}},
		},
		Reporter: agent.NewStatusReporter(batcher),
	}

	if _, err := reconciler.reconcileSeaweedManagedStorage(context.Background(), app); err != nil {
		t.Fatalf("first reconcileSeaweedManagedStorage: %v", err)
	}
	if batcher.Len() != 1 {
		t.Fatalf("expected one buffered event, got %d", batcher.Len())
	}

	posted = nil
	if _, err := reconciler.reconcileSeaweedManagedStorage(context.Background(), app); err != nil {
		t.Fatalf("second reconcileSeaweedManagedStorage: %v", err)
	}
	if batcher.Len() != 1 {
		t.Fatalf("expected buffered event to remain while in flight, got %d", batcher.Len())
	}
	batcher.Flush(t.Context())
	if len(posted) != 1 {
		t.Fatalf("expected one post after flush, got %d events", len(posted))
	}

	updated := &appsv1alpha1.ApplicationInstance{}
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(app), updated); err != nil {
		t.Fatalf("get app: %v", err)
	}
	if updated.Annotations[reportedManagedStorageAccessKeyAnnotation] != "" {
		t.Fatalf("expected no premature ack annotation, got %#v", updated.Annotations)
	}
}

func TestReconcileSeaweedManagedStorageClearsStalePendingOnRotation(t *testing.T) {
	t.Parallel()

	app := sampleSeaweedApplicationInstance()
	app.Status.Conditions = []metav1.Condition{{
		Type:               appsv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "SeaweedReady",
		LastTransitionTime: metav1.Now(),
	}}

	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(app).WithObjects(app).Build()

	var posted []agent.Event
	batcher := agent.NewEventBatcher(&managedStorageRecordingClient{postEvents: func(events []agent.Event) error {
		posted = append(posted, events...)
		return nil
	}})

	reconciler := &ApplicationInstanceReconciler{
		Client: cl,
		SeaweedEngine: &stagedManagedStorageEngine{
			states: []managedStorageState{{
				snapshot: &seaweedengine.ManagedStorageSnapshot{
					AccessKeyID:     "rotated",
					SecretAccessKey: "new-secret",
					BucketName:      testSeaweedRelease,
				},
			}},
		},
		Reporter: agent.NewStatusReporter(batcher),
	}
	batcher.OnEventsDelivered = reconciler.AckManagedStorageDelivered
	reconciler.markStorageInFlight(app.Namespace, app.Name, "stale-key")

	if _, err := reconciler.reconcileSeaweedManagedStorage(context.Background(), app); err != nil {
		t.Fatalf("reconcileSeaweedManagedStorage: %v", err)
	}
	batcher.Flush(t.Context())
	if len(posted) != 1 {
		t.Fatalf("expected rotated credentials event, got %d", len(posted))
	}
}

func sampleSeaweedApplicationInstance() *appsv1alpha1.ApplicationInstance {
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSeaweedRelease,
			Namespace: testSeaweedNamespace,
		},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{
				CatalogID: seaweedengine.CatalogIDSeaweedFS,
			},
			Release: &appsv1alpha1.ReleaseSpec{
				Name:      testSeaweedRelease,
				Namespace: testSeaweedNamespace,
			},
		},
	}
}

type managedStorageRecordingClient struct {
	postEvents func(events []agent.Event) error
}

func (c *managedStorageRecordingClient) FetchJobs(context.Context, int) ([]agent.Job, error) {
	return nil, nil
}
func (c *managedStorageRecordingClient) AckJob(context.Context, string) error { return nil }
func (c *managedStorageRecordingClient) ReportStatus(context.Context, string, agent.StatusUpdate) error {
	return nil
}
func (c *managedStorageRecordingClient) ReportResult(context.Context, string, agent.JobResult) error {
	return nil
}
func (c *managedStorageRecordingClient) Heartbeat(context.Context) error { return nil }
func (c *managedStorageRecordingClient) RotateCredentials(context.Context) (agent.RotateCredentialsResponse, error) {
	return agent.RotateCredentialsResponse{}, nil
}
func (c *managedStorageRecordingClient) PostEvents(_ context.Context, req agent.EventsRequest) error {
	if c.postEvents == nil {
		return nil
	}
	return c.postEvents(req.Events)
}

const (
	testSeaweedNamespace = seaweedengine.CatalogIDSeaweedFS
	testSeaweedRelease   = "seaweedfs-smoke"
)
