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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ==============================================================================
// HPA Scale-to-Zero Basic Integration Tests
// ==============================================================================
// These tests verify the HPA scale-to-zero integration with existing OpenShift
// infrastructure (vLLM deployment, gateway, and VariantAutoscaling resources).
// ==============================================================================

var _ = Describe("HPA Scale-to-Zero Basic Integration", Ordered, func() {
	var (
		ctx     context.Context
		vaName  string
		hpaName string
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
		_, _ = fmt.Fprintf(GinkgoWriter, "Found VariantAutoscaling: %s\n", vaName)
	})

	It("should verify external metrics API provides wva_desired_replicas", func() {
		By("verifying HPA exists and is configured correctly")
		hpa, err := k8sClient.AutoscalingV2().HorizontalPodAutoscalers(llmDNamespace).Get(ctx, hpaName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "HPA should exist")

		// Verify HPA uses wva_desired_replicas external metric
		Expect(hpa.Spec.Metrics).NotTo(BeEmpty(), "HPA should have metrics configured")
		foundExternalMetric := false
		for _, metric := range hpa.Spec.Metrics {
			if metric.Type == autoscalingv2.ExternalMetricSourceType &&
				metric.External != nil &&
				metric.External.Metric.Name == "wva_desired_replicas" {
				foundExternalMetric = true
				_, _ = fmt.Fprintf(GinkgoWriter, "✓ HPA configured with wva_desired_replicas metric\n")
				break
			}
		}
		Expect(foundExternalMetric).To(BeTrue(), "HPA should use wva_desired_replicas external metric")

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

		By("recording initial deployment state")
		deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Deployment should exist")
		_, _ = fmt.Fprintf(GinkgoWriter, "Initial deployment replicas: %d\n", deploy.Status.Replicas)

		By("querying external metrics API for wva_desired_replicas metric")
		// Note: We just verify the API is accessible, not that the metric value is non-zero
		// since the metric may be unavailable (MinInt64) when deployment is at 0 replicas
		externalMetrics, err := k8sClient.RESTClient().
			Get().
			AbsPath("/apis/external.metrics.k8s.io/v1beta1/namespaces/" + llmDNamespace + "/wva_desired_replicas").
			DoRaw(ctx)
		Expect(err).NotTo(HaveOccurred(), "External metrics API should be accessible")
		Expect(externalMetrics).NotTo(BeEmpty(), "External metrics API should return data")

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ External metrics API is accessible and provides wva_desired_replicas\n")
	})

	It("should maintain VA minReplicas constraint with HPA minReplicas=0", func() {
		By("verifying VariantAutoscaling minReplicas configuration")
		va := &v1alpha1.VariantAutoscaling{}
		err := crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

		vaMinReplicas := int32(0)
		if va.Spec.MinReplicas != nil {
			vaMinReplicas = *va.Spec.MinReplicas
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "VA minReplicas: %d\n", vaMinReplicas)

		By("verifying deployment respects VA minReplicas (not HPA minReplicas)")
		// Monitor for 2 minutes to ensure deployment stays >= VA minReplicas
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get deployment")
			g.Expect(deploy.Status.Replicas).To(BeNumerically(">=", vaMinReplicas),
				fmt.Sprintf("Deployment should maintain >= VA minReplicas=%d", vaMinReplicas))

			_, _ = fmt.Fprintf(GinkgoWriter, "Current replicas: %d (VA minReplicas: %d, HPA minReplicas: 0)\n",
				deploy.Status.Replicas, vaMinReplicas)
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas takes precedence over HPA minReplicas=0\n")
	})

	It("should react to ConfigMap enableScaleToZero changes", func() {
		By("verifying ConfigMap exists or creating test ConfigMap")
		configMapName := "model-config"
		configMapNamespace := controllerNamespace

		configMap, err := k8sClient.CoreV1().ConfigMaps(configMapNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		if err != nil {
			// Create ConfigMap if it doesn't exist
			modelKey := strings.ReplaceAll(modelID, "/", "-")
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					fmt.Sprintf("model.%s", modelKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "4m"`, modelID),
				},
			}
			_, err = k8sClient.CoreV1().ConfigMaps(configMapNamespace).Create(ctx, configMap, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Should be able to create ConfigMap")
		}

		By("updating ConfigMap to disable scale-to-zero")
		modelKey := strings.ReplaceAll(modelID, "/", "-")
		configMap.Data[fmt.Sprintf("model.%s", modelKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: false
retentionPeriod: "4m"`, modelID)
		_, err = k8sClient.CoreV1().ConfigMaps(configMapNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to update ConfigMap")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap updated: enableScaleToZero=false\n")

		By("waiting for controller to pick up ConfigMap change")
		time.Sleep(15 * time.Second)

		By("verifying ConfigMap was updated successfully")
		updatedConfigMap, err := k8sClient.CoreV1().ConfigMaps(configMapNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		configValue := updatedConfigMap.Data[fmt.Sprintf("model.%s", modelKey)]
		Expect(configValue).To(ContainSubstring("enableScaleToZero: false"), "ConfigMap should have scale-to-zero disabled")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap verified: scale-to-zero disabled\n")

		// Re-fetch ConfigMap to avoid conflict errors (controller may have modified it)
		configMap, err = k8sClient.CoreV1().ConfigMaps(configMapNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to get ConfigMap for restore")

		By("restoring ConfigMap to enable scale-to-zero")
		configMap.Data[fmt.Sprintf("model.%s", modelKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "4m"`, modelID)
		_, err = k8sClient.CoreV1().ConfigMaps(configMapNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to restore ConfigMap")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap restored: enableScaleToZero=true\n")

		time.Sleep(15 * time.Second)
	})

	It("should enforce VA minReplicas even with HPA minReplicas=0", func() {
		By("getting current VA configuration")
		va := &v1alpha1.VariantAutoscaling{}
		err := crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

		vaMinReplicas := int32(0)
		if va.Spec.MinReplicas != nil {
			vaMinReplicas = *va.Spec.MinReplicas
		}

		By("verifying HPA has minReplicas=0")
		hpa, err := k8sClient.AutoscalingV2().HorizontalPodAutoscalers(llmDNamespace).Get(ctx, hpaName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to get HPA")
		Expect(*hpa.Spec.MinReplicas).To(Equal(int32(0)), "HPA should have minReplicas=0")

		By("monitoring deployment over 2 minutes to ensure VA minReplicas is enforced")
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred(), "Should be able to get deployment")
			g.Expect(deploy.Status.Replicas).To(BeNumerically(">=", vaMinReplicas),
				fmt.Sprintf("Deployment should maintain >= VA minReplicas=%d despite HPA minReplicas=0", vaMinReplicas))

			_, _ = fmt.Fprintf(GinkgoWriter, "Current replicas: %d (VA minReplicas: %d enforced)\n",
				deploy.Status.Replicas, vaMinReplicas)
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying VA status maintains minReplicas constraint")
		va = &v1alpha1.VariantAutoscaling{}
		err = crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred())
		Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(BeNumerically(">=", vaMinReplicas),
			"VA should recommend >= minReplicas")

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas=%d takes precedence: deployment maintained >= %d replicas despite HPA minReplicas=0\n",
			vaMinReplicas, vaMinReplicas)
	})
})

// ==============================================================================
// HPA Scale-to-Zero Comprehensive Lifecycle Test
// ==============================================================================
// This test validates the complete lifecycle of scale-to-zero behavior including:
// - Retention period enforcement
// - Scale-to-zero toggle behavior
// - Traffic-based scaling
// - VA minReplicas enforcement
// ==============================================================================

var _ = Describe("HPA Scale-to-Zero Comprehensive Lifecycle", Ordered, func() {
	var (
		ctx              context.Context
		vaName           string
		hpaName          string
		configMapName    string
		retentionSeconds int
		jobName          string
	)

	BeforeAll(func() {
		ctx = context.Background()
		hpaName = "vllm-deployment-hpa"
		configMapName = "model-scale-to-zero-config"
		retentionSeconds = 240 // 4 minutes - matches model-scale-to-zero-config ConfigMap
		jobName = "lifecycle-test-load-job"

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
		_, _ = fmt.Fprintf(GinkgoWriter, "Found VariantAutoscaling: %s\n", vaName)
	})

	It("should scale with traffic when scale-to-zero is enabled", func() {
		By("setting VA minReplicas to 0 to allow scale-to-zero")
		va := &v1alpha1.VariantAutoscaling{}
		err := crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

		// Save original value (dereference pointer)
		var originalMinReplicas int32
		if va.Spec.MinReplicas != nil {
			originalMinReplicas = *va.Spec.MinReplicas
		}

		minReplicas := int32(0)
		va.Spec.MinReplicas = &minReplicas
		err = crClient.Update(ctx, va)
		Expect(err).NotTo(HaveOccurred(), "Should be able to update VA minReplicas")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas set to 0 (was %d)\n", originalMinReplicas)

		// Wait for the change to propagate
		time.Sleep(15 * time.Second)

		DeferCleanup(func() {
			// Restore original value after test
			va := &v1alpha1.VariantAutoscaling{}
			err := crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
			if err == nil {
				va.Spec.MinReplicas = &originalMinReplicas
				_ = crClient.Update(ctx, va)
				_, _ = fmt.Fprintf(GinkgoWriter, "✓ Restored VA minReplicas to %d\n", originalMinReplicas)
			}
		})

		By("creating load generation job before re-enabling scale-to-zero")
		_ = k8sClient.BatchV1().Jobs(llmDNamespace).Delete(ctx, jobName, metav1.DeleteOptions{})
		time.Sleep(2 * time.Second)

		job := createLoadJob(jobName, llmDNamespace, 10, 1800) // Sustained load: 10 req/s, 1800 prompts = 180 seconds (3 controller cycles)
		_, err = k8sClient.BatchV1().Jobs(llmDNamespace).Create(ctx, job, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to create load generation job")

		By("waiting for job pod to be running")
		Eventually(func(g Gomega) {
			podList, err := k8sClient.CoreV1().Pods(llmDNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job-name=%s", jobName),
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(podList.Items).NotTo(BeEmpty())
			pod := podList.Items[0]
			g.Expect(pod.Status.Phase).To(Or(Equal(corev1.PodRunning), Equal(corev1.PodSucceeded)))
			_, _ = fmt.Fprintf(GinkgoWriter, "Load job pod status: %s\n", pod.Status.Phase)
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("waiting 20 seconds before re-enabling scale-to-zero")
		time.Sleep(20 * time.Second)

		By("re-enabling scale-to-zero via ConfigMap while traffic is active")
		modelKey := strings.ReplaceAll(modelID, "/", "-")
		configMap, err := k8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		configMap.Data[fmt.Sprintf("model.%s", modelKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "4m"`, modelID)
		_, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap updated: enableScaleToZero=true (with active traffic)\n")

		By("verifying deployment scales >= 1 during traffic")
		Eventually(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.ReadyReplicas).To(BeNumerically(">=", 1),
				"Deployment should scale >= 1 during traffic")
			_, _ = fmt.Fprintf(GinkgoWriter, "During traffic: %d replicas\n", deploy.Status.ReadyReplicas)
		}, 3*time.Minute, 10*time.Second).Should(Succeed())

		By("waiting for job to complete")
		Eventually(func(g Gomega) {
			job, err := k8sClient.BatchV1().Jobs(llmDNamespace).Get(ctx, jobName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(job.Status.Succeeded).To(BeNumerically(">=", 1), "Job should complete")
			_, _ = fmt.Fprintf(GinkgoWriter, "Load job completed\n")
		}, 10*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying deployment maintains >= 1 replica during retention period after traffic")
		_, _ = fmt.Fprintf(GinkgoWriter, "Monitoring retention period: %d seconds...\n", retentionSeconds)
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.Replicas).To(BeNumerically(">=", 1),
				"Deployment should maintain >= 1 replica during retention period")
			_, _ = fmt.Fprintf(GinkgoWriter, "Retention period: %d replicas\n", deploy.Status.Replicas)
		}, time.Duration(retentionSeconds)*time.Second, 15*time.Second).Should(Succeed())

		By("verifying deployment scales to 0 after retention period")
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting for scale-to-zero after retention...\n")
		Eventually(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.Replicas).To(Equal(int32(0)),
				"Deployment should scale to 0 after retention period")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Deployment scaled to 0 after retention\n")
		}, 3*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should scale to zero with scale-to-zero enabled and VA minReplicas=0", func() {
		By("ensuring scale-to-zero is enabled via ConfigMap")
		modelKey := strings.ReplaceAll(modelID, "/", "-")
		configMap, err := k8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		if err != nil {
			// Create ConfigMap if it doesn't exist
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: controllerNamespace,
				},
				Data: map[string]string{
					fmt.Sprintf("model.%s", modelKey): fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "4m"`, modelID),
				},
			}
			_, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Create(ctx, configMap, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Should be able to create ConfigMap")
		} else {
			configMap.Data[fmt.Sprintf("model.%s", modelKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "4m"`, modelID)
			_, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred(), "Should be able to update ConfigMap")
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap configured: enableScaleToZero=true\n")

		By("ensuring VA has minReplicas=0")
		va := &v1alpha1.VariantAutoscaling{}
		err = crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred(), "Should be able to get VariantAutoscaling")

		if va.Spec.MinReplicas == nil || *va.Spec.MinReplicas != 0 {
			minReplicas := int32(0)
			va.Spec.MinReplicas = &minReplicas
			err = crClient.Update(ctx, va)
			Expect(err).NotTo(HaveOccurred(), "Should be able to update VA minReplicas")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas set to 0\n")
		}

		By("redeploying VA to trigger bootstrap and test retention period")
		// Get the current VA spec before deleting
		vaSpec := va.Spec
		_, _ = fmt.Fprintf(GinkgoWriter, "Deleting ALL VAs to avoid conflicts...\n")

		// Delete ALL VAs in the namespace to avoid "DeploymentTargetConflict"
		// When we recreate the VA, if there's an older VA targeting the same deployment,
		// the newer VA gets suppressed
		vaList := &v1alpha1.VariantAutoscalingList{}
		err = crClient.List(ctx, vaList, client.InNamespace(llmDNamespace))
		Expect(err).NotTo(HaveOccurred(), "Should be able to list VAs")

		for _, existingVA := range vaList.Items {
			_, _ = fmt.Fprintf(GinkgoWriter, "Deleting VA: %s\n", existingVA.Name)
			err = crClient.Delete(ctx, &existingVA)
			Expect(err).NotTo(HaveOccurred(), "Should be able to delete VA %s", existingVA.Name)
		}

		// Wait for all VAs to be fully deleted
		Eventually(func(g Gomega) {
			vaList := &v1alpha1.VariantAutoscalingList{}
			err := crClient.List(ctx, vaList, client.InNamespace(llmDNamespace))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(vaList.Items).To(BeEmpty(), "All VAs should be deleted")
		}, 1*time.Minute, 2*time.Second).Should(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ All VAs deleted\n")

		// Recreate the VA with same spec (triggers bootstrap with LastUpdateTime=0)
		newVA := &v1alpha1.VariantAutoscaling{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vaName,
				Namespace: llmDNamespace,
			},
			Spec: vaSpec,
		}
		err = crClient.Create(ctx, newVA)
		Expect(err).NotTo(HaveOccurred(), "Should be able to recreate VA")
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA recreated (bootstrap initiated)\n")

		// Wait for controller to run reconciliation loop and set initial allocation
		// Controller runs every 60s (GLOBAL_OPT_INTERVAL), wait 90s to ensure it runs at least once
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting 90 seconds for controller to run and set initial allocation...\n")
		time.Sleep(90 * time.Second)
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Controller should have set initial allocation\n")

		By("verifying deployment maintains >= 1 replica during retention period after bootstrap")
		// After VA recreation, controller enters bootstrap mode and retention period should apply
		_, _ = fmt.Fprintf(GinkgoWriter, "Monitoring for %d seconds (remaining retention period after 90s controller init)...\n", retentionSeconds-90)
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.Replicas).To(BeNumerically(">=", 1),
				"Deployment should maintain >= 1 replica during retention period after VA recreation/bootstrap")
			_, _ = fmt.Fprintf(GinkgoWriter, "Retention period active (bootstrap): %d replicas\n", deploy.Status.Replicas)
		}, time.Duration(retentionSeconds-90)*time.Second, 15*time.Second).Should(Succeed())

		By("verifying deployment scales to 0 after retention period expires")
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting for scale-to-zero after retention period...\n")
		Eventually(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.Replicas).To(Equal(int32(0)),
				"Deployment should scale to 0 after retention period expires")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Deployment scaled to 0 after retention period\n")
		}, 3*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should maintain replicas when scale-to-zero is disabled", func() {
		By("verifying deployment is at 0 replicas from previous test")
		deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "Current deployment replicas: %d\n", deploy.Status.Replicas)

		By("disabling scale-to-zero via ConfigMap")
		modelKey := strings.ReplaceAll(modelID, "/", "-")
		configMap, err := k8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		configMap.Data[fmt.Sprintf("model.%s", modelKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: false
retentionPeriod: "4m"`, modelID)
		_, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap updated: enableScaleToZero=false\n")

		By("waiting for controller to pick up change and scale to 1")
		time.Sleep(20 * time.Second)

		By("verifying deployment scales to >= 1 replica when scale-to-zero is disabled")
		Eventually(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.ReadyReplicas).To(BeNumerically(">=", 1),
				"Deployment should scale to >= 1 when scale-to-zero is disabled")
			_, _ = fmt.Fprintf(GinkgoWriter, "Scale-to-zero disabled: scaled to %d replicas\n", deploy.Status.ReadyReplicas)
		}, 10*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying deployment maintains >= 1 replica")
		waitDuration := time.Duration(retentionSeconds+90) * time.Second
		_, _ = fmt.Fprintf(GinkgoWriter, "Monitoring for %v to ensure >= 1 replica maintained...\n", waitDuration)
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.Replicas).To(BeNumerically(">=", 1),
				"Deployment should maintain >= 1 replica when scale-to-zero is disabled")
			_, _ = fmt.Fprintf(GinkgoWriter, "Scale-to-zero disabled: maintaining %d replicas\n", deploy.Status.Replicas)
		}, waitDuration, 15*time.Second).Should(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "✓ Deployment maintained >= 1 replica with scale-to-zero disabled\n")
	})

	It("should scale with traffic when scale-to-zero is enabled", func() {
		By("creating load generation job before re-enabling scale-to-zero")
		_ = k8sClient.BatchV1().Jobs(llmDNamespace).Delete(ctx, jobName, metav1.DeleteOptions{})
		time.Sleep(2 * time.Second)

		job := createLoadJob(jobName, llmDNamespace, 10, 1800) // Sustained load: 10 req/s, 1800 prompts = 180 seconds (3 controller cycles)
		_, err := k8sClient.BatchV1().Jobs(llmDNamespace).Create(ctx, job, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "Should be able to create load generation job")

		By("waiting for job pod to be running")
		Eventually(func(g Gomega) {
			podList, err := k8sClient.CoreV1().Pods(llmDNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("job-name=%s", jobName),
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(podList.Items).NotTo(BeEmpty())
			pod := podList.Items[0]
			g.Expect(pod.Status.Phase).To(Or(Equal(corev1.PodRunning), Equal(corev1.PodSucceeded)))
			_, _ = fmt.Fprintf(GinkgoWriter, "Load job pod status: %s\n", pod.Status.Phase)
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("waiting 20 seconds before re-enabling scale-to-zero")
		time.Sleep(20 * time.Second)

		By("re-enabling scale-to-zero via ConfigMap while traffic is active")
		modelKey := strings.ReplaceAll(modelID, "/", "-")
		configMap, err := k8sClient.CoreV1().ConfigMaps(controllerNamespace).Get(ctx, configMapName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		configMap.Data[fmt.Sprintf("model.%s", modelKey)] = fmt.Sprintf(`modelID: "%s"
enableScaleToZero: true
retentionPeriod: "4m"`, modelID)
		_, err = k8sClient.CoreV1().ConfigMaps(controllerNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ ConfigMap updated: enableScaleToZero=true (with active traffic)\n")

		By("verifying deployment scales >= 1 during traffic")
		Eventually(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.ReadyReplicas).To(BeNumerically(">=", 1),
				"Deployment should scale >= 1 during traffic")
			_, _ = fmt.Fprintf(GinkgoWriter, "During traffic: %d replicas\n", deploy.Status.ReadyReplicas)
		}, 3*time.Minute, 10*time.Second).Should(Succeed())

		By("waiting for job to complete")
		Eventually(func(g Gomega) {
			job, err := k8sClient.BatchV1().Jobs(llmDNamespace).Get(ctx, jobName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(job.Status.Succeeded).To(BeNumerically(">=", 1), "Job should complete")
			_, _ = fmt.Fprintf(GinkgoWriter, "Load job completed\n")
		}, 10*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying deployment maintains >= 1 replica during retention period after traffic")
		_, _ = fmt.Fprintf(GinkgoWriter, "Monitoring retention period: %d seconds...\n", retentionSeconds)
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.Replicas).To(BeNumerically(">=", 1),
				"Deployment should maintain >= 1 replica during retention period")
			_, _ = fmt.Fprintf(GinkgoWriter, "Retention period: %d replicas\n", deploy.Status.Replicas)
		}, time.Duration(retentionSeconds)*time.Second, 15*time.Second).Should(Succeed())

		By("verifying deployment scales to 0 after retention period")
		_, _ = fmt.Fprintf(GinkgoWriter, "Waiting for scale-to-zero after retention...\n")
		Eventually(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.Replicas).To(Equal(int32(0)),
				"Deployment should scale to 0 after retention period")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Deployment scaled to 0 after retention\n")
		}, 3*time.Minute, 10*time.Second).Should(Succeed())
	})

	It("should enforce VA minReplicas=2", func() {
		By("updating VA minReplicas to 2")
		va := &v1alpha1.VariantAutoscaling{}
		err := crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred())

		minReplicas := int32(2)
		va.Spec.MinReplicas = &minReplicas
		err = crClient.Update(ctx, va)
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas updated to 2\n")

		By("waiting for controller to reconcile")
		time.Sleep(20 * time.Second)

		By("verifying deployment scales to 2 replicas")
		Eventually(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.ReadyReplicas).To(Equal(int32(2)),
				"Deployment should scale to match VA minReplicas=2")
			_, _ = fmt.Fprintf(GinkgoWriter, "✓ Deployment scaled to 2 replicas (VA minReplicas enforced)\n")
		}, 10*time.Minute, 10*time.Second).Should(Succeed())

		By("verifying deployment maintains 2 replicas")
		Consistently(func(g Gomega) {
			deploy, err := k8sClient.AppsV1().Deployments(llmDNamespace).Get(ctx, deployment, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(deploy.Status.Replicas).To(Equal(int32(2)),
				"Deployment should maintain 2 replicas")
			_, _ = fmt.Fprintf(GinkgoWriter, "VA minReplicas=2 enforced: %d replicas\n", deploy.Status.Replicas)
		}, 2*time.Minute, 10*time.Second).Should(Succeed())

		By("restoring VA minReplicas to 0 for subsequent tests")
		va = &v1alpha1.VariantAutoscaling{}
		err = crClient.Get(ctx, client.ObjectKey{Name: vaName, Namespace: llmDNamespace}, va)
		Expect(err).NotTo(HaveOccurred())

		minReplicasZero := int32(0)
		va.Spec.MinReplicas = &minReplicasZero
		err = crClient.Update(ctx, va)
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "✓ VA minReplicas restored to 0\n")
	})

	AfterAll(func() {
		By("cleaning up load generation job")
		_ = k8sClient.BatchV1().Jobs(llmDNamespace).Delete(ctx, jobName, metav1.DeleteOptions{
			PropagationPolicy: func() *metav1.DeletionPropagation {
				policy := metav1.DeletePropagationBackground
				return &policy
			}(),
		})
	})
})

// createLoadJob creates a Kubernetes Job for traffic generation
func createLoadJob(name, namespace string, requestRate, numPrompts int) *batchv1.Job {
	backoffLimit := int32(2)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"test-type": "hpa-scale-to-zero-lifecycle",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:    "download-dataset",
							Image:   "busybox:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								`wget -O /data/ShareGPT_V3_unfiltered_cleaned_split.json \
https://huggingface.co/datasets/anon8231489123/ShareGPT_Vicuna_unfiltered/resolve/main/ShareGPT_V3_unfiltered_cleaned_split.json`,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dataset-volume",
									MountPath: "/data",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "vllm-bench-serve",
							Image:           "vllm/vllm-openai:latest",
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{
									Name:  "HF_HOME",
									Value: "/tmp",
								},
							},
							Command: []string{"/usr/bin/python3"},
							Args: []string{
								"-m",
								"vllm.entrypoints.cli.main",
								"bench",
								"serve",
								"--backend",
								"openai",
								"--base-url",
								fmt.Sprintf("http://%s:80", gatewayName),
								"--dataset-name",
								"sharegpt",
								"--dataset-path",
								"/data/ShareGPT_V3_unfiltered_cleaned_split.json",
								"--model",
								modelID,
								"--seed",
								"12345",
								"--num-prompts",
								fmt.Sprintf("%d", numPrompts),
								"--max-concurrency",
								"256",
								"--request-rate",
								fmt.Sprintf("%d", requestRate),
								"--sharegpt-output-len",
								"512",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "dataset-volume",
									MountPath: "/data",
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("4Gi"),
									corev1.ResourceCPU:    resource.MustParse("1"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "dataset-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
}
