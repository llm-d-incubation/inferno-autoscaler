package utils

import (
	"testing"
	"time"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
	infernoConfig "github.com/llm-d-incubation/workload-variant-autoscaler/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSuggestResourceNameFromVariantID(t *testing.T) {
	tests := []struct {
		name      string
		variantID string
		expected  string
	}{
		{
			name:      "variant with slashes and dots",
			variantID: "meta/llama-3.1-8b-A100-1",
			expected:  "meta-llama-3-1-8b-a100-1",
		},
		{
			name:      "variant with uppercase",
			variantID: "Meta/Llama-3.1-8B-A100-1",
			expected:  "meta-llama-3-1-8b-a100-1",
		},
		{
			name:      "variant with special characters",
			variantID: "model@name/variant_1",
			expected:  "modelname-variant1",
		},
		{
			name:      "variant with leading/trailing hyphens",
			variantID: "-model/variant-",
			expected:  "model-variant",
		},
		{
			name:      "simple variant",
			variantID: "vllm-deployment",
			expected:  "vllm-deployment",
		},
		{
			name:      "variant with multiple slashes",
			variantID: "org/team/model-A100-1",
			expected:  "org-team-model-a100-1",
		},
		{
			name:      "variant with underscores",
			variantID: "model_name_variant_1",
			expected:  "modelnamevariant1",
		},
		{
			name:      "variant with spaces",
			variantID: "model name variant 1",
			expected:  "modelnamevariant1",
		},
		{
			name:      "empty string",
			variantID: "",
			expected:  "",
		},
		{
			name:      "only invalid characters",
			variantID: "@#$%",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SuggestResourceNameFromVariantID(tt.variantID)
			if result != tt.expected {
				t.Errorf("SuggestResourceNameFromVariantID(%q) = %q, expected %q",
					tt.variantID, result, tt.expected)
			}
		})
	}
}

func TestValidateVariantAutoscalingName(t *testing.T) {
	tests := []struct {
		name          string
		vaName        string
		variantID     string
		shouldLogDiff bool
		description   string
	}{
		{
			name:          "matching normalized name",
			vaName:        "meta-llama-3-1-8b-a100-1",
			variantID:     "meta/llama-3.1-8b-A100-1",
			shouldLogDiff: false,
			description:   "VA name matches the normalized variant_id",
		},
		{
			name:          "different but valid names",
			vaName:        "vllm-deployment",
			variantID:     "meta/llama-3.1-8b-A100-1",
			shouldLogDiff: true,
			description:   "VA name differs from normalized variant_id (normal case)",
		},
		{
			name:          "identical strings",
			vaName:        "simple-variant",
			variantID:     "simple-variant",
			shouldLogDiff: false,
			description:   "Both names are identical",
		},
		{
			name:          "deployment-style name with complex variant_id",
			vaName:        "llm-inference",
			variantID:     "organization/model-name/variant-1-A100-8",
			shouldLogDiff: true,
			description:   "Deployment name style vs hierarchical variant_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			va := &llmdVariantAutoscalingV1alpha1.VariantAutoscaling{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.vaName,
					Namespace: "test-namespace",
				},
				Spec: llmdVariantAutoscalingV1alpha1.VariantAutoscalingSpec{
					VariantID: tt.variantID,
				},
			}

			// This function logs internally, we're just testing it doesn't panic
			// and completes successfully
			ValidateVariantAutoscalingName(va)

			// Verify the suggested name matches what we expect
			suggested := SuggestResourceNameFromVariantID(tt.variantID)
			if tt.shouldLogDiff {
				if va.Name == suggested {
					t.Errorf("Expected VA name %q to differ from suggested %q, but they match",
						va.Name, suggested)
				}
			} else {
				if va.Name != suggested {
					t.Errorf("Expected VA name %q to match suggested %q, but they differ",
						va.Name, suggested)
				}
			}
		})
	}
}

// TestDNS1123Compliance verifies that suggested names are valid Kubernetes resource names
func TestDNS1123Compliance(t *testing.T) {
	tests := []struct {
		name      string
		variantID string
	}{
		{
			name:      "complex variant",
			variantID: "Meta/Llama-3.1-8B-A100-1",
		},
		{
			name:      "special characters",
			variantID: "model@name/variant_1!test",
		},
		{
			name:      "unicode characters",
			variantID: "modèl/variánt",
		},
	}

	// DNS-1123 pattern: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ (or empty)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SuggestResourceNameFromVariantID(tt.variantID)

			// Check that result only contains valid characters
			for _, c := range result {
				if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
					t.Errorf("SuggestResourceNameFromVariantID(%q) = %q contains invalid character %q",
						tt.variantID, result, string(c))
				}
			}

			// Check it doesn't start or end with hyphen
			if len(result) > 0 {
				if result[0] == '-' || result[len(result)-1] == '-' {
					t.Errorf("SuggestResourceNameFromVariantID(%q) = %q starts or ends with hyphen",
						tt.variantID, result)
				}
			}

			t.Logf("Input: %q -> Output: %q (valid: %t)", tt.variantID, result, result == "" || len(result) > 0)
		})
	}
}

// TestRealWorldExamples tests with actual variant_id patterns from the codebase
func TestRealWorldExamples(t *testing.T) {
	tests := []struct {
		name      string
		variantID string
		vaName    string
	}{
		{
			name:      "e2e test pattern",
			variantID: "test-model-A100-1",
			vaName:    "vllm-deployment",
		},
		{
			name:      "openshift test pattern",
			variantID: "test-model/variant-1-A100-1",
			vaName:    "vllm-deployment",
		},
		{
			name:      "huggingface model pattern",
			variantID: "meta-llama/Llama-3.1-8B-Instruct",
			vaName:    "llama-deployment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggested := SuggestResourceNameFromVariantID(tt.variantID)
			t.Logf("variant_id: %q -> suggested: %q (actual va.Name: %q)",
				tt.variantID, suggested, tt.vaName)

			// Verify suggested name is valid
			if suggested != "" {
				// Must be lowercase alphanumeric with hyphens
				for _, c := range suggested {
					if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
						t.Errorf("Suggested name %q contains invalid character %q", suggested, string(c))
					}
				}
			}
		})
	}
}

// TestCreateOptimizedAlloc verifies that CreateOptimizedAlloc correctly sets VariantID
// from the parameter, matching the production code path in the optimizer.
func TestCreateOptimizedAlloc(t *testing.T) {
	tests := []struct {
		name          string
		vaName        string
		vaNamespace   string
		variantID     string
		accelerator   string
		numReplicas   int
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid allocation with variantID",
			vaName:      "vllm-deployment",
			vaNamespace: "default",
			variantID:   "meta/llama-3.1-8b-A100-1",
			accelerator: "A100",
			numReplicas: 3,
			expectError: false,
		},
		{
			name:        "variantID with slashes",
			vaName:      "test-deployment",
			vaNamespace: "test-ns",
			variantID:   "org/team/model-A100-4",
			accelerator: "A100",
			numReplicas: 2,
			expectError: false,
		},
		{
			name:        "simple variantID",
			vaName:      "simple-va",
			vaNamespace: "default",
			variantID:   "model-A100-1",
			accelerator: "A100",
			numReplicas: 1,
			expectError: false,
		},
		{
			name:          "server not found in solution",
			vaName:        "nonexistent",
			vaNamespace:   "default",
			variantID:     "test-A100-1",
			accelerator:   "A100",
			numReplicas:   1,
			expectError:   true,
			errorContains: "server nonexistent:default not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create allocation solution with the test data
			allocationSolution := &infernoConfig.AllocationSolution{
				Spec: map[string]infernoConfig.AllocationData{},
			}

			// Only add server to solution if we don't expect error
			if !tt.expectError {
				serverName := FullName(tt.vaName, tt.vaNamespace)
				allocationSolution.Spec[serverName] = infernoConfig.AllocationData{
					Accelerator: tt.accelerator,
					NumReplicas: tt.numReplicas,
					MaxBatch:    32,
					Cost:        40.0,
					ITLAverage:  50.0,
					TTFTAverage: 500.0,
				}
			}

			// Call CreateOptimizedAlloc
			result, err := CreateOptimizedAlloc(tt.vaName, tt.vaNamespace, tt.variantID, allocationSolution)

			// Check error expectations
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, but got nil", tt.errorContains)
					return
				}
				if tt.errorContains != "" {
					// Simple substring check
					found := false
					for i := 0; i <= len(err.Error())-len(tt.errorContains); i++ {
						if err.Error()[i:i+len(tt.errorContains)] == tt.errorContains {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected error containing %q, got %q", tt.errorContains, err.Error())
					}
				}
				return
			}

			// No error expected - verify result
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Error("Result should not be nil")
				return
			}

			// Verify VariantID is set correctly (this is the critical fix being tested)
			if result.VariantID != tt.variantID {
				t.Errorf("VariantID mismatch: got %q, expected %q", result.VariantID, tt.variantID)
			}

			// Verify other fields
			if result.Accelerator != tt.accelerator {
				t.Errorf("Accelerator mismatch: got %q, expected %q", result.Accelerator, tt.accelerator)
			}

			if result.NumReplicas != tt.numReplicas {
				t.Errorf("NumReplicas mismatch: got %d, expected %d", result.NumReplicas, tt.numReplicas)
			}

			// Verify LastRunTime is set (should be recent)
			if result.LastRunTime.Time.IsZero() {
				t.Error("LastRunTime should be set")
			}

			timeSinceCreation := time.Since(result.LastRunTime.Time)
			if timeSinceCreation > 5*time.Second {
				t.Errorf("LastRunTime seems too old: %v ago", timeSinceCreation)
			}

			t.Logf("Success: Created OptimizedAlloc with variant_id=%q, accelerator=%q, replicas=%d",
				result.VariantID, result.Accelerator, result.NumReplicas)
		})
	}
}
