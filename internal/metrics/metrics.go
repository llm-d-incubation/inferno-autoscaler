package metrics

import (
	"context"
	"fmt"
	"strings"
	"sync"

	llmdOptv1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/constants"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Package-level metric collectors
	replicaScalingTotal      *prometheus.CounterVec
	desiredReplicas          *prometheus.GaugeVec
	currentReplicas          *prometheus.GaugeVec
	desiredRatio             *prometheus.GaugeVec
	predictedTTFT            *prometheus.GaugeVec
	predictedITL             *prometheus.GaugeVec
	deploymentConflicts      *prometheus.GaugeVec
	conflictResolutionStatus *prometheus.GaugeVec

	// Thread-safe initialization guards
	initOnce sync.Once
	initErr  error
)

const (
	// maxLabelLength is the maximum length for Prometheus label values
	// Values exceeding this will be truncated to prevent cardinality issues
	maxLabelLength = 128
	// unknownLabel is used as a fallback for empty or invalid label values
	unknownLabel = "unknown"
)

// sanitizeLabel sanitizes a label value to ensure it's valid for Prometheus.
// - Empty strings are replaced with "unknown"
// - Values exceeding maxLabelLength are truncated
// - Whitespace is trimmed
func sanitizeLabel(value string) string {
	// Trim whitespace
	value = strings.TrimSpace(value)

	// Replace empty with unknown
	if value == "" {
		return unknownLabel
	}

	// Truncate if too long
	if len(value) > maxLabelLength {
		return value[:maxLabelLength]
	}

	return value
}

// InitMetrics registers all custom metrics with the provided registry.
// This function uses sync.Once to ensure metrics are only registered once,
// even if called multiple times concurrently.
//
// Note: If initialization fails, the application should not retry without restarting.
// Partial registration is not cleaned up automatically.
func InitMetrics(registry prometheus.Registerer) error {
	initOnce.Do(func() {
		replicaScalingTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: constants.WVAReplicaScalingTotal,
				Help: "Total number of replica scaling operations",
			},
			[]string{constants.LabelTargetName, constants.LabelTargetKind, constants.LabelNamespace, constants.LabelDirection, constants.LabelReason, constants.LabelAcceleratorType},
		)
		desiredReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: constants.WVADesiredReplicas,
				Help: "Desired number of replicas for each scale target",
			},
			[]string{constants.LabelTargetName, constants.LabelTargetKind, constants.LabelNamespace, constants.LabelAcceleratorType},
		)
		currentReplicas = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: constants.WVACurrentReplicas,
				Help: "Current number of replicas for each scale target",
			},
			[]string{constants.LabelTargetName, constants.LabelTargetKind, constants.LabelNamespace, constants.LabelAcceleratorType},
		)
		desiredRatio = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: constants.WVADesiredRatio,
				Help: "Ratio of the desired number of replicas and the current number of replicas for each scale target",
			},
			[]string{constants.LabelTargetName, constants.LabelTargetKind, constants.LabelNamespace, constants.LabelAcceleratorType},
		)
		predictedTTFT = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: constants.WVAPredictedTTFT,
				Help: "Predicted Time To First Token (TTFT) in seconds from ModelAnalyzer for each model and scale target",
			},
			[]string{constants.LabelModelName, constants.LabelTargetName, constants.LabelNamespace, constants.LabelAcceleratorType},
		)
		predictedITL = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: constants.WVAPredictedITL,
				Help: "Predicted Inter-Token Latency (ITL) in seconds from ModelAnalyzer for each model and scale target",
			},
			[]string{constants.LabelModelName, constants.LabelTargetName, constants.LabelNamespace, constants.LabelAcceleratorType},
		)
		deploymentConflicts = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "wva_deployment_target_conflicts_total",
				Help: "Number of VAs in conflict for each deployment (value > 1 indicates conflict)",
			},
			[]string{"deployment", "namespace"},
		)
		conflictResolutionStatus = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "wva_conflict_resolution_status",
				Help: "Conflict resolution status: 1=winner (active), 0=suppressed",
			},
			[]string{"variant_name", "namespace", "deployment", "resolution"},
		)

		// Register metrics with the registry
		if err := registry.Register(replicaScalingTotal); err != nil {
			initErr = fmt.Errorf("failed to register replicaScalingTotal metric: %w", err)
			return
		}
		if err := registry.Register(desiredReplicas); err != nil {
			initErr = fmt.Errorf("failed to register desiredReplicas metric: %w", err)
			return
		}
		if err := registry.Register(currentReplicas); err != nil {
			initErr = fmt.Errorf("failed to register currentReplicas metric: %w", err)
			return
		}
		if err := registry.Register(desiredRatio); err != nil {
			initErr = fmt.Errorf("failed to register desiredRatio metric: %w", err)
			return
		}
		if err := registry.Register(predictedTTFT); err != nil {
			initErr = fmt.Errorf("failed to register predictedTTFT metric: %w", err)
			return
		}
		if err := registry.Register(predictedITL); err != nil {
			initErr = fmt.Errorf("failed to register predictedITL metric: %w", err)
			return
		}
		if err := registry.Register(deploymentConflicts); err != nil {
			initErr = fmt.Errorf("failed to register deploymentConflicts metric: %w", err)
			return
		}
		if err := registry.Register(conflictResolutionStatus); err != nil {
			initErr = fmt.Errorf("failed to register conflictResolutionStatus metric: %w", err)
			return
		}
	})

	return initErr
}

// InitMetricsAndEmitter registers metrics with Prometheus and creates a metrics emitter
// This is a convenience function that handles both registration and emitter creation
func InitMetricsAndEmitter(registry prometheus.Registerer) (*MetricsEmitter, error) {
	if err := InitMetrics(registry); err != nil {
		return nil, err
	}
	return NewMetricsEmitter(), nil
}

// MetricsEmitter handles emission of custom metrics
type MetricsEmitter struct{}

// NewMetricsEmitter creates a new metrics emitter
func NewMetricsEmitter() *MetricsEmitter {
	return &MetricsEmitter{}
}

// EmitReplicaScalingMetrics emits metrics related to replica scaling.
// The ctx parameter is currently unused but reserved for future use (e.g., tracing, cancellation).
func (m *MetricsEmitter) EmitReplicaScalingMetrics(ctx context.Context, va *llmdOptv1alpha1.VariantAutoscaling, direction, reason, acceleratorType string) error {
	// ctx is reserved for future use (tracing, cancellation, etc.)
	_ = ctx

	labels := prometheus.Labels{
		constants.LabelTargetName:      sanitizeLabel(va.Spec.ScaleTargetRef.Name), // Deployment name
		constants.LabelTargetKind:      sanitizeLabel(va.Spec.ScaleTargetRef.Kind), // "Deployment"
		constants.LabelNamespace:       sanitizeLabel(va.Namespace),
		constants.LabelDirection:       sanitizeLabel(direction),
		constants.LabelReason:          sanitizeLabel(reason),
		constants.LabelAcceleratorType: sanitizeLabel(acceleratorType),
	}

	// These operations are local and should never fail, but we handle errors for debugging
	if replicaScalingTotal == nil {
		return fmt.Errorf("replicaScalingTotal metric not initialized")
	}

	replicaScalingTotal.With(labels).Inc()
	return nil
}

// EmitReplicaMetrics emits current and desired replica metrics.
// The ctx parameter is currently unused but reserved for future use (e.g., tracing, cancellation).
func (m *MetricsEmitter) EmitReplicaMetrics(ctx context.Context, va *llmdOptv1alpha1.VariantAutoscaling, current, desired int32, acceleratorType string) error {
	// ctx is reserved for future use (tracing, cancellation, etc.)
	_ = ctx

	baseLabels := prometheus.Labels{
		constants.LabelTargetName:      sanitizeLabel(va.Spec.ScaleTargetRef.Name), // Deployment name
		constants.LabelTargetKind:      sanitizeLabel(va.Spec.ScaleTargetRef.Kind), // "Deployment"
		constants.LabelNamespace:       sanitizeLabel(va.Namespace),
		constants.LabelAcceleratorType: sanitizeLabel(acceleratorType),
	}

	// These operations are local and should never fail, but we handle errors for debugging
	if currentReplicas == nil || desiredReplicas == nil || desiredRatio == nil {
		return fmt.Errorf("replica metrics not initialized")
	}

	currentReplicas.With(baseLabels).Set(float64(current))
	desiredReplicas.With(baseLabels).Set(float64(desired))

	// Avoid division by 0 if current replicas is zero: set the ratio to the desired replicas
	// Going 0 -> N is treated by using `desired_ratio = N`
	if current == 0 {
		desiredRatio.With(baseLabels).Set(float64(desired))
		return nil
	}
	desiredRatio.With(baseLabels).Set(float64(desired) / float64(current))
	return nil
}

// EmitPredictionMetrics emits predicted TTFT and ITL metrics from ModelAnalyzer.
// The ctx parameter is currently unused but reserved for future use (e.g., tracing, cancellation).
func (m *MetricsEmitter) EmitPredictionMetrics(ctx context.Context, va *llmdOptv1alpha1.VariantAutoscaling, modelName string, predictedTTFTValue, predictedITLValue float64, acceleratorType string) error {
	// ctx is reserved for future use (tracing, cancellation, etc.)
	_ = ctx

	labels := prometheus.Labels{
		constants.LabelModelName:       sanitizeLabel(modelName),
		constants.LabelTargetName:      sanitizeLabel(va.Spec.ScaleTargetRef.Name), // Deployment name
		constants.LabelNamespace:       sanitizeLabel(va.Namespace),
		constants.LabelAcceleratorType: sanitizeLabel(acceleratorType),
	}

	// These operations are local and should never fail, but we handle errors for debugging
	if predictedTTFT == nil || predictedITL == nil {
		return fmt.Errorf("prediction metrics not initialized")
	}

	predictedTTFT.With(labels).Set(predictedTTFTValue)
	predictedITL.With(labels).Set(predictedITLValue)
	return nil
}

// EmitConflictMetrics emits metrics for deployment target conflicts.
// deploymentKey format: "namespace/deploymentName"
// totalVAs is the total number of VAs targeting this deployment (should be > 1 for conflicts)
func EmitConflictMetrics(deploymentKey string, totalVAs int) error {
	parts := strings.Split(deploymentKey, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid deployment key format: %s", deploymentKey)
	}
	namespace, deployment := parts[0], parts[1]

	if deploymentConflicts == nil {
		return fmt.Errorf("deploymentConflicts metric not initialized")
	}

	deploymentConflicts.WithLabelValues(deployment, namespace).Set(float64(totalVAs))
	return nil
}

// EmitConflictResolutionMetrics emits metrics for conflict resolution status.
// resolution should be "winner" or "suppressed"
func EmitConflictResolutionMetrics(variantName, namespace, deployment, resolution string) error {
	if conflictResolutionStatus == nil {
		return fmt.Errorf("conflictResolutionStatus metric not initialized")
	}

	value := 0.0
	if resolution == "winner" {
		value = 1.0
	}

	conflictResolutionStatus.WithLabelValues(
		sanitizeLabel(variantName),
		sanitizeLabel(namespace),
		sanitizeLabel(deployment),
		sanitizeLabel(resolution),
	).Set(value)
	return nil
}

// ClearConflictMetrics clears conflict metrics for a deployment (called when conflict is resolved)
func ClearConflictMetrics(deploymentKey string) error {
	parts := strings.Split(deploymentKey, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid deployment key format: %s", deploymentKey)
	}
	namespace, deployment := parts[0], parts[1]

	if deploymentConflicts == nil {
		return fmt.Errorf("deploymentConflicts metric not initialized")
	}

	// Set to 1 (no conflict) or delete the metric
	deploymentConflicts.WithLabelValues(deployment, namespace).Set(1)
	return nil
}
