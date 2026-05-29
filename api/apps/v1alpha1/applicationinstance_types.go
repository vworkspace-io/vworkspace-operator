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

const ApplicationInstanceFinalizer = "apps.vworkspace.io/finalizer"

// ChartSourceType identifies how the chart is fetched.
// +kubebuilder:validation:Enum=oci;helm
type ChartSourceType string

const (
	ChartSourceOCI  ChartSourceType = "oci"
	ChartSourceHelm ChartSourceType = "helm"
)

// ValuesSource identifies where chart values come from.
// +kubebuilder:validation:Enum=inline;secretRef;configMapRef
type ValuesSource string

const (
	ValuesSourceInline       ValuesSource = "inline"
	ValuesSourceSecretRef    ValuesSource = "secretRef"
	ValuesSourceConfigMapRef ValuesSource = "configMapRef"
)

type AppRef struct {
	// +kubebuilder:validation:MinLength=1
	CatalogID string `json:"catalogId"`
}

type ChartSpec struct {
	SourceType ChartSourceType `json:"sourceType"`
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
}

type ReleaseSpec struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
}

type ObjectKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type ValuesSpec struct {
	Source       ValuesSource          `json:"source"`
	Inline       *runtime.RawExtension `json:"inline,omitempty"`
	SecretRef    *ObjectKeyRef         `json:"secretRef,omitempty"`
	ConfigMapRef *ObjectKeyRef         `json:"configMapRef,omitempty"`
}

type ExternalSecretsIntegration struct {
	Enabled bool `json:"enabled,omitempty"`
}

type CertManagerIntegration struct {
	Enabled bool `json:"enabled,omitempty"`
}

type IngressIntegration struct {
	Enabled bool   `json:"enabled,omitempty"`
	Host    string `json:"host,omitempty"`
}

type IntegrationsSpec struct {
	ExternalSecrets *ExternalSecretsIntegration `json:"externalSecrets,omitempty"`
	CertManager     *CertManagerIntegration     `json:"certManager,omitempty"`
	Ingress         *IngressIntegration         `json:"ingress,omitempty"`
}

// ApplicationInstanceSpec defines the desired state of ApplicationInstance.
type ApplicationInstanceSpec struct {
	AppRef       AppRef            `json:"appRef"`
	Chart        ChartSpec         `json:"chart"`
	Release      ReleaseSpec       `json:"release"`
	Values       ValuesSpec        `json:"values"`
	Integrations *IntegrationsSpec `json:"integrations,omitempty"`
}

type HelmReleaseRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type ChartSnapshot struct {
	SourceType ChartSourceType `json:"sourceType"`
	URL        string          `json:"url"`
	Name       string          `json:"name"`
	Version    string          `json:"version"`
}

type EndpointStatus struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Type  string `json:"type,omitempty"`
	Notes string `json:"notes,omitempty"`
}

// ApplicationInstanceStatus defines the observed state of ApplicationInstance.
type ApplicationInstanceStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions        []metav1.Condition `json:"conditions,omitempty"`
	HelmReleaseRef    *HelmReleaseRef    `json:"helmReleaseRef,omitempty"`
	LastAppliedChart  *ChartSnapshot     `json:"lastAppliedChart,omitempty"`
	LastReconcileTime *metav1.Time       `json:"lastReconcileTime,omitempty"`
	Endpoints         []EndpointStatus   `json:"endpoints,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=appinst
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Chart",type=string,JSONPath=`.spec.chart.name`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.chart.version`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ApplicationInstance is the Schema for the applicationinstances API.
type ApplicationInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationInstanceSpec   `json:"spec,omitempty"`
	Status ApplicationInstanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApplicationInstanceList contains a list of ApplicationInstance.
type ApplicationInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApplicationInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ApplicationInstance{}, &ApplicationInstanceList{})
}
