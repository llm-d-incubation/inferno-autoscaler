package actuator

import (
	"context"
	"fmt"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/llm-d-incubation/inferno-autoscaler/internal/logger"
	"github.com/llm-d-incubation/inferno-autoscaler/internal/metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Actuator struct {
	Client         client.Client
	MetricsEmitter *metrics.MetricsEmitter
}

func NewActuator(k8sClient client.Client) *Actuator {
	return &Actuator{
		Client:         k8sClient,
		MetricsEmitter: metrics.NewMetricsEmitter(),
	}
}

func (a *Actuator) ApplyReplicaTargets(ctx context.Context, VariantAutoscaling *llmdOptv1alpha1.VariantAutoscaling) error {
	desired := VariantAutoscaling.Status.DesiredOptimizedAlloc
	var deploy appsv1.Deployment
	err := a.Client.Get(ctx, types.NamespacedName{
		Name:      VariantAutoscaling.Name,
		Namespace: VariantAutoscaling.Namespace,
	}, &deploy)
	if err != nil {
		return fmt.Errorf("failed to get Deployment %s/%s: %w", VariantAutoscaling.Namespace, VariantAutoscaling.Name, err)
	}

	// Patch replicas field
	original := deploy.DeepCopy()
	replicas := int32(desired.NumReplicas)
	deploy.Spec.Replicas = &replicas

	patch := client.MergeFrom(original)
	if err := a.Client.Patch(ctx, &deploy, patch); err != nil {
		return fmt.Errorf("failed to patch Deployment %s: %w", deploy.Name, err)
	}

	logger.Log.Info("Patched Deployment: ", "name: ", deploy.Name, " num-replicas: ", replicas)

	// Emit metrics for replica scaling
	if replicas > *original.Spec.Replicas {
		if err := a.MetricsEmitter.EmitReplicaScalingMetrics(ctx, VariantAutoscaling, "scale_up", "load_increase"); err != nil {
			logger.Log.Error(err, "Failed to emit scale-up metrics", "name", VariantAutoscaling.Name)
			// Don't fail the deployment patch for metric emission errors
		}
	} else if replicas < *original.Spec.Replicas {
		if err := a.MetricsEmitter.EmitReplicaScalingMetrics(ctx, VariantAutoscaling, "scale_down", "load_decrease"); err != nil {
			logger.Log.Error(err, "Failed to emit scale-down metrics", "name", VariantAutoscaling.Name)
			// Don't fail the deployment patch for metric emission errors
		}
	}

	return nil
}

func (a *Actuator) EmitMetrics(ctx context.Context, VariantAutoscaling *llmdOptv1alpha1.VariantAutoscaling) error {
	// Emit replica metrics
	if VariantAutoscaling.Status.DesiredOptimizedAlloc.NumReplicas > 0 {
		if err := a.MetricsEmitter.EmitReplicaMetrics(
			ctx,
			VariantAutoscaling,
			int32(VariantAutoscaling.Status.CurrentAlloc.NumReplicas),
			int32(VariantAutoscaling.Status.DesiredOptimizedAlloc.NumReplicas),
			VariantAutoscaling.Status.DesiredOptimizedAlloc.Accelerator,
		); err != nil {
			logger.Log.Error(err, "Failed to emit replica metrics", "name", VariantAutoscaling.Name)
			// Don't fail the reconciliation for metric emission errors
			// Metrics are critical for HPA, but emission failures shouldn't break core functionality
		} else {
			logger.Log.Debug("EmitReplicaMetrics completed")
		}
	} else {
		logger.Log.Debug("Skipping EmitReplicaMetrics - NumReplicas is 0")
	}

	logger.Log.Info("Emitted metrics for variant", "name", VariantAutoscaling.Name, "namespace", VariantAutoscaling.Namespace)
	return nil
}
