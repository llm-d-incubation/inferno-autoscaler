package collector

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/constants"
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

// MetricsValidationResult contains the result of metrics availability check
type MetricsValidationResult struct {
	Available bool
	Reason    string
	Message   string
}

// ValidateMetricsAvailability checks if vLLM metrics are available for the given model and namespace
// Returns a validation result with details about metric availability
func ValidateMetricsAvailability(ctx context.Context, promAPI promv1.API, modelName, namespace string) MetricsValidationResult {
	// Query for basic vLLM metric to validate scraping is working
	// Try with namespace label first (real vLLM), fall back to just model_name (vllme emulator)
	testQuery := fmt.Sprintf(`vllm:request_success_total{model_name="%s",namespace="%s"}`, modelName, namespace)

	val, _, err := promAPI.Query(ctx, testQuery, time.Now())
	if err != nil {
		logger.Log.Error(err, "Error querying Prometheus for metrics validation",
			"model", modelName, "namespace", namespace)
		return MetricsValidationResult{
			Available: false,
			Reason:    llmdVariantAutoscalingV1alpha1.ReasonPrometheusError,
			Message:   fmt.Sprintf("Failed to query Prometheus: %v", err),
		}
	}

	// Check if we got any results
	if val.Type() != model.ValVector {
		return MetricsValidationResult{
			Available: false,
			Reason:    llmdVariantAutoscalingV1alpha1.ReasonMetricsMissing,
			Message:   fmt.Sprintf("No vLLM metrics found for model '%s' in namespace '%s'. Check ServiceMonitor configuration and ensure vLLM pods are exposing /metrics endpoint", modelName, namespace),
		}
	}

	vec := val.(model.Vector)
	// If no results with namespace label, try without it (for vllme emulator compatibility)
	if len(vec) == 0 {
		testQueryFallback := fmt.Sprintf(`vllm:request_success_total{model_name="%s"}`, modelName)
		val, _, err = promAPI.Query(ctx, testQueryFallback, time.Now())
		if err != nil {
			return MetricsValidationResult{
				Available: false,
				Reason:    llmdVariantAutoscalingV1alpha1.ReasonPrometheusError,
				Message:   fmt.Sprintf("Failed to query Prometheus: %v", err),
			}
		}

		if val.Type() == model.ValVector {
			vec = val.(model.Vector)
		}

		// If still no results, metrics are truly missing
		if len(vec) == 0 {
			return MetricsValidationResult{
				Available: false,
				Reason:    llmdVariantAutoscalingV1alpha1.ReasonMetricsMissing,
				Message:   fmt.Sprintf("No vLLM metrics found for model '%s' in namespace '%s'. Check: (1) ServiceMonitor exists in monitoring namespace, (2) ServiceMonitor selector matches vLLM service labels, (3) vLLM pods are running and exposing /metrics endpoint, (4) Prometheus is scraping the monitoring namespace", modelName, namespace),
			}
		}
	}

	// Check if metrics are stale (older than 5 minutes)
	for _, sample := range vec {
		age := time.Since(sample.Timestamp.Time())
		if age > 5*time.Minute {
			return MetricsValidationResult{
				Available: false,
				Reason:    llmdVariantAutoscalingV1alpha1.ReasonMetricsStale,
				Message:   fmt.Sprintf("vLLM metrics for model '%s' are stale (last update: %v ago). ServiceMonitor may not be scraping correctly.", modelName, age),
			}
		}
	}

	return MetricsValidationResult{
		Available: true,
		Reason:    llmdVariantAutoscalingV1alpha1.ReasonMetricsFound,
		Message:   "vLLM metrics are available and up-to-date",
	}
}

// CollectAggregateMetricsWithCache collects aggregate metrics for a model,
// using cache to avoid redundant Prometheus queries.
// If cache is nil, behaves like CollectAggregateMetrics (no caching).
func CollectAggregateMetricsWithCache(ctx context.Context,
	modelName string,
	namespace string,
	promAPI promv1.API,
	cache *ModelMetricsCache) (llmdVariantAutoscalingV1alpha1.LoadProfile, string, string, error) {

	// Check cache first if available
	if cache != nil {
		if cached, found := cache.Get(modelName, namespace); found && cached.Valid {
			logger.Log.Debug("Using cached metrics for model", "model", modelName, "namespace", namespace)
			return cached.Load, cached.TTFTAverage, cached.ITLAverage, nil
		}
	}

	// Cache miss or disabled - query Prometheus
	logger.Log.Debug("Querying Prometheus for model metrics", "model", modelName, "namespace", namespace)
	load, ttftAvg, itlAvg, err := CollectAggregateMetrics(ctx, modelName, namespace, promAPI)

	// Update cache even on error (mark as invalid) to prevent thundering herd
	if cache != nil {
		cache.Set(modelName, namespace, load, ttftAvg, itlAvg, err == nil)
	}

	return load, ttftAvg, itlAvg, err
}

// CollectAggregateMetrics collects aggregate metrics (Load, ITL, TTFT) for a modelID
// across all deployments serving that model. These metrics are shared across all variants.
//
// Note: For production use, prefer CollectAggregateMetricsWithCache to avoid redundant queries.
func CollectAggregateMetrics(ctx context.Context,
	modelName string,
	namespace string,
	promAPI promv1.API) (llmdVariantAutoscalingV1alpha1.LoadProfile, string, string, error) {

	// Query 1: Arrival rate (requests per minute)
	arrivalQuery := fmt.Sprintf(`sum(rate(%s{%s="%s",%s="%s"}[1m])) * 60`,
		constants.VLLMRequestSuccessTotal,
		constants.LabelModelName, modelName,
		constants.LabelNamespace, namespace)
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
		return llmdVariantAutoscalingV1alpha1.LoadProfile{}, "", "", err
	}
	FixValue(&arrivalVal)

	// TODO: add query to get prompt tokens
	avgInputTokens := 0.0

	// Query 2: Average token length
	// TODO: split composite query to individual queries
	avgDecToksQuery := fmt.Sprintf(`sum(rate(%s{%s="%s",%s="%s"}[1m]))/sum(rate(%s{%s="%s",%s="%s"}[1m]))`,
		constants.VLLMRequestGenerationTokensSum,
		constants.LabelModelName, modelName,
		constants.LabelNamespace, namespace,
		constants.VLLMRequestGenerationTokensCount,
		constants.LabelModelName, modelName,
		constants.LabelNamespace, namespace)
	avgOutputTokens := 0.0
	if val, _, err := promAPI.Query(ctx, avgDecToksQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			avgOutputTokens = float64(vec[0].Value)
		}
	} else {
		return llmdVariantAutoscalingV1alpha1.LoadProfile{}, "", "", err
	}
	FixValue(&avgOutputTokens)

	// TODO: change waiting time to TTFT

	// Query 3: Average waiting time
	ttftQuery := fmt.Sprintf(`sum(rate(%s{%s="%s",%s="%s"}[1m]))/sum(rate(%s{%s="%s",%s="%s"}[1m]))`,
		constants.VLLMRequestQueueTimeSecondsSum,
		constants.LabelModelName, modelName,
		constants.LabelNamespace, namespace,
		constants.VLLMRequestQueueTimeSecondsCount,
		constants.LabelModelName, modelName,
		constants.LabelNamespace, namespace)
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
	itlQuery := fmt.Sprintf(`sum(rate(%s{%s="%s",%s="%s"}[1m]))/sum(rate(%s{%s="%s",%s="%s"}[1m]))`,
		constants.VLLMTimePerOutputTokenSecondsSum,
		constants.LabelModelName, modelName,
		constants.LabelNamespace, namespace,
		constants.VLLMTimePerOutputTokenSecondsCount,
		constants.LabelModelName, modelName,
		constants.LabelNamespace, namespace)
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

	// Return aggregate metrics
	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     strconv.FormatFloat(float64(arrivalVal), 'f', 2, 32),
		AvgInputTokens:  strconv.FormatFloat(float64(avgInputTokens), 'f', 2, 32),
		AvgOutputTokens: strconv.FormatFloat(float64(avgOutputTokens), 'f', 2, 32),
	}
	ttftAvg := strconv.FormatFloat(float64(ttftAverageTime), 'f', 2, 32)
	itlAvg := strconv.FormatFloat(float64(itlAverage), 'f', 2, 32)

	return load, ttftAvg, itlAvg, nil
}

// CollectAllocationForDeployment collects allocation information for a single deployment.
// This includes replica count, accelerator type, cost, and max batch size.
// Aggregate metrics (Load, ITL, TTFT) are collected separately via CollectAggregateMetrics.
func CollectAllocationForDeployment(
	variantID string,
	accelerator string,
	deployment appsv1.Deployment,
	acceleratorCostVal float64,
) (llmdVariantAutoscalingV1alpha1.Allocation, error) {

	// number of replicas
	numReplicas := 0 // Default to 0 if not specified
	if deployment.Spec.Replicas != nil {
		numReplicas = int(*deployment.Spec.Replicas)
	}

	// cost
	discoveredCost := float64(numReplicas) * acceleratorCostVal

	// max batch size
	// TODO: collect value from server
	maxBatch := 256

	// populate allocation (without aggregate metrics)
	allocation := llmdVariantAutoscalingV1alpha1.Allocation{
		VariantID:   variantID,
		Accelerator: accelerator,
		NumReplicas: numReplicas,
		MaxBatch:    maxBatch,
		VariantCost: strconv.FormatFloat(float64(discoveredCost), 'f', 2, 32),
	}
	return allocation, nil
}

// Helper to handle if a value is NaN or infinite
func FixValue(x *float64) {
	if math.IsNaN(*x) || math.IsInf(*x, 0) {
		*x = 0
	}
}
