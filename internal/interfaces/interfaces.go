package controller

import (
	"context"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
)

// OptimizerEngine defines the interface for the optimization engine.
type OptimizerEngine interface {
	Optimize(
		ctx context.Context,
		spec llmdOptv1alpha1.OptimizerSpec,
		analysis []ModelAnalyzeResponse,
		metrics MetricsSnapshot,
	) (OptimizerStatus, error)
}

// ModelAnalyzer defines the interface for model analysis.
type ModelAnalyzer interface {
	AnalyzeModel(
		ctx context.Context,
		spec llmdOptv1alpha1.OptimizerSpec,
		metrics MetricsSnapshot,
	) (*ModelAnalyzeResponse, error)
}

type Actuator interface {
	// ApplyReplicaTargets mutates workloads (e.g., Deployments, InferenceServices) to match target replicas.
	// To be deprecated
	ApplyReplicaTargets(
		ctx context.Context,
		optimizer *llmdOptv1alpha1.Optimizer,
		targets []llmdOptv1alpha1.ReplicaTargetEntry,
	) error

	// EmitMetrics publishes metrics about the target state (e.g., desired replicas, reasons).
	EmitMetrics(
		ctx context.Context,
		optimizer *llmdOptv1alpha1.Optimizer,
		targets []llmdOptv1alpha1.ReplicaTargetEntry,
	) error
}
