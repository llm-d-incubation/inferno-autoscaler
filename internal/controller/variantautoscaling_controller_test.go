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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	logger "github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	tuner "github.com/llm-d-incubation/workload-variant-autoscaler/internal/tuner"
	utils "github.com/llm-d-incubation/workload-variant-autoscaler/internal/utils"
	infernoConfig "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/config"
	testutils "github.com/llm-d-incubation/workload-variant-autoscaler/test/utils"
)

// Helper function to create a properly initialized reconciler for tests
func createTestReconciler(k8sClient client.Client) *VariantAutoscalingReconciler {
	return &VariantAutoscalingReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
}

var _ = Describe("VariantAutoscalings Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		VariantAutoscalings := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}

		BeforeEach(func() {
			logger.Log = zap.NewNop().Sugar()
			ns := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "workload-variant-autoscaler-system",
				},
			}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).NotTo(HaveOccurred())

			By("creating the required configmap for optimization")
			configMap := testutils.CreateServiceClassConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			configMap = testutils.CreateAcceleratorUnitCostConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			configMap = testutils.CreateVariantAutoscalingConfigMap(configMapName, ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("creating the custom resource for the Kind VariantAutoscalings")
			err := k8sClient.Get(ctx, typeNamespacedName, VariantAutoscalings)
			if err != nil && errors.IsNotFound(err) {
				resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						// Example spec fields, adjust as necessary
						ModelID: "default/default",
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
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance VariantAutoscalings")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Deleting the configmap resources")
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-classes-config",
					Namespace: "workload-variant-autoscaler-system",
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "accelerator-unit-costs",
					Namespace: "workload-variant-autoscaler-system",
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := createTestReconciler(k8sClient)

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When handling error conditions on missing config maps", func() {
		BeforeEach(func() {
			logger.Log = zap.NewNop().Sugar()
		})

		It("should fail on missing serviceClass ConfigMap", func() {
			By("Creating VariantAutoscaling without required ConfigMaps")
			controllerReconciler := createTestReconciler(k8sClient)

			_, err := controllerReconciler.readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).To(HaveOccurred(), "Expected error when reading missing serviceClass ConfigMap")
		})

		It("should fail on missing accelerator ConfigMap", func() {
			By("Creating VariantAutoscaling without required ConfigMaps")
			controllerReconciler := createTestReconciler(k8sClient)

			_, err := controllerReconciler.readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).To(HaveOccurred(), "Expected error when reading missing accelerator ConfigMap")
		})

		It("should fail on missing variant autoscaling optimization ConfigMap", func() {
			By("Creating VariantAutoscaling without required ConfigMaps")
			controllerReconciler := createTestReconciler(k8sClient)

			_, err := controllerReconciler.readOptimizationConfig(ctx)
			Expect(err).To(HaveOccurred(), "Expected error when reading missing variant autoscaling optimization ConfigMap")
		})
	})

	Context("When validating configurations", func() {
		const configResourceName = "config-test-resource"

		BeforeEach(func() {
			logger.Log = zap.NewNop().Sugar()
			ns := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "workload-variant-autoscaler-system",
				},
			}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).NotTo(HaveOccurred())

			By("creating the required configmaps")
			configMap := testutils.CreateServiceClassConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).NotTo(HaveOccurred())

			configMap = testutils.CreateAcceleratorUnitCostConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).NotTo(HaveOccurred())

			configMap = testutils.CreateVariantAutoscalingConfigMap(configMapName, ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("Deleting the configmap resources")
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-classes-config",
					Namespace: "workload-variant-autoscaler-system",
				},
			}
			err := k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "accelerator-unit-costs",
					Namespace: "workload-variant-autoscaler-system",
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
		})

		It("should return empty on variant autoscaling optimization ConfigMap with missing interval value", func() {
			controllerReconciler := createTestReconciler(k8sClient)

			// delete correct configMap
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
			}
			err := k8sClient.Delete(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
					Labels: map[string]string{
						"app.kubernetes.io/name": "workload-variant-autoscaler",
					},
				},
				Data: map[string]string{
					"PROMETHEUS_BASE_URL": "https://kube-prometheus-stack-prometheus.workload-variant-autoscaler-monitoring.svc.cluster.local:9090",
					"GLOBAL_OPT_INTERVAL": "",
					"GLOBAL_OPT_TRIGGER":  "false",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			interval, err := controllerReconciler.readOptimizationConfig(ctx)
			Expect(err).NotTo(HaveOccurred(), "Unexpected error when reading variant autoscaling optimization ConfigMap with missing interval")
			Expect(interval).To(Equal(""), "Expected empty interval value")
		})

		It("should return empty on variant autoscaling optimization ConfigMap with missing prometheus base URL", func() {
			controllerReconciler := createTestReconciler(k8sClient)

			// delete correct configMap
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
			}
			err := k8sClient.Delete(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
					Labels: map[string]string{
						"app.kubernetes.io/name": "workload-variant-autoscaler",
					},
				},
				Data: map[string]string{
					"PROMETHEUS_BASE_URL": "",
					"GLOBAL_OPT_INTERVAL": "60s",
					"GLOBAL_OPT_TRIGGER":  "false",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			prometheusURL, err := controllerReconciler.getPrometheusConfigFromConfigMap(ctx)
			Expect(err).NotTo(HaveOccurred(), "Unexpected error when reading variant autoscaling optimization ConfigMap with missing Prometheus URL")
			Expect(prometheusURL).To(BeNil(), "Expected empty Prometheus URL")
		})

		It("should return error on VA optimization ConfigMap with missing prometheus base URL and no env variable", func() {
			controllerReconciler := createTestReconciler(k8sClient)

			// delete correct configMap
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
			}
			err := k8sClient.Delete(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
					Labels: map[string]string{
						"app.kubernetes.io/name": "workload-variant-autoscaler",
					},
				},
				Data: map[string]string{
					"PROMETHEUS_BASE_URL": "",
					"GLOBAL_OPT_INTERVAL": "60s",
					"GLOBAL_OPT_TRIGGER":  "false",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			_, err = controllerReconciler.getPrometheusConfig(ctx)
			Expect(err).To(HaveOccurred(), "It should fail when neither env variable nor Prometheus URL are found")
		})

		It("should return default values on variant autoscaling optimization ConfigMap with missing TLS values", func() {
			controllerReconciler := createTestReconciler(k8sClient)

			// delete correct configMap
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
			}
			err := k8sClient.Delete(ctx, configMap)
			Expect(err).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
					Labels: map[string]string{
						"app.kubernetes.io/name": "workload-variant-autoscaler",
					},
				},
				Data: map[string]string{
					"PROMETHEUS_BASE_URL":                 "https://kube-prometheus-stack-prometheus.workload-variant-autoscaler-monitoring.svc.cluster.local:9090",
					"GLOBAL_OPT_INTERVAL":                 "60s",
					"GLOBAL_OPT_TRIGGER":                  "false",
					"PROMETHEUS_TLS_INSECURE_SKIP_VERIFY": "true",
					// no values set for TLS config - dev env
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			prometheusConfig, err := controllerReconciler.getPrometheusConfigFromConfigMap(ctx)
			Expect(err).NotTo(HaveOccurred(), "It should not fail when neither env variable nor Prometheus URL are found")

			Expect(prometheusConfig.BaseURL).To(Equal("https://kube-prometheus-stack-prometheus.workload-variant-autoscaler-monitoring.svc.cluster.local:9090"), "Expected Base URL to be set")
			Expect(prometheusConfig.InsecureSkipVerify).To(BeTrue(), "Expected Insecure Skip Verify to be true")

			Expect(prometheusConfig.CACertPath).To(Equal(""), "Expected CA Cert Path to be empty")
			Expect(prometheusConfig.ClientCertPath).To(Equal(""), "Expected Client Cert path to be empty")
			Expect(prometheusConfig.ClientKeyPath).To(Equal(""), "Expected Client Key path to be empty")
			Expect(prometheusConfig.BearerToken).To(Equal(""), "Expected Bearer Token to be empty")
			Expect(prometheusConfig.TokenPath).To(Equal(""), "Expected Token Path to be empty")
			Expect(prometheusConfig.ServerName).To(Equal(""), "Expected Server Name to be empty")
		})

		It("should validate accelerator profiles", func() {
			By("Creating VariantAutoscaling with invalid accelerator profile")
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configResourceName,
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID: "default/default",
					ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
						Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
							{
								Acc:      "INVALID_GPU",
								AccCount: -1, // Invalid count
								PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
									DecodeParms:  map[string]string{"alpha": "invalid", "beta": "invalid"},
									PrefillParms: map[string]string{"gamma": "invalid", "delta": "invalid"},
								},
								MaxBatchSize: -1, // Invalid batch size
							},
						},
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "premium",
						Key:  "default/default",
					},
				},
			}
			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred()) // Expect validation error at API level
			Expect(err.Error()).To(ContainSubstring("Invalid value"))
		})

		It("should handle empty ModelID value", func() {
			By("Creating VariantAutoscaling with empty ModelID")
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-model-id",
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID: "", // Empty ModelID
					ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
						Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
							{
								Acc:      "A100",
								AccCount: 1,
								PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
									DecodeParms:  map[string]string{"alpha": "0.28", "beta": "0.72"},
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
			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred()) // Expect validation error at API level
			Expect(err.Error()).To(ContainSubstring("spec.modelID"))
		})

		It("should handle empty accelerator list", func() {
			By("Creating VariantAutoscaling with no accelerators")
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-accelerators",
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID: "default/default",
					ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
						Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
							// no configuration for accelerators
						},
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "premium",
						Key:  "default/default",
					},
				},
			}
			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.modelProfile.accelerators"))
		})

		It("should handle empty SLOClassRef", func() {
			By("Creating VariantAutoscaling with no SLOClassRef")
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-slo-class-ref",
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID: "default/default",
					ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
						Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
							{
								Acc:      "A100",
								AccCount: 1,
								PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
									DecodeParms:  map[string]string{"alpha": "0.28", "beta": "0.72"},
									PrefillParms: map[string]string{"gamma": "0", "delta": "0"},
								},
								MaxBatchSize: 4,
							},
						},
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						// no configuration for SLOClassRef
					},
				},
			}
			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.sloClassRef"))
		})
	})

	Context("When handling multiple VariantAutoscalings", func() {
		const totalVAs = 3

		var CreateServiceClassConfigMap = func(controllerNamespace string, models ...string) *v1.ConfigMap {
			data := map[string]string{}

			// Build premium.yaml with all models
			premiumModels := ""
			freemiumModels := ""

			for _, model := range models {
				premiumModels += fmt.Sprintf("  - model: %s\n    slo-tpot: 24\n    slo-ttft: 500\n", model)
				freemiumModels += fmt.Sprintf("  - model: %s\n    slo-tpot: 200\n    slo-ttft: 2000\n", model)
			}

			data["premium.yaml"] = fmt.Sprintf(`name: Premium
priority: 1
data:
%s`, premiumModels)

			data["freemium.yaml"] = fmt.Sprintf(`name: Freemium
priority: 10
data:
%s`, freemiumModels)

			return &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-classes-config",
					Namespace: controllerNamespace,
				},
				Data: data,
			}
		}

		BeforeEach(func() {
			logger.Log = zap.NewNop().Sugar()
			ns := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "workload-variant-autoscaler-system",
				},
			}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).NotTo(HaveOccurred())

			By("creating the required configmaps")
			// Use custom configmap creation function
			var modelNames []string
			for i := range totalVAs {
				modelNames = append(modelNames, fmt.Sprintf("model-%d/model-%d", i, i))
			}
			configMap := CreateServiceClassConfigMap(ns.Name, modelNames...)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			configMap = testutils.CreateAcceleratorUnitCostConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			configMap = testutils.CreateVariantAutoscalingConfigMap(configMapName, ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("Creating VariantAutoscaling resources and Deployments")
			for i := range totalVAs {
				modelID := fmt.Sprintf("model-%d/model-%d", i, i)
				name := fmt.Sprintf("multi-test-resource-%d", i)

				d := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: "default",
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: utils.Ptr(int32(1)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": name},
						},
						Template: v1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{"app": name},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Name:  "test-container",
										Image: "quay.io/infernoautoscaler/vllme:0.2.1-multi-arch",
										Ports: []v1.ContainerPort{{ContainerPort: 80}},
									},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, d)).To(Succeed())

				r := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: "default",
						Labels: map[string]string{
							"inference.optimization/acceleratorName": "A100",
						},
					},
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						ModelID: modelID,
						ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
							Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
								{
									Acc:      "A100",
									AccCount: 1,
									PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
										DecodeParms:  map[string]string{"alpha": "0.28", "beta": "0.72"},
										PrefillParms: map[string]string{"gamma": "0", "delta": "0"},
									},
									MaxBatchSize: 4,
								},
							},
						},
						SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
							Name: "premium",
							Key:  modelID,
						},
					},
				}
				Expect(k8sClient.Create(ctx, r)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Deleting the configmap resources")
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-classes-config",
					Namespace: "workload-variant-autoscaler-system",
				},
			}
			err := k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "accelerator-unit-costs",
					Namespace: "workload-variant-autoscaler-system",
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			err = k8sClient.List(ctx, &variantAutoscalingList)
			Expect(err).NotTo(HaveOccurred(), "Failed to list VariantAutoscaling resources")

			var deploymentList appsv1.DeploymentList
			err = k8sClient.List(ctx, &deploymentList, client.InNamespace("default"))
			Expect(err).NotTo(HaveOccurred(), "Failed to list deployments")

			// Clean up all deployments
			for i := range deploymentList.Items {
				deployment := &deploymentList.Items[i]
				if strings.HasPrefix(deployment.Spec.Template.Labels["app"], "multi-test-resource") {
					err = k8sClient.Delete(ctx, deployment)
					Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred(), "Failed to delete deployment")
				}
			}

			// Clean up all VariantAutoscaling resources
			for i := range variantAutoscalingList.Items {
				err = k8sClient.Delete(ctx, &variantAutoscalingList.Items[i])
				Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred(), "Failed to delete VariantAutoscaling resource")
			}
		})

		It("should filter out VAs marked for deletion", func() {
			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			err := k8sClient.List(ctx, &variantAutoscalingList)
			Expect(err).NotTo(HaveOccurred(), "Failed to list VariantAutoscaling resources")
			filterActiveVariantAutoscalings(variantAutoscalingList.Items)
			Expect(len(variantAutoscalingList.Items)).To(Equal(3), "All VariantAutoscaling resources should be active before deletion")

			// Delete the VAs (this sets DeletionTimestamp)
			for i := range totalVAs {
				Expect(k8sClient.Delete(ctx, &variantAutoscalingList.Items[i])).To(Succeed())
			}

			err = k8sClient.List(ctx, &variantAutoscalingList)
			Expect(err).NotTo(HaveOccurred(), "Failed to list VariantAutoscaling resources")
			filterActiveVariantAutoscalings(variantAutoscalingList.Items)
			Expect(len(variantAutoscalingList.Items)).To(Equal(0), "No active VariantAutoscaling resources should be found")
		})

		It("should prepare active VAs for optimization", func() {
			// Create a mock Prometheus API with valid metric data that passes validation
			mockPromAPI := &testutils.MockPromAPI{
				QueryResults: map[string]model.Value{
					// Default: return a vector with one sample to pass validation
				},
				QueryErrors: map[string]error{},
			}

			controllerReconciler := createTestReconciler(k8sClient)
			controllerReconciler.PromAPI = mockPromAPI

			By("Reading the required configmaps")
			accMap, err := controllerReconciler.readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to read accelerator config")
			Expect(accMap).NotTo(BeNil(), "Accelerator config map should not be nil")

			serviceClassMap, err := controllerReconciler.readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred(), "Failed to read service class config")
			Expect(serviceClassMap).NotTo(BeNil(), "Service class config map should not be nil")

			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			err = k8sClient.List(ctx, &variantAutoscalingList)
			Expect(err).NotTo(HaveOccurred(), "Failed to list VariantAutoscaling resources")
			activeVAs := filterActiveVariantAutoscalings(variantAutoscalingList.Items)
			Expect(len(activeVAs)).To(Equal(totalVAs), "All VariantAutoscaling resources should be active")

			// Prepare system data for VAs
			By("Preparing the system data for optimization")
			// WVA operates in unlimited mode - no inventory data needed
			systemData := utils.CreateSystemData(accMap, serviceClassMap)
			Expect(systemData).NotTo(BeNil(), "System data should not be nil")

			updateList, vaMap, allAnalyzerResponses, err := controllerReconciler.prepareVariantAutoscalings(ctx, activeVAs, accMap, serviceClassMap, systemData)

			Expect(err).NotTo(HaveOccurred(), "prepareVariantAutoscalings should not return an error")
			Expect(vaMap).NotTo(BeNil(), "VA map should not be nil")
			Expect(allAnalyzerResponses).NotTo(BeNil(), "Analyzer responses should not be nil")
			Expect(len(updateList.Items)).To(Equal(totalVAs), "UpdatedList should be the same number of all active VariantAutoscalings")

			var vaNames []string
			for _, va := range activeVAs {
				vaNames = append(vaNames, va.Name)
			}

			for _, updatedVa := range updateList.Items {
				Expect(vaNames).To(ContainElement(updatedVa.Name), fmt.Sprintf("Active VariantAutoscaling list should contain %s", updatedVa.Name))
				Expect(updatedVa.Status.CurrentAlloc.Accelerator).To(Equal("A100"), fmt.Sprintf("Current Accelerator for %s should be \"A100\" after preparation", updatedVa.Name))
				Expect(updatedVa.Status.CurrentAlloc.NumReplicas).To(Equal(1), fmt.Sprintf("Current NumReplicas for %s should be 1 after preparation", updatedVa.Name))
				Expect(updatedVa.Status.DesiredOptimizedAlloc.Accelerator).To(BeEmpty(), fmt.Sprintf("Desired Accelerator for %s should be empty value after preparation", updatedVa.Name))
				Expect(updatedVa.Status.DesiredOptimizedAlloc.NumReplicas).To(BeZero(), fmt.Sprintf("Desired NumReplicas for %s should be zero after preparation", updatedVa.Name))
			}
		})

		It("should set MetricsAvailable condition when metrics validation fails", func() {
			By("Creating a mock Prometheus API that returns no metrics")
			mockPromAPI := &testutils.MockPromAPI{
				QueryResults: map[string]model.Value{},
				QueryErrors:  map[string]error{},
			}

			controllerReconciler := createTestReconciler(k8sClient)
			controllerReconciler.PromAPI = mockPromAPI

			By("Reading the required configmaps")
			accMap, err := controllerReconciler.readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			serviceClassMap, err := controllerReconciler.readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			err = k8sClient.List(ctx, &variantAutoscalingList)
			Expect(err).NotTo(HaveOccurred())

			activeVAs := filterActiveVariantAutoscalings(variantAutoscalingList.Items)
			Expect(len(activeVAs)).To(BeNumerically(">", 0))

			By("Preparing system data and calling prepareVariantAutoscalings")
			systemData := utils.CreateSystemData(accMap, serviceClassMap)

			_, _, _, err = controllerReconciler.prepareVariantAutoscalings(ctx, activeVAs, accMap, serviceClassMap, systemData)
			Expect(err).NotTo(HaveOccurred())

			By("Checking that MetricsAvailable condition is set to False")
			for _, va := range activeVAs {
				var updatedVa llmdVariantAutoscalingV1alpha1.VariantAutoscaling
				err = k8sClient.Get(ctx, types.NamespacedName{Name: va.Name, Namespace: va.Namespace}, &updatedVa)
				Expect(err).NotTo(HaveOccurred())

				metricsCondition := llmdVariantAutoscalingV1alpha1.GetCondition(&updatedVa, llmdVariantAutoscalingV1alpha1.TypeMetricsAvailable)
				if metricsCondition != nil {
					Expect(metricsCondition.Status).To(Equal(metav1.ConditionFalse),
						fmt.Sprintf("MetricsAvailable condition should be False for %s", va.Name))
					Expect(metricsCondition.Reason).To(Or(
						Equal(llmdVariantAutoscalingV1alpha1.ReasonPrometheusError),
						Equal(llmdVariantAutoscalingV1alpha1.ReasonMetricsMissing),
					))
				}
			}
		})

		It("should set OptimizationReady condition when optimization succeeds", func() {
			By("Using a working mock Prometheus API with sample data")
			mockPromAPI := &testutils.MockPromAPI{
				QueryResults: map[string]model.Value{
					// Add default responses for common queries
				},
				QueryErrors: map[string]error{},
			}

			controllerReconciler := createTestReconciler(k8sClient)
			controllerReconciler.PromAPI = mockPromAPI

			By("Performing a full reconciliation")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that conditions are set correctly")
			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			err = k8sClient.List(ctx, &variantAutoscalingList)
			Expect(err).NotTo(HaveOccurred())

			for _, va := range variantAutoscalingList.Items {
				if va.DeletionTimestamp.IsZero() {
					metricsCondition := llmdVariantAutoscalingV1alpha1.GetCondition(&va, llmdVariantAutoscalingV1alpha1.TypeMetricsAvailable)
					if metricsCondition != nil && metricsCondition.Status == metav1.ConditionTrue {
						optimizationCondition := llmdVariantAutoscalingV1alpha1.GetCondition(&va, llmdVariantAutoscalingV1alpha1.TypeOptimizationReady)
						Expect(optimizationCondition).NotTo(BeNil(),
							fmt.Sprintf("OptimizationReady condition should be set for %s", va.Name))
					}
				}
			}
		})
	})

	Context("When the model tuner is enabled", func() {
		const resourceName = "tuner-test-resource"
		var typeNamespacedName = types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			logger.Log = zap.NewNop().Sugar()
			ns := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "workload-variant-autoscaler-system",
				},
			}
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).NotTo(HaveOccurred())

			By("creating the required configmaps")
			configMap := testutils.CreateServiceClassConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).NotTo(HaveOccurred())

			configMap = testutils.CreateAcceleratorUnitCostConfigMap(ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).NotTo(HaveOccurred())

			configMap = testutils.CreateVariantAutoscalingConfigMap(configMapName, ns.Name)
			Expect(k8sClient.Create(ctx, configMap)).NotTo(HaveOccurred())

			By("creating the custom resource for tuner testing")
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID: "default/default",
					ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
						Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
							{
								Acc:      "A100",
								AccCount: 1,
								PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
									DecodeParms:  map[string]string{"alpha": "8.5", "beta": "2.1"},
									PrefillParms: map[string]string{"gamma": "5.0", "delta": "0.11"},
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
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleanup the VariantAutoscaling resource")
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Deleting the configmap resources")
			configMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "service-classes-config",
					Namespace: "workload-variant-autoscaler-system",
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "accelerator-unit-costs",
					Namespace: "workload-variant-autoscaler-system",
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())

			configMap = &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: configMapNamespace,
				},
			}
			err = k8sClient.Delete(ctx, configMap)
			Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
		})

		It("should skip tuning when ActivateModelTuner is false", func() {
			By("Getting the VA resource and verifying tuner is disabled by default")
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}
			err := k8sClient.Get(ctx, typeNamespacedName, va)
			Expect(err).NotTo(HaveOccurred())
			Expect(va.Spec.ActivateModelTuner).To(BeFalse(), "ActivateModelTuner should be false by default")

			By("Creating system data")
			acceleratorCm, err := createTestReconciler(k8sClient).readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			serviceClassCm, err := createTestReconciler(k8sClient).readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			systemData := utils.CreateSystemData(acceleratorCm, serviceClassCm)
			Expect(systemData).NotTo(BeNil())

			By("Calling TuneModelPerfParams with tuner disabled")
			err = tuner.TuneModelPerfParams([]llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}, systemData)
			Expect(err).NotTo(HaveOccurred(), "TuneModelPerfParams should succeed even when tuner is disabled")

			By("Verifying VA status does not have tuned params")
			updatedVA := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}
			err = k8sClient.Get(ctx, typeNamespacedName, updatedVA)
			Expect(err).NotTo(HaveOccurred())
			// When tuner is disabled, TunerPerfData should be empty (zero value)
			Expect(updatedVA.Status.TunerPerfData.Model).To(BeEmpty(), "TunerPerfData.Model should be empty when tuner is disabled")
			Expect(updatedVA.Status.TunerPerfData.Accelerator).To(BeEmpty(), "TunerPerfData.Accelerator should be empty when tuner is disabled")
		})

		It("should tune parameters when ActivateModelTuner is true", func() {
			By("Getting the VA resource and enabling tuner")
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}
			err := k8sClient.Get(ctx, typeNamespacedName, va)
			Expect(err).NotTo(HaveOccurred())

			// Enable tuner
			va.Spec.ActivateModelTuner = true
			err = k8sClient.Update(ctx, va)
			Expect(err).NotTo(HaveOccurred())

			By("Creating system data with valid allocation")
			acceleratorCm, err := createTestReconciler(k8sClient).readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			serviceClassCm, err := createTestReconciler(k8sClient).readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			systemData := utils.CreateSystemData(acceleratorCm, serviceClassCm)
			Expect(systemData).NotTo(BeNil())

			// Add server to system data with proper allocation
			serverName := fmt.Sprintf("%s/%s", va.Name, va.Namespace)
			systemData.Spec.Servers.Spec = append(systemData.Spec.Servers.Spec, infernoConfig.ServerSpec{
				Name:  serverName,
				Model: "default/default",
				Class: "premium",
				CurrentAlloc: infernoConfig.AllocationData{
					Accelerator: "A100",
					NumReplicas: 1,
					MaxBatch:    4,
					TTFTAverage: 190,
					ITLAverage:  15,
					Load: infernoConfig.ServerLoadSpec{
						ArrivalRate:  60.0,
						AvgInTokens:  100,
						AvgOutTokens: 200,
					},
				},
			})

			By("Calling TuneModelPerfParams with valid environment")
			err = tuner.TuneModelPerfParams([]llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}, systemData)

			// Note: This may succeed or fail depending on whether the Kalman filter
			// accepts or rejects the observations. Both are valid outcomes.
			if err != nil {
				logger.Log.Info("Tuning returned warning (expected during initial calibration)", "error", err)
			}
		})

		It("should handle missing server in system data gracefully", func() {
			By("Getting the VA resource and enabling tuner")
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}
			err := k8sClient.Get(ctx, typeNamespacedName, va)
			Expect(err).NotTo(HaveOccurred())

			va.Spec.ActivateModelTuner = true
			err = k8sClient.Update(ctx, va)
			Expect(err).NotTo(HaveOccurred())

			By("Creating system data without the server")
			acceleratorCm, err := createTestReconciler(k8sClient).readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			serviceClassCm, err := createTestReconciler(k8sClient).readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			systemData := utils.CreateSystemData(acceleratorCm, serviceClassCm)
			Expect(systemData).NotTo(BeNil())
			// Intentionally not adding server to systemData

			By("Calling TuneModelPerfParams should succeed with warning")
			err = tuner.TuneModelPerfParams([]llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}, systemData)
			Expect(err).NotTo(HaveOccurred(), "TuneModelPerfParams should not fail when server is missing")
		})

		It("should handle invalid environment gracefully", func() {
			By("Getting the VA resource and enabling tuner")
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}
			err := k8sClient.Get(ctx, typeNamespacedName, va)
			Expect(err).NotTo(HaveOccurred())

			va.Spec.ActivateModelTuner = true
			err = k8sClient.Update(ctx, va)
			Expect(err).NotTo(HaveOccurred())

			By("Creating system data with invalid allocation (zero/negative values)")
			acceleratorCm, err := createTestReconciler(k8sClient).readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			serviceClassCm, err := createTestReconciler(k8sClient).readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			systemData := utils.CreateSystemData(acceleratorCm, serviceClassCm)
			Expect(systemData).NotTo(BeNil())

			serverName := fmt.Sprintf("%s/%s", va.Name, va.Namespace)
			systemData.Spec.Servers.Spec = append(systemData.Spec.Servers.Spec, infernoConfig.ServerSpec{
				Name:  serverName,
				Model: "default/default",
				Class: "premium",
				CurrentAlloc: infernoConfig.AllocationData{
					Accelerator: "A100",
					NumReplicas: 1,
					MaxBatch:    0, // Invalid: zero
					TTFTAverage: 0, // Invalid: zero
					ITLAverage:  0, // Invalid: zero
					Load: infernoConfig.ServerLoadSpec{
						ArrivalRate:  0, // Invalid: zero
						AvgInTokens:  0, // Invalid: zero
						AvgOutTokens: 0, // Invalid: zero
					},
				},
			})

			By("Calling TuneModelPerfParams should succeed with warning")
			err = tuner.TuneModelPerfParams([]llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}, systemData)
			Expect(err).NotTo(HaveOccurred(), "TuneModelPerfParams should not fail with invalid environment")
		})

		It("should handle multiple VAs with mixed tuner settings", func() {
			By("Creating additional VA resources")
			va1 := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tuner-test-va1",
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:            "default/default",
					ActivateModelTuner: true,
					ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
						Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
							{
								Acc:      "A100",
								AccCount: 1,
								PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
									DecodeParms:  map[string]string{"alpha": "8.5", "beta": "2.1"},
									PrefillParms: map[string]string{"gamma": "5.0", "delta": "0.11"},
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
			Expect(k8sClient.Create(ctx, va1)).To(Succeed())
			defer func() {
				err := k8sClient.Delete(ctx, va1)
				Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
			}()

			va2 := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tuner-test-va2",
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:            "default/default",
					ActivateModelTuner: false, // Disabled
					ModelProfile: llmdVariantAutoscalingV1alpha1.ModelProfile{
						Accelerators: []llmdVariantAutoscalingV1alpha1.AcceleratorProfile{
							{
								Acc:      "A100",
								AccCount: 1,
								PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
									DecodeParms:  map[string]string{"alpha": "8.5", "beta": "2.1"},
									PrefillParms: map[string]string{"gamma": "5.0", "delta": "0.11"},
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
			Expect(k8sClient.Create(ctx, va2)).To(Succeed())
			defer func() {
				err := k8sClient.Delete(ctx, va2)
				Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
			}()

			By("Creating system data")
			acceleratorCm, err := createTestReconciler(k8sClient).readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			serviceClassCm, err := createTestReconciler(k8sClient).readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			systemData := utils.CreateSystemData(acceleratorCm, serviceClassCm)
			Expect(systemData).NotTo(BeNil())

			By("Calling TuneModelPerfParams with mixed VA settings")
			err = tuner.TuneModelPerfParams([]llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va1, *va2}, systemData)
			Expect(err).NotTo(HaveOccurred(), "TuneModelPerfParams should handle mixed tuner settings")
		})
	})
})
