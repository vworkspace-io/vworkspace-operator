package integration_test

import (
	"context"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/controller"
	"github.com/vworkspace-io/vworkspace-operator/internal/helmengine"
	"github.com/vworkspace-io/vworkspace-operator/test/mockodoo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcilerPostsConditionEventsToMockOdoo(t *testing.T) {
	ts := mockodoo.NewTestServer()
	defer ts.Close()
	ts.SetBootstrapToken(testClusterID, testToken)

	httpClient, err := ts.NewAgentClient(testClusterID, testToken)
	if err != nil {
		t.Fatalf("NewAgentClient: %v", err)
	}
	batcher := agent.NewEventBatcher(httpClient)
	reporter := agent.NewStatusReporter(batcher)

	cl, scheme := newPullLoopClient(t)
	app := sampleApplicationInstance("status-report-app", "team-a")
	app.Status.Conditions = []metav1.Condition{}
	if err := cl.Create(context.Background(), app); err != nil {
		t.Fatalf("create app: %v", err)
	}

	engine := helmengine.NewFluxEngine(cl)
	reconciler := &controller.ApplicationInstanceReconciler{
		Client:   cl,
		Scheme:   scheme,
		Engine:   engine,
		Reporter: reporter,
	}

	key := types.NamespacedName{Namespace: app.Namespace, Name: app.Name}
	ctx := context.Background()
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	batcher.Flush(ctx)
	time.Sleep(50 * time.Millisecond)

	events := ts.EventsFiltered(testClusterID, mockodoo.EventFilter{
		Kind:      "ApplicationInstance",
		Namespace: app.Namespace,
		Name:      app.Name,
	})
	if len(events) == 0 {
		t.Fatal("expected condition transition events on mock Odoo")
	}

	foundCondition := false
	for _, ev := range events {
		if ev.Kind != "ConditionTransition" {
			continue
		}
		if ev.EventKey == "" {
			t.Fatal("expected eventKey on posted event")
		}
		for _, c := range ev.Conditions {
			if c.Type == appsv1alpha1.ConditionReconciling || c.Type == appsv1alpha1.ConditionReady {
				foundCondition = true
			}
		}
	}
	if !foundCondition {
		t.Fatalf("expected Reconciling or Ready condition in events: %+v", events)
	}
}
