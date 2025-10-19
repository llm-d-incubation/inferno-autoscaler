package utils

import (
	"os"
	"testing"
	"time"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsScaleToZeroEnabled(t *testing.T) {
	tests := []struct {
		name           string
		perModelValue  *bool
		globalEnvValue string
		expected       bool
	}{
		{
			name:           "per-model enabled overrides global disabled",
			perModelValue:  boolPtr(true),
			globalEnvValue: "false",
			expected:       true,
		},
		{
			name:           "per-model disabled overrides global enabled",
			perModelValue:  boolPtr(false),
			globalEnvValue: "true",
			expected:       false,
		},
		{
			name:           "per-model not set, global enabled",
			perModelValue:  nil,
			globalEnvValue: "true",
			expected:       true,
		},
		{
			name:           "per-model not set, global disabled",
			perModelValue:  nil,
			globalEnvValue: "false",
			expected:       false,
		},
		{
			name:           "per-model not set, global not set",
			perModelValue:  nil,
			globalEnvValue: "",
			expected:       false,
		},
		{
			name:           "per-model enabled explicitly",
			perModelValue:  boolPtr(true),
			globalEnvValue: "",
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.globalEnvValue != "" {
				os.Setenv("WVA_SCALE_TO_ZERO", tt.globalEnvValue)
				defer os.Unsetenv("WVA_SCALE_TO_ZERO")
			} else {
				os.Unsetenv("WVA_SCALE_TO_ZERO")
			}

			// Create test VA
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					EnableScaleToZero: tt.perModelValue,
				},
			}

			// Test
			result := IsScaleToZeroEnabled(va)
			if result != tt.expected {
				t.Errorf("IsScaleToZeroEnabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetScaleToZeroRetentionPeriod(t *testing.T) {
	tests := []struct {
		name     string
		duration *metav1.Duration
		expected time.Duration
	}{
		{
			name:     "retention period set to 5 minutes",
			duration: &metav1.Duration{Duration: 5 * time.Minute},
			expected: 5 * time.Minute,
		},
		{
			name:     "retention period set to 30 seconds",
			duration: &metav1.Duration{Duration: 30 * time.Second},
			expected: 30 * time.Second,
		},
		{
			name:     "retention period not set",
			duration: nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					ScaleToZeroPodRetentionPeriod: tt.duration,
				},
			}

			result := GetScaleToZeroRetentionPeriod(va)
			if result != tt.expected {
				t.Errorf("GetScaleToZeroRetentionPeriod() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetMinNumReplicas(t *testing.T) {
	tests := []struct {
		name              string
		enableScaleToZero *bool
		globalEnvValue    string
		expected          int
	}{
		{
			name:              "scale-to-zero enabled per-model",
			enableScaleToZero: boolPtr(true),
			globalEnvValue:    "false",
			expected:          0,
		},
		{
			name:              "scale-to-zero disabled per-model",
			enableScaleToZero: boolPtr(false),
			globalEnvValue:    "true",
			expected:          1,
		},
		{
			name:              "scale-to-zero enabled globally",
			enableScaleToZero: nil,
			globalEnvValue:    "true",
			expected:          0,
		},
		{
			name:              "scale-to-zero disabled globally",
			enableScaleToZero: nil,
			globalEnvValue:    "false",
			expected:          1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.globalEnvValue != "" {
				os.Setenv("WVA_SCALE_TO_ZERO", tt.globalEnvValue)
				defer os.Unsetenv("WVA_SCALE_TO_ZERO")
			} else {
				os.Unsetenv("WVA_SCALE_TO_ZERO")
			}

			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					EnableScaleToZero: tt.enableScaleToZero,
				},
			}

			result := GetMinNumReplicas(va)
			if result != tt.expected {
				t.Errorf("GetMinNumReplicas() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Helper function to create bool pointers
func boolPtr(b bool) *bool {
	return &b
}
