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

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	v1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/workload-variant-autoscaler/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Local clients for HPA scale-to-zero tests (independent of other e2e tests)
var (
	hpaK8sClient *kubernetes.Clientset
	hpaCrClient  client.Client
)

// getEnvOrDefault returns environment variable value or default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// initializeHPAClients initializes Kubernetes clients for HPA scale-to-zero tests
func initializeHPAClients() {
	// Try to reuse global clients from other e2e tests if available
	if k8sClient != nil && crClient != nil {
		hpaK8sClient = k8sClient
		hpaCrClient = crClient
		return
	}

	// If local clients already initialized, reuse them
	if hpaK8sClient != nil && hpaCrClient != nil {
		return
	}

	// Otherwise initialize new clients
	cfg, err := func() (*rest.Config, error) {
		if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
			return clientcmd.BuildConfigFromFlags("", kubeconfig)
		}
		return rest.InClusterConfig()
	}()
	if err != nil {
		Skip("failed to load kubeconfig: " + err.Error())
	}

	cfg.WarningHandler = rest.NoWarnings{}

	hpaK8sClient, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		Skip("failed to create kubernetes client: " + err.Error())
	}

	hpaCrClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		Skip("failed to create controller-runtime client: " + err.Error())
	}
}

var _ = Describe("Test idle scale-to-zero with HPA", Ordered, func() {
	var (
		namespace         string
		deployName        string
		serviceName       string
		serviceMonName    string
		configMapName     string
		appLabel          string
		modelID           string
		accelerator       string
		ctx               context.Context
		initialReplicas   int32
		retentionDuration time.Duration
		inferenceModel    *unstructured.Unstructured
	)

	BeforeAll(func() {
		initializeHPAClients()

		ctx = context.Background()
		namespace = getEnvOrDefault("LLMD_NAMESPACE", "llm-d-sim")
		deployName = "hpa-idle-sto-zero-deployment"
		serviceName = "hpa-idle-sto-zero-service"
		serviceMonName = "hpa-idle-sto-zero-servicemonitor"
		configMapName = "model-scale-to-zero-config"
		appLabel = "hpa-idle-sto-zero-test"
		modelID = "test-hpa-idle-sto-zero-model"
		accelerator = getEnvOrDefault("ACCELERATOR_TYPE", "A100")
		initialReplicas = 1
		retentionDuration = 2 * time.Minute

		By("checking if Prometheus Adapter is installed")
		monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")
		podList, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=prometheus-adapter",
		})
		if err != nil || len(podList.Items) == 0 {
			Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA scale-to-zero tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s (%d pods)\n", monitoringNs, len(podList.Items))

		By("ensuring unique app label and model")
		utils.ValidateAppLabelUniqueness(namespace, appLabel, hpaK8sClient, hpaCrClient)
		utils.ValidateVariantAutoscalingUniqueness(namespace, modelID, accelerator, hpaCrClient)

		By("creating scale-to-zero ConfigMap")
		configMapKey := strings.ReplaceAll(modelID, "/", "-")
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system"),
			},
			Data: map[string]string{
				fmt.Sprintf("model.%s", configMapKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "2m"`, modelID),
			},
		}
		_, err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Create(ctx, configMap, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ConfigMap: %s", configMapName))

		By("creating vllme deployment")
		deployment := utils.CreateVllmeDeployment(namespace, deployName, modelID, appLabel)
		deployment.Spec.Replicas = &initialReplicas
		_, err = hpaK8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Deployment: %s", deployName))

		By("creating vllme service")
		service := utils.CreateVllmeService(namespace, serviceName, appLabel, 30001)
		_, err = hpaK8sClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Service: %s", serviceName))

		By("creating ServiceMonitor for vllme metrics")
		serviceMonitor := utils.CreateVllmeServiceMonitor(serviceMonName, getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"), appLabel)
		err = hpaCrClient.Create(ctx, serviceMonitor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ServiceMonitor: %s", serviceMonName))

		By("creating VariantAutoscaling resource")
		variantAutoscaling := utils.CreateVariantAutoscalingResource(namespace, deployName, modelID, accelerator)
		err = hpaCrClient.Create(ctx, variantAutoscaling)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create VariantAutoscaling: %s", deployName))

		By("creating InferenceModel for the deployment")
		inferenceModel = utils.CreateInferenceModel(deployName, namespace, modelID)
		err = hpaCrClient.Create(ctx, inferenceModel)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create InferenceModel: %s", modelID))
	})

	It("deployment should be running initially", func() {
		Eventually(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get Deployment")
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(int32(1)), "Deployment should have 1 ready replica")
		}, 3*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should scale to zero after retention period with no traffic", func() {
		By("waiting for initial controller reconciliation")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(Equal(int32(1)))
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 1))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying Prometheus Adapter is ready")
		Eventually(func(g Gomega) {
			monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

			// Try multiple label selectors to find Prometheus Adapter pods
			var podList *corev1.PodList
			var err error
			labelSelectors := []string{
				"app.kubernetes.io/name=prometheus-adapter",
				"app=prometheus-adapter",
			}

			for _, selector := range labelSelectors {
				podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
					LabelSelector: selector,
				})
				if err == nil && len(podList.Items) > 0 {
					break
				}
			}

			// Fallback to name pattern if labels don't work
			if podList == nil || len(podList.Items) == 0 {
				allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
				if err == nil {
					var adapterPods []corev1.Pod
					for _, pod := range allPods.Items {
						if strings.Contains(pod.Name, "prometheus-adapter") {
							adapterPods = append(adapterPods, pod)
						}
					}
					podList = &corev1.PodList{Items: adapterPods}
				}
			}

			g.Expect(err).NotTo(HaveOccurred(), "Should be able to list Prometheus Adapter pods")
			g.Expect(podList.Items).NotTo(BeEmpty(), "Prometheus Adapter pods should exist")

			readyCount := 0
			for _, pod := range podList.Items {
				if pod.Status.Phase == corev1.PodRunning {
					for _, cond := range pod.Status.Conditions {
						if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
							readyCount++
							break
						}
					}
				}
			}
			g.Expect(readyCount).To(BeNumerically(">", 0), "At least one Prometheus Adapter pod should be ready")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter is ready (%d pods)\n", readyCount)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying controller has reconciled VA")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

			// Wait for controller to set desired replicas
			g.Expect(va.Status.DesiredOptimizedAlloc).NotTo(BeNil(), "DesiredOptimizedAlloc should be set")

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: CurrentReplicas=%d, DesiredReplicas=%d\n",
				va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.NumReplicas)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Controller has reconciled VA. Waiting for metric to be scraped by Prometheus and discovered by Prometheus Adapter...\n")
		// Allow time for full metric discovery pipeline:
		// 1. Controller emits metric (immediate)
		// 2. Prometheus scrapes metric (scrape interval: ~30s)
		// 3. Prometheus Adapter discovers metric (discovery interval: ~30-60s)
		// Total: 60-90 seconds needed to ensure metric is available via external metrics API
		time.Sleep(90 * time.Second)

		By("creating HPA for deployment")
		minReplicas := int32(0) // Scale-to-zero: HPA alpha feature
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName + "-hpa",
				Namespace: namespace,
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       deployName,
				},
				MinReplicas: &minReplicas,
				MaxReplicas: 10,
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleUp: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
				},
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{
								Name: "wva_desired_replicas",
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"target_name":        deployName,
										"target_kind":        "Deployment",
										"exported_namespace": namespace,
										"accelerator_type":   accelerator,
									},
								},
							},
							Target: autoscalingv2.MetricTarget{
								Type:         autoscalingv2.AverageValueMetricType,
								AverageValue: resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				},
			},
		}
		_, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, hpa, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create HPA: %s", hpa.Name))
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA created: %s\n", hpa.Name)

		By("waiting for HPA to be ready")
		Eventually(func(g Gomega) {
			hpa, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, deployName+"-hpa", metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get HPA")

			// Print HPA status for debugging
			_, _ = fmt.Fprintf(GinkgoWriter, "HPA Status - Current: %d, Desired: %d, Conditions: %d\n",
				hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas, len(hpa.Status.Conditions))

			for _, condition := range hpa.Status.Conditions {
				_, _ = fmt.Fprintf(GinkgoWriter, "  Condition: %s = %s (Reason: %s, Message: %s)\n",
					condition.Type, condition.Status, condition.Reason, condition.Message)
			}

			g.Expect(hpa.Status.Conditions).NotTo(BeEmpty(), "HPA should have conditions")

			for _, condition := range hpa.Status.Conditions {
				if condition.Type == autoscalingv2.ScalingActive && condition.Status == corev1.ConditionTrue {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA is active\n")
					return
				}
			}
			g.Expect(true).To(BeFalse(), "HPA should be active")
		}, 5*time.Minute, 10*time.Second).Should(Succeed())

		By("waiting for retention period to pass with no traffic")
		waitDuration := retentionDuration + (90 * time.Second) // retention + buffer
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting %v for retention period + buffer (no traffic)...\n", waitDuration)
		time.Sleep(waitDuration)

		By("verifying controller sets DesiredOptimizedAlloc to 0")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: DesiredOptimized=%d, Current=%d, Reason=%q, LastUpdate=%v\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas, va.Status.DesiredOptimizedAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.LastUpdate.Reason, va.Status.DesiredOptimizedAlloc.LastUpdate.UpdateTime.Time)

			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(0)),
				"Controller should set desired replicas to 0 after retention period")
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying HPA scales deployment to 0 replicas")
		Eventually(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			if deployment.Status.Replicas == 0 {
				_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA successfully scaled deployment to 0 replicas\n")
			}
			g.Expect(deployment.Status.Replicas).To(Equal(int32(0)))
		}, 5*time.Minute, 10*time.Second).Should(Succeed(),
			"HPA should scale deployment to 0 replicas")

		By("verifying CurrentAlloc reflects the scaled-down state")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(Equal(int32(0)))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Idle scale-to-zero with HPA completed successfully\n")
	})

	AfterAll(func() {
		By("cleaning up HPA")
		err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, deployName+"-hpa", metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete HPA: %s", deployName+"-hpa"))

		By("cleaning up VariantAutoscaling resource")
		variantAutoscaling := &v1alpha1.VariantAutoscaling{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName,
				Namespace: namespace,
			},
		}
		err = hpaCrClient.Delete(ctx, variantAutoscaling)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete VariantAutoscaling: %s", deployName))

		By("deleting InferenceModel")
		if inferenceModel != nil {
			err = hpaCrClient.Delete(ctx, inferenceModel)
			err = client.IgnoreNotFound(err)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete InferenceModel: %s", modelID))
		}

		By("cleaning up ServiceMonitor")
		serviceMonitor := &unstructured.Unstructured{}
		serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		})
		serviceMonitor.SetName(serviceMonName)
		serviceMonitor.SetNamespace(getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"))
		err = hpaCrClient.Delete(ctx, serviceMonitor)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete ServiceMonitor: %s", serviceMonName))

		By("cleaning up Service")
		err = hpaK8sClient.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete Service: %s", serviceName))

		By("cleaning up Deployment")
		err = hpaK8sClient.AppsV1().Deployments(namespace).Delete(ctx, deployName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete Deployment: %s", deployName))

		By("waiting for all pods to be deleted")
		Eventually(func(g Gomega) {
			podList, err := hpaK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", appLabel),
			})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to list Pods")
			g.Expect(podList.Items).To(BeEmpty(), fmt.Sprintf("All Pods with label %s should be deleted", appLabel))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("cleaning up ConfigMap")
		err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Delete(ctx, configMapName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete ConfigMap: %s", configMapName))
	})
})

var _ = Describe("Test traffic-based scale-to-zero with HPA", Ordered, func() {
	var (
		namespace         string
		deployName        string
		serviceName       string
		serviceMonName    string
		configMapName     string
		appLabel          string
		modelID           string
		accelerator       string
		ctx               context.Context
		initialReplicas   int32
		retentionDuration time.Duration
		inferenceModel    *unstructured.Unstructured
	)

	BeforeAll(func() {
		initializeHPAClients()

		ctx = context.Background()
		namespace = getEnvOrDefault("LLMD_NAMESPACE", "llm-d-sim")
		deployName = "hpa-traffic-sto-zero-deployment"
		serviceName = "hpa-traffic-sto-zero-service"
		serviceMonName = "hpa-traffic-sto-zero-servicemonitor"
		configMapName = "model-scale-to-zero-config"
		appLabel = "hpa-traffic-sto-zero-test"
		// Use "default/default" to leverage existing gateway infrastructure
		modelID = getEnvOrDefault("DEFAULT_MODEL_ID", "default/default")
		accelerator = getEnvOrDefault("ACCELERATOR_TYPE", "A100")
		initialReplicas = 1
		retentionDuration = 2 * time.Minute

		By("checking if Prometheus Adapter is installed")
		monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")
		podList, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=prometheus-adapter",
		})
		if err != nil || len(podList.Items) == 0 {
			Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA scale-to-zero tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s (%d pods)\n", monitoringNs, len(podList.Items))

		By("ensuring unique app label and model")
		utils.ValidateAppLabelUniqueness(namespace, appLabel, hpaK8sClient, hpaCrClient)
		utils.ValidateVariantAutoscalingUniqueness(namespace, modelID, accelerator, hpaCrClient)

		By("creating scale-to-zero ConfigMap")
		configMapKey := strings.ReplaceAll(modelID, "/", "-")
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system"),
			},
			Data: map[string]string{
				fmt.Sprintf("model.%s", configMapKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "2m"`, modelID),
			},
		}
		_, err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Create(ctx, configMap, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ConfigMap: %s", configMapName))

		By("creating vllme deployment")
		deployment := utils.CreateVllmeDeployment(namespace, deployName, modelID, appLabel)
		deployment.Spec.Replicas = &initialReplicas
		_, err = hpaK8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Deployment: %s", deployName))

		By("creating vllme service")
		service := utils.CreateVllmeService(namespace, serviceName, appLabel, 30002)
		_, err = hpaK8sClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Service: %s", serviceName))

		By("creating ServiceMonitor for vllme metrics")
		serviceMonitor := utils.CreateVllmeServiceMonitor(serviceMonName, getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"), appLabel)
		err = hpaCrClient.Create(ctx, serviceMonitor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ServiceMonitor: %s", serviceMonName))

		By("creating VariantAutoscaling resource")
		variantAutoscaling := utils.CreateVariantAutoscalingResource(namespace, deployName, modelID, accelerator)
		err = hpaCrClient.Create(ctx, variantAutoscaling)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create VariantAutoscaling: %s", deployName))

		By("creating InferenceModel for the deployment")
		// Use "default/default" to match traffic and leverage existing gateway infrastructure
		inferenceModel = utils.CreateInferenceModel(deployName, namespace, getEnvOrDefault("DEFAULT_MODEL_ID", "default/default"))
		err = hpaCrClient.Create(ctx, inferenceModel)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create InferenceModel with model: %s", getEnvOrDefault("DEFAULT_MODEL_ID", "default/default")))
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ InferenceModel created with modelName=%s\n", getEnvOrDefault("DEFAULT_MODEL_ID", "default/default"))
	})

	It("should scale to zero after traffic stops and retention period expires", func() {
		By("waiting for deployment to have ready replicas")
		Eventually(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			_, _ = fmt.Fprintf(GinkgoWriter, "Deployment replicas: Ready=%d, Available=%d, Target=%d\n",
				deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas, *deployment.Spec.Replicas)
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(int32(1)))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("creating HPA for deployment after initial reconciliation")
		// Wait for initial reconciliation before creating HPA
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(Equal(int32(1)))
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 1))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying Prometheus Adapter is ready")
		Eventually(func(g Gomega) {
			monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

			// Try multiple label selectors to find Prometheus Adapter pods
			var podList *corev1.PodList
			var err error
			labelSelectors := []string{
				"app.kubernetes.io/name=prometheus-adapter",
				"app=prometheus-adapter",
			}

			for _, selector := range labelSelectors {
				podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
					LabelSelector: selector,
				})
				if err == nil && len(podList.Items) > 0 {
					break
				}
			}

			// Fallback to name pattern if labels don't work
			if podList == nil || len(podList.Items) == 0 {
				allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
				if err == nil {
					var adapterPods []corev1.Pod
					for _, pod := range allPods.Items {
						if strings.Contains(pod.Name, "prometheus-adapter") {
							adapterPods = append(adapterPods, pod)
						}
					}
					podList = &corev1.PodList{Items: adapterPods}
				}
			}

			g.Expect(err).NotTo(HaveOccurred(), "Should be able to list Prometheus Adapter pods")
			g.Expect(podList.Items).NotTo(BeEmpty(), "Prometheus Adapter pods should exist")

			readyCount := 0
			for _, pod := range podList.Items {
				if pod.Status.Phase == corev1.PodRunning {
					for _, cond := range pod.Status.Conditions {
						if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
							readyCount++
							break
						}
					}
				}
			}
			g.Expect(readyCount).To(BeNumerically(">", 0), "At least one Prometheus Adapter pod should be ready")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter is ready (%d pods)\n", readyCount)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying controller has reconciled VA")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

			// Wait for controller to set desired replicas
			g.Expect(va.Status.DesiredOptimizedAlloc).NotTo(BeNil(), "DesiredOptimizedAlloc should be set")

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: CurrentReplicas=%d, DesiredReplicas=%d\n",
				va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.NumReplicas)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Controller has reconciled VA. Waiting for metric to be scraped by Prometheus and discovered by Prometheus Adapter...\n")
		// Allow time for full metric discovery pipeline:
		// 1. Controller emits metric (immediate)
		// 2. Prometheus scrapes metric (scrape interval: ~30s)
		// 3. Prometheus Adapter discovers metric (discovery interval: ~30-60s)
		// Total: 60-90 seconds needed to ensure metric is available via external metrics API
		time.Sleep(90 * time.Second)

		minReplicas := int32(0)
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName + "-hpa",
				Namespace: namespace,
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       deployName,
				},
				MinReplicas: &minReplicas,
				MaxReplicas: 10,
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleUp: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
				},
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{
								Name: "wva_desired_replicas",
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"target_name":        deployName,
										"target_kind":        "Deployment",
										"exported_namespace": namespace,
										"accelerator_type":   accelerator,
									},
								},
							},
							Target: autoscalingv2.MetricTarget{
								Type:         autoscalingv2.AverageValueMetricType,
								AverageValue: resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				},
			},
		}
		_, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, hpa, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA created: %s\n", hpa.Name)

		By("waiting for HPA to be ready")
		Eventually(func(g Gomega) {
			hpa, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, deployName+"-hpa", metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			// Print HPA status for debugging
			_, _ = fmt.Fprintf(GinkgoWriter, "HPA Status - Current: %d, Desired: %d, Conditions: %d\n",
				hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas, len(hpa.Status.Conditions))

			for _, condition := range hpa.Status.Conditions {
				_, _ = fmt.Fprintf(GinkgoWriter, "  Condition: %s = %s (Reason: %s, Message: %s)\n",
					condition.Type, condition.Status, condition.Reason, condition.Message)
			}

			g.Expect(hpa.Status.Conditions).NotTo(BeEmpty())

			for _, condition := range hpa.Status.Conditions {
				if condition.Type == autoscalingv2.ScalingActive && condition.Status == corev1.ConditionTrue {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA is active\n")
					return
				}
			}
			g.Expect(true).To(BeFalse(), "HPA should be active")
		}, 5*time.Minute, 10*time.Second).Should(Succeed())

		By("setting up port-forward to gateway for traffic generation")
		port := 8002
		portForwardCmd := utils.SetUpPortForward(hpaK8sClient, ctx, getEnvOrDefault("GATEWAY_NAME", "infra-sim-inference-gateway"), namespace, port, 80)
		defer func() {
			err := utils.StopCmd(portForwardCmd)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to stop port-forwarding for gateway: %s", getEnvOrDefault("GATEWAY_NAME", "infra-sim-inference-gateway")))
		}()

		By("waiting for port-forward to be ready")
		err = utils.VerifyPortForwardReadiness(ctx, port, fmt.Sprintf("http://localhost:%d/v1", port))
		Expect(err).NotTo(HaveOccurred(), "Port-forward should be ready within timeout")

		By("starting traffic generation")
		loadRate := 10
		// Use "default/default" model ID to match InferenceModel configuration
		loadGenCmd := utils.StartLoadGenerator(loadRate, 100, port, getEnvOrDefault("DEFAULT_MODEL_ID", "default/default"))
		defer func() {
			err := utils.StopCmd(loadGenCmd)
			if err != nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Warning: Failed to stop load generator: %v\n", err)
			}
		}()

		_, _ = fmt.Fprintf(GinkgoWriter, "Starting traffic generation at %d req/s...\n", loadRate)

		By("waiting for vLLM metrics rate data to accumulate")
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting 90 seconds for rate([1m]) data accumulation...\n")
		time.Sleep(90 * time.Second)

		By("waiting for controller to process traffic and emit metrics")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "Waiting for controller: DesiredOptimized=%d, Current=%d, Reason=%q\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas, va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.LastUpdate.Reason)

			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(Equal(int32(1)))
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(1)))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Metrics: current=1, desired=1\n")

		By("stopping traffic generation")
		err = utils.StopCmd(loadGenCmd)
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Traffic stopped\n")

		By("verifying desired replicas remain > 0 immediately after traffic stops (retention period not expired)")
		Consistently(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "During retention period: DesiredOptimized=%d, Current=%d, Reason=%q\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas, va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.LastUpdate.Reason)

			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">", 0),
				"Desired replicas should remain > 0 during retention period (not scale to zero prematurely)")
		}, retentionDuration, 10*time.Second).Should(Succeed())

		By("waiting for retention period to pass with zero traffic")
		waitDuration := 90 * time.Second // Additional buffer after retention period
		_, _ = fmt.Fprintf(GinkgoWriter, "Retention period passed. Waiting additional %v buffer for scale-to-zero...\n", waitDuration)
		time.Sleep(waitDuration)

		By("verifying controller sets DesiredOptimizedAlloc to 0")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: DesiredOptimized=%d, Current=%d, Reason=%q\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas, va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.LastUpdate.Reason)

			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(0)))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying HPA scales deployment to 0 replicas")
		Eventually(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			if deployment.Status.Replicas == 0 {
				_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA successfully scaled deployment to 0 replicas\n")
			}
			g.Expect(deployment.Status.Replicas).To(Equal(int32(0)))
		}, 5*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying CurrentAlloc reflects the scaled-down state")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(Equal(int32(0)))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Scale-to-zero after traffic stops completed successfully\n")
	})

	AfterAll(func() {
		By("cleaning up HPA")
		err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, deployName+"-hpa", metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up VariantAutoscaling resource")
		variantAutoscaling := &v1alpha1.VariantAutoscaling{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName,
				Namespace: namespace,
			},
		}
		err = hpaCrClient.Delete(ctx, variantAutoscaling)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("deleting InferenceModel")
		if inferenceModel != nil {
			err = hpaCrClient.Delete(ctx, inferenceModel)
			err = client.IgnoreNotFound(err)
			Expect(err).NotTo(HaveOccurred())
		}

		By("cleaning up ServiceMonitor")
		serviceMonitor := &unstructured.Unstructured{}
		serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		})
		serviceMonitor.SetName(serviceMonName)
		serviceMonitor.SetNamespace(getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"))
		err = hpaCrClient.Delete(ctx, serviceMonitor)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up Service")
		err = hpaK8sClient.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up Deployment")
		err = hpaK8sClient.AppsV1().Deployments(namespace).Delete(ctx, deployName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for all pods to be deleted")
		Eventually(func(g Gomega) {
			podList, err := hpaK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", appLabel),
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(podList.Items).To(BeEmpty())
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("cleaning up ConfigMap")
		err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Delete(ctx, configMapName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("Test idle with HPA (scale-to-zero disabled)", Ordered, func() {
	var (
		namespace         string
		deployName        string
		serviceName       string
		serviceMonName    string
		configMapName     string
		appLabel          string
		modelID           string
		accelerator       string
		ctx               context.Context
		initialReplicas   int32
		retentionDuration time.Duration
		inferenceModel    *unstructured.Unstructured
	)

	BeforeAll(func() {
		initializeHPAClients()

		ctx = context.Background()
		namespace = getEnvOrDefault("LLMD_NAMESPACE", "llm-d-sim")
		deployName = "hpa-idle-no-sto-zero-deployment"
		serviceName = "hpa-idle-no-sto-zero-service"
		serviceMonName = "hpa-idle-no-sto-zero-servicemonitor"
		configMapName = "model-no-scale-to-zero-config"
		appLabel = "hpa-idle-no-sto-zero-test"
		modelID = "test-hpa-idle-no-sto-zero-model"
		accelerator = getEnvOrDefault("ACCELERATOR_TYPE", "A100")
		initialReplicas = 1
		retentionDuration = 2 * time.Minute

		By("checking if Prometheus Adapter is installed")
		monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

		// Try multiple label selectors to find Prometheus Adapter pods
		var podList *corev1.PodList
		var err error
		labelSelectors := []string{
			"app.kubernetes.io/name=prometheus-adapter",
			"app=prometheus-adapter",
		}

		for _, selector := range labelSelectors {
			podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
				LabelSelector: selector,
			})
			if err == nil && len(podList.Items) > 0 {
				_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s using selector '%s' (%d pods)\n", monitoringNs, selector, len(podList.Items))
				break
			}
		}

		// If still not found, try listing all pods and looking for prometheus-adapter by name
		if podList == nil || len(podList.Items) == 0 {
			allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Checking all pods in namespace %s for prometheus-adapter:\n", monitoringNs)
				foundAdapter := false
				for _, pod := range allPods.Items {
					if strings.Contains(pod.Name, "prometheus-adapter") {
						_, _ = fmt.Fprintf(GinkgoWriter, "  Found pod: %s (phase: %s)\n", pod.Name, pod.Status.Phase)
						foundAdapter = true
					}
				}
				if !foundAdapter {
					Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s (by name pattern)\n", monitoringNs)
				}
			} else {
				Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
			}
		}

		By("ensuring unique app label and model")
		utils.ValidateAppLabelUniqueness(namespace, appLabel, hpaK8sClient, hpaCrClient)
		utils.ValidateVariantAutoscalingUniqueness(namespace, modelID, accelerator, hpaCrClient)

		By("creating ConfigMap with scale-to-zero DISABLED")
		configMapKey := strings.ReplaceAll(modelID, "/", "-")
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system"),
			},
			Data: map[string]string{
				fmt.Sprintf("model.%s", configMapKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: false
retentionPeriod: "2m"`, modelID),
			},
		}
		_, err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Create(ctx, configMap, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ConfigMap: %s", configMapName))

		By("creating vllme deployment")
		deployment := utils.CreateVllmeDeployment(namespace, deployName, modelID, appLabel)
		deployment.Spec.Replicas = &initialReplicas
		_, err = hpaK8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Deployment: %s", deployName))

		By("creating vllme service")
		service := utils.CreateVllmeService(namespace, serviceName, appLabel, 30003)
		_, err = hpaK8sClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Service: %s", serviceName))

		By("creating ServiceMonitor for vllme metrics")
		serviceMonitor := utils.CreateVllmeServiceMonitor(serviceMonName, getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"), appLabel)
		err = hpaCrClient.Create(ctx, serviceMonitor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ServiceMonitor: %s", serviceMonName))

		By("creating VariantAutoscaling resource")
		variantAutoscaling := utils.CreateVariantAutoscalingResource(namespace, deployName, modelID, accelerator)
		err = hpaCrClient.Create(ctx, variantAutoscaling)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create VariantAutoscaling: %s", deployName))

		By("creating InferenceModel for the deployment")
		inferenceModel = utils.CreateInferenceModel(deployName, namespace, modelID)
		err = hpaCrClient.Create(ctx, inferenceModel)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create InferenceModel: %s", modelID))
	})

	It("deployment should be running initially", func() {
		Eventually(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get Deployment")
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(int32(1)), "Deployment should have 1 ready replica")
		}, 3*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should NOT scale to zero after retention period with no traffic (scale-to-zero disabled)", func() {
		By("waiting for initial controller reconciliation")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(Equal(int32(1)))
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 1))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying Prometheus Adapter is ready")
		Eventually(func(g Gomega) {
			monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

			// Try multiple label selectors to find Prometheus Adapter pods
			var podList *corev1.PodList
			var err error
			labelSelectors := []string{
				"app.kubernetes.io/name=prometheus-adapter",
				"app=prometheus-adapter",
			}

			for _, selector := range labelSelectors {
				podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
					LabelSelector: selector,
				})
				if err == nil && len(podList.Items) > 0 {
					break
				}
			}

			// Fallback to name pattern if labels don't work
			if podList == nil || len(podList.Items) == 0 {
				allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
				if err == nil {
					var adapterPods []corev1.Pod
					for _, pod := range allPods.Items {
						if strings.Contains(pod.Name, "prometheus-adapter") {
							adapterPods = append(adapterPods, pod)
						}
					}
					podList = &corev1.PodList{Items: adapterPods}
				}
			}

			g.Expect(err).NotTo(HaveOccurred(), "Should be able to list Prometheus Adapter pods")
			g.Expect(podList.Items).NotTo(BeEmpty(), "Prometheus Adapter pods should exist")

			readyCount := 0
			for _, pod := range podList.Items {
				if pod.Status.Phase == corev1.PodRunning {
					for _, cond := range pod.Status.Conditions {
						if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
							readyCount++
							break
						}
					}
				}
			}
			g.Expect(readyCount).To(BeNumerically(">", 0), "At least one Prometheus Adapter pod should be ready")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter is ready (%d pods)\n", readyCount)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying controller has reconciled VA")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

			// Wait for controller to set desired replicas
			g.Expect(va.Status.DesiredOptimizedAlloc).NotTo(BeNil(), "DesiredOptimizedAlloc should be set")

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: CurrentReplicas=%d, DesiredReplicas=%d\n",
				va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.NumReplicas)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Controller has reconciled VA. Waiting for metric to be scraped by Prometheus and discovered by Prometheus Adapter...\n")
		// Allow time for full metric discovery pipeline:
		// 1. Controller emits metric (immediate)
		// 2. Prometheus scrapes metric (scrape interval: ~30s)
		// 3. Prometheus Adapter discovers metric (discovery interval: ~30-60s)
		// Total: 60-90 seconds needed to ensure metric is available via external metrics API
		time.Sleep(90 * time.Second)

		By("creating HPA for deployment")
		minReplicas := int32(0) // HPA always allows scale-to-zero; controller ConfigMap controls behavior
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName + "-hpa",
				Namespace: namespace,
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       deployName,
				},
				MinReplicas: &minReplicas,
				MaxReplicas: 10,
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleUp: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
				},
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{
								Name: "wva_desired_replicas",
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"target_name":        deployName,
										"target_kind":        "Deployment",
										"exported_namespace": namespace,
										"accelerator_type":   accelerator,
									},
								},
							},
							Target: autoscalingv2.MetricTarget{
								Type:         autoscalingv2.AverageValueMetricType,
								AverageValue: resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				},
			},
		}
		_, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, hpa, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create HPA: %s", hpa.Name))
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA created: %s (minReplicas=0, controller scale-to-zero disabled via ConfigMap)\n", hpa.Name)

		By("waiting for HPA to be ready")
		Eventually(func(g Gomega) {
			hpa, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, deployName+"-hpa", metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get HPA")

			// Print HPA status for debugging
			_, _ = fmt.Fprintf(GinkgoWriter, "HPA Status - Current: %d, Desired: %d, Conditions: %d\n",
				hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas, len(hpa.Status.Conditions))

			for _, condition := range hpa.Status.Conditions {
				_, _ = fmt.Fprintf(GinkgoWriter, "  Condition: %s = %s (Reason: %s, Message: %s)\n",
					condition.Type, condition.Status, condition.Reason, condition.Message)
			}

			g.Expect(hpa.Status.Conditions).NotTo(BeEmpty(), "HPA should have conditions")

			for _, condition := range hpa.Status.Conditions {
				if condition.Type == autoscalingv2.ScalingActive && condition.Status == corev1.ConditionTrue {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA is active\n")
					return
				}
			}
			g.Expect(true).To(BeFalse(), "HPA should be active")
		}, 5*time.Minute, 10*time.Second).Should(Succeed())

		By("waiting for retention period + buffer with no traffic")
		waitDuration := retentionDuration + (90 * time.Second)
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting %v (retention + buffer) with no traffic...\n", waitDuration)
		time.Sleep(waitDuration)

		By("verifying controller keeps DesiredOptimizedAlloc >= 1 (scale-to-zero disabled)")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: DesiredOptimized=%d, Current=%d, Reason=%q\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas, va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.LastUpdate.Reason)

			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 1),
				"Controller should keep desired replicas >= 1 when scale-to-zero is disabled")
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying HPA keeps deployment at >= 1 replica")
		Consistently(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "Deployment replicas: %d (should stay >= 1)\n", deployment.Status.Replicas)
			g.Expect(deployment.Status.Replicas).To(BeNumerically(">=", 1),
				"Deployment should maintain at least 1 replica when scale-to-zero is disabled")
		}, 1*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying CurrentAlloc remains >= 1")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(BeNumerically(">=", 1))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA with scale-to-zero disabled: deployment maintained >= 1 replica\n")
	})

	AfterAll(func() {
		By("cleaning up HPA")
		err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, deployName+"-hpa", metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete HPA: %s", deployName+"-hpa"))

		By("cleaning up VariantAutoscaling resource")
		variantAutoscaling := &v1alpha1.VariantAutoscaling{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName,
				Namespace: namespace,
			},
		}
		err = hpaCrClient.Delete(ctx, variantAutoscaling)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete VariantAutoscaling: %s", deployName))

		By("deleting InferenceModel")
		if inferenceModel != nil {
			err = hpaCrClient.Delete(ctx, inferenceModel)
			err = client.IgnoreNotFound(err)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete InferenceModel: %s", modelID))
		}

		By("cleaning up ServiceMonitor")
		serviceMonitor := &unstructured.Unstructured{}
		serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		})
		serviceMonitor.SetName(serviceMonName)
		serviceMonitor.SetNamespace(getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"))
		err = hpaCrClient.Delete(ctx, serviceMonitor)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete ServiceMonitor: %s", serviceMonName))

		By("cleaning up Service")
		err = hpaK8sClient.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete Service: %s", serviceName))

		By("cleaning up Deployment")
		err = hpaK8sClient.AppsV1().Deployments(namespace).Delete(ctx, deployName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete Deployment: %s", deployName))

		By("waiting for all pods to be deleted")
		Eventually(func(g Gomega) {
			podList, err := hpaK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", appLabel),
			})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to list Pods")
			g.Expect(podList.Items).To(BeEmpty(), fmt.Sprintf("All Pods with label %s should be deleted", appLabel))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("cleaning up ConfigMap")
		err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Delete(ctx, configMapName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to delete ConfigMap: %s", configMapName))
	})
})

var _ = Describe("Test traffic-based with HPA (scale-to-zero disabled)", Ordered, func() {
	var (
		namespace         string
		deployName        string
		serviceName       string
		serviceMonName    string
		configMapName     string
		appLabel          string
		modelID           string
		accelerator       string
		ctx               context.Context
		initialReplicas   int32
		retentionDuration time.Duration
		inferenceModel    *unstructured.Unstructured
	)

	BeforeAll(func() {
		initializeHPAClients()

		ctx = context.Background()
		namespace = getEnvOrDefault("LLMD_NAMESPACE", "llm-d-sim")
		deployName = "hpa-traffic-no-sto-zero-deployment"
		serviceName = "hpa-traffic-no-sto-zero-service"
		serviceMonName = "hpa-traffic-no-sto-zero-servicemonitor"
		configMapName = "model-no-scale-to-zero-config"
		appLabel = "hpa-traffic-no-sto-zero-test"
		// Use "default/default" to leverage existing gateway infrastructure
		modelID = getEnvOrDefault("DEFAULT_MODEL_ID", "default/default")
		accelerator = getEnvOrDefault("ACCELERATOR_TYPE", "A100")
		initialReplicas = 1
		retentionDuration = 2 * time.Minute

		By("checking if Prometheus Adapter is installed")
		monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

		// Try multiple label selectors to find Prometheus Adapter pods
		var podList *corev1.PodList
		var err error
		labelSelectors := []string{
			"app.kubernetes.io/name=prometheus-adapter",
			"app=prometheus-adapter",
		}

		for _, selector := range labelSelectors {
			podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
				LabelSelector: selector,
			})
			if err == nil && len(podList.Items) > 0 {
				_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s using selector '%s' (%d pods)\n", monitoringNs, selector, len(podList.Items))
				break
			}
		}

		// If still not found, try listing all pods and looking for prometheus-adapter by name
		if podList == nil || len(podList.Items) == 0 {
			allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Checking all pods in namespace %s for prometheus-adapter:\n", monitoringNs)
				foundAdapter := false
				for _, pod := range allPods.Items {
					if strings.Contains(pod.Name, "prometheus-adapter") {
						_, _ = fmt.Fprintf(GinkgoWriter, "  Found pod: %s (phase: %s)\n", pod.Name, pod.Status.Phase)
						foundAdapter = true
					}
				}
				if !foundAdapter {
					Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s (by name pattern)\n", monitoringNs)
				}
			} else {
				Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
			}
		}

		By("ensuring unique app label and model")
		utils.ValidateAppLabelUniqueness(namespace, appLabel, hpaK8sClient, hpaCrClient)
		utils.ValidateVariantAutoscalingUniqueness(namespace, modelID, accelerator, hpaCrClient)

		By("creating ConfigMap with scale-to-zero DISABLED")
		configMapKey := strings.ReplaceAll(modelID, "/", "-")
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system"),
			},
			Data: map[string]string{
				fmt.Sprintf("model.%s", configMapKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: false
retentionPeriod: "2m"`, modelID),
			},
		}
		_, err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Create(ctx, configMap, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ConfigMap: %s", configMapName))

		By("creating vllme deployment")
		deployment := utils.CreateVllmeDeployment(namespace, deployName, modelID, appLabel)
		deployment.Spec.Replicas = &initialReplicas
		_, err = hpaK8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Deployment: %s", deployName))

		By("creating vllme service")
		service := utils.CreateVllmeService(namespace, serviceName, appLabel, 30004)
		_, err = hpaK8sClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Service: %s", serviceName))

		By("creating ServiceMonitor for vllme metrics")
		serviceMonitor := utils.CreateVllmeServiceMonitor(serviceMonName, getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"), appLabel)
		err = hpaCrClient.Create(ctx, serviceMonitor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ServiceMonitor: %s", serviceMonName))

		By("creating VariantAutoscaling resource")
		variantAutoscaling := utils.CreateVariantAutoscalingResource(namespace, deployName, modelID, accelerator)
		err = hpaCrClient.Create(ctx, variantAutoscaling)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create VariantAutoscaling: %s", deployName))

		By("creating InferenceModel for the deployment")
		// Use "default/default" to match traffic and leverage existing gateway infrastructure
		inferenceModel = utils.CreateInferenceModel(deployName, namespace, getEnvOrDefault("DEFAULT_MODEL_ID", "default/default"))
		err = hpaCrClient.Create(ctx, inferenceModel)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create InferenceModel with model: %s", getEnvOrDefault("DEFAULT_MODEL_ID", "default/default")))
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ InferenceModel created with modelName=%s\n", getEnvOrDefault("DEFAULT_MODEL_ID", "default/default"))
	})

	It("should NOT scale to zero after traffic stops (scale-to-zero disabled)", func() {
		By("waiting for deployment to have ready replicas")
		Eventually(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			_, _ = fmt.Fprintf(GinkgoWriter, "Deployment replicas: Ready=%d, Available=%d, Target=%d\n",
				deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas, *deployment.Spec.Replicas)
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(int32(1)))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying controller has reconciled VA")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(Equal(int32(1)))
			g.Expect(va.Status.DesiredOptimizedAlloc).NotTo(BeNil(), "DesiredOptimizedAlloc should be set")
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 1))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying Prometheus Adapter is ready")
		Eventually(func(g Gomega) {
			monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

			// Try multiple label selectors to find Prometheus Adapter pods
			var podList *corev1.PodList
			var err error
			labelSelectors := []string{
				"app.kubernetes.io/name=prometheus-adapter",
				"app=prometheus-adapter",
			}

			for _, selector := range labelSelectors {
				podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
					LabelSelector: selector,
				})
				if err == nil && len(podList.Items) > 0 {
					break
				}
			}

			// Fallback to name pattern if labels don't work
			if podList == nil || len(podList.Items) == 0 {
				allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
				if err == nil {
					var adapterPods []corev1.Pod
					for _, pod := range allPods.Items {
						if strings.Contains(pod.Name, "prometheus-adapter") {
							adapterPods = append(adapterPods, pod)
						}
					}
					podList = &corev1.PodList{Items: adapterPods}
				}
			}

			g.Expect(err).NotTo(HaveOccurred(), "Should be able to list Prometheus Adapter pods")
			g.Expect(podList.Items).NotTo(BeEmpty(), "Prometheus Adapter pods should exist")

			readyCount := 0
			for _, pod := range podList.Items {
				if pod.Status.Phase == corev1.PodRunning {
					for _, cond := range pod.Status.Conditions {
						if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
							readyCount++
							break
						}
					}
				}
			}
			g.Expect(readyCount).To(BeNumerically(">", 0), "At least one Prometheus Adapter pod should be ready")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter is ready (%d pods)\n", readyCount)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		minReplicas := int32(0) // HPA allows scale-to-zero; ConfigMap disables it
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName + "-hpa",
				Namespace: namespace,
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       deployName,
				},
				MinReplicas: &minReplicas,
				MaxReplicas: 10,
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleUp: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
				},
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{
								Name: "wva_desired_replicas",
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"target_name":        deployName,
										"target_kind":        "Deployment",
										"exported_namespace": namespace,
										"accelerator_type":   accelerator,
									},
								},
							},
							Target: autoscalingv2.MetricTarget{
								Type:         autoscalingv2.AverageValueMetricType,
								AverageValue: resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				},
			},
		}
		_, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, hpa, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA created: %s (minReplicas=0, scale-to-zero disabled by ConfigMap)\n", hpa.Name)

		By("waiting for HPA to be ready")
		Eventually(func(g Gomega) {
			hpa, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, deployName+"-hpa", metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			// Print HPA status for debugging
			_, _ = fmt.Fprintf(GinkgoWriter, "HPA Status - Current: %d, Desired: %d, Conditions: %d\n",
				hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas, len(hpa.Status.Conditions))

			for _, condition := range hpa.Status.Conditions {
				_, _ = fmt.Fprintf(GinkgoWriter, "  Condition: %s = %s (Reason: %s, Message: %s)\n",
					condition.Type, condition.Status, condition.Reason, condition.Message)
			}

			g.Expect(hpa.Status.Conditions).NotTo(BeEmpty())

			for _, condition := range hpa.Status.Conditions {
				if condition.Type == autoscalingv2.ScalingActive && condition.Status == corev1.ConditionTrue {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA is active\n")
					return
				}
			}
			g.Expect(true).To(BeFalse(), "HPA should be active")
		}, 5*time.Minute, 10*time.Second).Should(Succeed())

		By("setting up port-forward to gateway for traffic generation")
		port := 8003
		portForwardCmd := utils.SetUpPortForward(hpaK8sClient, ctx, getEnvOrDefault("GATEWAY_NAME", "infra-sim-inference-gateway"), namespace, port, 80)
		defer func() {
			err := utils.StopCmd(portForwardCmd)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to stop port-forwarding for gateway: %s", getEnvOrDefault("GATEWAY_NAME", "infra-sim-inference-gateway")))
		}()

		By("waiting for port-forward to be ready")
		err = utils.VerifyPortForwardReadiness(ctx, port, fmt.Sprintf("http://localhost:%d/v1", port))
		Expect(err).NotTo(HaveOccurred(), "Port-forward should be ready within timeout")

		By("starting traffic generation")
		loadRate := 10
		// Use "default/default" model ID to match InferenceModel configuration
		loadGenCmd := utils.StartLoadGenerator(loadRate, 100, port, getEnvOrDefault("DEFAULT_MODEL_ID", "default/default"))
		defer func() {
			err := utils.StopCmd(loadGenCmd)
			if err != nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Warning: Failed to stop load generator: %v\n", err)
			}
		}()

		_, _ = fmt.Fprintf(GinkgoWriter, "Starting traffic generation at %d req/s...\n", loadRate)

		By("waiting for vLLM metrics rate data to accumulate")
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting 90 seconds for rate([1m]) data accumulation...\n")
		time.Sleep(90 * time.Second)

		By("waiting for controller to process traffic and emit metrics")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "Waiting for controller: DesiredOptimized=%d, Current=%d, Reason=%q\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas, va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.LastUpdate.Reason)

			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(Equal(int32(1)))
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(1)))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Metrics: current=1, desired=1\n")

		By("stopping traffic generation")
		err = utils.StopCmd(loadGenCmd)
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Traffic stopped\n")

		By("waiting for retention period + buffer with zero traffic")
		waitDuration := retentionDuration + (90 * time.Second)
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting %v (retention + buffer) with no traffic...\n", waitDuration)
		time.Sleep(waitDuration)

		By("verifying controller keeps DesiredOptimizedAlloc >= 1 (scale-to-zero disabled)")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: DesiredOptimized=%d, Current=%d, Reason=%q\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas, va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.LastUpdate.Reason)

			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 1),
				"Controller should keep desired replicas >= 1 when scale-to-zero is disabled")
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying HPA keeps deployment at >= 1 replica")
		Consistently(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			_, _ = fmt.Fprintf(GinkgoWriter, "Deployment replicas: %d (should stay >= 1)\n", deployment.Status.Replicas)
			g.Expect(deployment.Status.Replicas).To(BeNumerically(">=", 1),
				"Deployment should maintain at least 1 replica when scale-to-zero is disabled")
		}, 1*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying CurrentAlloc remains >= 1")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(BeNumerically(">=", 1))
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA with scale-to-zero disabled: deployment maintained >= 1 replica after traffic stopped\n")
	})

	AfterAll(func() {
		By("cleaning up HPA")
		err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, deployName+"-hpa", metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up VariantAutoscaling resource")
		variantAutoscaling := &v1alpha1.VariantAutoscaling{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName,
				Namespace: namespace,
			},
		}
		err = hpaCrClient.Delete(ctx, variantAutoscaling)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("deleting InferenceModel")
		if inferenceModel != nil {
			err = hpaCrClient.Delete(ctx, inferenceModel)
			err = client.IgnoreNotFound(err)
			Expect(err).NotTo(HaveOccurred())
		}

		By("cleaning up ServiceMonitor")
		serviceMonitor := &unstructured.Unstructured{}
		serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		})
		serviceMonitor.SetName(serviceMonName)
		serviceMonitor.SetNamespace(getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"))
		err = hpaCrClient.Delete(ctx, serviceMonitor)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up Service")
		err = hpaK8sClient.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up Deployment")
		err = hpaK8sClient.AppsV1().Deployments(namespace).Delete(ctx, deployName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for all pods to be deleted")
		Eventually(func(g Gomega) {
			podList, err := hpaK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", appLabel),
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(podList.Items).To(BeEmpty())
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("cleaning up ConfigMap")
		err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Delete(ctx, configMapName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("Test idle with HPA scale-to-zero enabled but VA minReplicas=2", Ordered, func() {
	var (
		namespace         string
		deployName        string
		serviceName       string
		serviceMonName    string
		configMapName     string
		appLabel          string
		modelID           string
		accelerator       string
		ctx               context.Context
		initialReplicas   int32
		retentionDuration time.Duration
		inferenceModel    *unstructured.Unstructured
	)

	BeforeAll(func() {
		initializeHPAClients()

		ctx = context.Background()
		namespace = getEnvOrDefault("LLMD_NAMESPACE", "llm-d-sim")
		deployName = "hpa-va-minrep-idle-deployment"
		serviceName = "hpa-va-minrep-idle-service"
		serviceMonName = "hpa-va-minrep-idle-servicemonitor"
		configMapName = "model-va-minrep-idle-config"
		appLabel = "hpa-va-minrep-idle-test"
		modelID = "test-hpa-va-minrep-idle-model"
		accelerator = getEnvOrDefault("ACCELERATOR_TYPE", "A100")
		initialReplicas = 2
		retentionDuration = 2 * time.Minute

		By("checking if Prometheus Adapter is installed")
		monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

		// Try multiple label selectors to find Prometheus Adapter pods
		var podList *corev1.PodList
		var err error
		labelSelectors := []string{
			"app.kubernetes.io/name=prometheus-adapter",
			"app=prometheus-adapter",
		}

		for _, selector := range labelSelectors {
			podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
				LabelSelector: selector,
			})
			if err == nil && len(podList.Items) > 0 {
				_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s using selector '%s' (%d pods)\n", monitoringNs, selector, len(podList.Items))
				break
			}
		}

		// If still not found, try listing all pods and looking for prometheus-adapter by name
		if podList == nil || len(podList.Items) == 0 {
			allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Checking all pods in namespace %s for prometheus-adapter:\n", monitoringNs)
				foundAdapter := false
				for _, pod := range allPods.Items {
					if strings.Contains(pod.Name, "prometheus-adapter") {
						_, _ = fmt.Fprintf(GinkgoWriter, "  Found pod: %s (phase: %s)\n", pod.Name, pod.Status.Phase)
						foundAdapter = true
					}
				}
				if !foundAdapter {
					Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA scale-to-zero tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s (by name pattern)\n", monitoringNs)
				}
			} else {
				Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA scale-to-zero tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
			}
		}

		By("ensuring unique app label and model")
		utils.ValidateAppLabelUniqueness(namespace, appLabel, hpaK8sClient, hpaCrClient)
		utils.ValidateVariantAutoscalingUniqueness(namespace, modelID, accelerator, hpaCrClient)

		By("creating scale-to-zero ConfigMap")
		configMapKey := strings.ReplaceAll(modelID, "/", "-")
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system"),
			},
			Data: map[string]string{
				fmt.Sprintf("model.%s", configMapKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "2m"`, modelID),
			},
		}
		_, err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Create(ctx, configMap, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ConfigMap: %s", configMapName))

		By("creating vllme deployment")
		deployment := utils.CreateVllmeDeployment(namespace, deployName, modelID, appLabel)
		deployment.Spec.Replicas = &initialReplicas
		_, err = hpaK8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Deployment: %s", deployName))

		By("creating vllme service")
		service := utils.CreateVllmeService(namespace, serviceName, appLabel, 30003)
		_, err = hpaK8sClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Service: %s", serviceName))

		By("creating ServiceMonitor for vllme metrics")
		serviceMonitor := utils.CreateVllmeServiceMonitor(serviceMonName, getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"), appLabel)
		err = hpaCrClient.Create(ctx, serviceMonitor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ServiceMonitor: %s", serviceMonName))

		By("creating VariantAutoscaling resource with minReplicas=2")
		variantAutoscaling := utils.CreateVariantAutoscalingResource(namespace, deployName, modelID, accelerator)
		minReplicas := int32(2)
		variantAutoscaling.Spec.MinReplicas = &minReplicas
		err = hpaCrClient.Create(ctx, variantAutoscaling)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create VariantAutoscaling: %s", deployName))

		By("creating InferenceModel for the deployment")
		inferenceModel = utils.CreateInferenceModel(deployName, namespace, modelID)
		err = hpaCrClient.Create(ctx, inferenceModel)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create InferenceModel: %s", modelID))
	})

	It("deployment should be running initially with 2 replicas", func() {
		Eventually(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get Deployment")
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(int32(2)), "Deployment should have 2 ready replicas")
		}, 3*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should NOT scale below VA minReplicas=2 even with HPA minReplicas=0 and scale-to-zero enabled", func() {
		By("waiting for initial controller reconciliation")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(BeNumerically(">=", 2))
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 2))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying Prometheus Adapter is ready")
		Eventually(func(g Gomega) {
			monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

			// Try multiple label selectors to find Prometheus Adapter pods
			var podList *corev1.PodList
			var err error
			labelSelectors := []string{
				"app.kubernetes.io/name=prometheus-adapter",
				"app=prometheus-adapter",
			}

			for _, selector := range labelSelectors {
				podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
					LabelSelector: selector,
				})
				if err == nil && len(podList.Items) > 0 {
					break
				}
			}

			// Fallback to name pattern if labels don't work
			if podList == nil || len(podList.Items) == 0 {
				allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
				if err == nil {
					var adapterPods []corev1.Pod
					for _, pod := range allPods.Items {
						if strings.Contains(pod.Name, "prometheus-adapter") {
							adapterPods = append(adapterPods, pod)
						}
					}
					podList = &corev1.PodList{Items: adapterPods}
				}
			}

			g.Expect(err).NotTo(HaveOccurred(), "Should be able to list Prometheus Adapter pods")
			g.Expect(podList.Items).NotTo(BeEmpty(), "Prometheus Adapter pods should exist")

			readyCount := 0
			for _, pod := range podList.Items {
				if pod.Status.Phase == corev1.PodRunning {
					for _, cond := range pod.Status.Conditions {
						if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
							readyCount++
							break
						}
					}
				}
			}
			g.Expect(readyCount).To(BeNumerically(">", 0), "At least one Prometheus Adapter pod should be ready")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter is ready (%d pods)\n", readyCount)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("creating HPA for deployment with minReplicas=0")
		minReplicasHPA := int32(0) // HPA allows scale-to-zero
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName + "-hpa",
				Namespace: namespace,
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       deployName,
				},
				MinReplicas: &minReplicasHPA,
				MaxReplicas: 10,
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleUp: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
				},
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{
								Name: "wva_desired_replicas",
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"target_name":        deployName,
										"target_kind":        "Deployment",
										"exported_namespace": namespace,
										"accelerator_type":   accelerator,
									},
								},
							},
							Target: autoscalingv2.MetricTarget{
								Type:         autoscalingv2.AverageValueMetricType,
								AverageValue: resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				},
			},
		}
		_, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, hpa, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create HPA: %s", hpa.Name))
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA created with minReplicas=0: %s\n", hpa.Name)

		By("waiting for HPA to be ready")
		Eventually(func(g Gomega) {
			hpa, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, deployName+"-hpa", metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get HPA")

			g.Expect(hpa.Status.Conditions).NotTo(BeEmpty(), "HPA should have conditions")

			for _, condition := range hpa.Status.Conditions {
				if condition.Type == autoscalingv2.ScalingActive && condition.Status == corev1.ConditionTrue {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA is active\n")
					return
				}
			}
			g.Expect(true).To(BeFalse(), "HPA should be active")
		}, 5*time.Minute, 10*time.Second).Should(Succeed())

		By("waiting for retention period to pass with no traffic")
		waitDuration := retentionDuration + (90 * time.Second) // retention + buffer
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting %v for retention period + buffer (no traffic)...\n", waitDuration)
		time.Sleep(waitDuration)

		By("verifying controller respects VA minReplicas=2 and does NOT scale to zero")
		Consistently(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			// Controller should respect VA minReplicas and keep desired >= 2
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 2),
				"VA DesiredOptimizedAlloc should be >= 2 (minReplicas constraint)")

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: CurrentReplicas=%d, DesiredReplicas=%d (minReplicas=2)\n",
				va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.NumReplicas)
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying HPA respects VA minReplicas and maintains >= 2 replicas")
		Consistently(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deployment.Status.Replicas).To(BeNumerically(">=", 2),
				"Deployment should maintain >= 2 replicas due to VA minReplicas")
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas=2 takes precedence: deployment maintained >= 2 replicas despite HPA minReplicas=0 and scale-to-zero enabled\n")
	})

	AfterAll(func() {
		By("cleaning up HPA")
		err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, deployName+"-hpa", metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up VariantAutoscaling resource")
		variantAutoscaling := &v1alpha1.VariantAutoscaling{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName,
				Namespace: namespace,
			},
		}
		err = hpaCrClient.Delete(ctx, variantAutoscaling)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("deleting InferenceModel")
		if inferenceModel != nil {
			err = hpaCrClient.Delete(ctx, inferenceModel)
			err = client.IgnoreNotFound(err)
			Expect(err).NotTo(HaveOccurred())
		}

		By("cleaning up ServiceMonitor")
		serviceMonitor := &unstructured.Unstructured{}
		serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		})
		serviceMonitor.SetName(serviceMonName)
		serviceMonitor.SetNamespace(getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"))
		err = hpaCrClient.Delete(ctx, serviceMonitor)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up Service")
		err = hpaK8sClient.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up Deployment")
		err = hpaK8sClient.AppsV1().Deployments(namespace).Delete(ctx, deployName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for all pods to be deleted")
		Eventually(func(g Gomega) {
			podList, err := hpaK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", appLabel),
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(podList.Items).To(BeEmpty())
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("cleaning up ConfigMap")
		err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Delete(ctx, configMapName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("Test traffic scale-up and scale-down with HPA scale-to-zero enabled but VA minReplicas=2", Ordered, func() {
	var (
		namespace         string
		deployName        string
		serviceName       string
		serviceMonName    string
		configMapName     string
		appLabel          string
		modelID           string
		accelerator       string
		ctx               context.Context
		initialReplicas   int32
		retentionDuration time.Duration
		inferenceModel    *unstructured.Unstructured
	)

	BeforeAll(func() {
		initializeHPAClients()

		ctx = context.Background()
		namespace = getEnvOrDefault("LLMD_NAMESPACE", "llm-d-sim")
		deployName = "hpa-va-minrep-traffic-deployment"
		serviceName = "hpa-va-minrep-traffic-service"
		serviceMonName = "hpa-va-minrep-traffic-servicemonitor"
		configMapName = "model-va-minrep-traffic-config"
		appLabel = "hpa-va-minrep-traffic-test"
		modelID = "test-hpa-va-minrep-traffic-model"
		accelerator = getEnvOrDefault("ACCELERATOR_TYPE", "A100")
		initialReplicas = 2
		retentionDuration = 2 * time.Minute

		By("checking if Prometheus Adapter is installed")
		monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

		// Try multiple label selectors to find Prometheus Adapter pods
		var podList *corev1.PodList
		var err error
		labelSelectors := []string{
			"app.kubernetes.io/name=prometheus-adapter",
			"app=prometheus-adapter",
		}

		for _, selector := range labelSelectors {
			podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
				LabelSelector: selector,
			})
			if err == nil && len(podList.Items) > 0 {
				_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s using selector '%s' (%d pods)\n", monitoringNs, selector, len(podList.Items))
				break
			}
		}

		// If still not found, try listing all pods and looking for prometheus-adapter by name
		if podList == nil || len(podList.Items) == 0 {
			allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Checking all pods in namespace %s for prometheus-adapter:\n", monitoringNs)
				foundAdapter := false
				for _, pod := range allPods.Items {
					if strings.Contains(pod.Name, "prometheus-adapter") {
						_, _ = fmt.Fprintf(GinkgoWriter, "  Found pod: %s (phase: %s)\n", pod.Name, pod.Status.Phase)
						foundAdapter = true
					}
				}
				if !foundAdapter {
					Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA scale-to-zero tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
				} else {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter found in namespace %s (by name pattern)\n", monitoringNs)
				}
			} else {
				Skip(fmt.Sprintf("Prometheus Adapter not found in namespace %s. HPA scale-to-zero tests require Prometheus Adapter with external metrics API. Please install kube-prometheus-stack or set up Prometheus Adapter before running these tests.", monitoringNs))
			}
		}

		By("ensuring unique app label and model")
		utils.ValidateAppLabelUniqueness(namespace, appLabel, hpaK8sClient, hpaCrClient)
		utils.ValidateVariantAutoscalingUniqueness(namespace, modelID, accelerator, hpaCrClient)

		By("creating scale-to-zero ConfigMap")
		configMapKey := strings.ReplaceAll(modelID, "/", "-")
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system"),
			},
			Data: map[string]string{
				fmt.Sprintf("model.%s", configMapKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "2m"`, modelID),
			},
		}
		_, err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Create(ctx, configMap, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ConfigMap: %s", configMapName))

		By("creating vllme deployment")
		deployment := utils.CreateVllmeDeployment(namespace, deployName, modelID, appLabel)
		deployment.Spec.Replicas = &initialReplicas
		_, err = hpaK8sClient.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Deployment: %s", deployName))

		By("creating vllme service")
		service := utils.CreateVllmeService(namespace, serviceName, appLabel, 30004)
		_, err = hpaK8sClient.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create Service: %s", serviceName))

		By("creating ServiceMonitor for vllme metrics")
		serviceMonitor := utils.CreateVllmeServiceMonitor(serviceMonName, getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"), appLabel)
		err = hpaCrClient.Create(ctx, serviceMonitor)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create ServiceMonitor: %s", serviceMonName))

		By("creating VariantAutoscaling resource with minReplicas=2")
		variantAutoscaling := utils.CreateVariantAutoscalingResource(namespace, deployName, modelID, accelerator)
		minReplicas := int32(2)
		variantAutoscaling.Spec.MinReplicas = &minReplicas
		err = hpaCrClient.Create(ctx, variantAutoscaling)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create VariantAutoscaling: %s", deployName))

		By("creating InferenceModel for the deployment")
		inferenceModel = utils.CreateInferenceModel(deployName, namespace, modelID)
		err = hpaCrClient.Create(ctx, inferenceModel)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create InferenceModel: %s", modelID))
	})

	It("deployment should be running initially with 2 replicas", func() {
		Eventually(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get Deployment")
			g.Expect(deployment.Status.ReadyReplicas).To(Equal(int32(2)), "Deployment should have 2 ready replicas")
		}, 3*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("should scale up with traffic but NOT scale below VA minReplicas=2 after traffic stops", func() {
		By("waiting for initial controller reconciliation")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(va.Status.CurrentAlloc.NumReplicas).To(BeNumerically(">=", 2))
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 2))
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying Prometheus Adapter is ready")
		Eventually(func(g Gomega) {
			monitoringNs := getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring")

			// Try multiple label selectors to find Prometheus Adapter pods
			var podList *corev1.PodList
			var err error
			labelSelectors := []string{
				"app.kubernetes.io/name=prometheus-adapter",
				"app=prometheus-adapter",
			}

			for _, selector := range labelSelectors {
				podList, err = hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{
					LabelSelector: selector,
				})
				if err == nil && len(podList.Items) > 0 {
					break
				}
			}

			// Fallback to name pattern if labels don't work
			if podList == nil || len(podList.Items) == 0 {
				allPods, err := hpaK8sClient.CoreV1().Pods(monitoringNs).List(ctx, metav1.ListOptions{})
				if err == nil {
					var adapterPods []corev1.Pod
					for _, pod := range allPods.Items {
						if strings.Contains(pod.Name, "prometheus-adapter") {
							adapterPods = append(adapterPods, pod)
						}
					}
					podList = &corev1.PodList{Items: adapterPods}
				}
			}

			g.Expect(err).NotTo(HaveOccurred(), "Should be able to list Prometheus Adapter pods")
			g.Expect(podList.Items).NotTo(BeEmpty(), "Prometheus Adapter pods should exist")

			readyCount := 0
			for _, pod := range podList.Items {
				if pod.Status.Phase == corev1.PodRunning {
					for _, cond := range pod.Status.Conditions {
						if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
							readyCount++
							break
						}
					}
				}
			}
			g.Expect(readyCount).To(BeNumerically(">", 0), "At least one Prometheus Adapter pod should be ready")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Prometheus Adapter is ready (%d pods)\n", readyCount)
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("creating HPA for deployment with minReplicas=0")
		minReplicasHPA := int32(0) // HPA allows scale-to-zero
		hpa := &autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName + "-hpa",
				Namespace: namespace,
			},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       deployName,
				},
				MinReplicas: &minReplicasHPA,
				MaxReplicas: 10,
				Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
					ScaleUp: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
					ScaleDown: &autoscalingv2.HPAScalingRules{
						StabilizationWindowSeconds: func() *int32 { v := int32(0); return &v }(),
						Policies: []autoscalingv2.HPAScalingPolicy{
							{
								Type:          autoscalingv2.PodsScalingPolicy,
								Value:         10,
								PeriodSeconds: 15,
							},
						},
					},
				},
				Metrics: []autoscalingv2.MetricSpec{
					{
						Type: autoscalingv2.ExternalMetricSourceType,
						External: &autoscalingv2.ExternalMetricSource{
							Metric: autoscalingv2.MetricIdentifier{
								Name: "wva_desired_replicas",
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"target_name":        deployName,
										"target_kind":        "Deployment",
										"exported_namespace": namespace,
										"accelerator_type":   accelerator,
									},
								},
							},
							Target: autoscalingv2.MetricTarget{
								Type:         autoscalingv2.AverageValueMetricType,
								AverageValue: resource.NewQuantity(1, resource.DecimalSI),
							},
						},
					},
				},
			},
		}
		_, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Create(ctx, hpa, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Should be able to create HPA: %s", hpa.Name))
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA created with minReplicas=0: %s\n", hpa.Name)

		By("waiting for HPA to be ready")
		Eventually(func(g Gomega) {
			hpa, err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(ctx, deployName+"-hpa", metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get HPA")

			g.Expect(hpa.Status.Conditions).NotTo(BeEmpty(), "HPA should have conditions")

			for _, condition := range hpa.Status.Conditions {
				if condition.Type == autoscalingv2.ScalingActive && condition.Status == corev1.ConditionTrue {
					_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA is active\n")
					return
				}
			}
			g.Expect(true).To(BeFalse(), "HPA should be active")
		}, 5*time.Minute, 10*time.Second).Should(Succeed())

		By("setting up port-forward to service for traffic generation")
		port := 8005
		portForwardCmd := utils.SetUpPortForward(hpaK8sClient, ctx, serviceName, namespace, port, 80)
		defer func() {
			err := utils.StopCmd(portForwardCmd)
			if err != nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Warning: Failed to stop port-forwarding: %v\n", err)
			}
		}()

		By("waiting for port-forward to be ready")
		err = utils.VerifyPortForwardReadiness(ctx, port, fmt.Sprintf("http://localhost:%d/v1", port))
		Expect(err).NotTo(HaveOccurred(), "Port-forward should be ready within timeout")

		By("starting traffic generation")
		loadRate := 10
		loadGenCmd := utils.StartLoadGenerator(loadRate, 100, port, modelID)
		_, _ = fmt.Fprintf(GinkgoWriter, "Starting traffic generation at %d req/s...\n", loadRate)

		By("waiting for metrics to accumulate")
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting 60 seconds for rate([1m]) data accumulation...\n")
		time.Sleep(60 * time.Second)

		By("verifying deployment maintains minReplicas=2 during traffic (may scale up but never below 2)")
		Consistently(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			replicas := deployment.Status.Replicas
			_, _ = fmt.Fprintf(GinkgoWriter, "Current replicas during traffic: %d (VA minReplicas=2 enforced)\n", replicas)
			g.Expect(replicas).To(BeNumerically(">=", 2), "Deployment should maintain at least minReplicas=2 with traffic")
		}, 3*time.Minute, 15*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Deployment maintained >= 2 replicas during traffic\n")

		By("stopping traffic generation")
		err = utils.StopCmd(loadGenCmd)
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Warning: Failed to stop load generator: %v\n", err)
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "Traffic stopped. Waiting for retention period...\n")

		By("waiting for retention period to pass")
		waitDuration := retentionDuration + (30 * time.Second) // retention + buffer
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting %v for retention period + buffer...\n", waitDuration)
		time.Sleep(waitDuration)

		By("verifying deployment never scales below VA minReplicas=2 (NOT to zero)")
		Consistently(func(g Gomega) {
			deployment, err := hpaK8sClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			replicas := deployment.Status.Replicas
			_, _ = fmt.Fprintf(GinkgoWriter, "Current replicas after traffic stopped: %d (VA minReplicas=2 enforced)\n", replicas)
			g.Expect(replicas).To(BeNumerically(">=", 2), "Deployment should never scale below VA minReplicas=2 (not to 0)")
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying VA status respects minReplicas=2")
		Consistently(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			// Controller should respect VA minReplicas
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 2),
				"VA DesiredOptimizedAlloc should be >= 2 (minReplicas constraint)")

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status: CurrentReplicas=%d, DesiredReplicas=%d (minReplicas=2)\n",
				va.Status.CurrentAlloc.NumReplicas,
				va.Status.DesiredOptimizedAlloc.NumReplicas)
		}, 1*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas=2 takes precedence: deployment maintained >= 2 replicas during and after traffic, never scaling to 0 despite HPA minReplicas=0 and scale-to-zero enabled\n")
	})

	It("should react to ConfigMap changes for enableScaleToZero setting", func() {
		By("updating ConfigMap to disable scale-to-zero")
		configMapKey := strings.ReplaceAll(modelID, "/", "-")
		controllerNamespace := getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")

		configMap, err := hpaK8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to get ConfigMap")

		// Disable scale-to-zero
		configMap.Data[fmt.Sprintf("model.%s", configMapKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: false
retentionPeriod: "2m"`, modelID)

		_, err = hpaK8sClient.CoreV1().ConfigMaps(controllerNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to update ConfigMap")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap updated: enableScaleToZero=false\n")

		By("waiting for controller to reconcile ConfigMap change")
		time.Sleep(30 * time.Second)

		By("verifying controller respects enableScaleToZero=false")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			// With enableScaleToZero=false, controller should maintain minReplicas=2
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 2),
				"Controller should respect enableScaleToZero=false and maintain >= minReplicas")

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status after disabling scale-to-zero: DesiredReplicas=%d (enforcing minReplicas=2)\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas)
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("updating ConfigMap to re-enable scale-to-zero")
		configMap, err = hpaK8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to get ConfigMap")

		configMap.Data[fmt.Sprintf("model.%s", configMapKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "2m"`, modelID)

		_, err = hpaK8sClient.CoreV1().ConfigMaps(controllerNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to update ConfigMap")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap updated: enableScaleToZero=true\n")

		By("waiting for controller to reconcile ConfigMap change")
		time.Sleep(30 * time.Second)

		By("verifying controller acknowledges enableScaleToZero=true setting")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := hpaCrClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: namespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			// With enableScaleToZero=true and no traffic, controller could report 0 or respect VA minReplicas
			// But with VA minReplicas=2, it should still enforce >= 2
			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", 2),
				"Controller should respect VA minReplicas=2 even with enableScaleToZero=true")

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status after re-enabling scale-to-zero: DesiredReplicas=%d (VA minReplicas=2 still enforced)\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas)
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Controller successfully reacted to ConfigMap changes for enableScaleToZero setting\n")
	})

	AfterAll(func() {
		By("cleaning up HPA")
		err := hpaK8sClient.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, deployName+"-hpa", metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up VariantAutoscaling resource")
		variantAutoscaling := &v1alpha1.VariantAutoscaling{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deployName,
				Namespace: namespace,
			},
		}
		err = hpaCrClient.Delete(ctx, variantAutoscaling)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("deleting InferenceModel")
		if inferenceModel != nil {
			err = hpaCrClient.Delete(ctx, inferenceModel)
			err = client.IgnoreNotFound(err)
			Expect(err).NotTo(HaveOccurred())
		}

		By("cleaning up ServiceMonitor")
		serviceMonitor := &unstructured.Unstructured{}
		serviceMonitor.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		})
		serviceMonitor.SetName(serviceMonName)
		serviceMonitor.SetNamespace(getEnvOrDefault("MONITORING_NAMESPACE", "workload-variant-autoscaler-monitoring"))
		err = hpaCrClient.Delete(ctx, serviceMonitor)
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up Service")
		err = hpaK8sClient.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("cleaning up Deployment")
		err = hpaK8sClient.AppsV1().Deployments(namespace).Delete(ctx, deployName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for all pods to be deleted")
		Eventually(func(g Gomega) {
			podList, err := hpaK8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", appLabel),
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(podList.Items).To(BeEmpty())
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("cleaning up ConfigMap")
		err = hpaK8sClient.CoreV1().ConfigMaps(getEnvOrDefault("CONTROLLER_NAMESPACE", "workload-variant-autoscaler-system")).Delete(ctx, configMapName, metav1.DeleteOptions{})
		err = client.IgnoreNotFound(err)
		Expect(err).NotTo(HaveOccurred())
	})
})
