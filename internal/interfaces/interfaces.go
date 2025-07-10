package controller

import (
	"context"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
)

// OptimizerEngine defines the interface for the optimization engine.
type OptimizerEngine interface {
	Optimize(
		ctx context.Context,
		va llmdOptv1alpha1.Optimizer,
		analysis ModelAnalyzeResponse,
		metrics MetricsSnapshot,
	) (llmdOptv1alpha1.OptimizedAlloc, error)
}

// ModelAnalyzer defines the interface for model analysis.
type ModelAnalyzer interface {
	AnalyzeModel(
		ctx context.Context,
		va llmdOptv1alpha1.Optimizer,
		metrics MetricsSnapshot,
	) (*ModelAnalyzeResponse, error)
}

type Actuator interface {
	// ApplyReplicaTargets mutates workloads (e.g., Deployments, InferenceServices) to match target replicas.
	// To be deprecated
	ApplyReplicaTargets(
		ctx context.Context,
		optimizer *llmdOptv1alpha1.Optimizer,
	) error

	// EmitMetrics publishes metrics about the target state (e.g., desired replicas, reasons).
	EmitMetrics(
		ctx context.Context,
		optimizer *llmdOptv1alpha1.Optimizer,
	) error
}
