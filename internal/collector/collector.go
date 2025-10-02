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
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AcceleratorModelInfo struct {
	Count  int
	Memory string
}

// Collector holds the k8s client and discovers GPU inventory
var vendors = []string{
	"nvidia.com",
	"amd.com",
	"intel.com",
}

// CollectInventory lists all Nodes and builds a map[nodeName][model]→info.
// It checks labels <vendor>/gpu.product, <vendor>/gpu.memory
// and capacity <vendor>/gpu.
func CollectInventoryK8S(ctx context.Context, r client.Client) (map[string]map[string]AcceleratorModelInfo, error) {
	var nodeList corev1.NodeList
	if err := r.List(ctx, &nodeList); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	inv := make(map[string]map[string]AcceleratorModelInfo)
	for _, node := range nodeList.Items {
		nodeName := node.Name
		for _, vendor := range vendors {
			prodKey := vendor + "/gpu.product"
			memKey := vendor + "/gpu.memory"
			if model, ok := node.Labels[prodKey]; ok {
				// found a GPU of this vendor
				mem := node.Labels[memKey]
				count := 0
				if cap, ok := node.Status.Allocatable[corev1.ResourceName(vendor+"/gpu")]; ok {
					count = int(cap.Value())
				}
				if inv[nodeName] == nil {
					inv[nodeName] = make(map[string]AcceleratorModelInfo)
				}
				inv[nodeName][model] = AcceleratorModelInfo{
					Count:  count,
					Memory: mem,
				}
				logger.Log.Debug("Found inventory: ", "nodeName - ", nodeName, " , model - ", model, " , count - ", count, " , mem - ", mem)
			}
		}
	}
	return inv, nil
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

	// Query 2: Average prompt length
	avgPromptToksQuery := fmt.Sprintf(`sum(rate(vllm:request_prompt_tokens_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:request_prompt_tokens_count{model_name="%s",namespace="%s"}[1m]))`,
		modelName, deployNamespace, modelName, deployNamespace)
	avgInputTokens := 0.0
	if val, warn, err := promAPI.Query(ctx, avgPromptToksQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			avgInputTokens = float64(vec[0].Value)
		}
		if warn != nil {
			logger.Log.Warn("Prometheus warnings - ", "warnings: ", warn)
		}
	}
	// In case of error, we work with 0 input tokens
	FixValue(&avgInputTokens)

	// Query 3: Average decode length
	avgDecToksQuery := fmt.Sprintf(`sum(rate(vllm:request_generation_tokens_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:request_generation_tokens_count{model_name="%s",namespace="%s"}[1m]))`,
		modelName, deployNamespace, modelName, deployNamespace)
	avgOutputTokens := 0.0
	if val, warn, err := promAPI.Query(ctx, avgDecToksQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			avgOutputTokens = float64(vec[0].Value)
		}
		if warn != nil {
			logger.Log.Warn("Prometheus warnings - ", "warnings: ", warn)
		}
	} else {
		return llmdVariantAutoscalingV1alpha1.Allocation{}, err
	}
	FixValue(&avgOutputTokens)

	// Query 4: Average TTFT
	ttftQuery := fmt.Sprintf(`sum(rate(vllm:time_to_first_token_seconds_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:time_to_first_token_seconds_count{model_name="%s",namespace="%s"}[1m]))`,
		modelName, deployNamespace, modelName, deployNamespace)
	ttftAverageTime := 0.0
	if val, warn, err := promAPI.Query(ctx, ttftQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			ttftAverageTime = float64(vec[0].Value) * 1000 //msec
		}
		if warn != nil {
			logger.Log.Warn("Prometheus warnings - ", "warnings: ", warn)
		}
	}
	// In case of error, we work with 0 TTFT time
	FixValue(&ttftAverageTime)

	// Query 5: Average ITL
	itlQuery := fmt.Sprintf(`sum(rate(vllm:time_per_output_token_seconds_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:time_per_output_token_seconds_count{model_name="%s",namespace="%s"}[1m]))`,
		modelName, deployNamespace, modelName, deployNamespace)
	itlAverage := 0.0
	if val, warn, err := promAPI.Query(ctx, itlQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			itlAverage = float64(vec[0].Value) * 1000 //msec
		}
		if warn != nil {
			logger.Log.Warn("Prometheus warnings - ", "warnings: ", warn)
		}
	} else {
		return llmdVariantAutoscalingV1alpha1.Allocation{}, err
	}
	FixValue(&itlAverage)

	// number of replicas
	// TODO: Should the numReplicas reflect the actual active servers?
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
