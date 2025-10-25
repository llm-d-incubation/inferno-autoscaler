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
	"time"

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
	utils "github.com/llm-d-incubation/workload-variant-autoscaler/internal/utils"
	testutils "github.com/llm-d-incubation/workload-variant-autoscaler/test/utils"
)

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
						ModelID:          "default/default",
						VariantID:        "default/default-A100-1",
						Accelerator:      "A100",
						AcceleratorCount: 1,
						VariantCost:      "10.5",
						VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
							PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
								DecodeParms:  map[string]string{"alpha": "20.28", "beta": "0.72"},
								PrefillParms: map[string]string{"gamma": "0", "delta": "0"},
							},
							MaxBatchSize: 4,
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
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

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
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).To(HaveOccurred(), "Expected error when reading missing serviceClass ConfigMap")
		})

		It("should fail on missing accelerator ConfigMap", func() {
			By("Creating VariantAutoscaling without required ConfigMaps")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).To(HaveOccurred(), "Expected error when reading missing accelerator ConfigMap")
		})

		It("should fail on missing variant autoscaling optimization ConfigMap", func() {
			By("Creating VariantAutoscaling without required ConfigMaps")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

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
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

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
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

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
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

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
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

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
					ModelID:          "default/default",
					VariantID:        "default/default-INVALID_GPU--1",
					Accelerator:      "INVALID_GPU",
					AcceleratorCount: -1, // Invalid count
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "invalid", "beta": "invalid"},
							PrefillParms: map[string]string{"gamma": "invalid", "delta": "invalid"},
						},
						MaxBatchSize: -1, // Invalid batch size
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
					ModelID:          "", // Empty ModelID
					VariantID:        "-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					VariantCost:      "10.5",
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.28", "beta": "0.72"},
							PrefillParms: map[string]string{"gamma": "0", "delta": "0"},
						},
						MaxBatchSize: 4,
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

		It("should handle empty accelerator field", func() {
			By("Creating VariantAutoscaling with no accelerator")
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-accelerators",
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "default/default",
					VariantID:        "default/default--1",
					Accelerator:      "", // Empty accelerator
					AcceleratorCount: 1,
					VariantCost:      "10.5",
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.28", "beta": "0.72"},
							PrefillParms: map[string]string{"gamma": "0", "delta": "0"},
						},
						MaxBatchSize: 4,
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "premium",
						Key:  "default/default",
					},
				},
			}
			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.accelerator"))
		})

		It("should handle empty SLOClassRef", func() {
			By("Creating VariantAutoscaling with no SLOClassRef")
			resource := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-slo-class-ref",
					Namespace: "default",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "default/default",
					VariantID:        "default/default-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					VariantCost:      "10.5",
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.28", "beta": "0.72"},
							PrefillParms: map[string]string{"gamma": "0", "delta": "0"},
						},
						MaxBatchSize: 4,
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
						ModelID:          modelID,
						VariantID:        fmt.Sprintf("%s-A100-1", modelID),
						Accelerator:      "A100",
						AcceleratorCount: 1,
						VariantCost:      "10.5",
						VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
							PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
								DecodeParms:  map[string]string{"alpha": "0.28", "beta": "0.72"},
								PrefillParms: map[string]string{"gamma": "0", "delta": "0"},
							},
							MaxBatchSize: 4,
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

			controllerReconciler := &VariantAutoscalingReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				PromAPI: mockPromAPI,
			}

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

			scaleToZeroConfigData := make(utils.ScaleToZeroConfigData)
			updateList, vaMap, allAnalyzerResponses, err := controllerReconciler.prepareVariantAutoscalings(ctx, activeVAs, accMap, serviceClassMap, systemData, scaleToZeroConfigData)

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
				// In single-variant architecture, check that CurrentAlloc has been populated by verifying NumReplicas
				Expect(updatedVa.Status.CurrentAlloc.NumReplicas).To(BeNumerically(">=", 0), fmt.Sprintf("CurrentAlloc should be populated for %s after preparation", updatedVa.Name))
				// In single-variant architecture, accelerator is in spec, not in status
				Expect(updatedVa.Spec.Accelerator).To(Equal("A100"), fmt.Sprintf("Accelerator in spec for %s should be \"A100\" after preparation", updatedVa.Name))
				Expect(updatedVa.Status.CurrentAlloc.NumReplicas).To(Equal(int32(1)), fmt.Sprintf("Current NumReplicas for %s should be 1 after preparation", updatedVa.Name))

				// DesiredOptimizedAlloc may be empty initially after preparation
				// In single-variant architecture, check NumReplicas > 0 to see if optimization has run
				if updatedVa.Status.DesiredOptimizedAlloc.NumReplicas > 0 {
					// Accelerator is in spec, already verified above
					Expect(updatedVa.Spec.Accelerator).NotTo(BeEmpty(), fmt.Sprintf("Accelerator in spec for %s should be set", updatedVa.Name))
				}
			}
		})

		It("should set MetricsAvailable condition when metrics validation fails", func() {
			By("Creating a mock Prometheus API that returns no metrics")
			mockPromAPI := &testutils.MockPromAPI{
				QueryResults: map[string]model.Value{},
				QueryErrors:  map[string]error{},
			}

			controllerReconciler := &VariantAutoscalingReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				PromAPI: mockPromAPI,
			}

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
			scaleToZeroConfigData := make(utils.ScaleToZeroConfigData)

			_, _, _, err = controllerReconciler.prepareVariantAutoscalings(ctx, activeVAs, accMap, serviceClassMap, systemData, scaleToZeroConfigData)
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

			controllerReconciler := &VariantAutoscalingReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				PromAPI: mockPromAPI,
			}

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

	Context("Scale-to-Zero ConfigMap Integration Tests", func() {
		It("should read scale-to-zero ConfigMap successfully", func() {
			By("Creating a scale-to-zero ConfigMap")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"model.meta_llama-3.1-8b": `modelID: "meta_llama-3.1-8b"
enableScaleToZero: true
retentionPeriod: "5m"`,
					"model.meta_llama-3.1-70b": `modelID: "meta_llama-3.1-70b"
enableScaleToZero: false`,
					"model.mistralai_Mistral-7B-v0.1": `modelID: "mistralai_Mistral-7B-v0.1"
enableScaleToZero: true
retentionPeriod: "15m"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the scale-to-zero ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(configData).NotTo(BeNil())

			By("Verifying ConfigMap data was parsed correctly")
			Expect(len(configData)).To(Equal(3))

			// Check llama-3.1-8b config
			config1, exists := configData["meta_llama-3.1-8b"]
			Expect(exists).To(BeTrue())
			Expect(config1.EnableScaleToZero).NotTo(BeNil())
			Expect(*config1.EnableScaleToZero).To(BeTrue())
			Expect(config1.RetentionPeriod).To(Equal("5m"))

			// Check llama-3.1-70b config
			config2, exists := configData["meta_llama-3.1-70b"]
			Expect(exists).To(BeTrue())
			Expect(config2.EnableScaleToZero).NotTo(BeNil())
			Expect(*config2.EnableScaleToZero).To(BeFalse())
			Expect(config2.RetentionPeriod).To(BeEmpty())

			// Check Mistral config
			config3, exists := configData["mistralai_Mistral-7B-v0.1"]
			Expect(exists).To(BeTrue())
			Expect(config3.EnableScaleToZero).NotTo(BeNil())
			Expect(*config3.EnableScaleToZero).To(BeTrue())
			Expect(config3.RetentionPeriod).To(Equal("15m"))

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should return empty config when ConfigMap does not exist", func() {
			By("Reading non-existent scale-to-zero ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "non-existent-configmap", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(configData).NotTo(BeNil())
			Expect(len(configData)).To(Equal(0))
		})

		It("should skip invalid JSON entries in ConfigMap", func() {
			By("Creating a ConfigMap with invalid YAML")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-invalid",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"model.meta_llama-3.1-8b": `modelID: "meta_llama-3.1-8b"
enableScaleToZero: true
retentionPeriod: "5m"`,
					"model.meta_llama-3.1-70b": `invalid yaml`,
					"model.mistralai_Mistral-7B-v0.1": `modelID: "mistralai_Mistral-7B-v0.1"
enableScaleToZero: true
retentionPeriod: "15m"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-invalid", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying only valid entries were parsed")
			Expect(len(configData)).To(Equal(2))

			// Check valid entries exist
			_, exists := configData["meta_llama-3.1-8b"]
			Expect(exists).To(BeTrue())
			_, exists = configData["mistralai_Mistral-7B-v0.1"]
			Expect(exists).To(BeTrue())

			// Check invalid entry was skipped
			_, exists = configData["meta_llama-3.1-70b"]
			Expect(exists).To(BeFalse())

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should apply scale-to-zero config during prepareVariantAutoscalings", func() {
			By("Creating required ConfigMaps")
			// Create accelerator ConfigMap
			acceleratorConfigMap := testutils.CreateAcceleratorUnitCostConfigMap(configMapNamespace)
			Expect(k8sClient.Create(ctx, acceleratorConfigMap)).To(Succeed())

			// Create service class ConfigMap
			serviceClassConfigMap := testutils.CreateServiceClassConfigMap(configMapNamespace)
			Expect(k8sClient.Create(ctx, serviceClassConfigMap)).To(Succeed())

			// Create scale-to-zero ConfigMap
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-test",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"vllm_meta_llama-3.1-8b": `{
						"enableScaleToZero": true,
						"retentionPeriod": "5m"
					}`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading ConfigMaps")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			accMap, err := controllerReconciler.readAcceleratorConfig(ctx, "accelerator-unit-costs", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			serviceClassMap, err := controllerReconciler.readServiceClassConfig(ctx, "service-classes-config", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			scaleToZeroConfigData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-test", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Listing VariantAutoscaling resources")
			var variantAutoscalingList llmdVariantAutoscalingV1alpha1.VariantAutoscalingList
			err = k8sClient.List(ctx, &variantAutoscalingList)
			Expect(err).NotTo(HaveOccurred())

			activeVAs := filterActiveVariantAutoscalings(variantAutoscalingList.Items)
			if len(activeVAs) > 0 {
				By("Creating system data")
				systemData := utils.CreateSystemData(accMap, serviceClassMap)
				Expect(systemData).NotTo(BeNil())

				By("Calling prepareVariantAutoscalings with scale-to-zero config")
				updateList, vaMap, _, err := controllerReconciler.prepareVariantAutoscalings(ctx, activeVAs, accMap, serviceClassMap, systemData, scaleToZeroConfigData)
				Expect(err).NotTo(HaveOccurred())
				Expect(vaMap).NotTo(BeNil())
				Expect(updateList).NotTo(BeNil())
			}

			By("Cleaning up ConfigMaps")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, acceleratorConfigMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, serviceClassConfigMap)).To(Succeed())
		})

		It("should handle empty ConfigMap data", func() {
			By("Creating an empty ConfigMap")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-empty",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the empty ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-empty", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(configData).NotTo(BeNil())
			Expect(len(configData)).To(Equal(0))

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		// NEW TESTS FOR PREFIXED-KEY FORMAT WITH YAML VALUES

		It("should parse prefixed-key format with YAML values", func() {
			By("Creating a ConfigMap with prefixed-key format and YAML values")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-yaml",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"__defaults__": `enableScaleToZero: true
retentionPeriod: "15m"`,
					"model.meta.llama-3.1-8b": `modelID: "meta/llama-3.1-8b"
retentionPeriod: "5m"`,
					"model.vllm.meta.llama-3.1-8b": `modelID: "vllm:meta/llama-3.1-8b"
enableScaleToZero: true
retentionPeriod: "3m"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-yaml", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying parsed data")
			Expect(len(configData)).To(Equal(3)) // __defaults__ + 2 models

			// Check defaults
			defaults, exists := configData["__defaults__"]
			Expect(exists).To(BeTrue())
			Expect(defaults.EnableScaleToZero).NotTo(BeNil())
			Expect(*defaults.EnableScaleToZero).To(BeTrue())
			Expect(defaults.RetentionPeriod).To(Equal("15m"))

			// Check model with slash in ID
			model1, exists := configData["meta/llama-3.1-8b"]
			Expect(exists).To(BeTrue())
			Expect(model1.ModelID).To(Equal("meta/llama-3.1-8b"))
			Expect(model1.RetentionPeriod).To(Equal("5m"))
			Expect(model1.EnableScaleToZero).To(BeNil()) // Not set, inherits from defaults

			// Check model with colon in ID
			model2, exists := configData["vllm:meta/llama-3.1-8b"]
			Expect(exists).To(BeTrue())
			Expect(model2.ModelID).To(Equal("vllm:meta/llama-3.1-8b"))
			Expect(model2.EnableScaleToZero).NotTo(BeNil())
			Expect(*model2.EnableScaleToZero).To(BeTrue())
			Expect(model2.RetentionPeriod).To(Equal("3m"))

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should handle duplicate modelID deterministically - first key wins", func() {
			By("Creating a ConfigMap with duplicate modelIDs")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-duplicates",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					// Same modelID in different keys - lexicographically first key should win
					"model.z.duplicate": `modelID: "test/model"
retentionPeriod: "999m"`,
					"model.a.duplicate": `modelID: "test/model"
retentionPeriod: "5m"`,
					"model.m.duplicate": `modelID: "test/model"
retentionPeriod: "10m"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-duplicates", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying only first key (lexicographically) wins")
			Expect(len(configData)).To(Equal(1)) // Only one entry for the duplicate modelID
			model, exists := configData["test/model"]
			Expect(exists).To(BeTrue())
			// "model.a.duplicate" comes first lexicographically, so its value should win
			Expect(model.RetentionPeriod).To(Equal("5m"))
			Expect(model.ModelID).To(Equal("test/model"))

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should skip entries without modelID field", func() {
			By("Creating a ConfigMap with missing modelID")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-no-modelid",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"model.valid": `modelID: "valid/model"
retentionPeriod: "5m"`,
					"model.invalid": `retentionPeriod: "10m"`, // Missing modelID field
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-no-modelid", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying only valid entry was parsed")
			Expect(len(configData)).To(Equal(1))
			model, exists := configData["valid/model"]
			Expect(exists).To(BeTrue())
			Expect(model.ModelID).To(Equal("valid/model"))

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should skip invalid YAML entries", func() {
			By("Creating a ConfigMap with invalid YAML")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-invalid-yaml",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"model.valid": `modelID: "valid/model"
retentionPeriod: "5m"`,
					"model.invalid": `this is not: valid: yaml: [[[`, // Invalid YAML
					"__defaults__":  `invalid yaml here too: {{{{`,   // Invalid defaults
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-invalid-yaml", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying only valid entry was parsed")
			Expect(len(configData)).To(Equal(1))
			model, exists := configData["valid/model"]
			Expect(exists).To(BeTrue())
			Expect(model.ModelID).To(Equal("valid/model"))

			// Invalid defaults and invalid model should be skipped
			_, exists = configData["__defaults__"]
			Expect(exists).To(BeFalse())

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should ignore non-prefixed keys", func() {
			By("Creating a ConfigMap with mixed key formats")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-mixed",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"model.prefixed": `modelID: "prefixed/model"
retentionPeriod: "5m"`,
					"notprefixed": `modelID: "should/be/ignored"
retentionPeriod: "10m"`,
					"another-key": `modelID: "also/ignored"
retentionPeriod: "15m"`,
					"__defaults__": `enableScaleToZero: true
retentionPeriod: "20m"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-mixed", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying only prefixed keys and defaults were parsed")
			Expect(len(configData)).To(Equal(2)) // defaults + 1 prefixed model

			// Check defaults
			defaults, exists := configData["__defaults__"]
			Expect(exists).To(BeTrue())
			Expect(defaults.RetentionPeriod).To(Equal("20m"))

			// Check prefixed model
			model, exists := configData["prefixed/model"]
			Expect(exists).To(BeTrue())
			Expect(model.ModelID).To(Equal("prefixed/model"))

			// Non-prefixed keys should be ignored
			_, exists = configData["should/be/ignored"]
			Expect(exists).To(BeFalse())
			_, exists = configData["also/ignored"]
			Expect(exists).To(BeFalse())

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should handle special characters in modelID", func() {
			By("Creating a ConfigMap with special characters in modelIDs")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-special-chars",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"model.test1": `modelID: "org/model-name_v1.2.3"
retentionPeriod: "5m"`,
					"model.test2": `modelID: "vllm:meta/llama@latest"
retentionPeriod: "10m"`,
					"model.test3": `modelID: "prefix:org/model-name:tag"
retentionPeriod: "15m"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-special-chars", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying all special character modelIDs were parsed correctly")
			Expect(len(configData)).To(Equal(3))

			model1, exists := configData["org/model-name_v1.2.3"]
			Expect(exists).To(BeTrue())
			Expect(model1.ModelID).To(Equal("org/model-name_v1.2.3"))
			Expect(model1.RetentionPeriod).To(Equal("5m"))

			model2, exists := configData["vllm:meta/llama@latest"]
			Expect(exists).To(BeTrue())
			Expect(model2.ModelID).To(Equal("vllm:meta/llama@latest"))
			Expect(model2.RetentionPeriod).To(Equal("10m"))

			model3, exists := configData["prefix:org/model-name:tag"]
			Expect(exists).To(BeTrue())
			Expect(model3.ModelID).To(Equal("prefix:org/model-name:tag"))
			Expect(model3.RetentionPeriod).To(Equal("15m"))

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should handle partial overrides correctly", func() {
			By("Creating a ConfigMap with partial overrides")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-partial",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"__defaults__": `enableScaleToZero: true
retentionPeriod: "15m"`,
					"model.only-retention": `modelID: "test/only-retention"
retentionPeriod: "5m"`,
					"model.only-enable": `modelID: "test/only-enable"
enableScaleToZero: false`,
					"model.both": `modelID: "test/both"
enableScaleToZero: false
retentionPeriod: "20m"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-partial", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying partial override behavior")
			Expect(len(configData)).To(Equal(4))

			// Model with only retentionPeriod set (EnableScaleToZero should be nil)
			model1, exists := configData["test/only-retention"]
			Expect(exists).To(BeTrue())
			Expect(model1.RetentionPeriod).To(Equal("5m"))
			Expect(model1.EnableScaleToZero).To(BeNil()) // Should inherit from defaults

			// Model with only EnableScaleToZero set (RetentionPeriod should be empty)
			model2, exists := configData["test/only-enable"]
			Expect(exists).To(BeTrue())
			Expect(model2.EnableScaleToZero).NotTo(BeNil())
			Expect(*model2.EnableScaleToZero).To(BeFalse())
			Expect(model2.RetentionPeriod).To(Equal("")) // Should inherit from defaults

			// Model with both set
			model3, exists := configData["test/both"]
			Expect(exists).To(BeTrue())
			Expect(model3.EnableScaleToZero).NotTo(BeNil())
			Expect(*model3.EnableScaleToZero).To(BeFalse())
			Expect(model3.RetentionPeriod).To(Equal("20m"))

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		// EDGE CASE TESTS FOR RETENTION PERIOD VALIDATION

		It("should handle invalid retention period formats", func() {
			By("Creating a ConfigMap with invalid retention periods")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-invalid-retention",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"__defaults__": `enableScaleToZero: true
retentionPeriod: "10m"`,
					"model.valid": `modelID: "test/valid"
retentionPeriod: "5m"`,
					"model.invalid-format": `modelID: "test/invalid-format"
retentionPeriod: "invalid"`,
					"model.number-only": `modelID: "test/number-only"
retentionPeriod: "5"`,
					"model.negative": `modelID: "test/negative"
retentionPeriod: "-5m"`,
					"model.zero": `modelID: "test/zero"
retentionPeriod: "0s"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-invalid-retention", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying invalid retention periods are still parsed but will fail validation during use")
			// All entries should be parsed (YAML is valid)
			Expect(len(configData)).To(Equal(6)) // defaults + 5 models

			// Check that entries are present (validation happens at use time via GetScaleToZeroRetentionPeriod)
			_, exists := configData["test/valid"]
			Expect(exists).To(BeTrue())
			_, exists = configData["test/invalid-format"]
			Expect(exists).To(BeTrue())
			_, exists = configData["test/number-only"]
			Expect(exists).To(BeTrue())
			_, exists = configData["test/negative"]
			Expect(exists).To(BeTrue())
			_, exists = configData["test/zero"]
			Expect(exists).To(BeTrue())

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should handle unusually long retention periods with warning", func() {
			By("Creating a ConfigMap with very long retention period")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-long-retention",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"model.long-retention": `modelID: "test/long"
retentionPeriod: "48h"`,
					"model.very-long": `modelID: "test/very-long"
retentionPeriod: "168h"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-long-retention", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying long retention periods are accepted")
			Expect(len(configData)).To(Equal(2))

			model1, exists := configData["test/long"]
			Expect(exists).To(BeTrue())
			Expect(model1.RetentionPeriod).To(Equal("48h"))

			model2, exists := configData["test/very-long"]
			Expect(exists).To(BeTrue())
			Expect(model2.RetentionPeriod).To(Equal("168h"))

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})

		It("should fall back to defaults when retention period is invalid", func() {
			By("Creating a ConfigMap with invalid retention and valid defaults")
			scaleToZeroConfigMap := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "model-scale-to-zero-config-fallback",
					Namespace: configMapNamespace,
				},
				Data: map[string]string{
					"__defaults__": `enableScaleToZero: true
retentionPeriod: "20m"`,
					"model.invalid-retention": `modelID: "test/invalid"
retentionPeriod: "not-a-duration"`,
				},
			}
			Expect(k8sClient.Create(ctx, scaleToZeroConfigMap)).To(Succeed())

			By("Reading the ConfigMap")
			controllerReconciler := &VariantAutoscalingReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			configData, err := controllerReconciler.readScaleToZeroConfig(ctx, "model-scale-to-zero-config-fallback", configMapNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying both entries are present")
			Expect(len(configData)).To(Equal(2))

			// GetScaleToZeroRetentionPeriod will fall back to defaults when model's retention is invalid
			duration := utils.GetScaleToZeroRetentionPeriod(configData, "test/invalid")
			Expect(duration).To(Equal(20 * time.Minute)) // Should use defaults, not system default

			By("Cleaning up ConfigMap")
			Expect(k8sClient.Delete(ctx, scaleToZeroConfigMap)).To(Succeed())
		})
	})
})
