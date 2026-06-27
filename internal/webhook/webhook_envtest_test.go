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

package webhook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/apps/v1alpha1"
	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/approvalclaim"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var (
	envtestCfg         *rest.Config
	envtestClient      client.Client
	envtestScheme      *runtime.Scheme
	envtestCancel      context.CancelFunc
	envtestEnvironment *envtest.Environment
)

const envtestApprovalClaimSecret = "envtest-approval-claim-secret"

func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stderr), zap.UseDevMode(true)))

	envtestScheme = runtime.NewScheme()
	utilruntime.Must(scheme.AddToScheme(envtestScheme))
	utilruntime.Must(appsv1alpha1.AddToScheme(envtestScheme))
	utilruntime.Must(opsv1alpha1.AddToScheme(envtestScheme))
	utilruntime.Must(corev1.AddToScheme(envtestScheme))

	webhookDir, err := filepath.Abs(filepath.Join("..", "..", "config", "webhook"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "webhook config path: %v\n", err)
		os.Exit(1)
	}

	envtestEnvironment = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{webhookDir},
		},
	}
	if dir := getEnvtestBinaryDir(); dir != "" {
		_ = os.Setenv("KUBEBUILDER_ASSETS", dir)
		envtestEnvironment.BinaryAssetsDirectory = dir
	}

	envtestCfg, err = envtestEnvironment.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "envtest start: %v\n", err)
		os.Exit(1)
	}

	envtestClient, err = client.New(envtestCfg, client.Options{Scheme: envtestScheme})
	if err != nil {
		fmt.Fprintf(os.Stderr, "envtest client: %v\n", err)
		_ = envtestEnvironment.Stop()
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	envtestCancel = cancel

	mgr, err := ctrl.NewManager(envtestCfg, ctrl.Options{
		Scheme: envtestScheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    envtestEnvironment.WebhookInstallOptions.LocalServingHost,
			Port:    envtestEnvironment.WebhookInstallOptions.LocalServingPort,
			CertDir: envtestEnvironment.WebhookInstallOptions.LocalServingCertDir,
		}),
		LeaderElection: false,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "manager: %v\n", err)
		teardownEnvtest()
		os.Exit(1)
	}

	opHook, err := NewOperationWebhook(mgr.GetScheme(), mgr.GetClient(), OperationWebhookOptions{
		ApprovalClaimSecret: envtestApprovalClaimSecret,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "operation webhook: %v\n", err)
		teardownEnvtest()
		os.Exit(1)
	}
	if err := opHook.SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "register operation webhook: %v\n", err)
		teardownEnvtest()
		os.Exit(1)
	}

	appHook, err := NewApplicationInstanceWebhook(mgr.GetScheme())
	if err != nil {
		fmt.Fprintf(os.Stderr, "applicationinstance webhook: %v\n", err)
		teardownEnvtest()
		os.Exit(1)
	}
	if err := appHook.SetupWithManager(mgr); err != nil {
		fmt.Fprintf(os.Stderr, "register applicationinstance webhook: %v\n", err)
		teardownEnvtest()
		os.Exit(1)
	}

	go func() {
		if startErr := mgr.Start(ctx); startErr != nil {
			fmt.Fprintf(os.Stderr, "manager start: %v\n", startErr)
		}
	}()

	time.Sleep(2 * time.Second)

	code := m.Run()
	teardownEnvtest()
	os.Exit(code)
}

func teardownEnvtest() {
	if envtestCancel != nil {
		envtestCancel()
	}
	if envtestEnvironment != nil {
		_ = envtestEnvironment.Stop()
	}
}

func getEnvtestBinaryDir() string {
	if dir := os.Getenv("KUBEBUILDER_ASSETS"); dir != "" {
		if filepath.IsAbs(dir) {
			return dir
		}
		moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			return dir
		}
		return filepath.Join(moduleRoot, dir)
	}
	localbin := os.Getenv("LOCALBIN")
	if localbin == "" {
		localbin = filepath.Join("..", "..", "bin")
	}
	if abs, err := filepath.Abs(localbin); err == nil {
		localbin = abs
	}
	basePath := filepath.Join(localbin, "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

func TestWebhookAllowedTypePasses(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-allowed-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "Backup,Restore")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	op := sampleEnvtestOperation(ns, "backup-1", "app", opsv1alpha1.OperationTypeBackup)
	if err := envtestClient.Create(ctx, op); err != nil {
		t.Fatalf("create Operation: %v", err)
	}
}

func TestWebhookDisallowedTypeRejected(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-disallowed-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "Backup")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	op := sampleEnvtestOperation(ns, "restore-1", "app", opsv1alpha1.OperationTypeRestore)
	err := envtestClient.Create(ctx, op)
	if err == nil {
		t.Fatal("expected admission rejection for disallowed operation type")
	}
	if !strings.Contains(err.Error(), "not allowed in namespace") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookConcurrentOperationRejected(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-concurrent-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	existing := sampleEnvtestOperation(ns, "upgrade-1", "app", opsv1alpha1.OperationTypeUpgrade)
	if err := envtestClient.Create(ctx, existing); err != nil {
		t.Fatalf("create first Operation: %v", err)
	}
	existing.Status.Phase = opsv1alpha1.PhaseRunning
	if err := envtestClient.Status().Update(ctx, existing); err != nil {
		t.Fatalf("update first Operation status: %v", err)
	}

	op := sampleEnvtestOperation(ns, "restore-2", "app", opsv1alpha1.OperationTypeRestore)
	op.Spec.Parameters = &runtime.RawExtension{Raw: []byte(`{"backupName":"b1"}`)}
	err := envtestClient.Create(ctx, op)
	if err == nil {
		t.Fatal("expected admission rejection for concurrent restore during upgrade")
	}
	if !strings.Contains(err.Error(), "conflicts with operation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookRejectsMissingCapability(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-capability-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	delete(app.Annotations, "ops.vworkspace.io/backup")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	op := sampleEnvtestOperation(ns, "backup-1", "app", opsv1alpha1.OperationTypeBackup)
	err := envtestClient.Create(ctx, op)
	if err == nil {
		t.Fatal("expected admission rejection for missing backup capability")
	}
	if !strings.Contains(err.Error(), "capability") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookRejectsUnknownTemplatePair(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-template-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	op := sampleEnvtestOperation(ns, "backup-volsync", "app", opsv1alpha1.OperationTypeBackup)
	op.Spec.Engine = opsv1alpha1.EngineVolsync
	err := envtestClient.Create(ctx, op)
	if err == nil {
		t.Fatal("expected admission rejection for unknown type+engine template pair")
	}
	if !strings.Contains(err.Error(), "template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookRejectsInvalidRestoreParameters(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-restore-params-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	op := sampleEnvtestOperation(ns, "restore-1", "app", opsv1alpha1.OperationTypeRestore)
	err := envtestClient.Create(ctx, op)
	if err == nil {
		t.Fatal("expected admission rejection for restore without backupName parameter")
	}
	if !strings.Contains(err.Error(), "backupName") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookRejectsPrivilegedServiceAccount(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-privileged-sa-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	app.Annotations["ops.vworkspace.io/runcommand"] = "job"
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	op := sampleEnvtestOperation(ns, "run-1", "app", opsv1alpha1.OperationTypeRunCommand)
	op.Spec.Engine = opsv1alpha1.EngineJob
	op.Spec.Parameters = &runtime.RawExtension{Raw: []byte(`{
		"image": "alpine:3.20",
		"serviceAccountName": "cluster-admin"
	}`)}
	err := envtestClient.Create(ctx, op)
	if err == nil {
		t.Fatal("expected admission rejection for privileged service account")
	}
	if !strings.Contains(err.Error(), "serviceAccountName") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookRejectsInvalidApprovalClaim(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-approval-invalid-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	op := sampleEnvtestOperation(ns, "restore-1", "app", opsv1alpha1.OperationTypeRestore)
	op.Spec.Parameters = &runtime.RawExtension{Raw: []byte(`{"backupName":"b1"}`)}
	op.Spec.Approvals = &opsv1alpha1.ApprovalsSpec{
		Required: true,
		Claim:    "vws1.invalid.body",
	}
	err := envtestClient.Create(ctx, op)
	if err == nil {
		t.Fatal("expected admission rejection for invalid approval claim")
	}
	if !strings.Contains(err.Error(), "spec.approvals.claim") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookAcceptsValidApprovalClaim(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-approval-valid-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	claim, err := approvalclaim.Issue(envtestApprovalClaimSecret, approvalclaim.Payload{
		Version:       1,
		OperationName: "restore-1",
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	op := sampleEnvtestOperation(ns, "restore-1", "app", opsv1alpha1.OperationTypeRestore)
	op.Spec.Parameters = &runtime.RawExtension{Raw: []byte(`{"backupName":"b1"}`)}
	op.Spec.Approvals = &opsv1alpha1.ApprovalsSpec{Required: true, Claim: claim}
	if err := envtestClient.Create(ctx, op); err != nil {
		t.Fatalf("create Operation with valid approval claim: %v", err)
	}
}

func TestWebhookAllowsBackupDuringUpgrade(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-backup-during-upgrade-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	existing := sampleEnvtestOperation(ns, "upgrade-1", "app", opsv1alpha1.OperationTypeUpgrade)
	if err := envtestClient.Create(ctx, existing); err != nil {
		t.Fatalf("create upgrade Operation: %v", err)
	}
	existing.Status.Phase = opsv1alpha1.PhaseRunning
	if err := envtestClient.Status().Update(ctx, existing); err != nil {
		t.Fatalf("update upgrade Operation status: %v", err)
	}

	op := sampleEnvtestOperation(ns, "backup-2", "app", opsv1alpha1.OperationTypeBackup)
	if err := envtestClient.Create(ctx, op); err != nil {
		t.Fatalf("expected backup during running upgrade to pass: %v", err)
	}
}

func TestWebhookRejectsConcurrentUpgrade(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-concurrent-upgrade-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create ApplicationInstance: %v", err)
	}

	existing := sampleEnvtestOperation(ns, "upgrade-1", "app", opsv1alpha1.OperationTypeUpgrade)
	if err := envtestClient.Create(ctx, existing); err != nil {
		t.Fatalf("create first Operation: %v", err)
	}
	existing.Status.Phase = opsv1alpha1.PhaseRunning
	if err := envtestClient.Status().Update(ctx, existing); err != nil {
		t.Fatalf("update first Operation status: %v", err)
	}

	op := sampleEnvtestOperation(ns, "upgrade-2", "app", opsv1alpha1.OperationTypeUpgrade)
	err := envtestClient.Create(ctx, op)
	if err == nil {
		t.Fatal("expected admission rejection for concurrent upgrade")
	}
	if !strings.Contains(err.Error(), "conflicts with operation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookInlineSecretRejected(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-inline-secret-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := sampleEnvtestApplicationInstance(ns, "app")
	app.Spec.Values.Inline = &runtime.RawExtension{Raw: []byte(`{"postgresql":{"auth":{"password":"hunter2"}}}`)}
	err := envtestClient.Create(ctx, app)
	if err == nil {
		t.Fatal("expected admission rejection for inline secret material")
	}
	if !strings.Contains(err.Error(), "inline secret material rejected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebhookAdmitsRunCommandAgainstPlaceholder(t *testing.T) {
	ctx := context.Background()
	ns := "webhook-placeholder-" + uniqueSuffix()
	createTestNamespace(t, ctx, ns, "")

	app := placeholderEnvtestApplicationInstance(ns, "cluster-ops")
	if err := envtestClient.Create(ctx, app); err != nil {
		t.Fatalf("create placeholder ApplicationInstance: %v", err)
	}

	op := sampleEnvtestOperation(ns, "run-1", "cluster-ops", opsv1alpha1.OperationTypeRunCommand)
	op.Spec.Engine = opsv1alpha1.EngineJob
	op.Spec.Parameters = &runtime.RawExtension{Raw: []byte(`{"image":"alpine:3.20"}`)}
	if err := envtestClient.Create(ctx, op); err != nil {
		t.Fatalf("expected RunCommand against placeholder to be admitted: %v", err)
	}
}

func createTestNamespace(t *testing.T, ctx context.Context, name, allowedTypes string) {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if allowedTypes != "" {
		ns.Annotations = map[string]string{
			namespaceAllowedTypesAnnotation: allowedTypes,
		}
	}
	if err := envtestClient.Create(ctx, ns); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return
		}
		t.Fatalf("create namespace: %v", err)
	}
}

func sampleEnvtestApplicationInstance(namespace, name string) *appsv1alpha1.ApplicationInstance { //nolint:unparam // test fixture
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"ops.vworkspace.io/backup":    "velero",
				"ops.vworkspace.io/restore":   "velero",
				"ops.vworkspace.io/upgrade":   "helm",
				"ops.vworkspace.io/migration": "helmHookJob",
			},
		},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "nextcloud"},
			Chart: &appsv1alpha1.ChartSpec{
				SourceType: appsv1alpha1.ChartSourceOCI,
				URL:        "oci://registry.example.com/charts",
				Name:       "nextcloud",
				Version:    "6.6.0",
			},
			Release: &appsv1alpha1.ReleaseSpec{Name: name, Namespace: namespace},
			Values: &appsv1alpha1.ValuesSpec{
				Source: appsv1alpha1.ValuesSourceInline,
				Inline: &runtime.RawExtension{Raw: []byte(`{"ingress":{"enabled":true}}`)},
			},
		},
	}
}

func placeholderEnvtestApplicationInstance(namespace, name string) *appsv1alpha1.ApplicationInstance { //nolint:unparam // test fixture
	return &appsv1alpha1.ApplicationInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"ops.vworkspace.io/runcommand": "job",
				"ops.vworkspace.io/runbook":    "workflow",
			},
		},
		Spec: appsv1alpha1.ApplicationInstanceSpec{
			AppRef: appsv1alpha1.AppRef{CatalogID: "cluster-ops"},
			Mode:   appsv1alpha1.InstanceModePlaceholder,
		},
	}
}

func sampleEnvtestOperation(namespace, name, target string, opType opsv1alpha1.OperationType) *opsv1alpha1.Operation { //nolint:unparam // test fixture
	engine := opsv1alpha1.EngineVelero
	switch opType {
	case opsv1alpha1.OperationTypeUpgrade:
		engine = opsv1alpha1.EngineHelm
	case opsv1alpha1.OperationTypeMigration:
		engine = opsv1alpha1.EngineHelmHookJob
	}
	return &opsv1alpha1.Operation{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: opsv1alpha1.OperationSpec{
			TargetRef: opsv1alpha1.TargetRef{
				APIVersion: appsv1alpha1.GroupVersion.String(),
				Kind:       "ApplicationInstance",
				Name:       target,
			},
			Type:   opType,
			Engine: engine,
		},
	}
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
