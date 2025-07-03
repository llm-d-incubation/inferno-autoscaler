package controller

import (
	"time"
)

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
