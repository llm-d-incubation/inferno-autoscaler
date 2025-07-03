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
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// OptimizerReconciler reconciles a Optimizer object
type OptimizerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	mu         sync.Mutex
	ticker     *time.Ticker
	stopTicker chan struct{}
}

// +kubebuilder:rbac:groups=llmd.ai,resources=optimizers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=llmd.ai,resources=optimizers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=llmd.ai,resources=optimizers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=nodes/status,verbs=get;list;update;patch;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;update;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

const (
	configMapName      = "inferno-optimizer-config"
	configMapNamespace = "default"
)

func (r *OptimizerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// each optimizer CR corresponds to a variant which spawns exactly one deployment.
	var optimizerList llmdOptv1alpha1.OptimizerList
	if err := r.List(ctx, &optimizerList); err != nil {
		logger.Error(err, "unable to list Optimizer resources")
		return ctrl.Result{}, err
	}

	logger.Info("reconciling")

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

	coll := NewCollector(r.Client)

	newInventory, err := coll.CollectInventoryK8S(ctx)

	if err == nil {
		logger.Info("current inventory in the cluster", "capacity", newInventory)
	} else {
		logger.Error(err, "failed to get cluster inventory")
	}

	r.fetchVLLMMetricsPerPod(ctx, "opt125m", "default")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OptimizerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Start watching ConfigMap and ticker logic
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		<-mgr.Elected() // Wait for leader election
		r.watchAndRunLoop()
		return nil
	})); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&llmdOptv1alpha1.Optimizer{}).
		Named("optimizer").
		Complete(r)
}

func (r *OptimizerReconciler) watchAndRunLoop() {
	var lastInterval string

	for {
		cm := &corev1.ConfigMap{}
		err := r.Get(context.Background(), types.NamespacedName{
			Name:      configMapName,
			Namespace: configMapNamespace,
		}, cm)
		if err != nil {
			log.Log.Error(err, "Unable to read optimization config")
			time.Sleep(30 * time.Second)
			continue
		}

		interval := cm.Data["GLOBAL_OPT_INTERVAL"]
		trigger := cm.Data["GLOBAL_OPT_TRIGGER"]

		// Handle manual trigger
		if trigger == "true" {
			log.Log.Info("Manual optimization trigger received")
			_, err := r.Reconcile(context.Background(), ctrl.Request{})
			if err != nil {
				log.Log.Error(err, "Manual reconcile failed")
			}

			// Reset trigger in ConfigMap
			cm.Data["GLOBAL_OPT_TRIGGER"] = "false"
			if err := r.Update(context.Background(), cm); err != nil {
				log.Log.Error(err, "Failed to reset GLOBAL_OPT_TRIGGER")
			}
		}

		r.mu.Lock()
		if interval != lastInterval {
			// Stop previous ticker if any
			if r.stopTicker != nil {
				close(r.stopTicker)
			}

			if interval != "" {
				d, err := time.ParseDuration(interval)
				if err != nil {
					log.Log.Error(err, "Invalid GLOBAL_OPT_INTERVAL")
					r.mu.Unlock()
					continue
				}

				r.stopTicker = make(chan struct{})
				ticker := time.NewTicker(d)
				r.ticker = ticker

				go func(stopCh <-chan struct{}, tick <-chan time.Time) {
					for {
						select {
						case <-tick:
							_, err := r.Reconcile(context.Background(), ctrl.Request{})
							if err != nil {
								log.Log.Error(err, "Manual reconcile failed")
							}
						case <-stopCh:
							return
						}
					}
				}(r.stopTicker, ticker.C)

				log.Log.Info("Started periodic optimization ticker", "interval", interval)
			} else {
				r.ticker = nil
				log.Log.Info("GLOBAL_OPT_INTERVAL unset, disabling periodic optimization")
			}
			lastInterval = interval
		}
		r.mu.Unlock()

		time.Sleep(10 * time.Second)
	}
}
