package controller

import (
	"context"

	llmdOptv1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	inferno "github.com/llm-d-incubation/workload-variant-autoscaler/hack/inferno/pkg/core"
	interfaces "github.com/llm-d-incubation/workload-variant-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/logger"
	"github.com/llm-d-incubation/workload-variant-autoscaler/internal/utils"
)

// Performance analyzer of queueing models associated with variant servers
type ModelAnalyzer struct {
	// data about the inferencing system
	// (accelerators, models, service classes, servers, capacities, allocations)
	system *inferno.System
}

// Create a new instance of a model analyzer
func NewModelAnalyzer(system *inferno.System) *ModelAnalyzer {
	return &ModelAnalyzer{system: system}
}

// Analyze a particular variant and generate corresponding allocations that achieve SLOs for all accelerators, used by the optimizer
func (ma *ModelAnalyzer) AnalyzeModel(ctx context.Context,
	va llmdOptv1alpha1.VariantAutoscaling) *interfaces.ModelAnalyzeResponse {

	serverName := utils.FullName(va.Name, va.Namespace)
	logger.Log.Debug("DELETE ME - ", "Analyzing model for serverName - : ", serverName)
	if server, exists := ma.system.Servers()[serverName]; exists {
		logger.Log.Debug("DELETE ME - Found server in system - ", server.Name())
		server.Calculate(ma.system.Accelerators())
		logger.Log.Debug("DELETE ME - serverAlloc - ", server.AllAllocations())
		return CreateModelAnalyzeResponseFromAllocations(server.AllAllocations())
	}
	logger.Log.Debug("DELETE ME - ModelAnalyzer unable to find server in system, skipping optimization - ", "variantAutoscaling-name: ", va.Name)
	return &interfaces.ModelAnalyzeResponse{}
}
