package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VariantAutoscalingSpec defines the desired state for autoscaling a model variant.
type VariantAutoscalingSpec struct {
	// ModelID specifies the unique identifier of the model to be autoscaled.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Required
	ModelID string `json:"modelID"`

	// SLOClassRef references the ConfigMap key containing Service Level Objective (SLO) configuration.
	// +kubebuilder:validation:Required
	SLOClassRef ConfigMapKeyRef `json:"sloClassRef"`

	// ModelProfile provides resource and performance characteristics for the model variant.
	// +kubebuilder:validation:Required
	ModelProfile ModelProfile `json:"modelProfile"`
}

// ConfigMapKeyRef references a specific key within a ConfigMap.
type ConfigMapKeyRef struct {
	// Name is the name of the ConfigMap.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key is the key within the ConfigMap.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// ModelProfile provides resource and performance characteristics for the model variant.
type ModelProfile struct {
	// Accelerators is a list of accelerator profiles for the model variant.
	// +kubebuilder:validation:MinItems=1
	Accelerators []AcceleratorProfile `json:"accelerators"`
}

type PerfParms struct {
	// DecodeParms contains parameters for the decode phase (ITL calculation)
	// Expected keys: "alpha", "beta" for equation: itl = alpha + beta * maxBatchSize
	// +kubebuilder:validation:MinProperties=1
	DecodeParms map[string]string `json:"decodeParms"`
	// PrefillParms contains parameters for the prefill phase (TTFT calculation)
	// Expected keys: "gamma", "delta" for equation: ttft = gamma + delta * tokens * maxBatchSize
	// +kubebuilder:validation:MinProperties=1
	PrefillParms map[string]string `json:"prefillParms"`
}

// AcceleratorProfile defines the configuration for an accelerator used in autoscaling.
// It specifies the type and count of accelerator, as well as parameters for scaling behavior.
type AcceleratorProfile struct {
	// Acc specifies the type or name of the accelerator (e.g., GPU type).
	// +kubebuilder:validation:MinLength=1
	Acc string `json:"acc"`

	// AccCount specifies the number of accelerator units to be used.
	// +kubebuilder:validation:Minimum=1
	AccCount int `json:"accCount"`

	// PerParms specifies the prefill and decode parameters for ttft and itl models
	PerfParms PerfParms `json:"perfParms"`

	// MaxBatchSize is the maximum batch size supported by the accelerator.
	// +kubebuilder:validation:Minimum=1
	MaxBatchSize int `json:"maxBatchSize"`
}

// VariantAutoscalingStatus represents the current status of autoscaling for a variant,
// including the current allocation, desired optimized allocation, and actuation status.
type VariantAutoscalingStatus struct {
	// CurrentAlloc specifies the current resource allocation for the variant.
	CurrentAlloc Allocation `json:"currentAlloc,omitempty"`

	// DesiredOptimizedAlloc indicates the target optimized allocation based on autoscaling logic.
	DesiredOptimizedAlloc OptimizedAlloc `json:"desiredOptimizedAlloc,omitempty"`

	// Actuation provides details about the actuation process and its current status.
	Actuation ActuationStatus `json:"actuation,omitempty"`
}

// Allocation describes the current resource allocation for a model variant.
type Allocation struct {
	// Accelerator is the type of accelerator currently allocated.
	// +kubebuilder:validation:MinLength=1
	Accelerator string `json:"accelerator"`

	// NumReplicas is the number of replicas currently allocated.
	// +kubebuilder:validation:Minimum=0
	NumReplicas int `json:"numReplicas"`

	// MaxBatch is the maximum batch size currently allocated.
	// +kubebuilder:validation:Minimum=0
	MaxBatch int `json:"maxBatch"`

	// VariantCost is the cost associated with the current variant allocation.
	// +kubebuilder:validation:Pattern=`^\d+(\.\d+)?$`
	VariantCost string `json:"variantCost"`

	// ITLAverage is the average inter token latency for the current allocation.
	// +kubebuilder:validation:Pattern=`^\d+(\.\d+)?$`
	ITLAverage string `json:"itlAverage"`

	// TTFTAverage is the average time to first token for the current allocation
	// +kubebuilder:validation:Pattern=`^\d+(\.\d+)?$`
	TTFTAverage string `json:"ttftAverage"`

	// Load describes the workload characteristics for the current allocation.
	Load LoadProfile `json:"load"`
}

// LoadProfile represents the configuration for workload characteristics,
// including the rate of incoming requests (ArrivalRate) and the average
// length of each request (AvgLength). Both fields are specified as strings
// to allow flexible input formats.
type LoadProfile struct {
	// ArrivalRate is the rate of incoming requests in inference server.
	ArrivalRate string `json:"arrivalRate"`

	// AvgInputTokens is the average number of input(prefill) tokens per request in inference server.
	AvgInputTokens string `json:"avgInputTokens"`

	// AvgOutputTokens is the average number of output(decode) tokens per request in inference server.
	AvgOutputTokens string `json:"avgOutputTokens"`
}

// OptimizedAlloc describes the target optimized allocation for a model variant.
type OptimizedAlloc struct {
	// LastRunTime is the timestamp of the last optimization run.
	LastRunTime metav1.Time `json:"lastRunTime,omitempty"`

	// Accelerator is the type of accelerator for the optimized allocation.
	// +kubebuilder:validation:MinLength=2
	Accelerator string `json:"accelerator"`

	// NumReplicas is the number of replicas for the optimized allocation.
	// +kubebuilder:validation:Minimum=0
	NumReplicas int `json:"numReplicas"`
}

// ActuationStatus provides details about the actuation process and its current status.
type ActuationStatus struct {
	// Applied indicates whether the actuation was successfully applied.
	Applied bool `json:"applied"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=va
// +kubebuilder:printcolumn:name="Model",type=string,JSONPath=".spec.modelID"
// +kubebuilder:printcolumn:name="Accelerator",type=string,JSONPath=".status.currentAlloc.accelerator"
// +kubebuilder:printcolumn:name="CurrentReplicas",type=integer,JSONPath=".status.currentAlloc.numReplicas"
// +kubebuilder:printcolumn:name="Optimized",type=string,JSONPath=".status.desiredOptimizedAlloc.numReplicas"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// VariantAutoscaling is the Schema for the variantautoscalings API.
// It represents the autoscaling configuration and status for a model variant.
type VariantAutoscaling struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state for autoscaling the model variant.
	Spec VariantAutoscalingSpec `json:"spec,omitempty"`

	// Status represents the current status of autoscaling for the model variant.
	Status VariantAutoscalingStatus `json:"status,omitempty"`
}

// VariantAutoscalingList contains a list of VariantAutoscaling resources.
// +kubebuilder:object:root=true
type VariantAutoscalingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of VariantAutoscaling resources.
	Items []VariantAutoscaling `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VariantAutoscaling{}, &VariantAutoscalingList{})
}
