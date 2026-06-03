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

// Package templates provides the operator's compiled-in operation template registry.
// Template refs align with vws_catalog / vws_operations BUILTIN_OPERATION_TEMPLATE_REFS.
package templates

import (
	"strings"

	opsv1alpha1 "github.com/vworkspace-io/vworkspace-operator/api/ops/v1alpha1"
)

const CapabilityAnnotationPrefix = "ops.vworkspace.io/"

// Template describes a built-in operation template (type, engine, capability verb, RBAC profile).
type Template struct {
	Ref            string
	Type           opsv1alpha1.OperationType
	Engine         opsv1alpha1.OperationEngine
	CapabilityVerb string
	RBACProfile    string
}

// BuiltinRefs lists stable template IDs shared with the control plane catalog.
var BuiltinRefs = []string{
	RefBackupVelero,
	RefRestoreVelero,
	RefUpgradeHelm,
	RefMigrationHelmHookJob,
	RefRunCommandJob,
	RefRunbookWorkflow,
}

const (
	RefBackupVelero         = "backup.velero"
	RefRestoreVelero        = "restore.velero"
	RefUpgradeHelm          = "upgrade.helm"
	RefMigrationHelmHookJob = "migration.helmHookJob"
	RefRunCommandJob        = "runCommand.job"
	RefRunbookWorkflow      = "runbook.workflow"
)

var builtin = []Template{
	{
		Ref:            RefBackupVelero,
		Type:           opsv1alpha1.OperationTypeBackup,
		Engine:         opsv1alpha1.EngineVelero,
		CapabilityVerb: "backup",
		RBACProfile:    RefBackupVelero,
	},
	{
		Ref:            RefRestoreVelero,
		Type:           opsv1alpha1.OperationTypeRestore,
		Engine:         opsv1alpha1.EngineVelero,
		CapabilityVerb: "restore",
		RBACProfile:    RefRestoreVelero,
	},
	{
		Ref:            RefUpgradeHelm,
		Type:           opsv1alpha1.OperationTypeUpgrade,
		Engine:         opsv1alpha1.EngineHelm,
		CapabilityVerb: "upgrade",
		RBACProfile:    RefUpgradeHelm,
	},
	{
		Ref:            RefMigrationHelmHookJob,
		Type:           opsv1alpha1.OperationTypeMigration,
		Engine:         opsv1alpha1.EngineHelmHookJob,
		CapabilityVerb: "migration",
		RBACProfile:    RefMigrationHelmHookJob,
	},
	{
		Ref:            RefRunCommandJob,
		Type:           opsv1alpha1.OperationTypeRunCommand,
		Engine:         opsv1alpha1.EngineJob,
		CapabilityVerb: "runcommand",
		RBACProfile:    RefRunCommandJob,
	},
	{
		Ref:            RefRunbookWorkflow,
		Type:           opsv1alpha1.OperationTypeRunbook,
		Engine:         opsv1alpha1.EngineWorkflow,
		CapabilityVerb: "runbook",
		RBACProfile:    RefRunbookWorkflow,
	},
}

var byTypeEngine map[typeEngineKey]Template

func init() {
	byTypeEngine = make(map[typeEngineKey]Template, len(builtin))
	for _, tpl := range builtin {
		byTypeEngine[typeEngineKey{tpl.Type, tpl.Engine}] = tpl
	}
}

type typeEngineKey struct {
	typ    opsv1alpha1.OperationType
	engine opsv1alpha1.OperationEngine
}

// Lookup returns the built-in template for the given type and engine pair.
func Lookup(typ opsv1alpha1.OperationType, engine opsv1alpha1.OperationEngine) (Template, bool) {
	tpl, ok := byTypeEngine[typeEngineKey{typ, engine}]
	return tpl, ok
}

// CapabilityAnnotationKey returns ops.vworkspace.io/<verb> for a template capability verb.
func CapabilityAnnotationKey(verb string) string {
	return CapabilityAnnotationPrefix + strings.TrimSpace(verb)
}
