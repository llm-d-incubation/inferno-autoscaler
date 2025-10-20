package utils

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Helper function to create bool pointers for test cases
func boolPtr(b bool) *bool {
	return &b
}

// TestIsScaleToZeroEnabled tests the IsScaleToZeroEnabled function
func TestIsScaleToZeroEnabled(t *testing.T) {
	tests := []struct {
		name           string
		configData     ScaleToZeroConfigData
		modelID        string
		envVarValue    string
		expectedResult bool
		setEnv         bool
	}{
		{
			name: "Model with scale-to-zero enabled in ConfigMap",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "5m",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			expectedResult: true,
			setEnv:         false,
		},
		{
			name: "Model with scale-to-zero disabled in ConfigMap",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-70b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-70b",
			expectedResult: false,
			setEnv:         false,
		},
		{
			name:           "Model not in ConfigMap, global env var true",
			configData:     ScaleToZeroConfigData{},
			modelID:        "meta/llama-3.1-13b",
			envVarValue:    "true",
			expectedResult: true,
			setEnv:         true,
		},
		{
			name:           "Model not in ConfigMap, global env var false",
			configData:     ScaleToZeroConfigData{},
			modelID:        "meta/llama-3.1-13b",
			envVarValue:    "false",
			expectedResult: false,
			setEnv:         true,
		},
		{
			name:           "Model not in ConfigMap, global env var TRUE (case insensitive)",
			configData:     ScaleToZeroConfigData{},
			modelID:        "meta/llama-3.1-13b",
			envVarValue:    "TRUE",
			expectedResult: true,
			setEnv:         true,
		},
		{
			name:           "Model not in ConfigMap, no env var",
			configData:     ScaleToZeroConfigData{},
			modelID:        "meta/llama-3.1-13b",
			expectedResult: false,
			setEnv:         false,
		},
		{
			name: "ConfigMap overrides global env var (enabled in ConfigMap)",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "5m",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			envVarValue:    "false",
			expectedResult: true,
			setEnv:         true,
		},
		{
			name: "ConfigMap overrides global env var (disabled in ConfigMap)",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-70b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-70b",
			envVarValue:    "true",
			expectedResult: false,
			setEnv:         true,
		},
		{
			name:           "Empty ConfigMap with nil data",
			configData:     nil,
			modelID:        "meta/llama-3.1-13b",
			envVarValue:    "true",
			expectedResult: true,
			setEnv:         true,
		},
		{
			name: "Model not in ConfigMap, global defaults enabled",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
			},
			modelID:        "meta/llama-3.1-13b",
			expectedResult: true,
			setEnv:         false,
		},
		{
			name: "Model not in ConfigMap, global defaults disabled",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-13b",
			expectedResult: false,
			setEnv:         false,
		},
		{
			name: "Per-model overrides global defaults (model enabled, defaults disabled)",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "5m",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			expectedResult: true,
			setEnv:         false,
		},
		{
			name: "Per-model overrides global defaults (model disabled, defaults enabled)",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
				"meta/llama-3.1-70b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-70b",
			expectedResult: false,
			setEnv:         false,
		},
		{
			name: "Global defaults override environment variable (defaults enabled, env false)",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
			},
			modelID:        "meta/llama-3.1-13b",
			envVarValue:    "false",
			expectedResult: true,
			setEnv:         true,
		},
		{
			name: "Global defaults override environment variable (defaults disabled, env true)",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-13b",
			envVarValue:    "true",
			expectedResult: false,
			setEnv:         true,
		},
		// NEW: Critical UX tests for partial overrides
		{
			name: "Partial override: only retentionPeriod specified, inherits enableScaleToZero from defaults",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					// EnableScaleToZero: nil (not set, should inherit from defaults)
					RetentionPeriod: "5m", // Override only retention
				},
			},
			modelID:        "meta/llama-3.1-8b",
			expectedResult: true, // Should inherit enabled=true from defaults
			setEnv:         false,
		},
		{
			name: "Partial override: only enableScaleToZero specified as false, retention from defaults",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
				"meta/llama-3.1-70b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false), // Explicitly disable
					// RetentionPeriod: "" (not set, but shouldn't matter since disabled)
				},
			},
			modelID:        "meta/llama-3.1-70b",
			expectedResult: false, // Explicitly disabled
			setEnv:         false,
		},
		{
			name: "Partial override: only enableScaleToZero specified as true, retention from defaults",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
					RetentionPeriod:   "30m",
				},
				"meta/llama-2-7b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true), // Override to enabled
					// RetentionPeriod: "" (will inherit "30m" from defaults in retention function)
				},
			},
			modelID:        "meta/llama-2-7b",
			expectedResult: true, // Explicitly enabled, overriding defaults
			setEnv:         false,
		},
		{
			name: "Model config exists but both fields nil, inherits everything from defaults",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "20m",
				},
				"meta/mistral-7b": ModelScaleToZeroConfig{
					// Both fields nil - complete inheritance
				},
			},
			modelID:        "meta/mistral-7b",
			expectedResult: true, // Inherits enabled=true from defaults
			setEnv:         false,
		},
		{
			name: "No model entry, no defaults, falls back to environment variable",
			configData: ScaleToZeroConfigData{
				"meta/other-model": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
				},
			},
			modelID:        "meta/llama-3.1-8b",
			envVarValue:    "true",
			expectedResult: true, // Falls back to env var
			setEnv:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variable
			if tt.setEnv {
				os.Setenv("WVA_SCALE_TO_ZERO", tt.envVarValue)
				defer os.Unsetenv("WVA_SCALE_TO_ZERO")
			} else {
				os.Unsetenv("WVA_SCALE_TO_ZERO")
			}

			result := IsScaleToZeroEnabled(tt.configData, tt.modelID)
			assert.Equal(t, tt.expectedResult, result, "IsScaleToZeroEnabled should return %v", tt.expectedResult)
		})
	}
}

// TestGetScaleToZeroRetentionPeriod tests the GetScaleToZeroRetentionPeriod function
func TestGetScaleToZeroRetentionPeriod(t *testing.T) {
	tests := []struct {
		name           string
		configData     ScaleToZeroConfigData
		modelID        string
		expectedResult time.Duration
	}{
		{
			name: "Model with custom retention period in ConfigMap",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "5m",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			expectedResult: 5 * time.Minute,
		},
		{
			name: "Model with 15 minute retention period",
			configData: ScaleToZeroConfigData{
				"mistralai/Mistral-7B-v0.1": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
			},
			modelID:        "mistralai/Mistral-7B-v0.1",
			expectedResult: 15 * time.Minute,
		},
		{
			name: "Model with 1 hour retention period",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-405b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "1h",
				},
			},
			modelID:        "meta/llama-3.1-405b",
			expectedResult: 1 * time.Hour,
		},
		{
			name: "Model with 30 second retention period",
			configData: ScaleToZeroConfigData{
				"meta/llama-2-7b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "30s",
				},
			},
			modelID:        "meta/llama-2-7b",
			expectedResult: 30 * time.Second,
		},
		{
			name: "Model with no retention period specified (default 10m)",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-70b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "",
				},
			},
			modelID:        "meta/llama-3.1-70b",
			expectedResult: 10 * time.Minute,
		},
		{
			name:           "Model not in ConfigMap (default 10m)",
			configData:     ScaleToZeroConfigData{},
			modelID:        "meta/llama-3.1-13b",
			expectedResult: 10 * time.Minute,
		},
		{
			name: "Model with invalid retention period (fallback to default)",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "invalid",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			expectedResult: 10 * time.Minute,
		},
		{
			name:           "Empty ConfigMap with nil data",
			configData:     nil,
			modelID:        "meta/llama-3.1-13b",
			expectedResult: 10 * time.Minute,
		},
		{
			name: "Model not in ConfigMap, uses global default retention period",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "20m",
				},
			},
			modelID:        "meta/llama-3.1-13b",
			expectedResult: 20 * time.Minute,
		},
		{
			name: "Per-model retention period overrides global defaults",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "20m",
				},
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "5m",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			expectedResult: 5 * time.Minute,
		},
		{
			name: "Global default with 1 hour retention period",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "1h",
				},
			},
			modelID:        "meta/llama-3.1-13b",
			expectedResult: 1 * time.Hour,
		},
		{
			name: "Invalid global default retention period falls back to system default",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "invalid",
				},
			},
			modelID:        "meta/llama-3.1-13b",
			expectedResult: 10 * time.Minute,
		},
		{
			name: "Model has invalid retention, falls back to global default",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "invalid",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			expectedResult: 15 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetScaleToZeroRetentionPeriod(tt.configData, tt.modelID)
			assert.Equal(t, tt.expectedResult, result, "GetScaleToZeroRetentionPeriod should return %v", tt.expectedResult)
		})
	}
}

// TestGetMinNumReplicas tests the GetMinNumReplicas function
func TestGetMinNumReplicas(t *testing.T) {
	tests := []struct {
		name           string
		configData     ScaleToZeroConfigData
		modelID        string
		envVarValue    string
		setEnv         bool
		expectedResult int
	}{
		{
			name: "Model with scale-to-zero enabled (min replicas 0)",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "5m",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			expectedResult: 0,
		},
		{
			name: "Model with scale-to-zero disabled (min replicas 1)",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-70b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-70b",
			expectedResult: 1,
		},
		{
			name:           "Model not in ConfigMap, global env var true (min replicas 0)",
			configData:     ScaleToZeroConfigData{},
			modelID:        "meta/llama-3.1-13b",
			envVarValue:    "true",
			setEnv:         true,
			expectedResult: 0,
		},
		{
			name:           "Model not in ConfigMap, global env var false (min replicas 1)",
			configData:     ScaleToZeroConfigData{},
			modelID:        "meta/llama-3.1-13b",
			envVarValue:    "false",
			setEnv:         true,
			expectedResult: 1,
		},
		{
			name:           "Model not in ConfigMap, no env var (min replicas 1)",
			configData:     ScaleToZeroConfigData{},
			modelID:        "meta/llama-3.1-13b",
			setEnv:         false,
			expectedResult: 1,
		},
		{
			name: "ConfigMap enabled overrides global env var false",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "5m",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			envVarValue:    "false",
			setEnv:         true,
			expectedResult: 0,
		},
		{
			name: "ConfigMap disabled overrides global env var true",
			configData: ScaleToZeroConfigData{
				"meta/llama-3.1-70b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-70b",
			envVarValue:    "true",
			setEnv:         true,
			expectedResult: 1,
		},
		{
			name: "Global defaults enabled (min replicas 0)",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
			},
			modelID:        "meta/llama-3.1-13b",
			setEnv:         false,
			expectedResult: 0,
		},
		{
			name: "Global defaults disabled (min replicas 1)",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-13b",
			setEnv:         false,
			expectedResult: 1,
		},
		{
			name: "Per-model overrides global defaults enabled",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
				"meta/llama-3.1-8b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "5m",
				},
			},
			modelID:        "meta/llama-3.1-8b",
			setEnv:         false,
			expectedResult: 0,
		},
		{
			name: "Per-model overrides global defaults disabled",
			configData: ScaleToZeroConfigData{
				"__defaults__": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(true),
					RetentionPeriod:   "15m",
				},
				"meta/llama-3.1-70b": ModelScaleToZeroConfig{
					EnableScaleToZero: boolPtr(false),
				},
			},
			modelID:        "meta/llama-3.1-70b",
			setEnv:         false,
			expectedResult: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment variable
			if tt.setEnv {
				os.Setenv("WVA_SCALE_TO_ZERO", tt.envVarValue)
				defer os.Unsetenv("WVA_SCALE_TO_ZERO")
			} else {
				os.Unsetenv("WVA_SCALE_TO_ZERO")
			}

			result := GetMinNumReplicas(tt.configData, tt.modelID)
			assert.Equal(t, tt.expectedResult, result, "GetMinNumReplicas should return %v", tt.expectedResult)
		})
	}
}

// TestScaleToZeroConfigDataType tests the ScaleToZeroConfigData type
func TestScaleToZeroConfigDataType(t *testing.T) {
	t.Run("Create and manipulate ScaleToZeroConfigData", func(t *testing.T) {
		// Create empty config data
		configData := make(ScaleToZeroConfigData)
		assert.NotNil(t, configData)
		assert.Equal(t, 0, len(configData))

		// Add model config
		configData["meta/llama-3.1-8b"] = ModelScaleToZeroConfig{
			EnableScaleToZero: boolPtr(true),
			RetentionPeriod:   "5m",
		}
		assert.Equal(t, 1, len(configData))

		// Retrieve and verify
		config, exists := configData["meta/llama-3.1-8b"]
		assert.True(t, exists)
		assert.True(t, *config.EnableScaleToZero)
		assert.Equal(t, "5m", config.RetentionPeriod)

		// Non-existent model
		_, exists = configData["meta/llama-3.1-70b"]
		assert.False(t, exists)
	})

	t.Run("Multiple models in ConfigData", func(t *testing.T) {
		configData := ScaleToZeroConfigData{
			"meta/llama-3.1-8b": ModelScaleToZeroConfig{
				EnableScaleToZero: boolPtr(true),
				RetentionPeriod:   "5m",
			},
			"meta/llama-3.1-70b": ModelScaleToZeroConfig{
				EnableScaleToZero: boolPtr(false),
			},
			"mistralai/Mistral-7B-v0.1": ModelScaleToZeroConfig{
				EnableScaleToZero: boolPtr(true),
				RetentionPeriod:   "15m",
			},
		}

		assert.Equal(t, 3, len(configData))

		// Verify each model
		assert.True(t, *configData["meta/llama-3.1-8b"].EnableScaleToZero)
		assert.Equal(t, "5m", configData["meta/llama-3.1-8b"].RetentionPeriod)

		assert.False(t, *configData["meta/llama-3.1-70b"].EnableScaleToZero)
		assert.Empty(t, configData["meta/llama-3.1-70b"].RetentionPeriod)

		assert.True(t, *configData["mistralai/Mistral-7B-v0.1"].EnableScaleToZero)
		assert.Equal(t, "15m", configData["mistralai/Mistral-7B-v0.1"].RetentionPeriod)
	})
}

// TestModelScaleToZeroConfig tests the ModelScaleToZeroConfig struct
func TestModelScaleToZeroConfig(t *testing.T) {
	t.Run("Create config with retention period", func(t *testing.T) {
		config := ModelScaleToZeroConfig{
			EnableScaleToZero: boolPtr(true),
			RetentionPeriod:   "5m",
		}

		assert.True(t, *config.EnableScaleToZero)
		assert.Equal(t, "5m", config.RetentionPeriod)
	})

	t.Run("Create config without retention period", func(t *testing.T) {
		config := ModelScaleToZeroConfig{
			EnableScaleToZero: boolPtr(true),
		}

		assert.True(t, *config.EnableScaleToZero)
		assert.Empty(t, config.RetentionPeriod)
	})

	t.Run("Create config with disabled scale-to-zero", func(t *testing.T) {
		config := ModelScaleToZeroConfig{
			EnableScaleToZero: boolPtr(false),
		}

		assert.False(t, *config.EnableScaleToZero)
		assert.Empty(t, config.RetentionPeriod)
	})
}

// TestScaleToZeroIntegration tests the integration of all scale-to-zero functions
func TestScaleToZeroIntegration(t *testing.T) {
	t.Run("End-to-end workflow for model with scale-to-zero enabled", func(t *testing.T) {
		configData := ScaleToZeroConfigData{
			"meta/llama-3.1-8b": ModelScaleToZeroConfig{
				EnableScaleToZero: boolPtr(true),
				RetentionPeriod:   "5m",
			},
		}
		modelID := "meta/llama-3.1-8b"

		// Check if scale-to-zero is enabled
		enabled := IsScaleToZeroEnabled(configData, modelID)
		assert.True(t, enabled)

		// Get retention period
		retention := GetScaleToZeroRetentionPeriod(configData, modelID)
		assert.Equal(t, 5*time.Minute, retention)

		// Get min replicas
		minReplicas := GetMinNumReplicas(configData, modelID)
		assert.Equal(t, 0, minReplicas)
	})

	t.Run("End-to-end workflow for model with scale-to-zero disabled", func(t *testing.T) {
		configData := ScaleToZeroConfigData{
			"meta/llama-3.1-70b": ModelScaleToZeroConfig{
				EnableScaleToZero: boolPtr(false),
			},
		}
		modelID := "meta/llama-3.1-70b"

		// Check if scale-to-zero is enabled
		enabled := IsScaleToZeroEnabled(configData, modelID)
		assert.False(t, enabled)

		// Get retention period (still returns default even if disabled)
		retention := GetScaleToZeroRetentionPeriod(configData, modelID)
		assert.Equal(t, 10*time.Minute, retention)

		// Get min replicas
		minReplicas := GetMinNumReplicas(configData, modelID)
		assert.Equal(t, 1, minReplicas)
	})

	t.Run("End-to-end workflow for model using global defaults", func(t *testing.T) {
		os.Setenv("WVA_SCALE_TO_ZERO", "true")
		defer os.Unsetenv("WVA_SCALE_TO_ZERO")

		configData := ScaleToZeroConfigData{}
		modelID := "meta/llama-3.1-13b"

		// Check if scale-to-zero is enabled
		enabled := IsScaleToZeroEnabled(configData, modelID)
		assert.True(t, enabled)

		// Get retention period (uses default)
		retention := GetScaleToZeroRetentionPeriod(configData, modelID)
		assert.Equal(t, 10*time.Minute, retention)

		// Get min replicas
		minReplicas := GetMinNumReplicas(configData, modelID)
		assert.Equal(t, 0, minReplicas)
	})
}
