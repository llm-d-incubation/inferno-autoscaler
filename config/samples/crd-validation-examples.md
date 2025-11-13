# VariantAutoscaling CRD Validation Examples

This document demonstrates the CEL validation rules enforced by the VariantAutoscaling CRD.

## Min/Max Replicas Validation

The CRD enforces that `maxReplicas >= minReplicas` using CEL validation at the API server level.

### ✅ Valid Configurations

These configurations will be **ACCEPTED** by the API server:

```yaml
# Example 1: Both fields set, maxReplicas > minReplicas
spec:
  minReplicas: 2
  maxReplicas: 10
  # ... other fields
```

```yaml
# Example 2: Both fields set, maxReplicas == minReplicas (fixed size)
spec:
  minReplicas: 5
  maxReplicas: 5
  # ... other fields
```

```yaml
# Example 3: Only minReplicas set
spec:
  minReplicas: 2
  # maxReplicas not set (unlimited)
  # ... other fields
```

```yaml
# Example 4: Only maxReplicas set
spec:
  maxReplicas: 10
  # minReplicas defaults to 0
  # ... other fields
```

```yaml
# Example 5: Neither field set
spec:
  # minReplicas defaults to 0
  # maxReplicas not set (unlimited)
  # ... other fields
```

### ❌ Invalid Configurations

These configurations will be **REJECTED** by the API server with validation error:

```yaml
# Example 1: maxReplicas < minReplicas
# ERROR: maxReplicas must be greater than or equal to minReplicas
spec:
  minReplicas: 5
  maxReplicas: 2  # ❌ 2 < 5
  # ... other fields
```

```yaml
# Example 2: maxReplicas much smaller than minReplicas
# ERROR: maxReplicas must be greater than or equal to minReplicas
spec:
  minReplicas: 10
  maxReplicas: 1  # ❌ 1 < 10
  # ... other fields
```

## Validation Implementation

The validation is implemented using Kubernetes CEL (Common Expression Language):

```yaml
x-kubernetes-validations:
- message: maxReplicas must be greater than or equal to minReplicas
  rule: '!has(self.maxReplicas) || !has(self.minReplicas) || self.maxReplicas >= self.minReplicas'
```

**CEL Rule Explanation:**
- `!has(self.maxReplicas)` - Pass if maxReplicas is not set (optional field)
- `!has(self.minReplicas)` - Pass if minReplicas is not set (optional field)
- `self.maxReplicas >= self.minReplicas` - Pass if maxReplicas >= minReplicas

The rule uses OR logic, so validation passes if:
- maxReplicas is not set, OR
- minReplicas is not set, OR
- maxReplicas >= minReplicas

## Testing Validation

To test the validation, try creating a VariantAutoscaling with invalid values:

```bash
# This should fail with validation error
kubectl apply -f - <<EOF
apiVersion: llmd.ai/v1alpha1
kind: VariantAutoscaling
metadata:
  name: test-invalid
  namespace: default
spec:
  modelID: "test-model"
  variantID: "test-model-A100-1"
  accelerator: "A100"
  acceleratorCount: 1
  minReplicas: 5
  maxReplicas: 2  # Invalid: 2 < 5
  scaleTargetRef:
    kind: Deployment
    name: test-deployment
  sloClassRef:
    name: service-classes-config
    key: gold
  variantProfile:
    maxBatchSize: 8
    perfParms:
      decodeParms:
        alpha: "0.8"
        beta: "0.2"
      prefillParms:
        gamma: "0.8"
        delta: "0.2"
EOF
```

Expected error:
```
The VariantAutoscaling "test-invalid" is invalid: spec: Invalid value: "object": maxReplicas must be greater than or equal to minReplicas
```

## Related Files

- **CRD Definition**: `config/crd/bases/llmd.ai_variantautoscalings.yaml`
- **Type Markers**: `api/v1alpha1/variantautoscaling_types.go`
