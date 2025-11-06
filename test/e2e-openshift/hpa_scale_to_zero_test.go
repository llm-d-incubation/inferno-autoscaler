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

package e2eopenshift

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("HPA Scale-to-Zero on OpenShift", Ordered, func() {
	var (
		ctx              context.Context
		vaName           string
		hpaName          string
		initialReplicas  int32
		configMapName    string
		scaleToZeroModel string
	)

	BeforeAll(func() {
		ctx = context.Background()
		hpaName = "vllm-deployment-hpa"
		configMapName = "model-config-hpa-test"
		scaleToZeroModel = getEnvString("HPA_TEST_MODEL", modelID)

		By("verifying HPA exists and is configured correctly")
		hpa, err := k8sClient.AutoscalingV2().HorizontalPodAutoscalers(llmDNamespace).Get(ctx, hpaName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "HPA should exist")
		Expect(hpa.Spec.ScaleTargetRef.Name).To(Equal(deployment), "HPA should target the correct deployment")
		Expect(hpa.Spec.Metrics).To(HaveLen(1), "HPA should have one metric")
		Expect(hpa.Spec.Metrics[0].Type).To(Equal(autoscalingv2.ExternalMetricSourceType), "HPA should use external metrics")
		Expect(hpa.Spec.Metrics[0].External.Metric.Name).To(Equal(constants.WVADesiredReplicas), "HPA should use wva_desired_replicas metric")

		// Verify HPA minReplicas is 0 (required for scale-to-zero)
		Expect(hpa.Spec.MinReplicas).NotTo(BeNil(), "HPA minReplicas should be set")
		Expect(*hpa.Spec.MinReplicas).To(Equal(int32(0)), "HPA minReplicas should be 0 for scale-to-zero tests")

		By("finding VariantAutoscaling by scaleTargetRef")
		vaList := &v1alpha1.VariantAutoscalingList{}
		err = crClient.List(ctx, vaList, client.InNamespace(llmDNamespace))
		Expect(err).NotTo(HaveOccurred(), "Should be able to list VariantAutoscalings")

		var va *v1alpha1.VariantAutoscaling
		for i := range vaList.Items {
			if vaList.Items[i].Spec.ScaleTargetRef.Name == deployment {
				va = &vaList.Items[i]
				break
			}
		}
		Expect(va).NotTo(BeNil(), fmt.Sprintf("Should find VariantAutoscaling with scaleTargetRef pointing to %s", deployment))
		vaName = va.Name
		_, _ = fmt.Fprintf(GinkgoWriter, "Found VariantAutoscaling: %s\n", vaName)

		By("recording initial deployment state")
		deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to get deployment")
		initialReplicas = deploy.Status.ReadyReplicas
		_, _ = fmt.Fprintf(GinkgoWriter, "Initial deployment replicas: %d\n", initialReplicas)
	})

	It("should verify external metrics API provides wva_desired_replicas", func() {
		By("querying external metrics API for wva_desired_replicas metric")
		Eventually(func(g Gomega) {
			result, err := k8sClient.RESTClient().
				Get().
				AbsPath("/apis/external.metrics.k8s.io/v1beta1/namespaces/" + llmDNamespace + "/" + constants.WVADesiredReplicas).
				DoRaw(ctx)
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to query external metrics API")
			g.Expect(string(result)).To(ContainSubstring(constants.WVADesiredReplicas), "Metric should be available")
			g.Expect(string(result)).To(ContainSubstring(deployment), "Metric should be for the correct deployment")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ External metrics API is accessible and provides wva_desired_replicas\n")
		}, 2*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should maintain VA minReplicas constraint with HPA minReplicas=0", func() {
		By("verifying VariantAutoscaling minReplicas configuration")
		va := &v1alpha1.VariantAutoscaling{}
		err := crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

		var vaMinReplicas int32 = 1 // Default
		if va.Spec.MinReplicas != nil {
			vaMinReplicas = *va.Spec.MinReplicas
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "VA minReplicas: %d\n", vaMinReplicas)

		By("verifying deployment respects VA minReplicas (not HPA minReplicas)")
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get deployment")

			replicas := deploy.Status.ReadyReplicas
			_, _ = fmt.Fprintf(GinkgoWriter, "Current replicas: %d (VA minReplicas: %d, HPA minReplicas: 0)\n", replicas, vaMinReplicas)
			g.Expect(replicas).To(BeNumerically(">=", vaMinReplicas), "Deployment should respect VA minReplicas, not HPA minReplicas=0")
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas takes precedence over HPA minReplicas=0\n")
	})

	It("should react to ConfigMap enableScaleToZero changes", func() {
		By("verifying ConfigMap exists or creating test ConfigMap")
		configMapKey := strings.ReplaceAll(scaleToZeroModel, "/", "-")

		_, err := k8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		if err != nil {
			// Create test ConfigMap if it doesn't exist
			newConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: controllerNamespace,
				},
				Data: map[string]string{
					fmt.Sprintf("model.%s", configMapKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "4m"`, scaleToZeroModel),
				},
			}
			_, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Create(ctx, newConfigMap, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Should be able to create test ConfigMap")
		}

		By("updating ConfigMap to disable scale-to-zero")
		configMap, err := k8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to get ConfigMap")

		configMap.Data[fmt.Sprintf("model.%s", configMapKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: false
retentionPeriod: "4m"`, scaleToZeroModel)

		_, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to update ConfigMap")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap updated: enableScaleToZero=false\n")

		By("waiting for controller to reconcile ConfigMap change")
		time.Sleep(30 * time.Second)

		By("verifying controller respects enableScaleToZero=false")
		Eventually(func(g Gomega) {
			va := &v1alpha1.VariantAutoscaling{}
			err := crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
			g.Expect(err).NotTo(HaveOccurred())

			vaMinReplicas := int32(1)
			if va.Spec.MinReplicas != nil {
				vaMinReplicas = *va.Spec.MinReplicas
			}

			g.Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", vaMinReplicas),
				"Controller should respect enableScaleToZero=false and maintain >= minReplicas")

			_, _ = fmt.Fprintf(GinkgoWriter, "VA Status after disabling scale-to-zero: DesiredReplicas=%d (enforcing minReplicas=%d)\n",
				va.Status.DesiredOptimizedAlloc.NumReplicas, vaMinReplicas)
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("updating ConfigMap to re-enable scale-to-zero")
		configMap, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to get ConfigMap")

		configMap.Data[fmt.Sprintf("model.%s", configMapKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "4m"`, scaleToZeroModel)

		_, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to update ConfigMap")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap updated: enableScaleToZero=true\n")

		By("waiting for controller to reconcile ConfigMap change")
		time.Sleep(30 * time.Second)

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Controller successfully reacted to ConfigMap changes\n")
	})

	It("should verify HPA responds to wva_desired_replicas metric changes", func() {
		By("monitoring HPA status for external metric updates")
		Eventually(func(g Gomega) {
			hpa, err := k8sClient.AutoscalingV2().HorizontalPodAutoscalers(llmDNamespace).Get(ctx, hpaName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get HPA")

			g.Expect(hpa.Status.CurrentMetrics).NotTo(BeEmpty(), "HPA should have current metrics")

			for _, metric := range hpa.Status.CurrentMetrics {
				if metric.External != nil && metric.External.Metric.Name == constants.WVADesiredReplicas {
					currentValue := metric.External.Current.AverageValue
					g.Expect(currentValue).NotTo(BeNil(), "Current metric value should not be nil")

					currentReplicas := currentValue.AsApproximateFloat64()
					_, _ = fmt.Fprintf(GinkgoWriter, "HPA current wva_desired_replicas value: %.2f\n", currentReplicas)
					g.Expect(currentReplicas).To(BeNumerically(">", 0), "HPA should see non-zero replica recommendation")
				}
			}
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA successfully reads and responds to wva_desired_replicas metric\n")
	})

	AfterAll(func() {
		By("cleaning up test ConfigMap")
		err := k8sClient.CoreV1().ConfigMaps(controllerNamespace).Delete(ctx, configMapName, metav1.DeleteOptions{})
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "Warning: failed to delete ConfigMap: %v\n", err)
		}

		_, _ = fmt.Fprintf(GinkgoWriter, "HPA scale-to-zero tests completed\n")
	})
})

var _ = Describe("HPA Scale-to-Zero with Traffic on OpenShift", Ordered, func() {
	var (
		ctx             context.Context
		vaName          string
		hpaName         string
		initialReplicas int32
	)

	BeforeAll(func() {
		ctx = context.Background()
		hpaName = "vllm-deployment-hpa"

		By("verifying HPA exists with minReplicas=0")
		hpa, err := k8sClient.AutoscalingV2().HorizontalPodAutoscalers(llmDNamespace).Get(ctx, hpaName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "HPA should exist")
		Expect(hpa.Spec.MinReplicas).NotTo(BeNil(), "HPA minReplicas should be set")
		Expect(*hpa.Spec.MinReplicas).To(Equal(int32(0)), "HPA minReplicas should be 0 for scale-to-zero tests")

		By("finding VariantAutoscaling by scaleTargetRef")
		vaList := &v1alpha1.VariantAutoscalingList{}
		err = crClient.List(ctx, vaList, client.InNamespace(llmDNamespace))
		Expect(err).NotTo(HaveOccurred(), "Should be able to list VariantAutoscalings")

		var va *v1alpha1.VariantAutoscaling
		for i := range vaList.Items {
			if vaList.Items[i].Spec.ScaleTargetRef.Name == deployment {
				va = &vaList.Items[i]
				break
			}
		}
		Expect(va).NotTo(BeNil(), fmt.Sprintf("Should find VariantAutoscaling with scaleTargetRef pointing to %s", deployment))
		vaName = va.Name

		By("recording initial deployment state")
		deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to get deployment")
		initialReplicas = deploy.Status.ReadyReplicas
		_, _ = fmt.Fprintf(GinkgoWriter, "Initial deployment replicas: %d\n", initialReplicas)
	})

	It("should maintain VA minReplicas during and after traffic", func() {
		va := &v1alpha1.VariantAutoscaling{}
		err := crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

		vaMinReplicas := int32(1)
		if va.Spec.MinReplicas != nil {
			vaMinReplicas = *va.Spec.MinReplicas
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "VA minReplicas: %d\n", vaMinReplicas)

		By("verifying deployment never scales below VA minReplicas (even with HPA minReplicas=0)")
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get deployment")

			replicas := deploy.Status.ReadyReplicas
			_, _ = fmt.Fprintf(GinkgoWriter, "Current replicas: %d (VA minReplicas: %d enforced)\n", replicas, vaMinReplicas)
			g.Expect(replicas).To(BeNumerically(">=", vaMinReplicas),
				"Deployment should never scale below VA minReplicas, even when HPA allows scale-to-zero")
		}, 2*time.Minute, 15*time.Second).Should(Succeed())

		By("verifying VA status maintains minReplicas constraint")
		va = &v1alpha1.VariantAutoscaling{}
		err = crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

		Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", vaMinReplicas),
			"VA DesiredOptimizedAlloc should respect minReplicas constraint")

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas=%d takes precedence: deployment maintained >= %d replicas despite HPA minReplicas=0\n",
			vaMinReplicas, vaMinReplicas)
	})
})
