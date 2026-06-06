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

const (
	// ClusterFinalizer guards Cluster deletion so the reconciler can run cleanup
	// (best-effort credential teardown) before the object and its owned Secrets are removed.
	ClusterFinalizer = "ops.vworkspace.io/cluster-cleanup"

	// DefaultRegistrationTokenKey is the Secret data key read when
	// registrationTokenSecretRef.key is omitted.
	DefaultRegistrationTokenKey = "registrationToken"

	// ClusterReRegisterAnnotation requests an immediate registration-token
	// re-exchange (for example after swapping the token Secret). Cleared after
	// a successful registration. Distinct from spec.rotateCredentials, which
	// rotates the long-lived bootstrap credential.
	ClusterReRegisterAnnotation = "ops.vworkspace.io/rotate-credentials"
)

// Cluster lifecycle phases reported in status.phase. They are a coarse,
// human-facing summary derived from conditions and the deletion timestamp.
const (
	ClusterPhasePending      = "Pending"
	ClusterPhaseRegistering  = "Registering"
	ClusterPhaseConnected    = "Connected"
	ClusterPhaseDisconnected = "Disconnected"
	ClusterPhaseError        = "Error"
	ClusterPhaseTerminating  = "Terminating"
)

// ClusterSpec defines operator cluster identity configuration.
type ClusterSpec struct {
	// ClusterID is the server-issued stable identity. Optional on first registration
	// (the server returns it); required for re-registration of a known cluster.
	// +optional
	ClusterID string `json:"clusterId,omitempty"`
	// ControlPlaneEndpoint is the HTTPS base URL of vWorkspace Server (Pull-mode).
	// Preferred over the deprecated controlPlaneBaseUrl alias.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +optional
	ControlPlaneEndpoint string `json:"controlPlaneEndpoint,omitempty"`
	// ControlPlaneBaseURL is the HTTPS base URL for Pull-mode connectivity to vWorkspace Server.
	// Deprecated: use controlPlaneEndpoint; retained as an alias for one minor release.
	// +optional
	ControlPlaneBaseURL string `json:"controlPlaneBaseUrl,omitempty"`
	// RegistrationTokenSecretRef points at a Secret (in the operator namespace) holding the
	// one-time registration token. Preferred over the deprecated inline registrationToken.
	// +optional
	RegistrationTokenSecretRef *SecretKeyRef `json:"registrationTokenSecretRef,omitempty"`
	// RegistrationToken is a one-time token exchanged for a long-lived credential.
	// Cleared from the spec after successful registration.
	// Deprecated: store the token in a Secret and use registrationTokenSecretRef instead.
	// +optional
	RegistrationToken string `json:"registrationToken,omitempty"`
	// Capabilities is a forward-looking placeholder for declaring which managed add-ons
	// (flux, velero, …) the operator should reconcile. RESERVED and not reconciled yet.
	// +optional
	Capabilities *ClusterCapabilities `json:"capabilities,omitempty"`
	// RotateCredentials requests an immediate bootstrap credential rotation via the control plane.
	// Cleared from the spec after a successful rotation.
	// +optional
	RotateCredentials bool `json:"rotateCredentials,omitempty"`
}

// SecretKeyRef identifies one key in a Secret. The Secret is resolved within the
// operator namespace only; cross-namespace references are not supported.
type SecretKeyRef struct {
	// Name is the Secret name in the operator namespace.
	Name string `json:"name"`
	// Key is the Secret data key holding the value. Defaults to "registrationToken".
	// +optional
	Key string `json:"key,omitempty"`
}

// SecretReference points at a Secret materialized by the operator.
type SecretReference struct {
	// Name is the Secret name.
	Name string `json:"name"`
	// Namespace is the Secret namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ClusterCapabilities is a RESERVED placeholder for declarative add-on management.
// It is not reconciled in this release (see cluster-bootstrap-v2 design, Non-goals).
type ClusterCapabilities struct {
	// +optional
	Flux *bool `json:"flux,omitempty"`
	// +optional
	Velero *bool `json:"velero,omitempty"`
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
	// Phase is a coarse, human-facing lifecycle summary derived from conditions.
	// One of: Pending, Registering, Connected, Disconnected, Error, Terminating.
	// +optional
	Phase string `json:"phase,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// LastHeartbeat is updated when a recent round-trip to the control plane succeeded.
	// +optional
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`
	// CredentialStatus reports where bootstrap credentials are stored.
	// +optional
	CredentialStatus *ClusterCredentialStatus `json:"credentialStatus,omitempty"`
	// CredentialsSecretRef is where the bootstrap credential was materialized.
	// +optional
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
	// ObservedToken is a fingerprint (sha256 prefix) of the last registration token consumed.
	// A spec/Secret token whose fingerprint differs triggers re-registration.
	// +optional
	ObservedToken string `json:"observedToken,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=cluster
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
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
