/*
Copyright 2025.

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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=opt
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=".spec.modelID",description="Target model ID"
// +kubebuilder:printcolumn:name="LastRun",type=date,JSONPath=".status.lastRunTime",description="Last optimization run"

// Optimizer is the Schema for the optimizers API
type Optimizer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OptimizerSpec   `json:"spec,omitempty"`
	Status OptimizerStatus `json:"status,omitempty"`
}

// OptimizerSpec defines the desired configuration for the optimizer controller.
type OptimizerSpec struct {
	// ModelID is the unique identifier for the model to optimize
	ModelID string `json:"modelID"`
}

// ReplicaTargetEntry defines how many replicas are needed for a given role and variant.
type ReplicaTargetEntry struct {
	VariantID        string `json:"variantID"`
	Role             string `json:"role"`
	Replicas         int    `json:"replicas"`
	PreviousReplicas int    `json:"previousReplicas,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// OptimizerStatus captures the latest outcome of optimization.
// +kubebuilder:validation:Optional
type OptimizerStatus struct {
	LastRunTime    metav1.Time          `json:"lastRunTime,omitempty"`
	Conditions     []metav1.Condition   `json:"conditions,omitempty"`
	ReplicaTargets []ReplicaTargetEntry `json:"replicaTargets,omitempty"`
}

// +kubebuilder:object:root=true

// OptimizerList contains a list of Optimizer
type OptimizerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Optimizer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Optimizer{}, &OptimizerList{})
}
