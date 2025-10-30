# CRD Schema Verification in CI/CD

## Problem

E2E tests were failing with `Reason=""` and `LastUpdate=0001-01-01` despite:
- Controller code correctly setting these fields
- Unit tests passing with mock Kubernetes client
- Condition messages showing the correct Reason text

## Root Cause

**The Kubernetes API server silently drops fields that aren't defined in the deployed CRD schema.**

When `Status().Update()` is called:
1. API server validates the object against the CRD schema
2. Fields not in the schema are dropped
3. `Conditions` array is preserved (standard Kubernetes type)
4. But custom fields like `reason` and `lastUpdate` need to be in the CRD

## Solution: CI/CD Verification

### What Was Added

Two GitHub Actions workflows now include **CRD Schema Verification** steps:

1. **`.github/workflows/ci-manual-trigger.yaml`** (Manual E2E tests)
2. **`.github/workflows/ci-pr-checks.yaml`** (PR validation)

### How It Works

After E2E tests run, the verification step:

1. ✅ Checks if the VariantAutoscaling CRD exists
2. ✅ Extracts the `desiredOptimizedAlloc` schema section
3. ✅ Verifies presence of required fields:
   - `numReplicas`
   - `lastRunTime`
   - `reason` ⭐
   - `lastUpdate` ⭐

4. ✅ Reports results in GitHub Actions Summary with a table:

   | Field | Status |
   |-------|--------|
   | `numReplicas` | ✅ FOUND |
   | `lastRunTime` | ✅ FOUND |
   | `reason` | ❌ MISSING |
   | `lastUpdate` | ❌ MISSING |

5. ✅ If `reason` or `lastUpdate` are missing, the step:
   - Shows a clear error message
   - Explains why E2E tests fail
   - Provides solution steps

### What You'll See

#### ✅ When CRD is Correct

```
========== Field Check Results ==========
numReplicas: ✅ FOUND
lastRunTime: ✅ FOUND
reason: ✅ FOUND
lastUpdate: ✅ FOUND

✅ All required fields found in CRD schema
```

#### ❌ When CRD is Missing Fields

```
========== Field Check Results ==========
numReplicas: ✅ FOUND
lastRunTime: ✅ FOUND
reason: ❌ MISSING
lastUpdate: ❌ MISSING

❌ CRITICAL: CRD is missing required fields (reason and/or lastUpdate)
   This explains the E2E test failures with empty Reason and LastUpdate!

## ⚠️ ISSUE FOUND: Missing CRD Fields

**The CRD is missing `reason` and/or `lastUpdate` fields!**

This explains why E2E tests show empty `Reason=""` and `LastUpdate=0001-01-01`.
The Kubernetes API server silently drops fields not defined in the CRD schema.

### Solution:
1. Run `make manifests` to regenerate CRDs
2. Ensure E2E tests deploy the latest CRD from `config/crd/bases/`
3. Verify with: `kubectl apply -f config/crd/bases/llm-d.llm-manager.io_variantautoscalings.yaml`
```

## How to Use

### Running Manual Workflow

1. Go to GitHub Actions → "CI - Manual Trigger (All Tests)"
2. Click "Run workflow"
3. Leave "Skip E2E tests" unchecked
4. After the workflow completes, check the Summary tab

### Viewing Results

The verification results appear in two places:

1. **In the workflow log** (under "Verify CRD Schema" step)
2. **In the GitHub Actions Summary** (formatted table with emojis)

### If CRD Verification Fails

1. **Regenerate CRDs locally:**
   ```bash
   make manifests
   ```

2. **Verify the generated CRD has the fields:**
   ```bash
   grep -A 30 "desiredOptimizedAlloc:" config/crd/bases/llm-d.llm-manager.io_variantautoscalings.yaml | grep -E "(reason:|lastUpdate:)"
   ```

3. **Ensure E2E tests deploy the latest CRD**
   Check that your E2E test setup scripts run:
   ```bash
   kubectl apply -f config/crd/bases/llm-d.llm-manager.io_variantautoscalings.yaml
   ```

4. **Commit and push the updated CRD:**
   ```bash
   git add config/crd/bases/
   git commit -m "fix: regenerate CRD with reason and lastUpdate fields"
   git push
   ```

## Unit Test Proof

A unit test was created (`internal/controller/status_update_test.go`) that proves:
- ✅ Controller code is correct
- ✅ `applyFallbackAllocation` sets Reason and LastUpdate
- ✅ `updateConditionsForAllocation` preserves these fields
- ✅ `Status().Update()` with fake client works correctly

The test passes, confirming the issue is **not in the code** but in the **deployed CRD schema**.

## Key Insight: Why Condition Works But Reason Doesn't

**They come from the same source!**

```go
// Line 794: Take a snapshot of DesiredOptimizedAlloc
desiredAlloc := updateVa.Status.DesiredOptimizedAlloc

// Line 809: Use snapshot to build condition message
fmt.Sprintf("%s (%d replicas)", desiredAlloc.Reason, desiredAlloc.NumReplicas)
```

The condition message is built from a **copy** of the Reason field taken at line 794.
This snapshot is used immediately, so it has the correct text.

But when saving to the API:
- ✅ `Conditions` array is preserved (standard Kubernetes field)
- ❌ `reason` and `lastUpdate` are dropped if not in CRD schema

## References

- **Unit Test**: `internal/controller/status_update_test.go:TestStatusUpdatePreservesReasonAndLastUpdate`
- **Manual Workflow**: `.github/workflows/ci-manual-trigger.yaml`
- **PR Checks Workflow**: `.github/workflows/ci-pr-checks.yaml`
- **CRD Definition**: `api/v1alpha1/variantautoscaling_types.go:167` (Reason field)
- **CRD YAML**: `config/crd/bases/llm-d.llm-manager.io_variantautoscalings.yaml`
