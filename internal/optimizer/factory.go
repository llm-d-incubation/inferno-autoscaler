package optimizer

import (
	"os"
	"path/filepath"

	interfaces "github.com/llm-d-incubation/inferno-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/inferno-autoscaler/internal/logger"
	inferno "github.com/llm-inferno/optimizer-light/pkg/core"
	infernoManager "github.com/llm-inferno/optimizer-light/pkg/manager"
	infernoSolver "github.com/llm-inferno/optimizer-light/pkg/solver"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OptimizerType represents the type of optimizer to create
type OptimizerType string

const (
	OptimizerTypeGo     OptimizerType = "go"
	OptimizerTypePython OptimizerType = "python"
)

// OptimizerConfig holds configuration for creating optimizers
type OptimizerConfig struct {
	Type           OptimizerType
	PythonPath     string
	PythonScript   string
	WorkingDir     string
}

// Optimizer holds both the engine and system for Go optimizer
type Optimizer struct {
	Engine interfaces.VariantAutoscalingsEngine
	System *inferno.System // Only populated for Go optimizer
}

// NewOptimizerEngine creates a new optimizer engine based on configuration
func NewOptimizerEngine(
	config OptimizerConfig,
	client client.Client,
	promAPI prometheusv1.API,
	clusterData *ComprehensiveClusterData,
) *Optimizer {

	switch config.Type {
	case OptimizerTypePython:
		logger.Log.Info("Creating Python optimizer engine", 
			"python_path", config.PythonPath,
			"script_path", config.PythonScript)
		
		engine := NewPythonVariantAutoscalingsEngine(
			config.PythonPath,
			config.PythonScript,
			config.WorkingDir,
			clusterData,
		)
		
		return &Optimizer{
			Engine: engine,
			System: nil, // Python optimizer doesn't need system
		}
		
	case OptimizerTypeGo:
		fallthrough
	default:
		logger.Log.Info("Creating Go optimizer engine")
		
		// Initialize Go-specific system components using cluster data
		system := inferno.NewSystem()
		optimizerSpec := system.SetFromSpec(&clusterData.SystemData.Spec)
		optimizer := infernoSolver.NewOptimizerFromSpec(optimizerSpec)
		manager := infernoManager.NewManager(system, optimizer)
		
		engine := NewVariantAutoscalingsEngine(manager, system)
		
		return &Optimizer{
			Engine: engine,
			System: system, // Return system for model analysis
		}
	}
}

// GetOptimizerConfigFromEnv reads optimizer configuration from environment variables
func GetOptimizerConfigFromEnv() OptimizerConfig {
	config := OptimizerConfig{
		Type:         OptimizerTypeGo, // Default
		PythonPath:   "python3",
		WorkingDir:   "/tmp",
	}
	
	// Read optimizer type
	if optimizerType := os.Getenv("INFERNO_OPTIMIZER_TYPE"); optimizerType != "" {
		config.Type = OptimizerType(optimizerType)
	}
	
	// Read Python configuration
	if pythonPath := os.Getenv("INFERNO_PYTHON_PATH"); pythonPath != "" {
		config.PythonPath = pythonPath
	}
	
	if pythonScript := os.Getenv("INFERNO_PYTHON_SCRIPT"); pythonScript != "" {
		config.PythonScript = pythonScript
	} else {
		// Default to relative path from project root
		config.PythonScript = "autoscaler/cmd_folder/go_autoscaler_wrapper.py"
	}
	
	if workingDir := os.Getenv("INFERNO_WORKING_DIR"); workingDir != "" {
		config.WorkingDir = workingDir
	}
	
	// Make script path absolute if it's relative
	if !filepath.IsAbs(config.PythonScript) {
		if wd, err := os.Getwd(); err == nil {
			config.PythonScript = filepath.Join(wd, config.PythonScript)
		}
	}
	
	logger.Log.Info("Optimizer configuration loaded", 
		"type", config.Type,
		"python_path", config.PythonPath,
		"python_script", config.PythonScript,
		"working_dir", config.WorkingDir)
	
	return config
}