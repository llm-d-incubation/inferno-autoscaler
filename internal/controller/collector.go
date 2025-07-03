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
	"io"
	"net/http"
	"strings"

	"github.com/prometheus/common/expfmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type AcceleratorModelInfo struct {
	Count  int
	Memory string
}

// Collector holds the k8s client and discovers GPU inventory
type Collector struct {
	Client client.Client
}

// NewCollector returns an initialized Collector
func NewCollector(c client.Client) *Collector {
	return &Collector{Client: c}
}

var vendors = []string{
	"nvidia.com",
	"amd.com",
	"intel.com",
}

// CollectInventory lists all Nodes and builds a map[nodeName][model]→info.
// It checks labels <vendor>/gpu.product, <vendor>/gpu.memory
// and capacity <vendor>/gpu.
func (c *Collector) CollectInventoryK8S(ctx context.Context) (map[string]map[string]AcceleratorModelInfo, error) {
	logger := logf.FromContext(ctx)

	logger.Info("collecting inventory")

	var nodeList corev1.NodeList
	if err := c.Client.List(ctx, &nodeList); err != nil {
		logger.Error(err, "unable to list nodes")
		return nil, err
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
				logger.Info("found inventory", "nodeName", nodeName, "model", model, "count", count, "mem", mem)
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

func (r *OptimizerReconciler) fetchVLLMMetricsPerPod(ctx context.Context, modelName string, namespace string) (map[string][]MetricKV, error) {
	labelSelector := labels.SelectorFromSet(map[string]string{
		"model": modelName,
	})

	var podList corev1.PodList
	if err := r.Client.List(ctx, &podList, &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
	}); err != nil {
		return nil, fmt.Errorf("failed to list pods for model %s: %w", modelName, err)
	}

	results := make(map[string][]MetricKV)

	for _, pod := range podList.Items {
		if pod.Status.PodIP == "" || !isPodReady(&pod) {
			continue
		}

		metricsURL := fmt.Sprintf("http://%s:8000/metrics", pod.Status.PodIP)
		req, err := http.NewRequestWithContext(ctx, "GET", metricsURL, nil)
		if err != nil {
			logf.Log.Error(err, "get failed", "details")
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logf.Log.Error(err, "send req failed", "details")
		}

		logf.Log.Info("data", "resp", resp.Body)

		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			continue
		}

		parser := expfmt.TextParser{}
		metricFamilies, err := parser.TextToMetricFamilies(strings.NewReader(string(bodyBytes)))
		if err != nil {
			continue
		}

		var podMetrics []MetricKV
		for name, family := range metricFamilies {
			if !strings.HasPrefix(name, "vllm:") {
				continue
			}

			for _, m := range family.Metric {
				val := 0.0
				switch {
				case m.Gauge != nil:
					val = m.Gauge.GetValue()
				case m.Counter != nil:
					val = m.Counter.GetValue()
				default:
					continue
				}

				labelMap := make(map[string]string)
				for _, lp := range m.Label {
					labelMap[lp.GetName()] = lp.GetValue()
				}

				podMetrics = append(podMetrics, MetricKV{
					Name:   name,
					Labels: labelMap,
					Value:  val,
				})
			}
		}

		results[pod.Name] = podMetrics
	}
	logf.Log.Info("data", "metrics", results)
	return results, nil
}

// Helper to check if pod is Ready
func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
