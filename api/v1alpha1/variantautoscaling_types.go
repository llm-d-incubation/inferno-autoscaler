// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=opt
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=".spec.modelID"
// +kubebuilder:printcolumn:name="Current",type=string,JSONPath=".status.currentAlloc.accelerator"
// +kubebuilder:printcolumn:name="Desired",type=string,JSONPath=".status.desiredOptimizedAlloc.accelerator"
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".status.currentAlloc.numReplicas"
// +kubebuilder:printcolumn:name="Actuated",type=string,JSONPath=".status.actuation.applied"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type VariantAutoscalingSpec struct {
	// +kubebuilder:validation:MinLength=1
	ModelID string `json:"modelID"`

	SLOClassRef  ConfigMapKeyRef `json:"sloClassRef"`
	ModelProfile ModelProfile    `json:"modelProfile"`
}

type ConfigMapKeyRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

type ModelProfile struct {
	// +kubebuilder:validation:MinItems=1
	Accelerators []AcceleratorProfile `json:"accelerators"`
}

type AcceleratorProfile struct {
	// +kubebuilder:validation:MinLength=1
	Acc string `json:"acc"`

	// +kubebuilder:validation:Minimum=1
	AccCount int `json:"accCount"`

	// +kubebuilder:validation:Pattern=`^\d+(\.\d+)?$`
	Alpha string `json:"alpha"`

	// +kubebuilder:validation:Pattern=`^\d+(\.\d+)?$`
	Beta string `json:"beta"`

	// +kubebuilder:validation:Minimum=1
	MaxBatchSize int `json:"maxBatchSize"`

	// +kubebuilder:validation:Minimum=1
	AtTokens int `json:"atTokens"`
}

type VariantAutoscalingStatus struct {
	CurrentAlloc          Allocation      `json:"currentAlloc,omitempty"`
	DesiredOptimizedAlloc OptimizedAlloc  `json:"desiredOptimizedAlloc,omitempty"`
	Actuation             ActuationStatus `json:"actuation,omitempty"`
}

type Allocation struct {
	// +kubebuilder:validation:MinLength=1
	Accelerator string `json:"accelerator"`

	// +kubebuilder:validation:Minimum=0
	NumReplicas int `json:"numReplicas"`

	// +kubebuilder:validation:Minimum=0
	MaxBatch int `json:"maxBatch"`

	// +kubebuilder:validation:Pattern=`^\d+(\.\d+)?$`
	VariantCost string `json:"variantCost"`

	// +kubebuilder:validation:Pattern=`^\d+(\.\d+)?$`
	ITLAverage string `json:"itlAverage"`

	// +kubebuilder:validation:Pattern=`^\d+(\.\d+)?$`
	WaitAverage string `json:"waitAverage"`

	Load LoadProfile `json:"load"`
}

type LoadProfile struct {
	ArrivalRate string `json:"arrivalRate"`

	AvgLength string `json:"avgLength"`
}

type OptimizedAlloc struct {
	LastRunTime metav1.Time `json:"lastRunTime,omitempty"`

	// +kubebuilder:validation:MinLength=2
	Accelerator string `json:"accelerator"`

	// +kubebuilder:validation:Minimum=0
	NumReplicas int `json:"numReplicas"`
}

type ActuationStatus struct {
	Applied         bool        `json:"applied"`
	LastAttemptTime metav1.Time `json:"lastAttemptTime,omitempty"`
	LastSuccessTime metav1.Time `json:"lastSuccessTime,omitempty"`
}

// +kubebuilder:object:root=true

type VariantAutoscaling struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VariantAutoscalingSpec   `json:"spec,omitempty"`
	Status VariantAutoscalingStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type VariantAutoscalingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VariantAutoscaling `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VariantAutoscaling{}, &VariantAutoscalingList{})
}
