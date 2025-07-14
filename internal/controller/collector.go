/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	"github.com/prometheus/common/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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

const DEBUG = 4

// CollectInventory lists all Nodes and builds a map[nodeName][model]→info.
// It checks labels <vendor>/gpu.product, <vendor>/gpu.memory
// and capacity <vendor>/gpu.
func (r *VariantAutoscalingReconciler) CollectInventoryK8S(ctx context.Context) (map[string]map[string]AcceleratorModelInfo, error) {
	logger := logf.FromContext(ctx)

	logger.Info("collecting inventory")

	var nodeList corev1.NodeList
	if err := r.Client.List(ctx, &nodeList); err != nil {
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
				logger.V(DEBUG).Info("found inventory", "nodeName", nodeName, "model", model, "count", count, "mem", mem)
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

func (r *VariantAutoscalingReconciler) addMetricsToOptStatus(ctx context.Context, opt *v1alpha1.VariantAutoscaling, deployment appsv1.Deployment, acceleratorCostVal float64) error {
	logger := logf.FromContext(ctx)
	deployNamespace := deployment.Namespace
	modelName := opt.Labels["inference.optimization/modelName"]
	// Setup Prometheus client
	// Query 1: Arrival rate (requests per minute)
	arrivalQuery := fmt.Sprintf(`sum(rate(vllm:requests_count_total{model_name="%s",namespace="%s"}[1m])) * 60`, modelName, deployNamespace)
	arrivalVal := 0.0
	if val, warn, err := r.PromAPI.Query(ctx, arrivalQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			arrivalVal = float64(vec[0].Value)
		}
		if warn != nil {
			logger.Info("Prometheus warnings", "warnings", warn)
		}
	} else {
		logger.Error(err, "failed to query Prometheus arrival rate")
	}

	// Query 2: Average token length
	tokenQuery := fmt.Sprintf(`delta(vllm:tokens_count_total{model_name="%s",namespace="%s"}[1m])/delta(vllm:requests_count_total{model_name="%s",namespace="%s"}[1m])`, modelName, deployNamespace, modelName, deployNamespace)
	avgLen := 0.0
	if val, _, err := r.PromAPI.Query(ctx, tokenQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			avgLen = float64(vec[0].Value)
		}
	} else {
		logger.Error(err, "failed to query Prometheus average token length")
	}

	if math.IsNaN(avgLen) || math.IsInf(avgLen, 0) {
		avgLen = 0
	}

	waitQuery := fmt.Sprintf(`sum(rate(vllm:request_queue_time_seconds_sum{model_name="%s",namespace="%s"}[1m]))/sum(rate(vllm:request_queue_time_seconds_count{model_name="%s",namespace="%s"}[1m]))`, modelName, deployNamespace, modelName, deployNamespace)
	waitAverageTime := 0.0
	if val, _, err := r.PromAPI.Query(ctx, waitQuery, time.Now()); err == nil && val.Type() == model.ValVector {
		vec := val.(model.Vector)
		if len(vec) > 0 {
			waitAverageTime = float64(vec[0].Value)
		}
	} else {
		logger.Error(err, "failed to query Prometheus average token length")
	}

	opt.Status.CurrentAlloc.NumReplicas = int(*deployment.Spec.Replicas)
	if acc, ok := opt.Labels["inference.optimization/acceleratorName"]; ok {
		opt.Status.CurrentAlloc.Accelerator = acc
	} else {
		logger.Info("acceleratorName label not found on deployment", "deployment", deployment.Name)
	}
	opt.Status.CurrentAlloc.WaitAverage = strconv.FormatFloat(float64(waitAverageTime), 'f', 2, 32)
	opt.Status.CurrentAlloc.ITLAverage = "50"
	// TODO: extract max batch size from vllm config present
	// present in the deployment
	opt.Status.CurrentAlloc.MaxBatch = 256
	opt.Status.CurrentAlloc.Load.ArrivalRate = strconv.FormatFloat(float64(arrivalVal), 'f', 2, 32)
	opt.Status.CurrentAlloc.Load.AvgLength = strconv.FormatFloat(float64(avgLen), 'f', 2, 32)
	// TODO read configmap and adjust this value
	discoveredCost := float64(*deployment.Spec.Replicas) * acceleratorCostVal
	opt.Status.CurrentAlloc.VariantCost = strconv.FormatFloat(float64(discoveredCost), 'f', 2, 32)
	return nil
}
