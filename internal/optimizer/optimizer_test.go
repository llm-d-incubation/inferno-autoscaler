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
package optimizer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/common/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	collector "github.com/llm-d-incubation/workload-variant-autoscaler/internal/collector"
	interfaces "github.com/llm-d-incubation/workload-variant-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	analyzer "github.com/llm-d-incubation/workload-variant-autoscaler/internal/modelanalyzer"
	utils "github.com/llm-d-incubation/workload-variant-autoscaler/internal/utils"
	infernoConfig "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/config"
	inferno "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/core"
	infernoManager "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/manager"
	infernoSolver "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/solver"
	testutils "github.com/llm-d-incubation/workload-variant-autoscaler/test/utils"
)

const (
	configMapName      = "workload-variant-autoscaler-variantautoscaling-config"
	configMapNamespace = "workload-variant-autoscaler-system"
)

var _ = Describe("Optimizer", Ordered, func() {
	var (
		ctx           context.Context
		scheme        *runtime.Scheme
		ns            *corev1.Namespace
		optimizer     *infernoSolver.Optimizer
		manager       *infernoManager.Manager
		systemData    *infernoConfig.SystemData
		system        *inferno.System
		engine        *VariantAutoscalingsEngine
		modelAnalyzer *analyzer.ModelAnalyzer

		acceleratorCm  map[string]map[string]string
		serviceClassCm map[string]string
		minNumReplicas = 1
	)

	Context("Testing optimization", func() {

		readAccFunc := func(c client.Client, ctx context.Context, cmName, cmNamespace string) (map[string]map[string]string, error) {
			cm := corev1.ConfigMap{}
			err := utils.GetConfigMapWithBackoff(ctx, c, cmName, cmNamespace, &cm)
			if err != nil {
				return nil, fmt.Errorf("failed to read ConfigMap %s/%s: %w", cmNamespace, cmName, err)
			}
			out := make(map[string]map[string]string)
			for acc, accInfoStr := range cm.Data {
				accInfoMap := make(map[string]string)
				if err := json.Unmarshal([]byte(accInfoStr), &accInfoMap); err != nil {
					return nil, fmt.Errorf("failed to read entry %s in ConfigMap %s/%s: %w", acc, cmNamespace, cmName, err)
				}
				out[acc] = accInfoMap
			}
			return out, nil
		}

		readCmFunc := func(c client.Client, ctx context.Context, cmName, cmNamespace string) (map[string]string, error) {
			cm := corev1.ConfigMap{}
			err := utils.GetConfigMapWithBackoff(ctx, c, cmName, cmNamespace, &cm)
			if err != nil {
				return nil, err
			}
			return cm.Data, nil
		}

		BeforeAll(func() {
			ctx = context.Background()

			scheme = runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(appsv1.AddToScheme(scheme)).To(Succeed())
			Expect(llmdVariantAutoscalingV1alpha1.AddToScheme(scheme)).To(Succeed())

			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: configMapNamespace,
				},
			}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			ns = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: configMapNamespace,
				},
			}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ns))).To(Succeed())
		})

		BeforeEach(func() {
			By("creating the required configmap for optimization")
			configMap := testutils.CreateServiceClassConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			configMap = testutils.CreateAcceleratorUnitCostConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			configMap = testutils.CreateVariantAutoscalingConfigMap(configMapName, ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			var err error
			acceleratorCm, err = readAccFunc(k8sClient, ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())
			serviceClassCm, err = readCmFunc(k8sClient, ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())
			wvaConfigCm, err := readCmFunc(k8sClient, ctx, configMapName, configMapNamespace)
			Expect(err).NotTo(HaveOccurred())
			if wvaConfigCm["WVA_SCALE_TO_ZERO"] == "true" {
				minNumReplicas = 0
			}

			// WVA operates in unlimited mode - no inventory data needed
			systemData = utils.CreateSystemData(acceleratorCm, serviceClassCm)

			By("Creating test VariantAutoscaling resources")
			for i := 1; i <= 3; i++ {
				// Create Deployment first
				d := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("test-variantautoscaling-%d", i),
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: utils.Ptr(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": fmt.Sprintf("test-variantautoscaling-%d", i)},
						},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": fmt.Sprintf("test-variantautoscaling-%d", i)},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "test-container",
										Image: "quay.io/infernoautoscaler/vllme:0.2.1-multi-arch",
										Ports: []corev1.ContainerPort{{ContainerPort: 80}},
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, d)).To(Succeed())

				// Create VariantAutoscaling
				variantAutoscaling := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "llm.d.incubation/v1alpha1",
						Kind:       "VariantAutoscaling",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("test-variantautoscaling-%d", i),
						Namespace: "default",
						Labels: map[string]string{
							"inference.optimization/acceleratorName": "A100",
						},
					},
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						ModelID: "meta/llama0-70b",
						ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
							Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
								{
									Acc:      "A100",
									AccCount: 1,
									PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
										DecodeParms:  map[string]string{"alpha": "20.28", "beta": "0.72"},
										PrefillParms: map[string]string{"gamma": "0", "delta": "0"},
									},
									MaxBatchSize: 4,
								},
							},
						},
						SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
							Name: "premium",
							Key:  "default/default",
						},
					},
				}
				Expect(k8sClient.Create(ctx, variantAutoscaling)).To(Succeed())
			}

		})

		AfterEach(func() {
			cmAcc := &corev1.ConfigMap{}
			err := utils.GetConfigMapWithBackoff(ctx, k8sClient, "accelerator-unit-costs", configMapNamespace, cmAcc)
			Expect(err).NotTo(HaveOccurred(), "failed to get accelerator-unit-costs configmap")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, cmAcc))).To(Succeed())

			cmServClass := &corev1.ConfigMap{}
			err = utils.GetConfigMapWithBackoff(ctx, k8sClient, "service-classes-config", configMapNamespace, cmServClass)
			Expect(err).NotTo(HaveOccurred(), "failed to get service-class-config configmap")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, cmServClass))).To(Succeed())

			cmWvaClass := &corev1.ConfigMap{}
			err = utils.GetConfigMapWithBackoff(ctx, k8sClient, configMapName, configMapNamespace, cmWvaClass)
			Expect(err).NotTo(HaveOccurred(), "failed to get service-class-config configmap")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, cmWvaClass))).To(Succeed())

			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			Expect(k8sClient.List(ctx, &variantAutoscalingList)).To(Succeed())
			for _, va := range variantAutoscalingList.Items {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &va))).To(Succeed())
			}

			var deploymentList appsv1.DeploymentList
			Expect(k8sClient.List(ctx, &deploymentList)).To(Succeed())
			for _, deploy := range deploymentList.Items {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &deploy))).To(Succeed())
			}
		})

		It(fmt.Sprintf("should perform optimization for multiple VariantAutoscalings - scaled to %d without load", minNumReplicas), func() {
			allAnalyzerResponses := make(map[string]*interfaces.ModelAnalyzeResponse)
			vaMap := make(map[string]*llmdVariantAutoscalingV1alpha1.VariantAutoscaling)

			By("Populating VariantAutoscalings map from the cluster")
			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			Expect(k8sClient.List(ctx, &variantAutoscalingList)).To(Succeed())
			Expect(len(variantAutoscalingList.Items)).To(BeNumerically(">", 0), "no VariantAutoscalings found in the cluster")

			// Prepare list of VariantAutoscalings to be updated after optimization
			By("Preparing list of VariantAutoscalings to be updated after optimization")
			var updateList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList

			// Filter out deleted VariantAutoscalings
			By("Filtering out deleted VariantAutoscalings")
			activeVAs := make([]llmdVariantAutoscalingV1alpha1.VariantAutoscaling, 0, len(variantAutoscalingList.Items))
			for _, va := range variantAutoscalingList.Items {
				if va.DeletionTimestamp.IsZero() {
					activeVAs = append(activeVAs, va)
				}
			}

			// Prepare system data with all VariantAutoscalings info
			By("Preparing system data with all VariantAutoscalings info")
			for _, va := range activeVAs {
				modelName := va.Spec.ModelID
				Expect(modelName).NotTo(BeEmpty(), "variantAutoscaling missing modelName label, skipping optimization - ", "variantAutoscaling-name: ", va.Name)

				_, className, err := utils.FindModelSLO(serviceClassCm, modelName)
				Expect(err).NotTo(HaveOccurred(), "failed to find model SLO for model - ", modelName, ", variantAutoscaling - ", va.Name)

				for _, modelAcceleratorProfile := range va.Spec.ModelProfile.Accelerators {
					err = utils.AddModelAcceleratorProfileToSystemData(systemData, modelName, &modelAcceleratorProfile)
					Expect(err).NotTo(HaveOccurred(), "failed to add model accelerator profile to system data for model - ", modelName, ", variantAutoscaling - ", va.Name)
				}

				accName := va.Labels["inference.optimization/acceleratorName"]
				Expect(accName).NotTo(BeEmpty(), "variantAutoscaling missing acceleratorName label, skipping optimization - ", "variantAutoscaling-name: ", va.Name)
				acceleratorCostVal, ok := acceleratorCm[accName]["cost"]
				Expect(ok).NotTo(BeFalse(), "variantAutoscaling missing accelerator cost in configMap, skipping optimization - ", "variantAutoscaling-name: ", va.Name)
				acceleratorCostValFloat, err := strconv.ParseFloat(acceleratorCostVal, 32)
				Expect(err).NotTo(HaveOccurred(), "failed to parse accelerator cost value to float for variantAutoscaling - ", "variantAutoscaling-name: ", va.Name)

				var deploy appsv1.Deployment
				err = utils.GetDeploymentWithBackoff(ctx, k8sClient, va.Name, va.Namespace, &deploy)
				Expect(err).NotTo(HaveOccurred(), "failed to get deployment for variantAutoscaling - ", "variantAutoscaling-name: ", va.Name)

				var updateVA llmdVariantAutoscalingV1alpha1.VariantAutoscaling
				err = utils.GetVariantAutoscalingWithBackoff(ctx, k8sClient, deploy.Name, deploy.Namespace, &updateVA)
				Expect(err).NotTo(HaveOccurred(), "failed to get variantAutoscaling for deployment - ", "deployment-name: ", deploy.Name)

				testMetricsCache := collector.NewModelMetricsCache()
				currentAllocation, err := collector.AddMetricsToOptStatus(ctx, &updateVA, deploy, acceleratorCostValFloat, &testutils.MockPromAPI{}, testMetricsCache, 10*time.Minute)
				Expect(err).NotTo(HaveOccurred(), "unable to fetch metrics and add to Optimizer status for variantAutoscaling - ", "variantAutoscaling-name: ", va.Name)
				updateVA.Status.CurrentAlloc = currentAllocation

				// For tests, pass empty scale-to-zero config data - will use global defaults
				scaleToZeroConfigData := make(utils.ScaleToZeroConfigData)
				err = utils.AddServerInfoToSystemData(systemData, &updateVA, className, scaleToZeroConfigData)
				Expect(err).NotTo(HaveOccurred(), "failed to add server info to system data for variantAutoscaling - ", "variantAutoscaling-name: ", va.Name)

				By("Updating system data with VariantAutoscaling info")
				vaFullName := utils.FullName(va.Name, va.Namespace)
				updateList.Items = append(updateList.Items, updateVA)
				vaMap[vaFullName] = &va
			}

			system = inferno.NewSystem()
			optimizerSpec := system.SetFromSpec(&systemData.Spec)
			optimizer = infernoSolver.NewOptimizerFromSpec(optimizerSpec)
			manager = infernoManager.NewManager(system, optimizer)

			engine = NewVariantAutoscalingsEngine(manager, system)
			modelAnalyzer = analyzer.NewModelAnalyzer(system)
			Expect(engine).NotTo(BeNil())
			Expect(modelAnalyzer).NotTo(BeNil())

			// Analyze
			By("Analyzing step")
			for _, s := range system.Servers() {
				modelAnalyzeResponse := modelAnalyzer.AnalyzeModel(ctx, *vaMap[s.Name()])
				Expect(len(modelAnalyzeResponse.Allocations)).To(BeNumerically(">", 0), "Expected at least one allocation from model analyzer for server - ", s.Name())
				allAnalyzerResponses[s.Name()] = modelAnalyzeResponse
			}

			By("Performing optimization")
			scaleToZeroConfigData := make(utils.ScaleToZeroConfigData)
			metricsCache := collector.NewModelMetricsCache()
			optimizedAllocs, err := engine.Optimize(ctx, updateList, allAnalyzerResponses, &scaleToZeroConfigData, metricsCache)
			Expect(err).NotTo(HaveOccurred(), "unable to perform model optimization")
			Expect(len(optimizedAllocs)).To(Equal(len(updateList.Items)), "Expected optimized allocations for all VariantAutoscalings")
			for key, value := range optimizedAllocs {
				logger.Log.Info("Optimized allocation entry - ", "key: ", key, ", value: ", value)
				Expect(value.NumReplicas).To(Equal(minNumReplicas), fmt.Sprintf("Expected optimized number of replicas to be %d under no load for VariantAutoscaling - %s", minNumReplicas, key))
			}
		})

		It("should perform optimization for multiple VariantAutoscalings - scale out under load pressure", func() {
			allAnalyzerResponses := make(map[string]*interfaces.ModelAnalyzeResponse)
			vaMap := make(map[string]*llmdVariantAutoscalingV1alpha1.VariantAutoscaling)

			// Setup MockPromAPI with high load metrics to simulate load pressure
			mockProm := &testutils.MockPromAPI{
				QueryResults: make(map[string]model.Value),
				QueryErrors:  make(map[string]error),
			}

			By("Populating VariantAutoscalings map from the cluster")
			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			Expect(k8sClient.List(ctx, &variantAutoscalingList)).To(Succeed())
			Expect(len(variantAutoscalingList.Items)).To(BeNumerically(">", 0), "no VariantAutoscalings found in the cluster")

			// Prepare list of VariantAutoscalings to be updated after optimization
			By("Preparing list of VariantAutoscalings to be updated after optimization")
			var updateList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList

			// Filter out deleted VariantAutoscalings
			By("Filtering out deleted VariantAutoscalings")
			activeVAs := make([]llmdVariantAutoscalingV1alpha1.VariantAutoscaling, 0, len(variantAutoscalingList.Items))
			for _, va := range variantAutoscalingList.Items {
				if va.DeletionTimestamp.IsZero() {
					activeVAs = append(activeVAs, va)
				}
			}

			// Prepare system data with all VariantAutoscalings info
			By("Preparing system data with all VariantAutoscalings info")
			for _, va := range activeVAs {
				modelName := va.Spec.ModelID
				Expect(modelName).NotTo(BeEmpty(), "variantAutoscaling missing modelName label, skipping optimization - ", "variantAutoscaling-name: ", va.Name)

				_, className, err := utils.FindModelSLO(serviceClassCm, modelName)
				Expect(err).NotTo(HaveOccurred(), "failed to find model SLO for model - ", modelName, ", variantAutoscaling - ", va.Name)

				for _, modelAcceleratorProfile := range va.Spec.ModelProfile.Accelerators {
					err = utils.AddModelAcceleratorProfileToSystemData(systemData, modelName, &modelAcceleratorProfile)
					Expect(err).NotTo(HaveOccurred(), "failed to add model accelerator profile to system data for model - ", modelName, ", variantAutoscaling - ", va.Name)
				}

				accName := va.Labels["inference.optimization/acceleratorName"]
				Expect(accName).NotTo(BeEmpty(), "variantAutoscaling missing acceleratorName label, skipping optimization - ", "variantAutoscaling-name: ", va.Name)
				acceleratorCostVal, ok := acceleratorCm[accName]["cost"]
				Expect(ok).NotTo(BeFalse(), "variantAutoscaling missing accelerator cost in configMap, skipping optimization - ", "variantAutoscaling-name: ", va.Name)
				acceleratorCostValFloat, err := strconv.ParseFloat(acceleratorCostVal, 32)
				Expect(err).NotTo(HaveOccurred(), "failed to parse accelerator cost value to float for variantAutoscaling - ", "variantAutoscaling-name: ", va.Name)

				var deploy appsv1.Deployment
				err = utils.GetDeploymentWithBackoff(ctx, k8sClient, va.Name, va.Namespace, &deploy)
				Expect(err).NotTo(HaveOccurred(), "failed to get deployment for variantAutoscaling - ", "variantAutoscaling-name: ", va.Name)

				var updateVA llmdVariantAutoscalingV1alpha1.VariantAutoscaling
				err = utils.GetVariantAutoscalingWithBackoff(ctx, k8sClient, deploy.Name, deploy.Namespace, &updateVA)
				Expect(err).NotTo(HaveOccurred(), "failed to get variantAutoscaling for deployment - ", "deployment-name: ", deploy.Name)

				// Setup high load metrics for simulation
				testNamespace := va.Namespace
				arrivalQuery := testutils.CreateArrivalQuery(modelName, testNamespace)
				avgDecToksQuery := testutils.CreateTokenQuery(modelName, testNamespace)
				ttftQuery := testutils.CreateWaitQuery(modelName, testNamespace)
				itlQuery := testutils.CreateITLQuery(modelName, testNamespace)

				// High load metrics that should trigger scaling up
				mockProm.QueryResults[arrivalQuery] = model.Vector{
					&model.Sample{Value: model.SampleValue(40.0)}, // 40 requests/min - high load
				}
				mockProm.QueryResults[avgDecToksQuery] = model.Vector{
					&model.Sample{Value: model.SampleValue(200.0)},
				}
				mockProm.QueryResults[ttftQuery] = model.Vector{
					&model.Sample{Value: model.SampleValue(0.02)},
				}
				mockProm.QueryResults[itlQuery] = model.Vector{
					&model.Sample{Value: model.SampleValue(0.008)},
				}

				testMetricsCache := collector.NewModelMetricsCache()
				currentAllocation, err := collector.AddMetricsToOptStatus(ctx, &updateVA, deploy, acceleratorCostValFloat, mockProm, testMetricsCache, 10*time.Minute)
				Expect(err).NotTo(HaveOccurred(), "unable to fetch metrics and add to Optimizer status for variantAutoscaling - ", "variantAutoscaling-name: ", va.Name)
				updateVA.Status.CurrentAlloc = currentAllocation

				// For tests, pass empty scale-to-zero config data - will use global defaults
				scaleToZeroConfigData := make(utils.ScaleToZeroConfigData)
				err = utils.AddServerInfoToSystemData(systemData, &updateVA, className, scaleToZeroConfigData)
				Expect(err).NotTo(HaveOccurred(), "failed to add server info to system data for variantAutoscaling - ", "variantAutoscaling-name: ", va.Name)

				By("Updating system data with VariantAutoscaling info")
				vaFullName := utils.FullName(va.Name, va.Namespace)
				updateList.Items = append(updateList.Items, updateVA)
				vaMap[vaFullName] = &va
			}

			system = inferno.NewSystem()
			optimizerSpec := system.SetFromSpec(&systemData.Spec)
			optimizer = infernoSolver.NewOptimizerFromSpec(optimizerSpec)
			manager = infernoManager.NewManager(system, optimizer)

			engine = NewVariantAutoscalingsEngine(manager, system)
			modelAnalyzer = analyzer.NewModelAnalyzer(system)
			Expect(engine).NotTo(BeNil())
			Expect(modelAnalyzer).NotTo(BeNil())

			// Analyze
			By("Analyzing step")
			for _, s := range system.Servers() {
				modelAnalyzeResponse := modelAnalyzer.AnalyzeModel(ctx, *vaMap[s.Name()])
				Expect(len(modelAnalyzeResponse.Allocations)).To(BeNumerically(">", 0), "Expected at least one allocation from model analyzer for server - ", s.Name())
				allAnalyzerResponses[s.Name()] = modelAnalyzeResponse
			}

			By("Performing optimization")
			scaleToZeroConfigData := make(utils.ScaleToZeroConfigData)
			metricsCache := collector.NewModelMetricsCache()
			optimizedAllocs, err := engine.Optimize(ctx, updateList, allAnalyzerResponses, &scaleToZeroConfigData, metricsCache)
			Expect(err).NotTo(HaveOccurred(), "unable to perform model optimization")
			Expect(len(optimizedAllocs)).To(Equal(len(updateList.Items)), "Expected optimized allocations for all VariantAutoscalings")
			for key, value := range optimizedAllocs {
				logger.Log.Info("Optimized allocation entry - ", "key: ", key, ", value: ", value)
				Expect(value.NumReplicas).To(BeNumerically(">", 1), "Expected optimized number of replicas to be higher than 1 under high load for VariantAutoscaling - ", key)
			}
		})
	})
})

var _ = Describe("Zero-Rate Handling Edge Cases", func() {
	var (
		ctx                   context.Context
		engine                *VariantAutoscalingsEngine
		manager               *infernoManager.Manager
		system                *inferno.System
		allocationSolution    *infernoConfig.AllocationSolution
		metricsCache          *collector.ModelMetricsCache
		scaleToZeroConfigData utils.ScaleToZeroConfigData
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Setup minimal inferno system using the same pattern as the main tests
		systemData := &infernoConfig.SystemData{
			Spec: infernoConfig.SystemSpec{
				Optimizer: infernoConfig.OptimizerData{
					Spec: infernoConfig.OptimizerSpec{},
				},
				Servers: infernoConfig.ServerData{
					Spec: []infernoConfig.ServerSpec{},
				},
			},
		}
		system = inferno.NewSystem()
		optimizerSpec := system.SetFromSpec(&systemData.Spec)
		optimizer := infernoSolver.NewOptimizerFromSpec(optimizerSpec)
		manager = infernoManager.NewManager(system, optimizer)
		engine = NewVariantAutoscalingsEngine(manager, system)

		// Setup metrics cache
		metricsCache = collector.NewModelMetricsCache()

		// Setup scale-to-zero config
		scaleToZeroConfigData = make(utils.ScaleToZeroConfigData)

		// Setup allocation solution
		allocationSolution = &infernoConfig.AllocationSolution{
			Spec: make(map[string]infernoConfig.AllocationData),
		}
	})

	Context("Nil parameter handling", func() {
		It("should handle nil scaleToZeroConfigData gracefully without panic", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "variant1",
							Namespace: "default",
						},
						Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
							ModelID: "model1",
						},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{
				NumReplicas: 0,
				Cost:        1.0,
			}

			// Should not panic with nil scaleToZeroConfigData
			Expect(func() {
				engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, nil, metricsCache)
			}).NotTo(Panic())

			// Should keep one replica (scale-to-zero disabled due to nil)
			Expect(allocationSolution.Spec["variant1:default"].NumReplicas).To(Equal(1))
		})

		It("should handle nil metricsCache gracefully without panic", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "variant1",
							Namespace: "default",
						},
						Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
							ModelID: "model1",
						},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{
				NumReplicas: 0,
				Cost:        1.0,
			}

			// Should not panic with nil metricsCache
			Expect(func() {
				engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, nil)
			}).NotTo(Panic())

			// Should keep one replica (totalRequests = 0 but scale-to-zero not enabled)
			Expect(allocationSolution.Spec["variant1:default"].NumReplicas).To(Equal(1))
		})

		It("should handle both nil parameters gracefully", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "variant1",
							Namespace: "default",
						},
						Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
							ModelID: "model1",
						},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{
				NumReplicas: 0,
				Cost:        1.0,
			}

			Expect(func() {
				engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, nil, nil)
			}).NotTo(Panic())

			Expect(allocationSolution.Spec["variant1:default"].NumReplicas).To(Equal(1))
		})
	})

	Context("Cost-based selection", func() {
		It("should select cheapest variant when all have zero replicas", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "expensive-variant", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "cheap-variant", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["expensive-variant:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 5.0}
			allocationSolution.Spec["cheap-variant:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Cheapest variant should get 1 replica
			Expect(allocationSolution.Spec["cheap-variant:default"].NumReplicas).To(Equal(1))
			Expect(allocationSolution.Spec["expensive-variant:default"].NumReplicas).To(Equal(0))
		})

		It("should handle variants with same cost by selecting first one found", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant2", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			// Both variants have same cost
			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 2.0}
			allocationSolution.Spec["variant2:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 2.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Exactly one variant should have 1 replica
			total := allocationSolution.Spec["variant1:default"].NumReplicas +
				allocationSolution.Spec["variant2:default"].NumReplicas
			Expect(total).To(Equal(1))
		})

		It("should fallback to first variant when cost info is missing", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant2", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			// No allocation solution entries - missing cost info
			variants := []*llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				&vaList.Items[0],
				&vaList.Items[1],
			}

			selected := engine.selectVariantToKeep(variants, allocationSolution)

			// Should fallback to first variant
			Expect(selected).NotTo(BeNil())
			Expect(selected.Name).To(Equal("variant1"))
		})
	})

	Context("Scale-to-zero scenarios", func() {
		It("should scale to zero when enabled and no recent requests", func() {
			// Enable scale-to-zero for model1
			enabled := true
			scaleToZeroConfigData["model1"] = utils.ModelScaleToZeroConfig{
				ModelID:           "model1",
				EnableScaleToZero: &enabled,
				RetentionPeriod:   "10m",
			}

			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}

			// No metrics in cache (totalRequests = 0)
			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Should scale to zero
			Expect(allocationSolution.Spec["variant1:default"].NumReplicas).To(Equal(0))
		})

		It("should keep one replica when scale-to-zero enabled but has recent requests", func() {
			// Enable scale-to-zero for model1
			enabled := true
			scaleToZeroConfigData["model1"] = utils.ModelScaleToZeroConfig{
				ModelID:           "model1",
				EnableScaleToZero: &enabled,
				RetentionPeriod:   "10m",
			}

			// Add metrics showing recent requests
			metricsCache.Set("model1", &collector.ModelMetrics{
				TotalRequestsOverRetentionPeriod: 10.0,
				RetentionPeriod:                  10 * time.Minute,
			})

			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Should keep one replica due to recent requests
			Expect(allocationSolution.Spec["variant1:default"].NumReplicas).To(Equal(1))
		})

		It("should keep one replica when scale-to-zero disabled regardless of request count", func() {
			// Disable scale-to-zero for model1
			disabled := false
			scaleToZeroConfigData["model1"] = utils.ModelScaleToZeroConfig{
				ModelID:           "model1",
				EnableScaleToZero: &disabled,
				RetentionPeriod:   "10m",
			}

			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}

			// No requests in metrics
			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Should keep one replica (scale-to-zero disabled)
			Expect(allocationSolution.Spec["variant1:default"].NumReplicas).To(Equal(1))
		})

		It("should scale all variants to zero when enabled and no requests", func() {
			// Enable scale-to-zero
			enabled := true
			scaleToZeroConfigData["model1"] = utils.ModelScaleToZeroConfig{
				ModelID:           "model1",
				EnableScaleToZero: &enabled,
				RetentionPeriod:   "10m",
			}

			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant2", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}
			allocationSolution.Spec["variant2:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 2.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Both should scale to zero
			Expect(allocationSolution.Spec["variant1:default"].NumReplicas).To(Equal(0))
			Expect(allocationSolution.Spec["variant2:default"].NumReplicas).To(Equal(0))
		})
	})

	Context("Variant selection priority", func() {
		It("should prefer variant with current replicas over cheaper variant", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "expensive-with-replicas", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 2}, // Has replicas
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "cheap-without-replicas", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0}, // No replicas
						},
					},
				},
			}

			allocationSolution.Spec["expensive-with-replicas:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 5.0}
			allocationSolution.Spec["cheap-without-replicas:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Should select expensive variant (has current replicas) even though cheap variant has lower cost
			Expect(allocationSolution.Spec["expensive-with-replicas:default"].NumReplicas).To(Equal(1))
			Expect(allocationSolution.Spec["cheap-without-replicas:default"].NumReplicas).To(Equal(0))
		})

		It("should select cheapest when multiple variants have non-zero replicas", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "expensive-variant", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 1},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "cheap-variant", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 2},
						},
					},
				},
			}

			allocationSolution.Spec["expensive-variant:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 5.0}
			allocationSolution.Spec["cheap-variant:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Should select cheapest among those with replicas
			Expect(allocationSolution.Spec["cheap-variant:default"].NumReplicas).To(Equal(1))
			Expect(allocationSolution.Spec["expensive-variant:default"].NumReplicas).To(Equal(0))
		})

		It("should keep only one variant when multiple models exist", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "model1-variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "model1-variant2", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "model2-variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model2"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["model1-variant1:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 2.0}
			allocationSolution.Spec["model1-variant2:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}
			allocationSolution.Spec["model2-variant1:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Each model should have exactly one variant with 1 replica
			model1Total := allocationSolution.Spec["model1-variant1:default"].NumReplicas +
				allocationSolution.Spec["model1-variant2:default"].NumReplicas
			model2Total := allocationSolution.Spec["model2-variant1:default"].NumReplicas

			Expect(model1Total).To(Equal(1))
			Expect(model2Total).To(Equal(1))
		})
	})

	Context("Empty list edge cases", func() {
		It("should handle empty variant list gracefully", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{},
			}

			Expect(func() {
				engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)
			}).NotTo(Panic())
		})

		It("should return nil when selecting from empty variant list", func() {
			variants := []*llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}

			selected := engine.selectVariantToKeep(variants, allocationSolution)

			Expect(selected).To(BeNil())
		})

		It("should return single variant when only one exists", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "only-variant", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			allocationSolution.Spec["only-variant:default"] = infernoConfig.AllocationData{NumReplicas: 0, Cost: 1.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Single variant should get 1 replica
			Expect(allocationSolution.Spec["only-variant:default"].NumReplicas).To(Equal(1))
		})
	})

	Context("Optimizer allocation preservation", func() {
		It("should not modify allocations when optimizer already allocated non-zero replicas", func() {
			vaList := llmdVariantAutoscalingV1alpha1.VariantAutoscalingList{
				Items: []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "variant1", Namespace: "default"},
						Spec:       llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{ModelID: "model1"},
						Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
							CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{NumReplicas: 0},
						},
					},
				},
			}

			// Optimizer already allocated 3 replicas
			allocationSolution.Spec["variant1:default"] = infernoConfig.AllocationData{NumReplicas: 3, Cost: 1.0}

			engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, &scaleToZeroConfigData, metricsCache)

			// Should preserve optimizer's decision
			Expect(allocationSolution.Spec["variant1:default"].NumReplicas).To(Equal(3))
		})
	})
})
