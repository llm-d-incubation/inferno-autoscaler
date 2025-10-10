package controller

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	appsv1 "k8s.io/api/apps/v1"
)

type AcceleratorModelInfo struct {
	Count  int
	Memory string
}

// TODO: Resource accounting and capacity tracking for limited mode.
// The WVA currently operates in unlimited mode only, where each variant receives
// optimal allocation independently without cluster capacity constraints.
// Limited mode support requires integration with the llmd stack and additional
// design work to handle degraded mode operations without violating SLOs.
// Future work: Implement CollectInventoryK8S and capacity-aware allocation for limited mode.

// vendors list for GPU vendors - kept for future limited mode support
var vendors = []string{
	"nvidia.com",
	"amd.com",
	"intel.com",
}

// CollectInventoryK8S is a stub for future limited mode support.
// Currently returns empty inventory as WVA operates in unlimited mode.
func CollectInventoryK8S(ctx context.Context, r interface{}) (map[string]map[string]AcceleratorModelInfo, error) {
	// Stub implementation - will be properly implemented for limited mode
	return make(map[string]map[string]AcceleratorModelInfo), nil
}

type MetricKV struct {
	Name   string
	Labels map[string]string
	Value  float64
}

func AddMetricsToOptStatus(ctx context.Context,
	opt *llmdVariantAutoscalingV1alpha1.VariantAutoscaling,
	deployment appsv1.Deployment,
	acceleratorCostVal float64,
	promAPI promv1.API) (llmdVariantAutoscalingV1alpha1.Allocation, error) {

	deployNamespace := deployment.Namespace
	modelName := opt.Spec.ModelID

	// Setup Prometheus client
	// TODO: agree on using standard vllm metrics
	// Query 1: Arrival rate (requests per minute)
	arrivalQuery := fmt.Sprintf(`sum(rate(vllm:request_success_total{model_name="%s",namespace="%s"}[1m])) * 60`, modelName, deployNamespace)
	arrivalVal := 0.0
	if val, warn, err := promAPI.Query(ctx, arrivalQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			arrivalVal = float64(vec[0].Value)
		}
		if warn != nil {
			logger.Log.Warn("Prometheus warnings - ", "warnings: ", warn)
		}
	} else {
		return llmdVariantAutoscalingV1alpha1.Allocation{}, err
	}
	FixValue(&arrivalVal)

	// TODO: add query to get prompt tokens
	avgInputTokens := 0.0

	// Query 2: Average token length
	// TODO: split composite query to individual queries
	avgDecToksQuery := fmt.Sprintf(`sum(rate(vllm:request_generation_tokens_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:request_generation_tokens_count{model_name="%s",namespace="%s"}[1m]))`,
		modelName, deployNamespace, modelName, deployNamespace)
	avgOutputTokens := 0.0
	if val, _, err := promAPI.Query(ctx, avgDecToksQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			avgOutputTokens = float64(vec[0].Value)
		}
	} else {
		return llmdVariantAutoscalingV1alpha1.Allocation{}, err
	}
	FixValue(&avgOutputTokens)

	// TODO: change waiting time to TTFT

	// Query 3: Average waiting time
	ttftQuery := fmt.Sprintf(`sum(rate(vllm:request_queue_time_seconds_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:request_queue_time_seconds_count{model_name="%s",namespace="%s"}[1m]))`,
		modelName, deployNamespace, modelName, deployNamespace)
	ttftAverageTime := 0.0
	if val, _, err := promAPI.Query(ctx, ttftQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			ttftAverageTime = float64(vec[0].Value) * 1000 //msec
		}
	} else {
		logger.Log.Warn("failed to get avg wait time, using 0: ", "model: ", modelName)
	}
	FixValue(&ttftAverageTime)

	// Query 4: Average ITL
	itlQuery := fmt.Sprintf(`sum(rate(vllm:time_per_output_token_seconds_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:time_per_output_token_seconds_count{model_name="%s",namespace="%s"}[1m]))`,
		modelName, deployNamespace, modelName, deployNamespace)
	itlAverage := 0.0
	if val, _, err := promAPI.Query(ctx, itlQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			itlAverage = float64(vec[0].Value) * 1000 //msec
		}
	} else {
		logger.Log.Warn("failed to get avg itl time, using 0: ", "model: ", modelName)
	}
	FixValue(&itlAverage)

	// number of replicas
	numReplicas := int(*deployment.Spec.Replicas)

	// accelerator type
	acc := ""
	var ok bool
	if acc, ok = opt.Labels["inference.optimization/acceleratorName"]; !ok {
		logger.Log.Warn("acceleratorName label not found on deployment - ", "deployment-name: ", deployment.Name)
	}

	// cost
	discoveredCost := float64(*deployment.Spec.Replicas) * acceleratorCostVal

	// max batch size
	// TODO: collect value from server
	maxBatch := 256

	// populate current alloc
	currentAlloc := llmdVariantAutoscalingV1alpha1.Allocation{
		Accelerator: acc,
		NumReplicas: numReplicas,
		MaxBatch:    maxBatch,
		VariantCost: strconv.FormatFloat(float64(discoveredCost), 'f', 2, 32),
		TTFTAverage: strconv.FormatFloat(float64(ttftAverageTime), 'f', 2, 32),
		ITLAverage:  strconv.FormatFloat(float64(itlAverage), 'f', 2, 32),
		Load: llmdVariantAutoscalingV1alpha1.LoadProfile{
			ArrivalRate:     strconv.FormatFloat(float64(arrivalVal), 'f', 2, 32),
			AvgInputTokens:  strconv.FormatFloat(float64(avgInputTokens), 'f', 2, 32),
			AvgOutputTokens: strconv.FormatFloat(float64(avgOutputTokens), 'f', 2, 32),
		},
	}
	return currentAlloc, nil
}

// Helper to handle if a value is NaN or infinite
func FixValue(x *float64) {
	if math.IsNaN(*x) || math.IsInf(*x, 0) {
		*x = 0
	}
}
