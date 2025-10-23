package optimizer

import (
	"context"
	"fmt"
	"math"

	llmdOptv1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	collector "github.com/llm-d-incubation/workload-variant-autoscaler/internal/collector"
	interfaces "github.com/llm-d-incubation/workload-variant-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/utils"
	infernoConfig "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/config"
	inferno "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/core"
	infernoManager "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/manager"
)

// Engine holding all necessary data to perform global optimization across all variants
type VariantAutoscalingsEngine struct {
	manager *infernoManager.Manager
	system  *inferno.System
}

// Create a new instance of a variants autoscaling engine
func NewVariantAutoscalingsEngine(manager *infernoManager.Manager, system *inferno.System) *VariantAutoscalingsEngine {
	return &VariantAutoscalingsEngine{
		manager: manager,
		system:  system,
	}
}

// Perform a global optimization producing optimized allocations for all variants
func (engine *VariantAutoscalingsEngine) Optimize(ctx context.Context,
	vaList llmdOptv1alpha1.VariantAutoscalingList,
	analysis map[string]*interfaces.ModelAnalyzeResponse,
	scaleToZeroConfigData *utils.ScaleToZeroConfigData,
	metricsCache *collector.ModelMetricsCache,
) (map[string]llmdOptv1alpha1.OptimizedAlloc, error) {

	if err := engine.manager.Optimize(); err != nil {
		return nil, err
	}
	allocationSolution := engine.system.GenerateSolution()
	if allocationSolution == nil || len(allocationSolution.Spec) == 0 {
		return nil, fmt.Errorf("no feasible allocations found for %d variants", len(vaList.Items))
	}

	logger.Log.Debug("Optimization solution - ", "system: ", engine.system)

	// Apply zero-rate handling logic before creating final allocations
	engine.applyZeroRateHandling(ctx, &vaList, allocationSolution, scaleToZeroConfigData, metricsCache)

	optimizedAllocMap := make(map[string]llmdOptv1alpha1.OptimizedAlloc)
	for _, va := range vaList.Items {
		vaName := va.Name
		vaNamespace := va.Namespace
		optimizedAllocation, err := utils.CreateOptimizedAlloc(vaName, vaNamespace, allocationSolution)
		if err != nil {
			logger.Log.Error(err, "Failed to create optimized allocation",
				"variant", vaName, "namespace", vaNamespace)
			continue
		}
		optimizedAllocMap[vaName] = *optimizedAllocation
	}
	return optimizedAllocMap, nil
}

// applyZeroRateHandling modifies the allocation solution for zero-rate scenarios
// based on scale-to-zero configuration and request metrics over retention period
func (engine *VariantAutoscalingsEngine) applyZeroRateHandling(
	ctx context.Context,
	vaList *llmdOptv1alpha1.VariantAutoscalingList,
	allocationSolution *infernoConfig.AllocationSolution,
	scaleToZeroConfigData *utils.ScaleToZeroConfigData,
	metricsCache *collector.ModelMetricsCache,
) {
	// Group variants by ModelID
	// Estimate capacity: assume average 2-3 variants per model
	estimatedModels := len(vaList.Items) / 2
	if estimatedModels == 0 {
		estimatedModels = 1
	}
	modelVariants := make(map[string][]*llmdOptv1alpha1.VariantAutoscaling, estimatedModels)
	for i := range vaList.Items {
		va := &vaList.Items[i]
		modelID := va.Spec.ModelID
		modelVariants[modelID] = append(modelVariants[modelID], va)
	}

	// Process each model
	for modelID, variants := range modelVariants {
		// Check scale-to-zero configuration
		var scaleToZeroEnabled bool
		if scaleToZeroConfigData == nil {
			logger.Log.Warn("Scale-to-zero config is nil, treating as disabled", "modelID", modelID)
			scaleToZeroEnabled = false
		} else {
			scaleToZeroEnabled = utils.IsScaleToZeroEnabled(*scaleToZeroConfigData, modelID)
		}

		// Get total requests over retention period from metrics cache
		totalRequests := 0.0
		if metricsCache != nil {
			if metrics, exists := metricsCache.Get(modelID); exists {
				totalRequests = metrics.TotalRequestsOverRetentionPeriod
			}
		}

		logger.Log.Debug("Zero-rate handling",
			"modelID", modelID,
			"scaleToZeroEnabled", scaleToZeroEnabled,
			"totalRequestsOverRetention", totalRequests,
			"variantCount", len(variants))

		// Determine if we should keep at least one replica
		shouldKeepOneReplica := !scaleToZeroEnabled || totalRequests > 0

		if !shouldKeepOneReplica {
			// Scale all variants to zero
			logger.Log.Info("Scaling all variants to zero (scale-to-zero enabled, no recent requests)",
				"modelID", modelID)
			for _, va := range variants {
				serverName := utils.FullName(va.Name, va.Namespace)
				if allocData, exists := allocationSolution.Spec[serverName]; exists {
					allocData.NumReplicas = 0
					allocationSolution.Spec[serverName] = allocData
				}
			}
			continue
		}

		// Check if optimizer already allocated replicas (non-zero rate)
		hasNonZeroAllocation := false
		for _, va := range variants {
			serverName := utils.FullName(va.Name, va.Namespace)
			if allocData, exists := allocationSolution.Spec[serverName]; exists && allocData.NumReplicas > 0 {
				hasNonZeroAllocation = true
				break
			}
		}

		// If optimizer already allocated replicas, no need for zero-rate handling
		if hasNonZeroAllocation {
			logger.Log.Debug("Optimizer already allocated replicas, skipping zero-rate handling",
				"modelID", modelID)
			continue
		}

		// Zero-rate scenario: keep exactly one replica of one variant
		reason := "scale-to-zero disabled"
		if scaleToZeroEnabled {
			reason = fmt.Sprintf("recent requests (%.0f over retention period)", totalRequests)
		}
		logger.Log.Info("Applying zero-rate handling: keeping one replica",
			"modelID", modelID,
			"reason", reason)

		// Choose which variant to keep based on current state and cost
		variantToKeep := engine.selectVariantToKeep(variants, allocationSolution)
		if variantToKeep == nil {
			logger.Log.Warn("No variant selected to keep, skipping", "modelID", modelID)
			continue
		}

		logger.Log.Info("Selected variant to keep one replica",
			"modelID", modelID,
			"variant", variantToKeep.Name,
			"namespace", variantToKeep.Namespace)

		// Set one replica for selected variant, zero for others
		for _, va := range variants {
			serverName := utils.FullName(va.Name, va.Namespace)
			if allocData, exists := allocationSolution.Spec[serverName]; exists {
				if va.Name == variantToKeep.Name && va.Namespace == variantToKeep.Namespace {
					allocData.NumReplicas = 1
				} else {
					allocData.NumReplicas = 0
				}
				allocationSolution.Spec[serverName] = allocData
			}
		}
	}
}

// selectVariantToKeep chooses which variant should keep one replica in zero-rate scenario
// Priority:
// 1. If only one variant exists, keep it
// 2. If only one variant has non-zero current replicas, keep it
// 3. If multiple variants have non-zero replicas, keep the cheapest one
// 4. If all variants have zero replicas, keep the cheapest one from solution
func (engine *VariantAutoscalingsEngine) selectVariantToKeep(
	variants []*llmdOptv1alpha1.VariantAutoscaling,
	allocationSolution *infernoConfig.AllocationSolution,
) *llmdOptv1alpha1.VariantAutoscaling {
	if len(variants) == 0 {
		return nil
	}

	// Case 1: Only one variant exists
	if len(variants) == 1 {
		return variants[0]
	}

	// Find variants with non-zero current replicas
	variantsWithReplicas := make([]*llmdOptv1alpha1.VariantAutoscaling, 0)
	for _, va := range variants {
		if va.Status.CurrentAlloc.NumReplicas > 0 {
			variantsWithReplicas = append(variantsWithReplicas, va)
		}
	}

	// Case 2: Only one variant has non-zero current replicas
	if len(variantsWithReplicas) == 1 {
		return variantsWithReplicas[0]
	}

	// Case 3 & 4: Choose cheapest variant
	// If multiple have replicas, choose from those; otherwise choose from all variants
	candidateVariants := variantsWithReplicas
	if len(candidateVariants) == 0 {
		candidateVariants = variants
	}

	return engine.selectCheapestVariant(candidateVariants, allocationSolution)
}

// selectCheapestVariant returns the variant with lowest cost from the allocation solution
func (engine *VariantAutoscalingsEngine) selectCheapestVariant(
	variants []*llmdOptv1alpha1.VariantAutoscaling,
	allocationSolution *infernoConfig.AllocationSolution,
) *llmdOptv1alpha1.VariantAutoscaling {
	if len(variants) == 0 {
		return nil
	}

	var cheapestVariant *llmdOptv1alpha1.VariantAutoscaling
	var minCost float32 = math.MaxFloat32

	for _, va := range variants {
		serverName := utils.FullName(va.Name, va.Namespace)
		if allocData, exists := allocationSolution.Spec[serverName]; exists {
			if allocData.Cost < minCost {
				minCost = allocData.Cost
				cheapestVariant = va
			}
		}
	}

	// Fallback to first variant if no cost info available
	if cheapestVariant == nil {
		logger.Log.Warn("No cost information available in allocation solution, selecting first variant",
			"variant", variants[0].Name,
			"namespace", variants[0].Namespace)
		return variants[0]
	}

	return cheapestVariant
}
