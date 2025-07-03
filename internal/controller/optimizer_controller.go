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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list

func (r *OptimizerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// each optimizer CR corresponds to a variant which spawns exactly one deployment.
	var optimizerList llmdOptv1alpha1.OptimizerList
	if err := r.List(ctx, &optimizerList); err != nil {
		logger.Error(err, "unable to list Optimizer resources")
		return ctrl.Result{}, err
	}

	groupedOptimizerObjsWithDeployment := make(map[string][]llmdOptv1alpha1.Optimizer)
	groupOptimizerObjsWithoutDeployment := make(map[string][]llmdOptv1alpha1.Optimizer)

	for _, opt := range optimizerList.Items {
		modelName := opt.Labels["inference.optimization/modelName"]
		if modelName == "" {
			logger.Info("optimizer missing modelName label, skipping", "name", opt.Name)
			continue
		}

		// Check if Deployment exists for this Optimizer
		var deploy appsv1.Deployment
		err := r.Get(ctx, types.NamespacedName{
			Name:      opt.Name,
			Namespace: opt.Namespace,
		}, &deploy)
		if err != nil {
			if apierrors.IsNotFound(err) {
				groupOptimizerObjsWithoutDeployment[modelName] = append(groupOptimizerObjsWithoutDeployment[modelName], opt)
				continue
			}
			logger.Error(err, "failed to get Deployment", "optimizer", opt.Name)
			return ctrl.Result{}, err
		}
		// at this point, the optimizer will optimize a variant
		// grouping variants ie optimizer objects by modelfamily is not required.
		// This will be explored when same inferencepool has multiple modelfamilies (eg: llama and granite).
		groupedOptimizerObjsWithDeployment[modelName] = append(groupedOptimizerObjsWithDeployment[modelName], opt)
	}

	if len(groupOptimizerObjsWithoutDeployment) > 0 {
		for modelName, optimizers := range groupOptimizerObjsWithoutDeployment {
			for _, opt := range optimizers {
				logger.Info("missing Deployment for Optimizer", "modelName", modelName, "optimizer", opt.Name)
			}
		}
	}
	var nodeList corev1.NodeList

	if err := r.Client.List(ctx, &nodeList); err != nil {
		logger.Error(err, "unable to list nodes")
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

	logger.Info("current inventory in the cluster", "capacity", newInventory)

	// call collector to path each optimizer object with accelarator, maxBatch and numReplicas
	// acceleraotor and maxBatch are obtained from deployment labels, numReplicas is available from spec.

	// Output of the collector is passed to Model Analyzer

	// The result of Model Analyzer is then passed to the Optimizer

	// Output of the Optimizer is then consumed by actuator to emit prometheus metrics or change replicas directly

	return ctrl.Result{}, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *OptimizerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			<-ticker.C
			ctx := context.Background()

			if _, err := r.Reconcile(ctx, ctrl.Request{}); err != nil {
				log.Log.Error(err, "Periodic reconcile failed")
			}
		}
	}()
	return ctrl.NewControllerManagedBy(mgr).
		For(&llmdOptv1alpha1.Optimizer{}).
		Named("optimizer").
		Complete(r)
}
