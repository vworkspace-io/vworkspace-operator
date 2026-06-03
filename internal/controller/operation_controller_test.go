/*
Copyright 2026 vWorkspace Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/agent"
	"github.com/vworkspace-io/vworkspace-operator/internal/engines"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/approvalclaim"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type stubOperationEngine struct {
	name opsv1alpha1.OperationEngine
}

func (e stubOperationEngine) Name() opsv1alpha1.OperationEngine { return e.name }

func (stubOperationEngine) Materialize(context.Context, *opsv1alpha1.Operation, *appsv1alpha1.ApplicationInstance) error {
	return nil
}

func (stubOperationEngine) Status(context.Context, *opsv1alpha1.Operation) (engines.Status, error) {
	return engines.Status{Phase: opsv1alpha1.PhaseRunning, Done: false}, nil
}

func (stubOperationEngine) Cancel(context.Context, *opsv1alpha1.Operation) error { return nil }

func testOperationControllerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := opsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("ops scheme: %v", err)
	}
	if err := appsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("apps scheme: %v", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("batch scheme: %v", err)
	}
	return scheme
}

func sampleTargetApp(namespace, name string) *appsv1alpha1.ApplicationInstance {
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"ops.vworkspace.io/restore": "velero",
			},
		},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "nextcloud"},
			Chart: appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceOCI,
				URL:        "oci://registry.example.com/charts",
				Name:       "nextcloud",
				Version:    "6.6.0",
			},
			Release: appsv1alpha1.ReleaseSpec{Name: name, Namespace: namespace},
			Values: appsv1alpha1.ValuesSpec{
				Source: appsv1alpha1.ValuesSourceInline,
				Inline: &runtime.RawExtension{Raw: []byte(`{}`)},
			},
		},
	}
}

func sampleRestoreOperation(name, namespace, target string) *opsv1alpha1.Operation {
	return &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{opsv1alpha1.OperationFinalizer},
		},
		Spec: opsv1alpha1.OperationSpec{
			TargetRef: opsv1alpha1.TargetRef{
				APIVersion: appsv1alpha1.GroupVersion.String(),
				Kind:       "ApplicationInstance",
				Name:       target,
			},
			Type:       opsv1alpha1.OperationTypeRestore,
			Engine:     opsv1alpha1.EngineVelero,
			Parameters: &runtime.RawExtension{Raw: []byte(`{"backupName":"b1"}`)},
		},
	}
}

func TestOperationReconcilerBlocksRestoreWithoutApprovalClaim(t *testing.T) {
	t.Parallel()
	const (
		namespace = "team-a"
		name      = "restore-1"
	)
	scheme := testOperationControllerScheme(t)
	op := sampleRestoreOperation(name, namespace, "app")
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(sampleTargetApp(namespace, "app"), op).
		WithStatusSubresource(op).
		Build()

	reconciler := &OperationReconciler{
		Client:              cl,
		Scheme:              scheme,
		Registry:            engines.NewRegistry(stubOperationEngine{name: opsv1alpha1.EngineVelero}),
		Reporter:            agent.NoopStatusReporter(),
		ApprovalClaimSecret: "test-secret",
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &opsv1alpha1.Operation{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, updated); err != nil {
		t.Fatalf("get operation: %v", err)
	}
	if updated.Status.Phase != opsv1alpha1.PhasePending {
		t.Fatalf("expected phase Pending, got %q", updated.Status.Phase)
	}
	blocked := findCondition(updated.Status.Conditions, opsv1alpha1.ConditionBlocked)
	if blocked == nil || blocked.Status != metav1.ConditionTrue || blocked.Reason != "AwaitingApproval" {
		t.Fatalf("expected Blocked/AwaitingApproval, got %#v", blocked)
	}
}

func TestOperationReconcilerDoesNotRegressRunningOnExpiredClaim(t *testing.T) {
	t.Parallel()
	const (
		namespace = "team-a"
		name      = "restore-1"
		secret    = "test-secret"
	)
	claim, err := approvalclaim.Issue(secret, approvalclaim.Payload{
		Version:       1,
		OperationName: name,
		ExpiresAt:     time.Now().Add(-time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	scheme := testOperationControllerScheme(t)
	op := sampleRestoreOperation(name, namespace, "app")
	op.Spec.Approvals = &opsv1alpha1.ApprovalsSpec{Claim: claim}
	op.Status = opsv1alpha1.OperationStatus{Phase: opsv1alpha1.PhaseRunning}

	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(sampleTargetApp(namespace, "app"), op).
		WithStatusSubresource(op).
		Build()

	reconciler := &OperationReconciler{
		Client:              cl,
		Scheme:              scheme,
		Registry:            engines.NewRegistry(stubOperationEngine{name: opsv1alpha1.EngineVelero}),
		Reporter:            agent.NoopStatusReporter(),
		ApprovalClaimSecret: secret,
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	updated := &opsv1alpha1.Operation{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, updated); err != nil {
		t.Fatalf("get operation: %v", err)
	}
	if updated.Status.Phase != opsv1alpha1.PhaseRunning {
		t.Fatalf("expected phase to stay Running, got %q", updated.Status.Phase)
	}
	blocked := findCondition(updated.Status.Conditions, opsv1alpha1.ConditionBlocked)
	if blocked != nil && blocked.Status == metav1.ConditionTrue && blocked.Reason == "AwaitingApproval" {
		t.Fatal("expected running operation not to regress to AwaitingApproval on expired claim")
	}
}

func TestOperationReconcilerFailsWhenEngineWorkloadMissing(t *testing.T) {
	t.Parallel()
	const namespace = "team-a"
	scheme := testOperationControllerScheme(t)
	op := sampleRestoreOperation("run-1", namespace, "app")
	op.Spec.Engine = opsv1alpha1.EngineJob
	op.Status = opsv1alpha1.OperationStatus{Phase: opsv1alpha1.PhaseRunning}
	cl := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(sampleTargetApp(namespace, "app"), op).
		WithStatusSubresource(op).
		Build()
	reconciler := &OperationReconciler{
		Client:   cl,
		Scheme:   scheme,
		Registry: engines.NewRegistry(engines.NewJobEngine(cl)),
		Reporter: agent.NoopStatusReporter(),
	}
	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "run-1", Namespace: namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	updated := &opsv1alpha1.Operation{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "run-1", Namespace: namespace}, updated); err != nil {
		t.Fatalf("get operation: %v", err)
	}
	if updated.Status.Phase != opsv1alpha1.PhaseFailed {
		t.Fatalf("expected Failed when workload missing, got %q", updated.Status.Phase)
	}
}

func TestOperationReconcilerFinalizeDeletesLabeledJob(t *testing.T) {
	t.Parallel()
	const namespace = "team-a"
	now := metav1.Now()
	scheme := testOperationControllerScheme(t)
	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "run-1",
			Namespace:         namespace,
			UID:               types.UID("op-uid"),
			DeletionTimestamp: &now,
			Finalizers:        []string{opsv1alpha1.OperationFinalizer},
		},
		Spec: opsv1alpha1.OperationSpec{
			TargetRef: opsv1alpha1.TargetRef{Name: "app"},
			Engine:    opsv1alpha1.EngineJob,
		},
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "orphan-job", Namespace: namespace,
			Labels: map[string]string{engines.OperationLabelKey: "op-uid"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(op, job).Build()
	reconciler := &OperationReconciler{
		Client:   cl,
		Scheme:   scheme,
		Registry: engines.NewRegistry(engines.NewJobEngine(cl)),
		Reporter: agent.NoopStatusReporter(),
	}

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "run-1", Namespace: namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if err := cl.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: "orphan-job"}, &batchv1.Job{}); !apierrors.IsNotFound(err) {
		t.Fatalf("expected labeled job deleted, get err: %v", err)
	}
	updated := &opsv1alpha1.Operation{}
	err := cl.Get(context.Background(), types.NamespacedName{Name: "run-1", Namespace: namespace}, updated)
	if err == nil {
		if len(updated.Finalizers) != 0 {
			t.Fatalf("expected finalizer removed, got %v", updated.Finalizers)
		}
		return
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("get operation: %v", err)
	}
}

func findCondition(conditions []metav1.Condition, typ string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == typ {
			return &conditions[i]
		}
	}
	return nil
}
