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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	actuator "github.com/llm-d-incubation/inferno-autoscaler/internal/actuator"
	collector "github.com/llm-d-incubation/inferno-autoscaler/internal/collector"
	interfaces "github.com/llm-d-incubation/inferno-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/inferno-autoscaler/internal/logger"
	analyzer "github.com/llm-d-incubation/inferno-autoscaler/internal/modelanalyzer"
	variantAutoscalingOptimizer "github.com/llm-d-incubation/inferno-autoscaler/internal/optimizer"
	"github.com/llm-d-incubation/inferno-autoscaler/internal/utils"
	inferno "github.com/llm-inferno/optimizer-light/pkg/core"
	infernoManager "github.com/llm-inferno/optimizer-light/pkg/manager"
	infernoSolver "github.com/llm-inferno/optimizer-light/pkg/solver"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VariantAutoscalingReconciler reconciles a variantAutoscaling object
type VariantAutoscalingReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	mu         sync.Mutex
	ticker     *time.Ticker
	stopTicker chan struct{}

	PromAPI promv1.API
}

// +kubebuilder:rbac:groups=llmd.ai,resources=variantautoscalings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=llmd.ai,resources=variantautoscalings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=llmd.ai,resources=variantautoscalings/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=nodes/status,verbs=get;list;update;patch;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;update;list;watch

const (
	configMapName      = "inferno-variantautoscaling-config"
	configMapNamespace = "default"
)

func (r *VariantAutoscalingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	// TODO: decide on whether to keep accelerator properties (device name, cost) in same configMap, provided by administrator
	acceleratorCm, err := r.readAcceleratorConfig(ctx, "accelerator-unit-costs", "default")
	if err != nil {
		logger.Log.Error(err, "unable to read accelerator configmap, skipping optimiziing")
		return ctrl.Result{}, nil
	}

	serviceClassCm, err := r.readServiceClassConfig(ctx, "service-classes-config", "default")
	if err != nil {
		logger.Log.Error(err, "unable to read serviceclass configmap, skipping optimiziing")
		return ctrl.Result{}, nil
	}

	// each variantAutoscaling CR corresponds to a variant which spawns exactly one deployment.
	var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
	if err := r.List(ctx, &variantAutoscalingList); err != nil {
		logger.Log.Error(err, "unable to list variantAutoscaling resources")
		return ctrl.Result{}, err
	}

	newInventory, err := collector.CollectInventoryK8S(ctx, r.Client)

	if err == nil {
		logger.Log.Info("current inventory in the cluster", "capacity", newInventory)
	} else {
		logger.Log.Error(err, "failed to get cluster inventory")
	}

	systemData := utils.CreateSystemData(acceleratorCm, serviceClassCm, newInventory)

	var updateList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
	var allAnalyzerResponses = make(map[string]*interfaces.ModelAnalyzeResponse)
	var vaMap = make(map[string]*llmdVariantAutoscalingV1alpha1.VariantAutoscaling)

	for _, va := range variantAutoscalingList.Items {
		modelName := va.Labels["inference.optimization/modelName"]
		if modelName == "" {
			logger.Log.Info("variantAutoscaling missing modelName label, skipping optimization", "name", va.Name)
			return ctrl.Result{}, err
		}

		entry, className, err := findModelSLO(serviceClassCm, modelName)
		if err != nil {
			logger.Log.Error(err, "failed to locate SLO for model")
			return ctrl.Result{}, nil
		}
		logger.Log.Info("Found SLO", "model", entry.Model, "class", className, "slo-itl", entry.SLOITL, "slo-ttw", entry.SLOTTW)

		for _, modelAcceleratorProfile := range va.Spec.ModelProfile.Accelerators {
			if utils.AddModelAcceleratorProfileToSystemData(systemData, modelName, &modelAcceleratorProfile) != nil {
				logger.Log.Info("variantAutoscaling bad model accelerator profile data, skipping optimization", "name", va.Name)
				return ctrl.Result{}, err
			}
		}

		acceleratorCostVal, ok := acceleratorCm["A100"]["cost"]
		if !ok {
			logger.Log.Info("variantAutoscaling missing accelerator cost in configmap, skipping optimization", "name", va.Name)
		}
		acceleratorCostValFloat, err := strconv.ParseFloat(acceleratorCostVal, 32)
		if err != nil {
			logger.Log.Info("variantAutoscaling unable to parse accelerator cost in configmap, skipping optimization", "name", va.Name)
		}

		//TODO: remove calling duplicate deployment calls
		// Check if Deployment exists for this variantAutoscaling
		var deploy appsv1.Deployment
		err = r.Get(ctx, types.NamespacedName{
			Name:      va.Name,
			Namespace: va.Namespace,
		}, &deploy)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			logger.Log.Error(err, "failed to get Deployment", "variantAutoscaling", va.Name)
			return ctrl.Result{}, err
		}

		var updateVA llmdVariantAutoscalingV1alpha1.VariantAutoscaling
		if err := r.Get(ctx, client.ObjectKey{Name: deploy.Name, Namespace: deploy.Namespace}, &updateVA); err != nil {
			logger.Log.Error(err, "unable to get variantAutoscaling")
		}

		currentAllocation, err := collector.AddMetricsToOptStatus(ctx, &updateVA, deploy, acceleratorCostValFloat, r.PromAPI)
		if err != nil {
			logger.Log.Error(err, "unable to fetch metrics, skipping this variantAutoscaling loop")
			return ctrl.Result{}, nil
		}
		updateVA.Status.CurrentAlloc = currentAllocation

		if err := utils.AddServerInfoToSystemData(systemData, &updateVA, className); err != nil {
			logger.Log.Info("variantAutoscaling bad deployment server data, skipping optimization", "name", updateVA.Name)
			return ctrl.Result{}, err
		}

		vaFullName := utils.FullName(va.Name, va.Namespace)
		updateList.Items = append(updateList.Items, updateVA)
		vaMap[vaFullName] = &va
	}

	// analyze
	// TODO: keep data specific to inferno in own new class
	system := inferno.NewSystem()
	optimizerSpec := system.SetFromSpec(&systemData.Spec)
	optimizer := infernoSolver.NewOptimizerFromSpec(optimizerSpec)
	manager := infernoManager.NewManager(system, optimizer)

	modelAnalyzer := analyzer.NewModelAnalyzer(system)
	for _, s := range system.Servers() {
		modelAnalyzeResponse, err := modelAnalyzer.AnalyzeModel(ctx, *vaMap[s.Name()])
		if err != nil {
			logger.Log.Error("model analyzer error", "failed to analyze", err)
			return ctrl.Result{}, err
		}
		allAnalyzerResponses[s.Name()] = modelAnalyzeResponse
	}
	logger.Log.Info("inferno data ", "systemData ", systemData)

	// Call Optimize ONCE across all variants
	engine := variantAutoscalingOptimizer.NewVariantAutoscalingsEngine(manager, system)
	optimizedAllocation, err := engine.Optimize(ctx, updateList, allAnalyzerResponses)
	if err != nil {
		logger.Log.Error(err, "unable to perform model optimization, skipping this variantAutoscaling loop")
		return ctrl.Result{}, nil
	}

	for i := range updateList.Items {
		va := &updateList.Items[i]
		// Fetch the latest version from API server
		var updateVa llmdVariantAutoscalingV1alpha1.VariantAutoscaling
		if err := r.Get(ctx, client.ObjectKeyFromObject(va), &updateVa); err != nil {
			logger.Log.Error(err, "failed to get latest VariantAutoscaling from API server", "name", va.Name)
			continue
		}

		original := updateVa.DeepCopy()

		//TODO: remove calling duplicate deployment calls
		// Check if Deployment exists for this variantAutoscaling
		var deploy appsv1.Deployment
		err = r.Get(ctx, types.NamespacedName{
			Name:      va.Name,
			Namespace: va.Namespace,
		}, &deploy)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			logger.Log.Error(err, "failed to get Deployment", "variantAutoscaling", updateVa.Name)
			return ctrl.Result{}, err
		}

		// Add OwnerReference if not already set
		if !metav1.IsControlledBy(&updateVa, &deploy) {
			updateVa.OwnerReferences = append(updateVa.OwnerReferences, metav1.OwnerReference{
				APIVersion:         deploy.APIVersion,
				Kind:               deploy.Kind,
				Name:               deploy.Name,
				UID:                deploy.UID,
				Controller:         ptr(true),
				BlockOwnerDeletion: ptr(true),
			})

			// Patch metadata change (ownerReferences)
			patch := client.MergeFrom(original)
			if err := r.Client.Patch(ctx, &updateVa, patch); err != nil {
				logger.Log.Error(err, "failed to patch ownerReference", "name", updateVa.Name)
				return ctrl.Result{}, err
			}
		}

		updateVa.Status.CurrentAlloc = va.Status.CurrentAlloc
		updateVa.Status.DesiredOptimizedAlloc = optimizedAllocation[va.Name]
		updateVa.Status.Actuation.Applied = true

		act := actuator.NewDummyActuator(r.Client)
		if err := act.ApplyReplicaTargets(ctx, &updateVa); err != nil {
			logger.Log.Error(err, "failed to apply replicas")
		}

		if err := r.Client.Status().Update(ctx, &updateVa); err != nil {
			logger.Log.Error(err, "failed to patch status", "name", updateVa.Name)
			continue
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VariantAutoscalingReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// To run locally, set the environment variable to Prometheus base URL e.g. PROMETHEUS_BASE_URL=http://localhost:9090
	prom_addr := os.Getenv("PROMETHEUS_BASE_URL")
	if prom_addr == "" {
		// Running in cluster
		prom_addr = "http://prometheus-operated.default.svc.cluster.local:9090"
	}
	promClient, err := api.NewClient(api.Config{
		Address: prom_addr,
	})
	if err != nil {
		return fmt.Errorf("failed to create prometheus client: %w", err)
	}

	r.PromAPI = promv1.NewAPI(promClient)
	logger.Log.Info("Prometheus client initialized")

	// Start watching ConfigMap and ticker logic
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			// Controller shutdown before becoming leader
			logger.Log.Info("Shutdown before leader election")
			return nil
		case <-mgr.Elected():
			// Now leader — safe to run loop
			logger.Log.Info("Elected as leader, starting optimization loop")
			r.watchAndRunLoop(ctx)
			return nil
		}
	})); err != nil {
		return fmt.Errorf("failed to add watchAndRunLoop: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}).
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				// Return nothing, we only want the Node object cached
				return nil
			}),
			builder.WithPredicates(predicate.Funcs{ // minimal predicate that returns false
				CreateFunc:  func(_ event.CreateEvent) bool { return false },
				UpdateFunc:  func(_ event.UpdateEvent) bool { return false },
				DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
				GenericFunc: func(_ event.GenericEvent) bool { return false },
			}), // never trigger reconciliation
		).
		Named("variantAutoscaling").
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
		}).
		Complete(r)
}

func (r *VariantAutoscalingReconciler) watchAndRunLoop(ctx context.Context) {
	var lastInterval string

	for {
		cm := &corev1.ConfigMap{}
		err := r.Get(context.Background(), types.NamespacedName{
			Name:      configMapName,
			Namespace: configMapNamespace,
		}, cm)
		if err != nil {
			logger.Log.Error(err, "Unable to read optimization config")
			time.Sleep(30 * time.Second)
			continue
		}

		interval := cm.Data["GLOBAL_OPT_INTERVAL"]
		trigger := cm.Data["GLOBAL_OPT_TRIGGER"]

		// Handle manual trigger
		if trigger == "true" {
			logger.Log.Info("Manual optimization trigger received")
			_, err := r.Reconcile(context.Background(), ctrl.Request{})
			if err != nil {
				logger.Log.Error(err, "Manual reconcile failed")
			}

			// Reset trigger in ConfigMap
			cm.Data["GLOBAL_OPT_TRIGGER"] = "false"
			if err := r.Update(context.Background(), cm); err != nil {
				logger.Log.Error(err, "Failed to reset GLOBAL_OPT_TRIGGER")
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
					logger.Log.Error(err, "Invalid GLOBAL_OPT_INTERVAL")
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
							_, err := r.Reconcile(ctx, ctrl.Request{})
							if err != nil {
								logger.Log.Error(err, "Manual reconcile failed")
							}
						case <-stopCh:
							return
						case <-ctx.Done():
							logger.Log.Info("Context cancelled, stopping ticker loop")
							return
						}
					}
				}(r.stopTicker, ticker.C)

				logger.Log.Info("Started periodic optimization ticker", "interval", interval)
			} else {
				r.ticker = nil
				logger.Log.Info("GLOBAL_OPT_INTERVAL unset, disabling periodic optimization")
			}
			lastInterval = interval
		}
		r.mu.Unlock()

		time.Sleep(10 * time.Second)
	}
}

func (r *VariantAutoscalingReconciler) readServiceClassConfig(ctx context.Context, cmName, cmNamespace string) (map[string]string, error) {
	if cmPtr, err := r.getConfigMap(ctx, cmName, cmNamespace); err == nil {
		return (*cmPtr).Data, nil
	} else {
		return nil, err
	}
}

func (r *VariantAutoscalingReconciler) readAcceleratorConfig(ctx context.Context, cmName, cmNamespace string) (map[string]map[string]string, error) {
	var cmPtr *corev1.ConfigMap
	var err error
	if cmPtr, err = r.getConfigMap(ctx, cmName, cmNamespace); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]string)
	for acc, accInfoStr := range (*cmPtr).Data {
		accInfoMap := make(map[string]string)
		if err := json.Unmarshal([]byte(accInfoStr), &accInfoMap); err != nil {
			return nil, fmt.Errorf("failed to read entry %s in ConfigMap %s/%s: %w", acc, cmNamespace, cmName, err)
		}
		out[acc] = accInfoMap
	}
	return out, nil
}

func (r *VariantAutoscalingReconciler) getConfigMap(ctx context.Context, cmName, cmNamespace string) (*corev1.ConfigMap, error) {
	var cm corev1.ConfigMap
	backoff := wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    5,
	}

	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		err := r.Get(ctx, client.ObjectKey{Name: cmName, Namespace: cmNamespace}, &cm)
		if err == nil {
			return true, nil
		}

		if apierrors.IsNotFound(err) {
			logger.Log.Error(err, "ConfigMap not found, will not retry", "name", cmName, "namespace", cmNamespace)
			return false, err
		}

		logger.Log.Error(err, "Transient error fetching ConfigMap, retrying...")
		return false, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to read ConfigMap %s/%s: %w", cmNamespace, cmName, err)
	}
	return &cm, nil
}
