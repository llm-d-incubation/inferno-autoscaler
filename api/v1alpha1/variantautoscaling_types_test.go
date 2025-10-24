package v1alpha1

import (
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// helper: build a valid VariantAutoscaling object
// TODO: move to utils??
func makeValidVA() *VariantAutoscaling {
	return &VariantAutoscaling{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "inferencev1alpha1",
			Kind:       "VariantAutoscaling",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "va-sample",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/name": "workload-variant-autoscaler",
			},
		},
		Spec: VariantAutoscalingSpec{
			ModelID:          "model-123",
			VariantID:        "model-123-A100-1",
			Accelerator:      "A100",
			AcceleratorCount: 1,
			SLOClassRef: ConfigMapKeyRef{
				Name: "slo-config",
				Key:  "gold",
			},
			VariantProfile: VariantProfile{
				PerfParms: PerfParms{
					DecodeParms:  map[string]string{"alpha": "0.8", "beta": "0.2"},
					PrefillParms: map[string]string{"gamma": "0.8", "delta": "0.2"},
				},
				MaxBatchSize: 8,
			},
		},
		Status: VariantAutoscalingStatus{
			CurrentAlloc: Allocation{
				// Note: In single-variant architecture, variantID, accelerator, maxBatch, and variantCost
				// are in the parent VA spec, not in Allocation status
				NumReplicas: 1,
			},
			DesiredOptimizedAlloc: OptimizedAlloc{
				LastRunTime: metav1.NewTime(time.Unix(1730000000, 0).UTC()),
				// Note: In single-variant architecture, variantID and accelerator are in the parent VA spec
				NumReplicas: 2,
			},
			Actuation: ActuationStatus{
				Applied: true,
			},
		},
	}
}

func TestSchemeRegistration(t *testing.T) {
	s := runtime.NewScheme()
	if err := SchemeBuilder.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}

	kinds, _, err := s.ObjectKinds(&VariantAutoscaling{})
	if err != nil {
		t.Fatalf("ObjectKinds for VariantAutoscaling failed: %v", err)
	}
	if len(kinds) == 0 {
		t.Fatalf("no GVK registered for VariantAutoscaling")
	}

	listKinds, _, err := s.ObjectKinds(&VariantAutoscalingList{})
	if err != nil {
		t.Fatalf("ObjectKinds for VariantAutoscalingList failed: %v", err)
	}
	if len(listKinds) == 0 {
		t.Fatalf("no GVK registered for VariantAutoscalingList")
	}
}

func TestDeepCopyIndependence(t *testing.T) {
	orig := makeValidVA()
	cp := orig.DeepCopy()

	cp.Spec.ModelID = "model-456"
	cp.Spec.SLOClassRef.Name = "slo-config-2"
	cp.Spec.VariantProfile.MaxBatchSize = 16

	if orig.Spec.ModelID == cp.Spec.ModelID {
		t.Errorf("DeepCopy did not create independent copy for Spec.ModelID")
	}
	if orig.Spec.SLOClassRef.Name == cp.Spec.SLOClassRef.Name {
		t.Errorf("DeepCopy did not create independent copy for Spec.SLOClassRef.Name")
	}
	if orig.Spec.VariantProfile.MaxBatchSize == cp.Spec.VariantProfile.MaxBatchSize {
		t.Errorf("DeepCopy did not create independent copy for VariantProfile.MaxBatchSize")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	orig := makeValidVA()

	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var back VariantAutoscaling
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Note: In single-variant architecture, VariantID is in spec, not in OptimizedAlloc
	// Check NumReplicas instead to ensure unmarshal worked
	if back.Status.DesiredOptimizedAlloc.NumReplicas != orig.Status.DesiredOptimizedAlloc.NumReplicas {
		t.Fatalf("DesiredOptimizedAlloc.NumReplicas mismatch after unmarshal")
	}

	ot := orig.Status.DesiredOptimizedAlloc.LastRunTime.Time
	bt := back.Status.DesiredOptimizedAlloc.LastRunTime.Time
	if !ot.Equal(bt) {
		t.Fatalf("LastRunTime mismatch by instant: orig=%v back=%v", ot, bt)
	}

	back.Status.DesiredOptimizedAlloc.LastRunTime = orig.Status.DesiredOptimizedAlloc.LastRunTime

	if !reflect.DeepEqual(orig, &back) {
		t.Errorf("round-trip mismatch:\norig=%#v\nback=%#v", orig, &back)
	}
}

func TestListDeepCopyAndItemsIndependence(t *testing.T) {
	va1 := makeValidVA()
	va2 := makeValidVA()
	va2.Name = "va-other"
	list := &VariantAutoscalingList{
		Items: []VariantAutoscaling{*va1, *va2},
	}

	cp := list.DeepCopy()
	if len(cp.Items) != 2 {
		t.Fatalf("DeepCopy list items count mismatch: got %d", len(cp.Items))
	}
	// mutate copy
	cp.Items[0].Spec.ModelID = "changed"

	if list.Items[0].Spec.ModelID == cp.Items[0].Spec.ModelID {
		t.Errorf("DeepCopy did not isolate list items")
	}
}

func TestStatusOmitEmpty(t *testing.T) {
	empty := &VariantAutoscaling{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "va-empty-status",
			Namespace: "default",
		},
		Spec: VariantAutoscalingSpec{
			ModelID:          "m",
			VariantID:        "m-A100-1",
			Accelerator:      "A100",
			AcceleratorCount: 1,
			SLOClassRef: ConfigMapKeyRef{
				Name: "slo",
				Key:  "bronze",
			},
			VariantProfile: VariantProfile{
				PerfParms: PerfParms{
					DecodeParms:  map[string]string{"alpha": "1", "beta": "1"},
					PrefillParms: map[string]string{"gamma": "1", "delta": "1"},
				},
				MaxBatchSize: 1,
			},
		},
	}

	b, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if !jsonContainsKey(b, "status") {
		t.Fatalf("expected status to be present for non-pointer struct with omitempty; got: %s", string(b))
	}

	// Optional: sanity-check a couple of zero values inside status
	var probe struct {
		Status struct {
			CurrentAllocs []struct {
				Accelerator string `json:"accelerator"`
			} `json:"currentAllocs"`
			DesiredOptimizedAllocs []struct {
				LastRunTime *string `json:"lastRunTime"`
				NumReplicas int     `json:"numReplicas"`
			} `json:"desiredOptimizedAllocs"`
			Actuation struct {
				Applied bool `json:"applied"`
			} `json:"actuation"`
		} `json:"status"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		t.Fatalf("unmarshal probe failed: %v", err)
	}
	if probe.Status.Actuation.Applied != false {
		t.Errorf("unexpected non-zero defaults in status: %+v", probe.Status)
	}
	empty.Status.Actuation.Applied = true
	b2, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !jsonContainsKey(b2, "status") {
		t.Errorf("status should be present when non-zero, but json did not contain it: %s", string(b2))
	}
}

func TestOptimizedAllocLastRunTimeJSON(t *testing.T) {
	va := makeValidVA()
	// ensure LastRunTime survives marshal/unmarshal with RFC3339 format used by metav1.Time
	raw, err := json.Marshal(va)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	type optimizedAllocs struct {
		Status struct {
			DesiredOptimizedAlloc struct {
				LastRunTime string `json:"lastRunTime"`
			} `json:"desiredOptimizedAlloc"`
		} `json:"status"`
	}
	var probe optimizedAllocs
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal probe failed: %v", err)
	}
	if probe.Status.DesiredOptimizedAlloc.LastRunTime == "" {
		t.Errorf("expected lastRunTime to be serialized, got empty")
	}
}

func jsonContainsKey(b []byte, key string) bool {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

// TestVariantCostOmitEmpty verifies that variantCost can be omitted in JSON
// and will use the default value "10" set by the CRD webhook
func TestVariantCostOmitEmpty(t *testing.T) {
	// Test 1: When variantCost is explicitly set, it should be in JSON
	vaWithCost := makeValidVA()
	vaWithCost.Spec.VariantCost = "15.5"

	b, err := json.Marshal(vaWithCost)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var probe1 struct {
		Spec struct {
			VariantCost string `json:"variantCost"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(b, &probe1); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if probe1.Spec.VariantCost != "15.5" {
		t.Errorf("expected variantCost=15.5, got %s", probe1.Spec.VariantCost)
	}

	// Test 2: When variantCost is empty, it should be omitted from JSON (omitempty)
	vaWithoutCost := makeValidVA()
	vaWithoutCost.Spec.VariantCost = ""

	b2, err := json.Marshal(vaWithoutCost)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Parse to check if variantCost is absent
	var rawSpec map[string]interface{}
	var probe2 struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(b2, &probe2); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if err := json.Unmarshal(probe2.Spec, &rawSpec); err != nil {
		t.Fatalf("unmarshal spec failed: %v", err)
	}

	// variantCost should be omitted when empty due to omitempty tag
	if _, exists := rawSpec["variantCost"]; exists {
		t.Errorf("expected variantCost to be omitted when empty, but it was present")
	}
}

// TestVariantCostDefaultValue verifies the default value behavior
// Note: The actual default value "10" is set by Kubernetes API server via
// the +kubebuilder:default="10" marker, not by Go struct defaults
func TestVariantCostDefaultValue(t *testing.T) {
	tests := []struct {
		name         string
		variantCost  string
		expectInJSON bool
		expectedVal  string
	}{
		{
			name:         "explicit cost set",
			variantCost:  "20.5",
			expectInJSON: true,
			expectedVal:  "20.5",
		},
		{
			name:         "empty cost - should be omitted",
			variantCost:  "",
			expectInJSON: false,
			expectedVal:  "",
		},
		{
			name:         "default value explicitly set",
			variantCost:  "10",
			expectInJSON: true,
			expectedVal:  "10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			va := makeValidVA()
			va.Spec.VariantCost = tt.variantCost

			b, err := json.Marshal(va)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			var probe struct {
				Spec struct {
					VariantCost string `json:"variantCost,omitempty"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(b, &probe); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}

			if tt.expectInJSON {
				if probe.Spec.VariantCost != tt.expectedVal {
					t.Errorf("expected variantCost=%s, got %s", tt.expectedVal, probe.Spec.VariantCost)
				}
			} else {
				// Check raw JSON to ensure field is truly omitted
				var rawMap map[string]interface{}
				if err := json.Unmarshal(b, &rawMap); err != nil {
					t.Fatalf("unmarshal to map failed: %v", err)
				}
				specMap := rawMap["spec"].(map[string]interface{})
				if _, exists := specMap["variantCost"]; exists {
					t.Errorf("expected variantCost to be omitted, but it exists in JSON")
				}
			}
		})
	}
}

// TestVariantIDPatternValidation tests the VariantID pattern validation
// Pattern: ^.+-[A-Za-z0-9_-]+-[1-9][0-9]*$
// Format: {modelID}-{accelerator}-{count}
func TestVariantIDPatternValidation(t *testing.T) {
	tests := []struct {
		name        string
		variantID   string
		shouldMatch bool
		reason      string
	}{
		// Valid cases - standard format
		{
			name:        "simple alphanumeric",
			variantID:   "llama-A100-1",
			shouldMatch: true,
			reason:      "basic format with alphanumeric accelerator",
		},
		{
			name:        "model with slash",
			variantID:   "meta/llama-3.1-8b-A100-4",
			shouldMatch: true,
			reason:      "HuggingFace style model name with slash",
		},
		{
			name:        "model with lowercase",
			variantID:   "llama-8b-h100-1",
			shouldMatch: true,
			reason:      "lowercase accelerator name",
		},

		// Valid cases - complex accelerator names (NEW with flexible pattern)
		{
			name:        "accelerator with hyphen",
			variantID:   "meta/llama-8b-H100-SXM-4",
			shouldMatch: true,
			reason:      "accelerator name with hyphen (H100-SXM)",
		},
		{
			name:        "accelerator with underscore",
			variantID:   "model-A100_80GB-1",
			shouldMatch: true,
			reason:      "accelerator name with underscore (A100_80GB)",
		},
		{
			name:        "complex accelerator",
			variantID:   "model-H100-SXM4-80GB-2",
			shouldMatch: true,
			reason:      "complex accelerator with multiple hyphens",
		},
		{
			name:        "accelerator with mixed separators",
			variantID:   "model-A100_PCIE-SXM4-1",
			shouldMatch: true,
			reason:      "accelerator with both underscore and hyphen",
		},

		// Valid cases - various model names
		{
			name:        "multiple slashes in model",
			variantID:   "org/team/model-GPU-1",
			shouldMatch: true,
			reason:      "model name with multiple organization levels",
		},
		{
			name:        "model with dots",
			variantID:   "gpt-4.0-turbo-H100-2",
			shouldMatch: true,
			reason:      "model name with version dots",
		},
		{
			name:        "model with underscores",
			variantID:   "model_name-A100-1",
			shouldMatch: true,
			reason:      "model name with underscores",
		},

		// Invalid cases - count must start with 1-9
		{
			name:        "count starting with zero",
			variantID:   "model-A100-0",
			shouldMatch: false,
			reason:      "replica count cannot start with 0",
		},
		{
			name:        "count is zero",
			variantID:   "model-GPU-00",
			shouldMatch: false,
			reason:      "replica count of 00 not allowed",
		},

		// Invalid cases - missing components
		{
			name:        "missing count",
			variantID:   "model-A100",
			shouldMatch: false,
			reason:      "missing replica count",
		},
		{
			name:        "missing accelerator",
			variantID:   "model-1",
			shouldMatch: false,
			reason:      "missing accelerator name",
		},
		{
			name:        "only model name",
			variantID:   "model",
			shouldMatch: false,
			reason:      "missing accelerator and count",
		},

		// Invalid cases - empty or malformed
		{
			name:        "empty string",
			variantID:   "",
			shouldMatch: false,
			reason:      "empty variantID",
		},
		{
			name:        "only dashes",
			variantID:   "---",
			shouldMatch: false,
			reason:      "no valid components",
		},
	}

	// The actual pattern from the CRD validation
	pattern := `^.+-[A-Za-z0-9_-]+-[1-9][0-9]*$`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test using Go regex matching (simulating Kubernetes validation)
			matched, err := regexp.MatchString(pattern, tt.variantID)
			if err != nil {
				t.Fatalf("regex error: %v", err)
			}

			if matched != tt.shouldMatch {
				if tt.shouldMatch {
					t.Errorf("Expected '%s' to MATCH pattern, but it didn't. Reason: %s",
						tt.variantID, tt.reason)
				} else {
					t.Errorf("Expected '%s' to NOT MATCH pattern, but it did. Reason: %s",
						tt.variantID, tt.reason)
				}
			}
		})
	}
}
