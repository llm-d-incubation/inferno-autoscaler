// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=opt
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=".spec.modelID"
// +kubebuilder:printcolumn:name="Replicas",type=string,JSONPath=".status.desiredOptimizedAlloc.numReplicas"
// +kubebuilder:printcolumn:name="Actuated",type=string,JSONPath=".status.actuation.applied"
// +kubebuilder:printcolumn:name="LastRun",type=date,JSONPath=".status.lastRunTime"

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OptimizerSpec struct {
	// ModelID is the model name this optimizer is optimizing
	// +kubebuilder:validation:MinLength=1
	ModelID string `json:"modelID"`

	ModelProfile    ModelProfile       `json:"modelProfile"`
	ServiceClassSLO ServiceClassSLO    `json:"serviceClassSLO"`
	DeployedAlloc   DeployedAllocation `json:"deployedAlloc"`
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

	// +kubebuilder:validation:Minimum=0
	Alpha float64 `json:"alpha"`

	// +kubebuilder:validation:Minimum=0
	Beta float64 `json:"beta"`

	// +kubebuilder:validation:Minimum=1
	MaxBatchSize int `json:"maxBatchSize"`

	// +kubebuilder:validation:Minimum=1
	AtTokens int `json:"atTokens"`
}

type ServiceClassSLO struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +kubebuilder:validation:Minimum=0
	Priority int `json:"priority"`

	SLO SLOProfile `json:"slo"`
}

type SLOProfile struct {
	// +kubebuilder:validation:Minimum=0
	SLOITL int `json:"slo-itl"`

	// +kubebuilder:validation:Minimum=0
	SLOTTW int `json:"slo-ttw"`
}

type DeployedAllocation struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	CurrentAlloc Allocation `json:"currentAlloc"`
}

type Allocation struct {
	// +kubebuilder:validation:MinLength=1
	Accelerator string `json:"accelerator"`

	// +kubebuilder:validation:Minimum=0
	NumReplicas int `json:"numReplicas"`

	// +kubebuilder:validation:Minimum=0
	MaxBatch int `json:"maxBatch"`

	// +kubebuilder:validation:Minimum=0
	Cost float64 `json:"cost"`

	// +kubebuilder:validation:Minimum=0
	ITLAverage float64 `json:"itlAverage"`

	// +kubebuilder:validation:Minimum=0
	WaitAverage float64 `json:"waitAverage"`

	Load LoadProfile `json:"load"`
}

type LoadProfile struct {
	// +kubebuilder:validation:Minimum=0
	ArrivalRate float64 `json:"arrivalRate"`

	// +kubebuilder:validation:Minimum=0
	AvgLength int `json:"avgLength"`

	// +kubebuilder:validation:Minimum=0
	ArrivalCOV float64 `json:"arrivalCOV,omitempty"`

	// +kubebuilder:validation:Minimum=0
	ServiceCOV float64 `json:"serviceCOV,omitempty"`
}

type OptimizerStatus struct {
	LastRunTime metav1.Time `json:"lastRunTime,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`

	ReplicaTargets []ReplicaTarget `json:"replicaTargets,omitempty"`

	DesiredOptimizedAlloc Allocation `json:"desiredOptimizedAlloc,omitempty"`

	Actuation ActuationStatus `json:"actuation,omitempty"`
}

type ReplicaTarget struct {
	// +kubebuilder:validation:MinLength=1
	VariantID string `json:"variantID"`

	// +kubebuilder:validation:MinLength=1
	Role string `json:"role"`

	// +kubebuilder:validation:Minimum=0
	Replicas int `json:"replicas"`

	// +kubebuilder:validation:Minimum=0
	PreviousReplicas int `json:"previousReplicas,omitempty"`

	Reason string `json:"reason,omitempty"`
}

type ActuationStatus struct {
	Applied bool `json:"applied"`

	LastAttemptTime metav1.Time `json:"lastAttemptTime,omitempty"`

	LastSuccessTime metav1.Time `json:"lastSuccessTime,omitempty"`

	Message string `json:"message,omitempty"`

	Reason string `json:"reason,omitempty"`
}

// +kubebuilder:object:root=true

type Optimizer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OptimizerSpec   `json:"spec,omitempty"`
	Status OptimizerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type OptimizerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Optimizer `json:"items"`
}
