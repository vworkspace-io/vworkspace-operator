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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const OperationFinalizer = "ops.vworkspace.io/finalizer"

// OperationType is the verb executed by the operation.
// +kubebuilder:validation:Enum=Backup;Restore;Upgrade;Migration;RunCommand;Runbook
type OperationType string

const (
	OperationTypeBackup     OperationType = "Backup"
	OperationTypeRestore    OperationType = "Restore"
	OperationTypeUpgrade    OperationType = "Upgrade"
	OperationTypeMigration  OperationType = "Migration"
	OperationTypeRunCommand OperationType = "RunCommand"
	OperationTypeRunbook    OperationType = "Runbook"
)

// OperationEngine is the executor for the operation.
// +kubebuilder:validation:Enum=velero;workflow;job;helm;volsync;helmHookJob
type OperationEngine string

const (
	EngineVelero      OperationEngine = "velero"
	EngineWorkflow    OperationEngine = "workflow"
	EngineJob         OperationEngine = "job"
	EngineHelm        OperationEngine = "helm"
	EngineVolsync     OperationEngine = "volsync"
	EngineHelmHookJob OperationEngine = "helmHookJob"
)

// OperationPhase summarizes operation progress.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed;Cancelled
type OperationPhase string

const (
	PhasePending   OperationPhase = "Pending"
	PhaseRunning   OperationPhase = "Running"
	PhaseSucceeded OperationPhase = "Succeeded"
	PhaseFailed    OperationPhase = "Failed"
	PhaseCancelled OperationPhase = "Cancelled"
)

type TargetRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type ApprovalsSpec struct {
	Required   bool   `json:"required,omitempty"`
	Claim      string `json:"claim,omitempty"`
	ApprovedBy string `json:"approvedBy,omitempty"`
	ApprovedAt string `json:"approvedAt,omitempty"`
}

// OperationSpec defines the desired state of Operation.
type OperationSpec struct {
	TargetRef  TargetRef             `json:"targetRef"`
	Type       OperationType         `json:"type"`
	Engine     OperationEngine       `json:"engine"`
	Parameters *runtime.RawExtension `json:"parameters,omitempty"`
	Approvals  *ApprovalsSpec        `json:"approvals,omitempty"`
}

type LogsRef struct {
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	URL       string `json:"url,omitempty"`
}

// EngineResourceRef records the namespace/name of the materialized engine workload.
type EngineResourceRef struct {
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// OperationStatus defines the observed state of Operation.
type OperationStatus struct {
	Phase      OperationPhase     `json:"phase,omitempty"`
	StartedAt  *metav1.Time       `json:"startedAt,omitempty"`
	FinishedAt *metav1.Time       `json:"finishedAt,omitempty"`
	EngineRef  *EngineResourceRef `json:"engineRef,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition   `json:"conditions,omitempty"`
	Outputs    runtime.RawExtension `json:"outputs,omitempty"`
	LogsRef    *LogsRef             `json:"logsRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=op
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Engine",type=string,JSONPath=`.spec.engine`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Operation is the Schema for the operations API.
type Operation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperationSpec   `json:"spec,omitempty"`
	Status OperationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OperationList contains a list of Operation.
type OperationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Operation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Operation{}, &OperationList{})
}
