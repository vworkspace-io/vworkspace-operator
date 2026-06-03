package engines

import (
	"context"
	"testing"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRegistrySelection(t *testing.T) {
	registry := NewRegistry(
		NewVeleroEngine(fake.NewClientBuilder().Build()),
		NewJobEngine(fake.NewClientBuilder().Build()),
		NewWorkflowEngine(fake.NewClientBuilder().Build()),
		NewHelmHookJobEngine(fake.NewClientBuilder().Build()),
	)
	for _, engine := range []opsv1alpha1.OperationEngine{
		opsv1alpha1.EngineVelero,
		opsv1alpha1.EngineJob,
		opsv1alpha1.EngineWorkflow,
		opsv1alpha1.EngineHelmHookJob,
	} {
		if !registry.Has(engine) {
			t.Fatalf("expected %q engine to be registered", engine)
		}
	}
	if _, err := registry.Get(opsv1alpha1.OperationEngine("unknown")); err == nil {
		t.Fatal("expected missing engine error")
	}
}

func TestVeleroEngineCreatesBackup(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = opsv1alpha1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	engine := NewVeleroEngine(c)

	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "backup-1", Namespace: "team-a"},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeBackup,
			Engine: opsv1alpha1.EngineVelero,
		},
	}
	target := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec:       appsv1alpha1.ApplicationInstanceSpec{Release: appsv1alpha1.ReleaseSpec{Namespace: "team-a"}},
	}

	if err := engine.Materialize(context.Background(), op, target); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	backup := &unstructured.Unstructured{}
	backup.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Backup"})
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "team-a", Name: "backup-1"}, backup); err != nil {
		t.Fatalf("expected backup CR: %v", err)
	}
	namespaces, _, _ := unstructured.NestedStringSlice(backup.Object, "spec", "includedNamespaces")
	if len(namespaces) != 1 || namespaces[0] != "team-a" {
		t.Fatalf("unexpected includedNamespaces: %v", namespaces)
	}
}

func TestVeleroEngineCreatesBackupWithDocumentedParameters(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = opsv1alpha1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	engine := NewVeleroEngine(c)

	snapshotVolumes := true
	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "backup-2", Namespace: "team-a"},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeBackup,
			Engine: opsv1alpha1.EngineVelero,
			Parameters: &runtime.RawExtension{Raw: []byte(`{
				"storageLocation": "aws-primary",
				"snapshotVolumes": true,
				"csiSnapshotClassName": "csi-rbd",
				"ttl": "720h",
				"includedResources": ["*"],
				"excludedResources": ["events"]
			}`)},
		},
	}
	target := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec:       appsv1alpha1.ApplicationInstanceSpec{Release: appsv1alpha1.ReleaseSpec{Namespace: "team-a"}},
	}

	if err := engine.Materialize(context.Background(), op, target); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	backup := &unstructured.Unstructured{}
	backup.SetGroupVersionKind(schema.GroupVersionKind{Group: "velero.io", Version: "v1", Kind: "Backup"})
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "team-a", Name: "backup-2"}, backup); err != nil {
		t.Fatalf("expected backup CR: %v", err)
	}
	location, _, _ := unstructured.NestedString(backup.Object, "spec", "storageLocation")
	if location != "aws-primary" {
		t.Fatalf("unexpected storageLocation: %q", location)
	}
	snapshots, _, _ := unstructured.NestedBool(backup.Object, "spec", "snapshotVolumes")
	if snapshots != snapshotVolumes {
		t.Fatalf("unexpected snapshotVolumes: %v", snapshots)
	}
	csiClass, _, _ := unstructured.NestedString(backup.Object, "spec", "csiSnapshotClassName")
	if csiClass != "csi-rbd" {
		t.Fatalf("unexpected csiSnapshotClassName: %q", csiClass)
	}
	ttl, _, _ := unstructured.NestedString(backup.Object, "spec", "ttl")
	if ttl != "720h" {
		t.Fatalf("unexpected ttl: %q", ttl)
	}
	included, _, _ := unstructured.NestedStringSlice(backup.Object, "spec", "includedResources")
	if len(included) != 1 || included[0] != "*" {
		t.Fatalf("unexpected includedResources: %v", included)
	}
	excluded, _, _ := unstructured.NestedStringSlice(backup.Object, "spec", "excludedResources")
	if len(excluded) != 1 || excluded[0] != "events" {
		t.Fatalf("unexpected excludedResources: %v", excluded)
	}
}

func TestJobEngineCreatesJob(t *testing.T) {
	scheme := testScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	engine := NewJobEngine(c)

	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "team-a", UID: types.UID("op-uid")},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeRunCommand,
			Engine: opsv1alpha1.EngineJob,
			Parameters: &runtime.RawExtension{Raw: []byte(`{
				"image": "alpine:3.20",
				"command": ["/bin/sh", "-c"],
				"args": ["echo hello"]
			}`)},
		},
	}
	target := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec:       appsv1alpha1.ApplicationInstanceSpec{Release: appsv1alpha1.ReleaseSpec{Namespace: "team-a"}},
	}

	if err := engine.Materialize(context.Background(), op, target); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	job := &batchv1.Job{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "team-a", Name: "run-1"}, job); err != nil {
		t.Fatalf("expected job: %v", err)
	}
	if job.Spec.Template.Spec.Containers[0].Image != "alpine:3.20" {
		t.Fatalf("unexpected image: %q", job.Spec.Template.Spec.Containers[0].Image)
	}
	if job.Labels[OperationLabelKey] != "op-uid" {
		t.Fatalf("unexpected operation label: %q", job.Labels[OperationLabelKey])
	}
}

func TestJobEngineStatusComplete(t *testing.T) {
	scheme := testScheme()
	target := testTarget("team-a")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "team-a"},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type:   batchv1.JobComplete,
			Status: corev1.ConditionTrue,
		}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job, target).Build()
	engine := NewJobEngine(c)

	status, err := engine.Status(context.Background(), &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "team-a"},
		Spec:       opsv1alpha1.OperationSpec{TargetRef: opsv1alpha1.TargetRef{Name: "app"}},
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Done || status.Phase != opsv1alpha1.PhaseSucceeded {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestJobEngineStatusCrossNamespace(t *testing.T) {
	scheme := testScheme()
	releaseNS := "org-myteam"
	target := testTarget(releaseNS)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: releaseNS},
		Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
			Type:   batchv1.JobComplete,
			Status: corev1.ConditionTrue,
		}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job, target).Build()
	engine := NewJobEngine(c)

	status, err := engine.Status(context.Background(), &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "team-a"},
		Spec:       opsv1alpha1.OperationSpec{TargetRef: opsv1alpha1.TargetRef{Name: "app"}},
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Done || status.Phase != opsv1alpha1.PhaseSucceeded {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestWorkflowEngineStatusSucceeded(t *testing.T) {
	scheme := testScheme()
	releaseNS := "org-myteam"
	target := testTarget(releaseNS)
	wf := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Workflow",
			"metadata": map[string]any{
				"name":      "runbook-1",
				"namespace": releaseNS,
			},
			"status": map[string]any{"phase": "Succeeded"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wf, target).Build()
	engine := NewWorkflowEngine(c)

	status, err := engine.Status(context.Background(), &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "runbook-1", Namespace: "team-a"},
		Spec:       opsv1alpha1.OperationSpec{TargetRef: opsv1alpha1.TargetRef{Name: "app"}},
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Done || status.Phase != opsv1alpha1.PhaseSucceeded {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHelmHookJobEnginePreservesOperationLabels(t *testing.T) {
	scheme := testScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	engine := NewHelmHookJobEngine(c)

	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "migrate-1", Namespace: "team-a", UID: types.UID("hook-uid")},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeMigration,
			Engine: opsv1alpha1.EngineHelmHookJob,
			Parameters: &runtime.RawExtension{Raw: []byte(`{
				"hookName": "pre-upgrade",
				"image": "alpine:3.20"
			}`)},
		},
	}
	target := testTarget("team-a")

	if err := engine.Materialize(context.Background(), op, target); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	job := &batchv1.Job{}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "team-a", Name: "migrate-1"}, job); err != nil {
		t.Fatalf("expected job: %v", err)
	}
	if job.Spec.Template.Labels[helmHookNameLabel] != "pre-upgrade" {
		t.Fatalf("missing hook label on pod template")
	}
	if job.Spec.Template.Labels[OperationLabelKey] != "hook-uid" {
		t.Fatalf("operation label dropped from pod template: %q", job.Spec.Template.Labels[OperationLabelKey])
	}
}

func TestWorkflowEngineCreatesWorkflow(t *testing.T) {
	scheme := testScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	engine := NewWorkflowEngine(c)

	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "runbook-1", Namespace: "team-a", UID: types.UID("wf-uid")},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeRunbook,
			Engine: opsv1alpha1.EngineWorkflow,
			Parameters: &runtime.RawExtension{Raw: []byte(`{
				"workflowTemplate": "app-migration-with-snapshot",
				"workflowTemplateParameters": {"targetName": "app"}
			}`)},
		},
	}
	target := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec:       appsv1alpha1.ApplicationInstanceSpec{Release: appsv1alpha1.ReleaseSpec{Namespace: "team-a"}},
	}

	if err := engine.Materialize(context.Background(), op, target); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	wf := &unstructured.Unstructured{}
	wf.SetGroupVersionKind(schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Workflow"})
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "team-a", Name: "runbook-1"}, wf); err != nil {
		t.Fatalf("expected workflow: %v", err)
	}
	refName, _, _ := unstructured.NestedString(wf.Object, "spec", "workflowTemplateRef", "name")
	if refName != "app-migration-with-snapshot" {
		t.Fatalf("unexpected workflowTemplateRef: %q", refName)
	}
}

func TestHelmHookJobEngineRequiresHookName(t *testing.T) {
	engine := NewHelmHookJobEngine(fake.NewClientBuilder().Build())
	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "migrate-1", Namespace: "team-a"},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeMigration,
			Engine: opsv1alpha1.EngineHelmHookJob,
		},
	}
	target := &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec:       appsv1alpha1.ApplicationInstanceSpec{Release: appsv1alpha1.ReleaseSpec{Namespace: "team-a"}},
	}
	if err := engine.Materialize(context.Background(), op, target); err == nil {
		t.Fatal("expected hookName error")
	}
}

func TestJobEngineMaterializeIdempotent(t *testing.T) {
	scheme := testScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	engine := NewJobEngine(c)

	op := &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: "run-1", Namespace: "team-a", UID: types.UID("op-uid")},
		Spec: opsv1alpha1.OperationSpec{
			Type:   opsv1alpha1.OperationTypeRunCommand,
			Engine: opsv1alpha1.EngineJob,
			Parameters: &runtime.RawExtension{Raw: []byte(`{
				"image": "alpine:3.20",
				"env": {"B": "2", "A": "1"}
			}`)},
		},
	}
	target := testTarget("team-a")

	for range 2 {
		if err := engine.Materialize(context.Background(), op, target); err != nil {
			t.Fatalf("Materialize: %v", err)
		}
	}
}

func TestValidateRuntimeParametersRejectsPrivilegedServiceAccount(t *testing.T) {
	op := &opsv1alpha1.Operation{
		Spec: opsv1alpha1.OperationSpec{
			Engine: opsv1alpha1.EngineJob,
			Parameters: &runtime.RawExtension{Raw: []byte(`{
				"serviceAccountName": "cluster-admin"
			}`)},
		},
	}
	if err := ValidateRuntimeParameters(op); err == nil {
		t.Fatal("expected service account validation error")
	}
}

func testTarget(releaseNamespace string) *appsv1alpha1.ApplicationInstance {
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a"},
		Spec:       appsv1alpha1.ApplicationInstanceSpec{Release: appsv1alpha1.ReleaseSpec{Namespace: releaseNamespace}},
	}
}

func testScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = opsv1alpha1.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	return scheme
}
