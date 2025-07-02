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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// OptimizerReconciler reconciles a Optimizer object
type OptimizerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type AcceleratorModelInfo struct {
	Count  int
	Memory string
}

// +kubebuilder:rbac:groups=llmd.llm-d.ai,resources=optimizers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=llmd.llm-d.ai,resources=optimizers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=llmd.llm-d.ai,resources=optimizers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=nodes/status,verbs=get;list;update;patch;watch

func (r *OptimizerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	var nodeList corev1.NodeList

	if err := r.Client.List(ctx, &nodeList); err != nil {
		logf.Log.Error(err, "unable to list nodes")
		return ctrl.Result{}, err
	}

	newInventory := make(map[string]map[string]AcceleratorModelInfo)

	for _, node := range nodeList.Items {
		nodeName := node.Name
		labels := node.Labels
		model, ok := labels["nvidia.com/gpu.product"]
		if !ok {
			continue
		}
		memory := labels["nvidia.com/gpu.memory"]
		count := 0
		if cap, ok := node.Status.Capacity["nvidia.com/gpu"]; ok {
			count = int(cap.Value())
		}
		newInventory[nodeName] = make(map[string]AcceleratorModelInfo)
		newInventory[nodeName][model] = AcceleratorModelInfo{
			Count:  count,
			Memory: memory,
		}

	}

	logf.Log.Info("current inventory in the cluster", "capacity", newInventory)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OptimizerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&llmdOptv1alpha1.Optimizer{}).
		Named("optimizer").
		Complete(r)
}
