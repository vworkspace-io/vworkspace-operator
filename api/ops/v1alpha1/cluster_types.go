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
)

const (
	ConditionConnected          = "Connected"
	ConditionDisconnected       = "Disconnected"
	ConditionAuthenticated      = "Authenticated"
	ConditionControllersHealthy = "ControllersHealthy"
	ConditionBufferOverflow     = "BufferOverflow"
)

// ClusterSpec defines operator cluster identity configuration.
type ClusterSpec struct {
	// ClusterID is the stable identity registered with the control plane.
	// +optional
	ClusterID string `json:"clusterId,omitempty"`
	// ControlPlaneBaseURL is the HTTPS base URL for Pull-mode connectivity to vWorkspace Server.
	// +optional
	ControlPlaneBaseURL string `json:"controlPlaneBaseUrl,omitempty"`
	// RegistrationToken is a one-time token exchanged for a long-lived credential.
	// Cleared from the spec after successful registration.
	// +optional
	RegistrationToken string `json:"registrationToken,omitempty"`
	// RotateCredentials requests an immediate bootstrap credential rotation via the control plane.
	// Cleared from the spec after a successful rotation.
	// +optional
	RotateCredentials bool `json:"rotateCredentials,omitempty"`
}

// ClusterCredentialStatus reports bootstrap credential materialization.
type ClusterCredentialStatus struct {
	// SecretName is the Kubernetes Secret holding control-plane-base-url, cluster-id, and token.
	SecretName string `json:"secretName,omitempty"`
	// SecretNamespace is the namespace of the credentials Secret.
	SecretNamespace string `json:"secretNamespace,omitempty"`
	// RegisteredAt is when the one-time registration token was exchanged.
	// +optional
	RegisteredAt *metav1.Time `json:"registeredAt,omitempty"`
	// RegistrationTokenConsumed indicates the one-time token was exchanged successfully.
	RegistrationTokenConsumed bool `json:"registrationTokenConsumed,omitempty"`
}

// ClusterStatus defines observed cluster connectivity posture.
type ClusterStatus struct {
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// LastHeartbeat is updated when a recent round-trip to the control plane succeeded.
	// +optional
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`
	// CredentialStatus reports where bootstrap credentials are stored.
	// +optional
	CredentialStatus *ClusterCredentialStatus `json:"credentialStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=cluster
// +kubebuilder:printcolumn:name="Connected",type=string,JSONPath=`.status.conditions[?(@.type=="Connected")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Cluster reports operator connectivity and controller health.
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster.
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cluster{}, &ClusterList{})
}
