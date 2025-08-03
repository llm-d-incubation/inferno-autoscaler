package optimizer

import (
	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	collector "github.com/llm-d-incubation/inferno-autoscaler/internal/collector"
	interfaces "github.com/llm-d-incubation/inferno-autoscaler/internal/interfaces"
	infernoConfig "github.com/llm-inferno/optimizer-light/pkg/config"
)

// ComprehensiveClusterData holds all data needed for optimization from controller's Reconcile method
type ComprehensiveClusterData struct {
	AcceleratorConfig   map[string]map[string]string
	ServiceClassConfig  map[string]string
	VariantAutoscalings []llmdVariantAutoscalingV1alpha1.VariantAutoscaling
	Inventory          map[string]map[string]collector.AcceleratorModelInfo
	SystemData         *infernoConfig.SystemData
	UpdateList         *llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
	VAMap              map[string]*llmdVariantAutoscalingV1alpha1.VariantAutoscaling
	AllAnalyzerResponses map[string]*interfaces.ModelAnalyzeResponse
}

