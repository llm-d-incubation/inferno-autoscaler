package controller

import (
	"context"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
)

// VariantAutoscalingsEngine defines the interface for the optimization engine.
type VariantAutoscalingsEngine interface {
	Optimize(
		ctx context.Context,
		va llmdOptv1alpha1.VariantAutoscalingList,
		analysis map[string]*ModelAnalyzeResponse,
	) (map[string]llmdOptv1alpha1.OptimizedAlloc, error)
}

// ModelAnalyzer defines the interface for model analysis.
type ModelAnalyzer interface {
	AnalyzeModel(
		ctx context.Context,
		va llmdOptv1alpha1.VariantAutoscaling,
	) (*ModelAnalyzeResponse, error)
}

type Actuator interface {
	// EmitMetrics publishes optimization signals for external autoscalers (e.g., HPA, KEDA).
	// This includes real-time current state and Inferno's optimization targets.
	EmitMetrics(
		ctx context.Context,
		VariantAutoscalings *llmdOptv1alpha1.VariantAutoscaling,
	) error
}
