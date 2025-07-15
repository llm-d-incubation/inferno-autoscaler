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
	"strconv"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	actuator "github.com/llm-d-incubation/inferno-autoscaler/internal/actuator"
	collector "github.com/llm-d-incubation/inferno-autoscaler/internal/collector"
	interfaces "github.com/llm-d-incubation/inferno-autoscaler/internal/interfaces"
	analyzer "github.com/llm-d-incubation/inferno-autoscaler/internal/modelanalyzer"
	variantAutoscalingOptimizer "github.com/llm-d-incubation/inferno-autoscaler/internal/optimizer"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type ServiceClassEntry struct {
	Model  string `yaml:"model"`
	SLOITL int    `yaml:"slo-itl"`
	SLOTTW int    `yaml:"slo-ttw"`
}

type ServiceClass struct {
	Name     string              `yaml:"name"`
	Priority int                 `yaml:"priority"`
	Data     []ServiceClassEntry `yaml:"data"`
}

func (r *VariantAutoscalingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	serviceClassCm, err := r.readServiceClassConfig(ctx, "service-classes-config", "default")
	if err != nil {
		log.Log.Error(err, "unable to read serviceclass configmap, skipping optimiziing")
		return ctrl.Result{}, nil
	}

	acceleratorUnitCostCm, err := r.readServiceClassConfig(ctx, "accelerator-unit-costs", "default")
	if err != nil {
		log.Log.Error(err, "unable to read accelerator unit cost configmap, skipping optimiziing")
		return ctrl.Result{}, nil
	}

	// each variantAutoscaling CR corresponds to a variant which spawns exactly one deployment.
	var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
	if err := r.List(ctx, &variantAutoscalingList); err != nil {
		logger.Error(err, "unable to list variantAutoscaling resources")
		return ctrl.Result{}, err
	}

	newInventory, err := collector.CollectInventoryK8S(ctx, r.Client)

	if err == nil {
		logger.Info("current inventory in the cluster", "capacity", newInventory)
	} else {
		logger.Error(err, "failed to get cluster inventory")
	}

	var updateList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
	var allAnalyzerResponses = make(map[string]interfaces.ModelAnalyzeResponse)
	var allMetrics = make(map[string]interfaces.MetricsSnapshot)

	for _, opt := range variantAutoscalingList.Items {
		modelName := opt.Labels["inference.optimization/modelName"]
		if modelName == "" {
			logger.Info("variantAutoscaling missing modelName label, skipping optimization", "name", opt.Name)
			return ctrl.Result{}, err
		}

		entry, className, err := findModelSLO(serviceClassCm, modelName)
		if err != nil {
			logger.Error(err, "failed to locate SLO for model")
			return ctrl.Result{}, nil
		}

		logger.Info("Found SLO", "model", entry.Model, "class", className, "slo-itl", entry.SLOITL, "slo-ttw", entry.SLOTTW)

		acceleratorCostVal, ok := acceleratorUnitCostCm["A100"]
		if !ok {
			logger.Info("variantAutoscaling missing accelerator cost in configmap, skipping optimization", "name", opt.Name)
		}
		acceleratorCostValFloat, err := strconv.ParseFloat(acceleratorCostVal, 32)
		if err != nil {
			logger.Info("variantAutoscaling unable to parse accelerator cost in configmap, skipping optimization", "name", opt.Name)
		}
		//TODO: remove calling duplicate deployment calls
		// Check if Deployment exists for this variantAutoscaling
		var deploy appsv1.Deployment
		err = r.Get(ctx, types.NamespacedName{
			Name:      opt.Name,
			Namespace: opt.Namespace,
		}, &deploy)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			logger.Error(err, "failed to get Deployment", "variantAutoscaling", opt.Name)
			return ctrl.Result{}, err
		}

		var updateOpt llmdVariantAutoscalingV1alpha1.VariantAutoscaling
		if err := r.Get(ctx, client.ObjectKey{Name: deploy.Name, Namespace: deploy.Namespace}, &updateOpt); err != nil {
			logger.Error(err, "unable to get variantAutoscaling")
		}

		//original := updateOpt.DeepCopy()

		err = collector.AddMetricsToOptStatus(ctx, &updateOpt, deploy, acceleratorCostValFloat, r.PromAPI)

		if err != nil {
			logger.Error(err, "unable to fetch metrics, skipping this variantAutoscaling loop")
			return ctrl.Result{}, nil
		}
		dummyQps := 50.0
		metrics := interfaces.MetricsSnapshot{
			ActualQPS: dummyQps,
		}
		dummyAnalyzer := analyzer.NewSimplePrefillDecodeAnalyzer()
		dummyModelAnalyzerResponse, err := dummyAnalyzer.AnalyzeModel(ctx, updateOpt, metrics)
		if err != nil {
			logger.Error(err, "unable to perform model optimization, skipping this variantAutoscaling loop")
			return ctrl.Result{}, nil
		}
		allMetrics[opt.Name] = metrics
		allAnalyzerResponses[opt.Name] = dummyModelAnalyzerResponse
		updateList.Items = append(updateList.Items, updateOpt)
	}
	// Call Optimize ONCE across all variants
	dummyvariantAutoscaling := variantAutoscalingOptimizer.NewDummyVariantAutoscalingsEngine()
	optimizedAllocation, err := dummyvariantAutoscaling.Optimize(ctx, updateList, allAnalyzerResponses, allMetrics)
	if err != nil {
		logger.Error(err, "unable to perform model optimization, skipping this variantAutoscaling loop")
		return ctrl.Result{}, nil
	}

	for i := range updateList.Items {
		va := &updateList.Items[i]
		// Fetch the latest version from API server
		var updateVa llmdVariantAutoscalingV1alpha1.VariantAutoscaling
		if err := r.Get(ctx, client.ObjectKeyFromObject(va), &updateVa); err != nil {
			logger.Error(err, "failed to get latest VariantAutoscaling from API server", "name", va.Name)
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
			logger.Error(err, "failed to get Deployment", "variantAutoscaling", updateVa.Name)
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
				logger.Error(err, "failed to patch ownerReference", "name", updateVa.Name)
				return ctrl.Result{}, err
			}
		}
		updateVa.Status.CurrentAlloc = va.Status.CurrentAlloc
		updateVa.Status.DesiredOptimizedAlloc = optimizedAllocation[va.Name]
		//patch := client.MergeFrom(original)
		//patch
		if err := r.Client.Status().Update(ctx, &updateVa); err != nil {
			logger.Error(err, "failed to patch status", "name", updateVa.Name)
			continue
		}

		act := actuator.NewDummyActuator(r.Client)
		if err := act.ApplyReplicaTargets(ctx, &updateVa); err != nil {
			logger.Error(err, "failed to apply replicas")
		}

	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VariantAutoscalingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Start watching ConfigMap and ticker logic
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		<-mgr.Elected() // Wait for leader election
		r.watchAndRunLoop()
		return nil
	})); err != nil {
		return err
	}

	client, err := api.NewClient(api.Config{
		Address: "http://prometheus-operated.default.svc.cluster.local:9090",
	})
	if err != nil {
		return fmt.Errorf("failed to create prometheus client: %w", err)
	}

	r.PromAPI = promv1.NewAPI(client)

	return ctrl.NewControllerManagedBy(mgr).
		For(&llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}).
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

func (r *VariantAutoscalingReconciler) watchAndRunLoop() {
	var lastInterval string

	for {
		cm := &corev1.ConfigMap{}
		err := r.Get(context.Background(), types.NamespacedName{
			Name:      configMapName,
			Namespace: configMapNamespace,
		}, cm)
		if err != nil {
			logf.Log.Error(err, "Unable to read optimization config")
			time.Sleep(30 * time.Second)
			continue
		}

		interval := cm.Data["GLOBAL_OPT_INTERVAL"]
		trigger := cm.Data["GLOBAL_OPT_TRIGGER"]

		// Handle manual trigger
		if trigger == "true" {
			logf.Log.Info("Manual optimization trigger received")
			_, err := r.Reconcile(context.Background(), ctrl.Request{})
			if err != nil {
				logf.Log.Error(err, "Manual reconcile failed")
			}

			// Reset trigger in ConfigMap
			cm.Data["GLOBAL_OPT_TRIGGER"] = "false"
			if err := r.Update(context.Background(), cm); err != nil {
				logf.Log.Error(err, "Failed to reset GLOBAL_OPT_TRIGGER")
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
					logf.Log.Error(err, "Invalid GLOBAL_OPT_INTERVAL")
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
								logf.Log.Error(err, "Manual reconcile failed")
							}
						case <-stopCh:
							return
						}
					}
				}(r.stopTicker, ticker.C)

				logf.Log.Info("Started periodic optimization ticker", "interval", interval)
			} else {
				r.ticker = nil
				logf.Log.Info("GLOBAL_OPT_INTERVAL unset, disabling periodic optimization")
			}
			lastInterval = interval
		}
		r.mu.Unlock()

		time.Sleep(10 * time.Second)
	}
}

func (r *VariantAutoscalingReconciler) readServiceClassConfig(ctx context.Context, cmName, cmNamespace string) (map[string]string, error) {
	logger := log.FromContext(ctx)

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
			logger.Error(err, "ConfigMap not found, will not retry", "name", cmName, "namespace", cmNamespace)
			return false, err
		}

		logger.Error(err, "Transient error fetching ConfigMap, retrying...")
		return false, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to read ConfigMap %s/%s: %w", cmNamespace, cmName, err)
	}

	return cm.Data, nil
}

func findModelSLO(cmData map[string]string, targetModel string) (*ServiceClassEntry, string /* class name */, error) {
	for key, val := range cmData {
		var sc ServiceClass
		if err := yaml.Unmarshal([]byte(val), &sc); err != nil {
			return nil, "", fmt.Errorf("failed to parse %s: %w", key, err)
		}

		for _, entry := range sc.Data {
			if entry.Model == targetModel {
				return &entry, sc.Name, nil
			}
		}
	}
	return nil, "", fmt.Errorf("model %q not found in any service class", targetModel)
}

func ptr[T any](v T) *T {
	return &v
}
