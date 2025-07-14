package controller

import (
	"context"
	"fmt"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type DummyActuator struct {
	Client client.Client
}

func NewDummyActuator(k8sClient client.Client) *DummyActuator {
	return &DummyActuator{Client: k8sClient}
}

func (a *DummyActuator) ApplyReplicaTargets(ctx context.Context, VariantAutoscalings *llmdOptv1alpha1.VariantAutoscaling) error {
	logger := logf.FromContext(ctx)
	desired := VariantAutoscalings.Status.DesiredOptimizedAlloc

	logger.Info("ApplyReplicaTargets - Model: %s, Accelerator: %s, TargetReplicas: %d\n",
		VariantAutoscalings.Spec.ModelID,
		desired.Accelerator,
		desired.NumReplicas,
	)

	var deploy appsv1.Deployment
	err := a.Client.Get(ctx, types.NamespacedName{
		Name:      VariantAutoscalings.Name,
		Namespace: VariantAutoscalings.Namespace,
	}, &deploy)
	if err != nil {
		return fmt.Errorf("failed to get Deployment %s/%s: %w", VariantAutoscalings.Namespace, VariantAutoscalings.Name, err)
	}

	// Patch replicas field
	original := deploy.DeepCopy()
	replicas := int32(desired.NumReplicas)
	deploy.Spec.Replicas = &replicas

	patch := client.MergeFrom(original)
	if err := a.Client.Patch(ctx, &deploy, patch); err != nil {
		return fmt.Errorf("failed to patch Deployment %s: %w", deploy.Name, err)
	}

	logger.Info("Patched Deployment %s to %d replicas\n", deploy.Name, replicas)
	return nil
}
