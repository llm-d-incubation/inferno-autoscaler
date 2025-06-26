package controller

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OptimizerSpec struct {
	ModelID string `json:"modelID"`
}

type ReplicaTargetEntry struct {
	VariantID        string `json:"variantID"`
	Role             string `json:"role"`
	Replicas         int    `json:"replicas"`
	PreviousReplicas int    `json:"previousReplicas,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

type OptimizerStatus struct {
	LastRunTime    metav1.Time          `json:"lastRunTime"`
	Conditions     []metav1.Condition   `json:"conditions,omitempty"`
	ReplicaTargets []ReplicaTargetEntry `json:"replicaTargets,omitempty"`
}

type ModelAnalyzeResponse struct {
	VariantID              string  `json:"variantID"`
	Role                   string  `json:"role"`
	RequiredServiceRate    float64 `json:"requiredServiceRate"`
	CurrentServiceCapacity float64 `json:"currentServiceCapacity"`
	Confidence             float64 `json:"confidence,omitempty"`
	SLOClass               string  `json:"sloClass,omitempty"`
	Explanation            string  `json:"explanation,omitempty"`
}

type MetricsSnapshot struct {
	ModelID     string
	CollectedAt time.Time
	// PodStats        map[string]PodMetrics
	// DeploymentStats map[string]DeploymentMetrics
	// NodeHardware    map[string]GPUInfo
	// RuntimeMetrics  map[string]LLMRuntimeMetrics
	// InstanceCounts  []VariantInstanceCount
	// Traffic         TrafficDataSnapshot
	// PerformanceData *ModelPerformanceData
}
