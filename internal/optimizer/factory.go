package optimizer

import (
	"context"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

const (
	configMapName      = "inferno-autoscaler-variantautoscaling-config"
	configMapNamespace = "inferno-autoscaler-system"
)

// getOptimizerConfigFromConfigMap retrieves a configuration value from the ConfigMap
func getOptimizerConfigFromConfigMap(client client.Client, ctx context.Context, key string) string {
	cm := &corev1.ConfigMap{}
	backoff := wait.Backoff{
		Duration: 100,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    3, // Fewer retries since this is fallback
	}

	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		err := client.Get(ctx, types.NamespacedName{
			Name:      configMapName,
			Namespace: configMapNamespace,
		}, cm)
		if err == nil {
			return true, nil
		}

		if apierrors.IsNotFound(err) {
			logger.Log.Debug("ConfigMap not found for optimizer config", "name", configMapName, "namespace", configMapNamespace)
			return false, err
		}

		logger.Log.Debug("Transient error fetching ConfigMap for optimizer config, retrying...", "error", err)
		return false, nil
	})

	if err != nil {
		logger.Log.Debug("Failed to get ConfigMap for optimizer config", "error", err, "key", key)
		return ""
	}

	if value, exists := cm.Data[key]; exists {
		logger.Log.Debug("Found optimizer config value in ConfigMap", "key", key, "value", value)
		return value
	}

	return ""
}

// GetOptimizerConfig reads optimizer configuration using hybrid approach (env vars first, then ConfigMap)
func GetOptimizerConfig(client client.Client, ctx context.Context) OptimizerConfig {
	config := OptimizerConfig{
		Type:         OptimizerTypeGo, // Default
		PythonPath:   "python3",
		WorkingDir:   "/tmp",
	}
	
	// Read optimizer type - try env var first, then ConfigMap
	if optimizerType := os.Getenv("INFERNO_OPTIMIZER_TYPE"); optimizerType != "" {
		config.Type = OptimizerType(optimizerType)
		logger.Log.Debug("Using optimizer type from environment variable", "type", optimizerType)
	} else if optimizerType := getOptimizerConfigFromConfigMap(client, ctx, "INFERNO_OPTIMIZER_TYPE"); optimizerType != "" {
		config.Type = OptimizerType(optimizerType)
		logger.Log.Debug("Using optimizer type from ConfigMap", "type", optimizerType)
	}
	
	// Read Python path - try env var first, then ConfigMap
	if pythonPath := os.Getenv("INFERNO_PYTHON_PATH"); pythonPath != "" {
		config.PythonPath = pythonPath
		logger.Log.Debug("Using Python path from environment variable", "path", pythonPath)
	} else if pythonPath := getOptimizerConfigFromConfigMap(client, ctx, "INFERNO_PYTHON_PATH"); pythonPath != "" {
		config.PythonPath = pythonPath
		logger.Log.Debug("Using Python path from ConfigMap", "path", pythonPath)
	}
	
	// Read Python script - try env var first, then ConfigMap, then default
	if pythonScript := os.Getenv("INFERNO_PYTHON_SCRIPT"); pythonScript != "" {
		config.PythonScript = pythonScript
		logger.Log.Debug("Using Python script from environment variable", "script", pythonScript)
	} else if pythonScript := getOptimizerConfigFromConfigMap(client, ctx, "INFERNO_PYTHON_SCRIPT"); pythonScript != "" {
		config.PythonScript = pythonScript
		logger.Log.Debug("Using Python script from ConfigMap", "script", pythonScript)
	} else {
		// Default to relative path from project root
		config.PythonScript = "autoscaler/cmd_folder/go_autoscaler_wrapper.py"
		logger.Log.Debug("Using default Python script", "script", config.PythonScript)
	}
	
	// Read working directory - try env var first, then ConfigMap
	if workingDir := os.Getenv("INFERNO_WORKING_DIR"); workingDir != "" {
		config.WorkingDir = workingDir
		logger.Log.Debug("Using working directory from environment variable", "dir", workingDir)
	} else if workingDir := getOptimizerConfigFromConfigMap(client, ctx, "INFERNO_WORKING_DIR"); workingDir != "" {
		config.WorkingDir = workingDir
		logger.Log.Debug("Using working directory from ConfigMap", "dir", workingDir)
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

