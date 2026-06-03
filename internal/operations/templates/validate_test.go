package templates_test

import (
	"strings"
	"testing"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/templates"
)

func TestValidateParametersBackupAllowsEmpty(t *testing.T) {
	if err := templates.ValidateParameters(opsv1alpha1.OperationTypeBackup, opsv1alpha1.EngineVelero, nil); err != nil {
		t.Fatalf("expected empty backup parameters to pass: %v", err)
	}
}

func TestValidateParametersRestoreRequiresBackupName(t *testing.T) {
	err := templates.ValidateParameters(opsv1alpha1.OperationTypeRestore, opsv1alpha1.EngineVelero, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "backupName") {
		t.Fatalf("expected backupName required, got: %v", err)
	}
}

func TestValidateParametersRestoreRejectsUnknownField(t *testing.T) {
	raw := []byte(`{"backupName":"b1","ttl":"720h"}`)
	err := templates.ValidateParameters(opsv1alpha1.OperationTypeRestore, opsv1alpha1.EngineVelero, raw)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected schema rejection for unknown field, got: %v", err)
	}
}

func TestValidateParametersRunCommandRequiresImage(t *testing.T) {
	err := templates.ValidateParameters(opsv1alpha1.OperationTypeRunCommand, opsv1alpha1.EngineJob, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("expected image required, got: %v", err)
	}
}

func TestValidateParametersBackupAcceptsTTLAlias(t *testing.T) {
	raw := []byte(`{"ttl":"720h","storageLocation":"aws-primary"}`)
	if err := templates.ValidateParameters(opsv1alpha1.OperationTypeBackup, opsv1alpha1.EngineVelero, raw); err != nil {
		t.Fatalf("expected backup ttl alias to pass: %v", err)
	}
}

func TestValidateParametersRunCommandAcceptsDocumentedEnv(t *testing.T) {
	raw := []byte(`{
		"image": "ghcr.io/example/tools:1",
		"env": [{"name": "PG_HOST", "value": "db"}],
		"activeDeadlineSeconds": 1800,
		"backoffLimit": 2
	}`)
	if err := templates.ValidateParameters(opsv1alpha1.OperationTypeRunCommand, opsv1alpha1.EngineJob, raw); err != nil {
		t.Fatalf("expected documented runCommand parameters to pass: %v", err)
	}
}

func TestValidateParametersRunbookAcceptsDocumentedWorkflowArgs(t *testing.T) {
	raw := []byte(`{
		"template": "app-migration-with-snapshot",
		"targetChartVersion": "7.0.0",
		"snapshotClassName": "csi-rbd",
		"timeoutSeconds": 1800,
		"failureAction": "rollback"
	}`)
	if err := templates.ValidateParameters(opsv1alpha1.OperationTypeRunbook, opsv1alpha1.EngineWorkflow, raw); err != nil {
		t.Fatalf("expected documented runbook parameters to pass: %v", err)
	}
}

func TestValidateParametersMigrationHelmHookAcceptsJobTuning(t *testing.T) {
	raw := []byte(`{"hookName":"pre-upgrade","activeDeadlineSeconds":1800,"backoffLimit":1}`)
	if err := templates.ValidateParameters(opsv1alpha1.OperationTypeMigration, opsv1alpha1.EngineHelmHookJob, raw); err != nil {
		t.Fatalf("expected documented helm hook parameters to pass: %v", err)
	}
}
