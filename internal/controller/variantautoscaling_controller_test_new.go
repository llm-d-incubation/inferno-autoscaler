// Additional unit tests for coverage improvement

package controller

import (
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	utils "github.com/llm-d-incubation/workload-variant-autoscaler/internal/utils"
)

var _ = Describe("Pure Function Unit Tests", func() {
	Context("getConflictingVAPattern", func() {
		It("should return empty string for empty resolutions", func() {
			resolutions := make(map[string]ConflictResolution)
			pattern := getConflictingVAPattern(resolutions)
			Expect(pattern).To(Equal(""))
		})

		It("should return single VA name", func() {
			resolutions := map[string]ConflictResolution{
				"default/deploy1": {
					Winner: "va-1",
					Losers: []string{},
				},
			}
			pattern := getConflictingVAPattern(resolutions)
			Expect(pattern).To(Equal("va-1"))
		})

		It("should format multiple VAs with pipe separator", func() {
			resolutions := map[string]ConflictResolution{
				"default/deploy1": {
					Winner: "va-1",
					Losers: []string{"va-2", "va-3"},
				},
			}
			pattern := getConflictingVAPattern(resolutions)
			// Should contain all VAs separated by |
			Expect(pattern).To(ContainSubstring("va-1"))
			Expect(pattern).To(ContainSubstring("va-2"))
			Expect(pattern).To(ContainSubstring("va-3"))
			Expect(strings.Count(pattern, "|")).To(Equal(2)) // 3 VAs = 2 separators
		})

		It("should handle multiple deployments with conflicts", func() {
			resolutions := map[string]ConflictResolution{
				"default/deploy1": {
					Winner: "va-1",
					Losers: []string{"va-2"},
				},
				"default/deploy2": {
					Winner: "va-3",
					Losers: []string{"va-4", "va-5"},
				},
			}
			pattern := getConflictingVAPattern(resolutions)
			// Should contain all 5 VAs
			Expect(strings.Count(pattern, "|")).To(Equal(4)) // 5 VAs = 4 separators
		})
	})

	Context("createOptimizedAllocWithCreationTime", func() {
		It("should create allocation with CreationTimestamp as UpdateTime", func() {
			creationTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
			previousAlloc := llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
				NumReplicas: 2,
			}

			alloc := createOptimizedAllocWithCreationTime(3, "test reason", previousAlloc, creationTime)

			Expect(alloc.NumReplicas).To(Equal(int32(3)))
			Expect(alloc.LastUpdate.UpdateTime).To(Equal(creationTime))
			Expect(alloc.LastUpdate.Reason).To(Equal("test reason"))
			Expect(alloc.LastUpdate.NumReplicasChanged).To(Equal(int32(1))) // 3 - 2
		})

		It("should calculate negative delta when scaling down", func() {
			creationTime := metav1.Now()
			previousAlloc := llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
				NumReplicas: 5,
			}

			alloc := createOptimizedAllocWithCreationTime(2, "scaling down", previousAlloc, creationTime)

			Expect(alloc.LastUpdate.NumReplicasChanged).To(Equal(int32(-3))) // 2 - 5
		})

		It("should handle zero replicas", func() {
			creationTime := metav1.Now()
			previousAlloc := llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
				NumReplicas: 3,
			}

			alloc := createOptimizedAllocWithCreationTime(0, "scaling to zero", previousAlloc, creationTime)

			Expect(alloc.NumReplicas).To(Equal(int32(0)))
			Expect(alloc.LastUpdate.NumReplicasChanged).To(Equal(int32(-3)))
		})

		It("should handle zero delta when replicas unchanged", func() {
			creationTime := metav1.Now()
			previousAlloc := llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
				NumReplicas: 3,
			}

			alloc := createOptimizedAllocWithCreationTime(3, "no change", previousAlloc, creationTime)

			Expect(alloc.LastUpdate.NumReplicasChanged).To(Equal(int32(0)))
		})
	})

	Context("initMetricsEmitter", func() {
		It("should initialize metrics emitter without panic", func() {
			Expect(func() {
				initMetricsEmitter()
			}).NotTo(Panic())
		})
	})

	Context("isCheapestVariantForModel - Edge Cases", func() {
		It("should handle nil currentVariant", func() {
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}
			result := isCheapestVariantForModel(nil, allVariants, "model-1")
			Expect(result).To(BeFalse())
		})

		It("should handle empty allVariants slice", func() {
			currentVariant := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "model-1",
					VariantID:        "var-1",
					AcceleratorCount: 2,
				},
			}
			result := isCheapestVariantForModel(currentVariant, []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}, "model-1")
			Expect(result).To(BeTrue()) // Only variant, therefore cheapest
		})

		It("should handle all variants for different model", func() {
			currentVariant := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "model-1",
					VariantID:        "var-1",
					AcceleratorCount: 2,
				},
			}
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				{
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						ModelID:          "model-2", // Different model
						VariantID:        "var-2",
						AcceleratorCount: 1,
					},
				},
			}
			result := isCheapestVariantForModel(currentVariant, allVariants, "model-1")
			Expect(result).To(BeTrue()) // Only variant for model-1
		})

		It("should break ties deterministically by VariantID", func() {
			currentVariant := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "model-1",
					VariantID:        "var-b",
					AcceleratorCount: 2,
				},
			}
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				*currentVariant,
				{
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						ModelID:          "model-1",
						VariantID:        "var-a", // Same cost, earlier alphabetically
						AcceleratorCount: 2,
					},
				},
			}
			result := isCheapestVariantForModel(currentVariant, allVariants, "model-1")
			Expect(result).To(BeFalse()) // var-a wins the tiebreaker
		})

		It("should identify cheapest among 3+ variants", func() {
			currentVariant := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:          "model-1",
					VariantID:        "var-medium",
					AcceleratorCount: 2,
				},
			}
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				{
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						ModelID:          "model-1",
						VariantID:        "var-expensive",
						AcceleratorCount: 4,
					},
				},
				*currentVariant,
				{
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						ModelID:          "model-1",
						VariantID:        "var-cheap",
						AcceleratorCount: 1,
					},
				},
			}
			result := isCheapestVariantForModel(currentVariant, allVariants, "model-1")
			Expect(result).To(BeFalse()) // var-cheap is cheapest
		})
	})

	Context("allVariantsHaveMinReplicasZero - Edge Cases", func() {
		It("should handle nil minReplicas as zero", func() {
			minReplicasZero := int32(0)
			variants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				{
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						ModelID:     "model-1",
						MinReplicas: nil, // nil treated as 0
					},
				},
				{
					Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
						ModelID:     "model-1",
						MinReplicas: &minReplicasZero,
					},
				},
			}

			result := allVariantsHaveMinReplicasZero(variants, "model-1")
			Expect(result).To(BeTrue())
		})
	})

	Context("filterActiveVariantAutoscalings - Edge Cases", func() {
		It("should return empty slice when all VAs have deletionTimestamp", func() {
			now := metav1.Now()
			vas := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "va-1",
						DeletionTimestamp: &now,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "va-2",
						DeletionTimestamp: &now,
					},
				},
			}

			result := filterActiveVariantAutoscalings(vas)
			Expect(result).To(BeEmpty())
		})
	})
})

var _ = Describe("applyFallbackAllocation - Additional Edge Cases", func() {
	Context("PATH 2 - Bounds Changes", func() {
		It("should clamp previous allocation to new maxReplicas", func() {
			maxReplicas := int32(3)
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{Name: "va-1", Namespace: "default"},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:     "model-1",
					VariantID:   "var-1",
					MaxReplicas: &maxReplicas, // NEW: max clamped down
				},
				Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
					DesiredOptimizedAlloc: llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
						NumReplicas: 5, // Previous was 5
						LastUpdate: llmdVariantAutoscalingV1alpha1.LastUpdateInfo{
							UpdateTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
							Reason:     "Previous optimization",
						},
					},
				},
			}

			scaleToZeroConfig := make(utils.ScaleToZeroConfigData)
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}

			applyFallbackAllocation(va, allVariants, scaleToZeroConfig, true, "Fallback")

			Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(3)))
			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.Reason).To(ContainSubstring("clamped"))
			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.Reason).To(ContainSubstring("from 5 to 3"))
		})

		It("should clamp previous allocation to new minReplicas", func() {
			minReplicas := int32(3)
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{Name: "va-1", Namespace: "default"},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:     "model-1",
					VariantID:   "var-1",
					MinReplicas: &minReplicas, // NEW: min increased
				},
				Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
					DesiredOptimizedAlloc: llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
						NumReplicas: 1, // Previous was 1
						LastUpdate: llmdVariantAutoscalingV1alpha1.LastUpdateInfo{
							UpdateTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
							Reason:     "Previous optimization",
						},
					},
				},
			}

			scaleToZeroConfig := make(utils.ScaleToZeroConfigData)
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}

			applyFallbackAllocation(va, allVariants, scaleToZeroConfig, true, "Fallback")

			Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(3)))
			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.Reason).To(ContainSubstring("clamped"))
		})

		It("should set default reason when Reason is empty", func() {
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{Name: "va-1", Namespace: "default"},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:   "model-1",
					VariantID: "var-1",
				},
				Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
					DesiredOptimizedAlloc: llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
						NumReplicas: 2,
						LastUpdate: llmdVariantAutoscalingV1alpha1.LastUpdateInfo{
							UpdateTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
							Reason:     "", // Empty reason
						},
					},
				},
			}

			scaleToZeroConfig := make(utils.ScaleToZeroConfigData)
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}

			applyFallbackAllocation(va, allVariants, scaleToZeroConfig, true, "Fallback")

			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.Reason).To(Equal("Fallback: preserving previous allocation (no optimizer solution)"))
		})

		It("should set UpdateTime when LastUpdate.UpdateTime is zero", func() {
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{Name: "va-1", Namespace: "default"},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:   "model-1",
					VariantID: "var-1",
				},
				Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
					DesiredOptimizedAlloc: llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
						NumReplicas: 2,
						LastUpdate: llmdVariantAutoscalingV1alpha1.LastUpdateInfo{
							UpdateTime: metav1.Time{}, // Zero time
							Reason:     "Some reason",
						},
					},
				},
			}

			scaleToZeroConfig := make(utils.ScaleToZeroConfigData)
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}

			before := time.Now()
			applyFallbackAllocation(va, allVariants, scaleToZeroConfig, true, "Fallback")
			after := time.Now()

			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.UpdateTime.IsZero()).To(BeFalse())
			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.UpdateTime.Time).To(BeTemporally(">=", before))
			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.UpdateTime.Time).To(BeTemporally("<=", after))
		})
	})

	Context("PATH 3 - Edge Cases", func() {
		It("should handle deployment discovered late scenario", func() {
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{Name: "va-1", Namespace: "default"},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:   "model-1",
					VariantID: "var-1",
				},
				Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
					CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{
						NumReplicas: 5, // Current discovered as 5
					},
					DesiredOptimizedAlloc: llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
						NumReplicas: 0, // Previous desired was 0
						LastUpdate: llmdVariantAutoscalingV1alpha1.LastUpdateInfo{
							UpdateTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
							Reason:     "Previous",
						},
					},
				},
			}

			scaleToZeroConfig := make(utils.ScaleToZeroConfigData)
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}

			applyFallbackAllocation(va, allVariants, scaleToZeroConfig, false, "Last resort")

			Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(5)))
			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.Reason).To(ContainSubstring("deployment discovered late"))
		})

		It("should use previous optimized allocation when not first run", func() {
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{Name: "va-1", Namespace: "default"},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:   "model-1",
					VariantID: "var-1",
				},
				Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
					CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{
						NumReplicas: 2, // Current matches previous desired
					},
					DesiredOptimizedAlloc: llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
						NumReplicas: 2, // Previous desired was 2
						LastUpdate: llmdVariantAutoscalingV1alpha1.LastUpdateInfo{
							UpdateTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
							Reason:     "Previous",
						},
					},
				},
			}

			scaleToZeroConfig := make(utils.ScaleToZeroConfigData)
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va}

			applyFallbackAllocation(va, allVariants, scaleToZeroConfig, false, "Last resort")

			Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(2)))
			Expect(va.Status.DesiredOptimizedAlloc.LastUpdate.Reason).To(ContainSubstring("maintaining controller intent"))
		})

		It("should handle first run with other running variants", func() {
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{Name: "va-1", Namespace: "default"},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:   "model-1",
					VariantID: "var-1",
				},
				Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
					CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{
						NumReplicas: 0, // First run, current = 0
					},
					DesiredOptimizedAlloc: llmdVariantAutoscalingV1alpha1.OptimizedAlloc{
						// No previous allocation
					},
				},
			}

			otherVariant := llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{Name: "va-2", Namespace: "default"},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ModelID:   "model-1",
					VariantID: "var-2",
				},
				Status: llmdVariantAutoscalingV1alpha1.VariantAutoscalingStatus{
					CurrentAlloc: llmdVariantAutoscalingV1alpha1.Allocation{
						NumReplicas: 3, // Other variant running
					},
				},
			}

			scaleToZeroConfig := make(utils.ScaleToZeroConfigData)
			allVariants := []llmdVariantAutoscalingV1alpha1.VariantAutoscaling{*va, otherVariant}

			applyFallbackAllocation(va, allVariants, scaleToZeroConfig, false, "Last resort")

			// Should remain at 0 since other variant is serving
			Expect(va.Status.DesiredOptimizedAlloc.NumReplicas).To(Equal(int32(0)))
		})
	})
})
