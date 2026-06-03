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

package templates

// inputSchemaJSON holds draft-07 JSON Schemas for built-in template parameters.
// Fields align with engine parsers in internal/engines and docs/operations/engines/.
var inputSchemaJSON = map[string]string{
	RefBackupVelero: `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "retention": { "type": "string" },
    "ttl": { "type": "string" },
    "snapshotClassName": { "type": "string" },
    "csiSnapshotClassName": { "type": "string" },
    "snapshotVolumes": { "type": "boolean" },
    "storageLocation": { "type": "string" },
    "includedResources": {
      "type": "array",
      "items": { "type": "string" }
    },
    "excludedResources": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}`,
	RefRestoreVelero: `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["backupName"],
  "properties": {
    "backupName": { "type": "string", "minLength": 1 },
    "namespaceMapping": {
      "type": "object",
      "additionalProperties": { "type": "string" }
    },
    "restorePVs": { "type": "boolean" },
    "existingResourcePolicy": { "type": "string" },
    "includedResources": {
      "type": "array",
      "items": { "type": "string" }
    },
    "excludedResources": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}`,
	RefUpgradeHelm: `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "targetVersion": { "type": "string", "minLength": 1 },
    "valuesPatch": { "type": "object" }
  }
}`,
	RefMigrationHelmHookJob: `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["hookName"],
  "properties": {
    "hookName": { "type": "string", "minLength": 1 },
    "activeDeadlineSeconds": { "type": "integer", "minimum": 1 },
    "backoffLimit": { "type": "integer", "minimum": 0 }
  }
}`,
	RefRunCommandJob: `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["image"],
  "properties": {
    "image": { "type": "string", "minLength": 1 },
    "command": { "type": "array", "items": { "type": "string" } },
    "args": { "type": "array", "items": { "type": "string" } },
    "env": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name"],
        "properties": {
          "name": { "type": "string", "minLength": 1 },
          "value": { "type": "string" },
          "valueFrom": { "type": "object" }
        }
      }
    },
    "activeDeadlineSeconds": { "type": "integer", "minimum": 1 },
    "timeoutSeconds": { "type": "integer", "minimum": 1 },
    "backoffLimit": { "type": "integer", "minimum": 0 },
    "serviceAccountName": { "type": "string", "minLength": 1 }
  }
}`,
	RefRunbookWorkflow: `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["template"],
  "properties": {
    "template": { "type": "string", "minLength": 1 },
    "targetChartVersion": { "type": "string" },
    "snapshotClassName": { "type": "string" },
    "timeoutSeconds": { "type": "integer", "minimum": 1 },
    "failureAction": { "type": "string" }
  }
}`,
}

// InputSchema returns the JSON Schema document for a built-in template ref.
func InputSchema(ref string) (string, bool) {
	schema, ok := inputSchemaJSON[ref]
	return schema, ok
}
