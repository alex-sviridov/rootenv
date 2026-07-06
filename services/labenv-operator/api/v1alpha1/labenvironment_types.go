/*
Copyright 2026.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LabEnvironmentSpec defines the desired state of LabEnvironment
type LabEnvironmentSpec struct {
	// OwnerId is the ID of the user who owns this lab environment.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9](?:[-A-Za-z0-9_.]*[A-Za-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	OwnerId string `json:"ownerId"`
	// OwnerName is the name of the user who owns this lab environment (for display purposes only).
	OwnerName string `json:"ownerName,omitempty"`
	// LabId identifies which lab definition to provision.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9](?:[-A-Za-z0-9_.]*[A-Za-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	LabId string `json:"labId"`
	// TTL (minutes) is the time-to-live for this lab environment, after which it should be automatically deleted.
	// +kubebuilder:default=60
	TTL int32 `json:"ttl,omitempty"`
	// Assets is a list of assets that should be provisioned as part of this lab environment.
	Assets []Asset `json:"assets"`
	// Exercises is a list of gradeable exercises extracted from the lab's task markdown.
	Exercises []Exercise `json:"exercises,omitempty"`
}

// Asset defines a single asset to be provisioned as part of the lab environment, such as a VM or container.
type Asset struct {
	// Name is a unique identifier for this asset within the lab environment.
	Name string `json:"name"`
	// Image specifies the lab image name. labenv-operator resolves it against the lab-images
	// ConfigMap; if no match is found, the name is used as-is (e.g. a public image like "ubuntu").
	Image string `json:"image"`
	// CPU limit for this asset, e.g. "500m" for 0.5 CPU cores.
	// +kubebuilder:default="100m"
	CPU string `json:"cpu,omitempty"`
	// Memory limit for this asset, e.g. "256Mi" for 256 MiB of memory.
	// +kubebuilder:default="128Mi"
	Memory string `json:"memory,omitempty"`
	// Disk is the ephemeral-storage limit for this asset (protects the host from disk exhaustion), e.g. "2Gi".
	// +kubebuilder:default="1Gi"
	Disk string `json:"disk,omitempty"`
	// Protocols is a list of protocols that should be enabled for this asset, e.g. ["ssh", "http"].
	// +kubebuilder:validation:items:Enum=exec;http
	Protocols []string `json:"protocols,omitempty"`
	// Setup is an optional script that should be executed to set up the asset after it is provisioned.
	Setup string `json:"setup,omitempty"`
}

// Exercise is a gradeable item embedded in a lab's task markdown, copied
// verbatim from the labs PocketBase record's `exercises` field via
// attempt-controller.
type Exercise struct {
	// ID identifies this exercise, computed as "<task#>.<exercise#>" at lab-sync time.
	ID string `json:"id"`
	// Description is shown to the user; never used by the grader itself.
	Description string `json:"description"`
	// Type must currently be "term" — the only type relay-grader supports.
	Type string `json:"type"`
	// Asset optionally scopes this exercise's check to one asset's terminal.
	// Empty means the grader does not filter by terminal.
	Asset string `json:"asset,omitempty"`
	// Template is the shell check the grader runs to determine completion.
	Template string `json:"template"`
}

// LabEnvironmentStatus defines the observed state of LabEnvironment.
type LabEnvironmentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the LabEnvironment resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Phase reflects overall labenv status
	// +optional
	Phase string `json:"phase,omitempty"`
	// Namespace is the name of the Kubernetes namespace where the lab environment is provisioned.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// ExpiresAt is the timestamp when the lab environment is scheduled to expire and be automatically deleted.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// TotalAssets shows total count of assets in this labenv
	TotalAssets int `json:"totalAssets,omitempty"`
	// ReadyAssets shows count of ready assets in this labenv
	ReadyAssets int `json:"readyAssets,omitempty"`
	// Ready shows human-readable counter
	Ready string `json:"ready,omitempty"`

	// Assets lists the state of each asset
	// +optional
	Assets []AssetStatus `json:"assets,omitempty"`
}

type AssetStatus struct {
	Name      string   `json:"name"`
	Phase     string   `json:"phase"`
	Reason    string   `json:"reason,omitempty"`
	Ready     bool     `json:"ready"`
	Address   string   `json:"address,omitempty"`
	Protocols []string `json:"protocols,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=labenv
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.ownerName`,priority=1
// +kubebuilder:printcolumn:name="Lab",type=string,JSONPath=`.spec.labId`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="Expires At",type=string,JSONPath=`.status.expiresAt`

// LabEnvironment is the Schema for the labenvironments API
type LabEnvironment struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LabEnvironment
	// +required
	Spec LabEnvironmentSpec `json:"spec"`

	// status defines the observed state of LabEnvironment
	// +optional
	Status LabEnvironmentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LabEnvironmentList contains a list of LabEnvironment
type LabEnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LabEnvironment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LabEnvironment{}, &LabEnvironmentList{})
}
