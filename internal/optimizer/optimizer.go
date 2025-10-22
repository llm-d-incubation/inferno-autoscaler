package optimizer

import (
	"context"
	"fmt"

	llmdOptv1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	interfaces "github.com/llm-d-incubation/workload-variant-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/utils"
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
) (map[string]llmdOptv1alpha1.OptimizedAlloc, error) {

	if err := engine.manager.Optimize(); err != nil {
		// Return empty map instead of nil to prevent panic in controller
		return make(map[string]llmdOptv1alpha1.OptimizedAlloc), err
	}
	allocationSolution := engine.system.GenerateSolution()
	if allocationSolution == nil || len(allocationSolution.Spec) == 0 {
		// Return empty map instead of nil to prevent panic in controller
		return make(map[string]llmdOptv1alpha1.OptimizedAlloc), fmt.Errorf("no feasible allocations found for all variants")
	}

	logger.Log.Debug("Optimization solution - ", "system: ", engine.system)

	optimizedAllocMap := make(map[string]llmdOptv1alpha1.OptimizedAlloc)
	for _, va := range vaList.Items {
		vaName := va.Name
		vaNamespace := va.Namespace
		variantID := va.Spec.VariantID
		if optimizedAllocation, err := utils.CreateOptimizedAlloc(vaName, vaNamespace, variantID, allocationSolution); err == nil {
			optimizedAllocMap[vaName] = *optimizedAllocation
		}
	}
	return optimizedAllocMap, nil
}
