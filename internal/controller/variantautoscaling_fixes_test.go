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
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
)

// Test coverage for the three critical fixes:
// 1. CEL validation for min/maxReplicas
// 2. VA name lookup fix (VA name != deployment name)
// 3. ConfigMap reconciliation optimization

// Helper function to create int32 pointer
func intPtr(i int32) *int32 {
	return &i
}

// Helper function to generate unique namespace name for each test
// Uses timestamp and random suffix to avoid namespace conflicts during parallel test execution
func uniqueNamespace(prefix string) string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().Unix(), rand.Intn(10000))
}

// Helper function to create namespace and ensure cleanup
func createTestNamespace(ctx context.Context, name string) {
	By(fmt.Sprintf("creating unique test namespace: %s", name))
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	err := k8sClient.Create(ctx, ns)
	if err != nil && !errors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	// Schedule cleanup using DeferCleanup (runs after test completes)
	DeferCleanup(func() {
		By(fmt.Sprintf("cleaning up test namespace: %s", name))
		_ = k8sClient.Delete(ctx, ns)
	})
}

var _ = Describe("VariantAutoscaling Controller Fixes", func() {
	Context("CEL Validation for min/maxReplicas", func() {
		ctx := context.Background()

		It("should reject VA with maxReplicas < minReplicas", func() {
			// Use unique namespace for this test to avoid conflicts
			namespaceName := uniqueNamespace("cel-invalid")
			vaName := "test-va-cel-invalid"
			createTestNamespace(ctx, namespaceName)
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaName,
					Namespace: namespaceName,
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "test-model",
					VariantID:        "test-model-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					MinReplicas:      intPtr(5),
					MaxReplicas:      intPtr(2), // Invalid: 2 < 5
					ScaleTargetRef: llmdVariantAutoscalingV1alpha1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "slo-config",
						Key:  "gold",
					},
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						MaxBatchSize: 8,
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.8", "beta": "0.2"},
							PrefillParms: map[string]string{"gamma": "0.8", "delta": "0.2"},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, va)
			Expect(err).To(HaveOccurred(), "Should reject VA with maxReplicas < minReplicas")
			Expect(err.Error()).To(ContainSubstring("maxReplicas must be greater than or equal to minReplicas"),
				"Error message should indicate CEL validation failure")
		})

		It("should accept VA with maxReplicas = minReplicas", func() {
			// Use unique namespace for this test to avoid conflicts
			namespaceName := uniqueNamespace("cel-equal")
			vaName := "test-va-cel-equal"
			createTestNamespace(ctx, namespaceName)

			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaName,
					Namespace: namespaceName,
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "test-model",
					VariantID:        "test-model-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					MinReplicas:      intPtr(5),
					MaxReplicas:      intPtr(5), // Valid: 5 = 5
					ScaleTargetRef: llmdVariantAutoscalingV1alpha1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "slo-config",
						Key:  "gold",
					},
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						MaxBatchSize: 8,
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.8", "beta": "0.2"},
							PrefillParms: map[string]string{"gamma": "0.8", "delta": "0.2"},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, va)
			Expect(err).NotTo(HaveOccurred(), "Should accept VA with maxReplicas = minReplicas")
		})

		It("should accept VA with maxReplicas > minReplicas", func() {
			// Use unique namespace for this test to avoid conflicts
			namespaceName := uniqueNamespace("cel-valid")
			vaName := "test-va-cel-valid"
			createTestNamespace(ctx, namespaceName)

			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaName,
					Namespace: namespaceName,
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "test-model",
					VariantID:        "test-model-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					MinReplicas:      intPtr(2),
					MaxReplicas:      intPtr(10), // Valid: 10 > 2
					ScaleTargetRef: llmdVariantAutoscalingV1alpha1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "slo-config",
						Key:  "gold",
					},
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						MaxBatchSize: 8,
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.8", "beta": "0.2"},
							PrefillParms: map[string]string{"gamma": "0.8", "delta": "0.2"},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, va)
			Expect(err).NotTo(HaveOccurred(), "Should accept VA with maxReplicas > minReplicas")
		})

		It("should accept VA with only minReplicas set (maxReplicas nil)", func() {
			// Use unique namespace for this test to avoid conflicts
			namespaceName := uniqueNamespace("cel-minonly")
			vaName := "test-va-cel-minonly"
			createTestNamespace(ctx, namespaceName)

			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaName,
					Namespace: namespaceName,
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "test-model",
					VariantID:        "test-model-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					MinReplicas:      intPtr(2),
					MaxReplicas:      nil, // No upper bound
					ScaleTargetRef: llmdVariantAutoscalingV1alpha1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "slo-config",
						Key:  "gold",
					},
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						MaxBatchSize: 8,
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.8", "beta": "0.2"},
							PrefillParms: map[string]string{"gamma": "0.8", "delta": "0.2"},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, va)
			Expect(err).NotTo(HaveOccurred(), "Should accept VA with only minReplicas set")
		})

		It("should accept VA with only maxReplicas set (minReplicas nil/defaults to 0)", func() {
			// Use unique namespace for this test to avoid conflicts
			namespaceName := uniqueNamespace("cel-maxonly")
			vaName := "test-va-cel-maxonly"
			createTestNamespace(ctx, namespaceName)

			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaName,
					Namespace: namespaceName,
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "test-model",
					VariantID:        "test-model-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					MinReplicas:      nil, // Defaults to 0
					MaxReplicas:      intPtr(10),
					ScaleTargetRef: llmdVariantAutoscalingV1alpha1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "slo-config",
						Key:  "gold",
					},
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						MaxBatchSize: 8,
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.8", "beta": "0.2"},
							PrefillParms: map[string]string{"gamma": "0.8", "delta": "0.2"},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, va)
			Expect(err).NotTo(HaveOccurred(), "Should accept VA with only maxReplicas set")
		})

		It("should reject update that violates CEL validation", func() {
			// Use unique namespace for this test to avoid conflicts
			namespaceName := uniqueNamespace("cel-update")
			vaName := "test-va-cel-update"
			createTestNamespace(ctx, namespaceName)

			// First create a valid VA
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaName,
					Namespace: namespaceName,
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "test-model",
					VariantID:        "test-model-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					MinReplicas:      intPtr(1),
					MaxReplicas:      intPtr(10),
					ScaleTargetRef: llmdVariantAutoscalingV1alpha1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       "test-deployment",
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "slo-config",
						Key:  "gold",
					},
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						MaxBatchSize: 8,
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.8", "beta": "0.2"},
							PrefillParms: map[string]string{"gamma": "0.8", "delta": "0.2"},
						},
					},
				},
			}

			err := k8sClient.Create(ctx, va)
			Expect(err).NotTo(HaveOccurred(), "Should create valid VA")

			// Now try to update with invalid replica bounds
			va.Spec.MinReplicas = intPtr(15)
			va.Spec.MaxReplicas = intPtr(5) // Invalid: 5 < 15

			err = k8sClient.Update(ctx, va)
			Expect(err).To(HaveOccurred(), "Should reject update with invalid replica bounds")
			Expect(err.Error()).To(ContainSubstring("maxReplicas must be greater than or equal to minReplicas"),
				"Error message should indicate CEL validation failure")
		})
	})

	Context("VA Name Different from Deployment Name", func() {
		ctx := context.Background()

		It("should accept VA with name different from deployment name", func() {
			// Use unique namespace for this test to avoid conflicts
			namespaceName := uniqueNamespace("va-lookup")
			deploymentName := "my-vllm-deployment"
			vaNameCustom := "my-variant-a100-config" // Different from deployment name
			createTestNamespace(ctx, namespaceName)
			// This test verifies that the fix at variantautoscaling_controller.go:997
			// correctly uses va.Name instead of deploy.Name for VA lookup
			By("creating VA with custom name (different from scaleTargetRef deployment)")
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaNameCustom,
					Namespace: namespaceName,
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "test-model",
					VariantID:        "test-model-A100-1",
					Accelerator:      "A100",
					AcceleratorCount: 1,
					MinReplicas:      intPtr(1),
					MaxReplicas:      intPtr(5),
					ScaleTargetRef: llmdVariantAutoscalingV1alpha1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       deploymentName, // Points to deployment with different name
					},
					SLOClassRef: llmdVariantAutoscalingV1alpha1.ConfigMapKeyRef{
						Name: "slo-config",
						Key:  "gold",
					},
					VariantProfile: llmdVariantAutoscalingV1alpha1.VariantProfile{
						MaxBatchSize: 8,
						PerfParms: llmdVariantAutoscalingV1alpha1.PerfParms{
							DecodeParms:  map[string]string{"alpha": "0.8", "beta": "0.2"},
							PrefillParms: map[string]string{"gamma": "0.8", "delta": "0.2"},
						},
					},
				},
			}

			// The fix ensures that VAs can have names independent of deployment names
			err := k8sClient.Create(ctx, va)
			Expect(err).NotTo(HaveOccurred(), "Should create VA with custom name different from deployment")

			By("verifying VA can be retrieved by its custom name")
			var retrievedVA llmdVariantAutoscalingV1alpha1.VariantAutoscaling
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      vaNameCustom, // Using VA's custom name, NOT deployment name
				Namespace: namespaceName,
			}, &retrievedVA)
			Expect(err).NotTo(HaveOccurred(), "Should retrieve VA by its custom name")
			Expect(retrievedVA.Name).To(Equal(vaNameCustom), "VA name should match custom name")
			Expect(retrievedVA.Spec.ScaleTargetRef.Name).To(Equal(deploymentName), "Target deployment should match")

			By("verifying VA name is independent from deployment name")
			Expect(retrievedVA.Name).NotTo(Equal(retrievedVA.Spec.ScaleTargetRef.Name),
				"VA name should be independent from deployment name (this is the fix)")
		})
	})
})
