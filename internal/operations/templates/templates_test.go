package templates_test

import (
	"testing"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
	"github.com/vworkspace-io/vworkspace-operator/internal/operations/templates"
)

func TestLookupBuiltinTemplates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typ    opsv1alpha1.OperationType
		engine opsv1alpha1.OperationEngine
		ref    string
		verb   string
	}{
		{opsv1alpha1.OperationTypeBackup, opsv1alpha1.EngineVelero, templates.RefBackupVelero, "backup"},
		{opsv1alpha1.OperationTypeRestore, opsv1alpha1.EngineVelero, templates.RefRestoreVelero, "restore"},
		{opsv1alpha1.OperationTypeUpgrade, opsv1alpha1.EngineHelm, templates.RefUpgradeHelm, "upgrade"},
		{opsv1alpha1.OperationTypeMigration, opsv1alpha1.EngineHelmHookJob, templates.RefMigrationHelmHookJob, "migration"},
	}
	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			t.Parallel()
			tpl, ok := templates.Lookup(tc.typ, tc.engine)
			if !ok {
				t.Fatalf("expected template for %s/%s", tc.typ, tc.engine)
			}
			if tpl.Ref != tc.ref || tpl.CapabilityVerb != tc.verb {
				t.Fatalf("got %+v, want ref=%s verb=%s", tpl, tc.ref, tc.verb)
			}
			key := templates.CapabilityAnnotationKey(tc.verb)
			if key != templates.CapabilityAnnotationPrefix+tc.verb {
				t.Fatalf("unexpected key %q", key)
			}
		})
	}
}

func TestLookupRejectsUnknownPair(t *testing.T) {
	t.Parallel()
	if _, ok := templates.Lookup(opsv1alpha1.OperationTypeBackup, opsv1alpha1.EngineVolsync); ok {
		t.Fatal("expected no template for backup+volsync")
	}
}
