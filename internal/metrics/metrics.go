package metrics

import (
	"context"
	"fmt"

	llmdOptv1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/constants"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	replicaScalingTotal *prometheus.CounterVec
	desiredReplicas     *prometheus.GaugeVec
	currentReplicas     *prometheus.GaugeVec
	desiredRatio        *prometheus.GaugeVec
)

// InitMetrics registers all custom metrics with the provided registry
func InitMetrics(registry prometheus.Registerer) error {
	replicaScalingTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: constants.InfernoReplicaScalingTotal,
			Help: "Total number of replica scaling operations",
		},
		[]string{constants.LabelVariantName, constants.LabelNamespace, constants.LabelDirection, constants.LabelReason},
	)
	desiredReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: constants.InfernoDesiredReplicas,
			Help: "Desired number of replicas for each variant",
		},
		[]string{constants.LabelVariantName, constants.LabelNamespace, constants.LabelAcceleratorType, constants.LabelVariantID},
	)
	currentReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: constants.InfernoCurrentReplicas,
			Help: "Current number of replicas for each variant",
		},
		[]string{constants.LabelVariantName, constants.LabelNamespace, constants.LabelAcceleratorType, constants.LabelVariantID},
	)
	desiredRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: constants.InfernoDesiredRatio,
			Help: "Ratio of the desired number of replicas and the current number of replicas for each variant",
		},
		[]string{constants.LabelVariantName, constants.LabelNamespace, constants.LabelAcceleratorType, constants.LabelVariantID},
	)

	// Register metrics with the registry
	if err := registry.Register(replicaScalingTotal); err != nil {
		return fmt.Errorf("failed to register replicaScalingTotal metric: %w", err)
	}
	if err := registry.Register(desiredReplicas); err != nil {
		return fmt.Errorf("failed to register desiredReplicas metric: %w", err)
	}
	if err := registry.Register(currentReplicas); err != nil {
		return fmt.Errorf("failed to register currentReplicas metric: %w", err)
	}
	if err := registry.Register(desiredRatio); err != nil {
		return fmt.Errorf("failed to register desiredRatio metric: %w", err)
	}

	return nil
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

// EmitReplicaScalingMetrics emits metrics related to replica scaling
func (m *MetricsEmitter) EmitReplicaScalingMetrics(ctx context.Context, va *llmdOptv1alpha1.VariantAutoscaling, direction, reason string) error {
	labels := prometheus.Labels{
		constants.LabelVariantName: va.Name,
		constants.LabelNamespace:   va.Namespace,
		constants.LabelDirection:   direction,
		constants.LabelReason:      reason,
	}

	// These operations are local and should never fail, but we handle errors for debugging
	if replicaScalingTotal == nil {
		return fmt.Errorf("replicaScalingTotal metric not initialized")
	}

	replicaScalingTotal.With(labels).Inc()
	return nil
}

// EmitReplicaMetrics emits current and desired replica metrics
func (m *MetricsEmitter) EmitReplicaMetrics(ctx context.Context, va *llmdOptv1alpha1.VariantAutoscaling, current, desired int32, acceleratorType, variantID string) error {
	baseLabels := prometheus.Labels{
		constants.LabelVariantName:     va.Name,
		constants.LabelNamespace:       va.Namespace,
		constants.LabelAcceleratorType: acceleratorType,
		constants.LabelVariantID:       variantID,
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
