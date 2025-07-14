package controller

import (
	"context"
	"math"
	"time"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	interfaces "github.com/llm-d-incubation/inferno-autoscaler/internal/interfaces"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DummyVariantAutoscalingsEngine struct{}

func NewDummyVariantAutoscalingsEngine() *DummyVariantAutoscalingsEngine {
	return &DummyVariantAutoscalingsEngine{}
}

// Optimize implements dummy logic to produce one OptimizedAlloc in status.
func (e *DummyVariantAutoscalingsEngine) Optimize(
	ctx context.Context,
	va llmdOptv1alpha1.VariantAutoscaling,
	analysis interfaces.ModelAnalyzeResponse,
	metrics interfaces.MetricsSnapshot,
) (llmdOptv1alpha1.OptimizedAlloc, error) {

	var totalPrefillQPS, totalDecodeQPS float64

	totalPrefillQPS = analysis.RequiredPrefillQPS
	totalDecodeQPS = analysis.RequiredDecodeQPS

	// Dummy per-replica capacities
	perReplicaPrefill := 100.0
	perReplicaDecode := 300.0

	// Determine required replicas
	replicasPrefill := math.Ceil(totalPrefillQPS / perReplicaPrefill)
	replicasDecode := math.Ceil(totalDecodeQPS / perReplicaDecode)
	replicaTarget := int(math.Max(replicasPrefill, replicasDecode))

	alloc := llmdOptv1alpha1.OptimizedAlloc{
		LastRunTime: metav1.NewTime(time.Now()),
		Accelerator: "A100", // or read from VariantAutoscalings spec / label if available
		NumReplicas: replicaTarget,
	}

	return alloc, nil
}
