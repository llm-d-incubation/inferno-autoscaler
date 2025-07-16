package controller

import (
	"context"
	"fmt"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/llm-d-incubation/inferno-autoscaler/internal/logger"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DummyActuator struct {
	Client client.Client
}

func NewDummyActuator(k8sClient client.Client) *DummyActuator {
	return &DummyActuator{Client: k8sClient}
}

func (a *DummyActuator) ApplyReplicaTargets(ctx context.Context, VariantAutoscaling *llmdOptv1alpha1.VariantAutoscaling) error {
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

	logger.Log.Info("Patched Deployment", "name", deploy.Name, "num replicas", replicas)
	return nil
}
