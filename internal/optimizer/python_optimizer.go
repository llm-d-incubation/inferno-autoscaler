package optimizer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	llmdOptv1alpha1 "github.com/llm-d-incubation/inferno-autoscaler/api/v1alpha1"
	collector "github.com/llm-d-incubation/inferno-autoscaler/internal/collector"
	interfaces "github.com/llm-d-incubation/inferno-autoscaler/internal/interfaces"
	"github.com/llm-d-incubation/inferno-autoscaler/internal/logger"
	infernoConfig "github.com/llm-inferno/optimizer-light/pkg/config"
)

// PythonOptimizerInput represents all data needed by Python optimizer
type PythonOptimizerInput struct {
	VariantAutoscalings []llmdOptv1alpha1.VariantAutoscaling             `json:"variant_autoscalings"`
	ModelAnalysis       map[string]*interfaces.ModelAnalyzeResponse      `json:"model_analysis"`
	ClusterInventory    map[string]map[string]collector.AcceleratorModelInfo `json:"cluster_inventory"`
	AcceleratorConfig   map[string]map[string]string                     `json:"accelerator_config"`
	ServiceClassConfig  map[string]string                                `json:"service_class_config"`
	SystemData          *infernoConfig.SystemData                        `json:"system_data"`
	Timestamp           time.Time                                        `json:"timestamp"`
}

// PythonOptimizerOutput represents the expected output from Python optimizer
type PythonOptimizerOutput struct {
	OptimizedAllocations    map[string]llmdOptv1alpha1.OptimizedAlloc `json:"optimized_allocations"`
	RawOptimizationResult   map[string]interface{}                    `json:"raw_optimization_result,omitempty"`
	Success                 bool                                      `json:"success"`
	Error                   string                                    `json:"error,omitempty"`
	ProcessingTime          float64                                   `json:"processing_time"`
	Timestamp               time.Time                                 `json:"timestamp"`
}

// PythonVariantAutoscalingsEngine implements VariantAutoscalingsEngine using Python subprocess
type PythonVariantAutoscalingsEngine struct {
	pythonPath  string
	scriptPath  string
	workingDir  string
	timeout     time.Duration
	clusterData *ComprehensiveClusterData
}

// NewPythonVariantAutoscalingsEngine creates a new Python-based optimizer engine
func NewPythonVariantAutoscalingsEngine(
	pythonPath string,
	scriptPath string,
	workingDir string,
	clusterData *ComprehensiveClusterData,
) *PythonVariantAutoscalingsEngine {
	if pythonPath == "" {
		pythonPath = "python3"
	}
	if workingDir == "" {
		workingDir = "/tmp"
	}
	
	return &PythonVariantAutoscalingsEngine{
		pythonPath:  pythonPath,
		scriptPath:  scriptPath,
		workingDir:  workingDir,
		timeout:     5 * time.Minute, // Default timeout
		clusterData: clusterData,
	}
}

// Optimize implements the VariantAutoscalingsEngine interface using Python subprocess
func (e *PythonVariantAutoscalingsEngine) Optimize(
	ctx context.Context,
	vaList llmdOptv1alpha1.VariantAutoscalingList,
	analysis map[string]*interfaces.ModelAnalyzeResponse,
) (map[string]llmdOptv1alpha1.OptimizedAlloc, error) {
	
	logger.Log.Info("Starting Python optimization process")
	
	// Use the comprehensive cluster data passed to constructor
	input := PythonOptimizerInput{
		VariantAutoscalings: e.clusterData.UpdateList.Items, // Use prepared variants from cluster data
		ModelAnalysis:       analysis, // Use the analysis parameter passed to Optimize method
		ClusterInventory:    e.clusterData.Inventory,
		AcceleratorConfig:   e.clusterData.AcceleratorConfig,
		ServiceClassConfig:  e.clusterData.ServiceClassConfig,
		SystemData:          e.clusterData.SystemData,
		Timestamp:           time.Now(),
	}
	
	// Execute Python optimization
	output, err := e.executePythonOptimizer(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("Python optimization failed: %w", err)
	}
	
	if !output.Success {
		return nil, fmt.Errorf("Python optimization returned error: %s", output.Error)
	}
	
	logger.Log.Info("Python optimization completed successfully", 
		"processing_time", output.ProcessingTime,
		"allocations_count", len(output.OptimizedAllocations))
	
	// Log raw optimization result for debugging if available
	if output.RawOptimizationResult != nil {
		logger.Log.Debug("Python raw optimization result", "raw_result", output.RawOptimizationResult)
	}
	
	return output.OptimizedAllocations, nil
}


// executePythonOptimizer executes the Python optimization process
func (e *PythonVariantAutoscalingsEngine) executePythonOptimizer(
	ctx context.Context,
	input PythonOptimizerInput,
) (*PythonOptimizerOutput, error) {
	
	// Create temporary files
	inputFile, err := e.createTempFile("input_*.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create input file: %w", err)
	}
	defer os.Remove(inputFile)
	
	outputFile, err := e.createTempFile("output_*.json")
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer os.Remove(outputFile)
	
	// Write input data to file
	if err := e.writeJSONFile(inputFile, input); err != nil {
		return nil, fmt.Errorf("failed to write input file: %w", err)
	}
	
	logger.Log.Debug("Executing Python optimizer", 
		"script", e.scriptPath,
		"input_file", inputFile,
		"output_file", outputFile)
	
	// Execute Python process
	cmd := exec.CommandContext(ctx, e.pythonPath, e.scriptPath, inputFile, outputFile)
	cmd.Dir = filepath.Dir(e.scriptPath)
	
	// Set timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	cmd = exec.CommandContext(timeoutCtx, e.pythonPath, e.scriptPath, inputFile, outputFile)
	
	// Execute and capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("Python process failed: %w, output: %s", err, string(output))
	}
	
	logger.Log.Debug("Python process completed", "output", string(output))
	
	// Read and parse output
	result, err := e.readJSONFile(outputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read output file: %w", err)
	}
	
	return result, nil
}

// createTempFile creates a temporary file in the working directory
func (e *PythonVariantAutoscalingsEngine) createTempFile(pattern string) (string, error) {
	file, err := os.CreateTemp(e.workingDir, pattern)
	if err != nil {
		return "", err
	}
	file.Close()
	return file.Name(), nil
}

// writeJSONFile writes data as JSON to the specified file
func (e *PythonVariantAutoscalingsEngine) writeJSONFile(filename string, data interface{}) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// readJSONFile reads and unmarshals JSON data from the specified file
func (e *PythonVariantAutoscalingsEngine) readJSONFile(filename string) (*PythonOptimizerOutput, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	var result PythonOptimizerOutput
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	
	return &result, nil
}

// SetTimeout sets the timeout for Python process execution
func (e *PythonVariantAutoscalingsEngine) SetTimeout(timeout time.Duration) {
	e.timeout = timeout
}