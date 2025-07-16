package controller

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/inferno-autoscaler/internal/logger"
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
				if cap, ok := node.Status.Capacity[corev1.ResourceName(vendor+"/gpu")]; ok {
					count = int(cap.Value())
				}
				if inv[nodeName] == nil {
					inv[nodeName] = make(map[string]AcceleratorModelInfo)
				}
				inv[nodeName][model] = AcceleratorModelInfo{
					Count:  count,
					Memory: mem,
				}
				logger.Log.Debug("found inventory", "nodeName", nodeName, "model", model, "count", count, "mem", mem)
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

func AddMetricsToOptStatus(ctx context.Context, opt *v1alpha1.VariantAutoscaling, deployment appsv1.Deployment, acceleratorCostVal float64, promAPI promv1.API) (llmdVariantAutoscalingV1alpha1.Allocation, error) {
	deployNamespace := deployment.Namespace
	modelName := opt.Labels["inference.optimization/modelName"]

	// hardcoded for dummy optimizer, need to change when real optimizer is integrated
	currentAlloc := llmdVariantAutoscalingV1alpha1.Allocation{
		MaxBatch:   256,
		ITLAverage: "50",
	}
	// Setup Prometheus client
	// Query 1: Arrival rate (requests per minute)
	arrivalQuery := fmt.Sprintf(`sum(rate(vllm:requests_count_total{model_name="%s",namespace="%s"}[1m])) * 60`, modelName, deployNamespace)
	arrivalVal := 0.0
	if val, warn, err := promAPI.Query(ctx, arrivalQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			arrivalVal = float64(vec[0].Value)
		}
		if warn != nil {
			logger.Log.Info("Prometheus warnings", "warnings", warn)
		}
	} else {
		return llmdVariantAutoscalingV1alpha1.Allocation{}, err
	}

	// Query 2: Average token length
	tokenQuery := fmt.Sprintf(`delta(vllm:tokens_count_total{model_name="%s",namespace="%s"}[1m])/delta(vllm:requests_count_total{model_name="%s",namespace="%s"}[1m])`, modelName, deployNamespace, modelName, deployNamespace)
	avgLen := 0.0
	if val, _, err := promAPI.Query(ctx, tokenQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			avgLen = float64(vec[0].Value)
		}
	} else {
		return llmdVariantAutoscalingV1alpha1.Allocation{}, err
	}

	if math.IsNaN(avgLen) || math.IsInf(avgLen, 0) {
		avgLen = 0
	}

	waitQuery := fmt.Sprintf(`sum(rate(vllm:request_queue_time_seconds_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:request_queue_time_seconds_count{model_name="%s",namespace="%s"}[1m]))`, modelName, deployNamespace, modelName, deployNamespace)
	waitAverageTime := 0.0
	if val, _, err := promAPI.Query(ctx, waitQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			waitAverageTime = float64(vec[0].Value)
		}
	} else {
		return llmdVariantAutoscalingV1alpha1.Allocation{}, err
	}

	currentAlloc.NumReplicas = int(*deployment.Spec.Replicas)
	if acc, ok := opt.Labels["inference.optimization/acceleratorName"]; ok {
		currentAlloc.Accelerator = acc
	} else {
		logger.Log.Info("acceleratorName label not found on deployment", "deployment", deployment.Name)
	}
	currentAlloc.WaitAverage = strconv.FormatFloat(float64(waitAverageTime), 'f', 2, 32)
	opt.Status.CurrentAlloc.ITLAverage = "50"
	// TODO: extract max batch size from vllm config present
	// present in the deployment
	currentAlloc.MaxBatch = 256
	currentAlloc.Load.ArrivalRate = strconv.FormatFloat(float64(arrivalVal), 'f', 2, 32)
	currentAlloc.Load.AvgLength = strconv.FormatFloat(float64(avgLen), 'f', 2, 32)
	// TODO read configmap and adjust this value
	discoveredCost := float64(*deployment.Spec.Replicas) * acceleratorCostVal
	currentAlloc.VariantCost = strconv.FormatFloat(float64(discoveredCost), 'f', 2, 32)
	return currentAlloc, nil
}
