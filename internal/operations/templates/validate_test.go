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
