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
	va llmdOptv1alpha1.VariantAutoscalingList,
	analysis map[string]interfaces.ModelAnalyzeResponse,
	metrics map[string]interfaces.MetricsSnapshot,
) (map[string]llmdOptv1alpha1.OptimizedAlloc, error) {

	result := make(map[string]llmdOptv1alpha1.OptimizedAlloc)

	for _, va := range va.Items {
		name := va.Name

		analysis, ok1 := analysis[name]
		_, ok2 := metrics[name]
		if !ok1 || !ok2 {
			// Skip if either analysis or metrics are missing
			continue
		}

		// Dummy per-replica capacities
		perReplicaPrefill := 100.0
		perReplicaDecode := 300.0

		// Compute required replicas
		replicasPrefill := math.Ceil(analysis.RequiredPrefillQPS / perReplicaPrefill)
		replicasDecode := math.Ceil(analysis.RequiredDecodeQPS / perReplicaDecode)
		replicaTarget := int(math.Max(replicasPrefill, replicasDecode))

		alloc := llmdOptv1alpha1.OptimizedAlloc{
			LastRunTime: metav1.NewTime(time.Now()),
			Accelerator: "A100", // hardcoded dummy value
			NumReplicas: replicaTarget + 1,
		}

		result[name] = alloc
	}

	return result, nil
}
