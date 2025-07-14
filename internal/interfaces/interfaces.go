package controller

import (
	"context"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
)

// VariantAutoscalingsEngine defines the interface for the optimization engine.
type VariantAutoscalingsEngine interface {
	Optimize(
		ctx context.Context,
		va llmdOptv1alpha1.VariantAutoscaling,
		analysis ModelAnalyzeResponse,
		metrics MetricsSnapshot,
	) (llmdOptv1alpha1.OptimizedAlloc, error)
}

// ModelAnalyzer defines the interface for model analysis.
type ModelAnalyzer interface {
	AnalyzeModel(
		ctx context.Context,
		va llmdOptv1alpha1.VariantAutoscaling,
		metrics MetricsSnapshot,
	) (*ModelAnalyzeResponse, error)
}

type Actuator interface {
	// ApplyReplicaTargets mutates workloads (e.g., Deployments, InferenceServices) to match target replicas.
	// To be deprecated
	ApplyReplicaTargets(
		ctx context.Context,
		VariantAutoscalings *llmdOptv1alpha1.VariantAutoscaling,
	) error

	// EmitMetrics publishes metrics about the target state (e.g., desired replicas, reasons).
	EmitMetrics(
		ctx context.Context,
		VariantAutoscalings *llmdOptv1alpha1.VariantAutoscaling,
	) error
}
