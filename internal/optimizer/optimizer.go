package optimizer

import (
	"context"
	"fmt"

	llmdOptv1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	inferno "github.com/llm-d-incubation/workload-variant-autoscaler/hack/inferno/pkg/core"
	infernoManager "github.com/llm-d-incubation/workload-variant-autoscaler/hack/inferno/pkg/manager"
	interfaces "github.com/llm-d-incubation/workload-variant-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/utils"
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
		return nil, err
	}
	allocationSolution := engine.system.GenerateSolution()
	if allocationSolution == nil || len(allocationSolution.Spec) == 0 {
		return nil, fmt.Errorf("no feasible allocations found for all variants: ")
	}

	logger.Log.Debug("Optimization solution - ", "system: ", engine.system)

	optimizedAllocMap := make(map[string]llmdOptv1alpha1.OptimizedAlloc)
	for _, va := range vaList.Items {
		vaName := va.Name
		vaNamespace := va.Namespace
		if optimizedAllocation, err := utils.CreateOptimizedAlloc(vaName, vaNamespace, allocationSolution); err == nil {
			optimizedAllocMap[vaName] = *optimizedAllocation
		}
	}
	return optimizedAllocMap, nil
}
