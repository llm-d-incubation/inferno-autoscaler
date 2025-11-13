# OpenShift E2E Tests

This directory contains end-to-end tests for the Workload-Variant-Autoscaler on OpenShift clusters with real vLLM deployments.

## Overview

These tests validate the autoscaling behavior of the Workload-Variant-Autoscaler integrated with HPA on OpenShift using real workloads. Unlike the emulated tests in `test/e2e`, these tests run against actual vLLM deployments with the llm-d infrastructure.

## Prerequisites

### Infrastructure Requirements

The tests assume the following infrastructure is already deployed on the OpenShift cluster:

1. **Workload-Variant-Autoscaler** controller running in `workload-variant-autoscaler-system` namespace
2. **llm-d infrastructure** deployed in `llm-d-inference-scheduling` namespace:
   - Gateway (infra-inference-scheduling-inference-gateway)
   - Inference Scheduler (GAIE)
   - vLLM deployment (ms-inference-scheduling-llm-d-modelservice-decode)
3. **Prometheus** and **Thanos** for metrics collection
4. **Prometheus Adapter** for exposing external metrics to HPA
5. **HPA** configured to read `wva_desired_replicas` metric
6. **VariantAutoscaling** resource created for the vLLM deployment

### Environment Setup

1. Set `KUBECONFIG` to point to your OpenShift cluster:
   ```bash
   export KUBECONFIG=/path/to/your/kubeconfig
   ```

2. Verify you have access to the cluster:
   ```bash
   oc whoami
   oc get nodes
   ```

3. Verify the infrastructure is running:
   ```bash
   # Check WVA controller
   oc get pods -n workload-variant-autoscaler-system
   
   # Check llm-d infrastructure
   oc get pods -n llm-d-inference-scheduling
   
   # Check Prometheus Adapter
   oc get pods -n openshift-user-workload-monitoring | grep prometheus-adapter
   
   # Check HPA
   oc get hpa -n llm-d-inference-scheduling
   
   # Check VariantAutoscaling
   oc get variantautoscaling -n llm-d-inference-scheduling
   ```

## Test Structure

### Test Files

- **`e2e_suite_test.go`**: Test suite setup and infrastructure verification
- **`hpa_scale_to_zero_test.go`**: HPA scale-to-zero integration and comprehensive lifecycle tests
- **`sharegpt_scaleup_test.go`**: ShareGPT load generation and scale-up validation test

### Test Suites Overview

The OpenShift e2e test suite contains 15 tests organized into 3 test suites:

1. **HPA Scale-to-Zero Basic Integration** (4 tests)
   - Validates HPA scale-to-zero integration with existing infrastructure
   - Tests external metrics API, VA minReplicas constraints, and ConfigMap changes

2. **HPA Scale-to-Zero Comprehensive Lifecycle** (4 tests)
   - Validates complete scale-to-zero lifecycle behavior
   - Tests retention period, scale-to-zero toggle, traffic-based scaling, and VA minReplicas enforcement

3. **ShareGPT Scale-Up Test** (7 tests)
   - Validates scale-up behavior under realistic load
   - Tests external metrics API, load generation, WVA detection, HPA triggering, and deployment scaling

## Test Descriptions

### 1. HPA Scale-to-Zero Basic Integration

This test suite validates the HPA scale-to-zero integration with existing OpenShift infrastructure, including vLLM deployment, gateway, and VariantAutoscaling resources.

#### Test Cases

**1.1. External Metrics API Verification**
- **Purpose**: Verify that Prometheus Adapter exposes `wva_desired_replicas` metric to HPA
- **Steps**:
  - Query external metrics API at `/apis/external.metrics.k8s.io/v1beta1/namespaces/{namespace}/wva_desired_replicas`
  - Verify metric is accessible and contains data for the deployment
- **Expected Result**: External metrics API successfully provides `wva_desired_replicas` metric

**1.2. VA minReplicas Constraint with HPA minReplicas=0**
- **Purpose**: Verify that VA minReplicas takes precedence over HPA minReplicas=0
- **Steps**:
  - Verify HPA has minReplicas=0
  - Monitor deployment for 2 minutes
  - Verify deployment maintains >= VA minReplicas
- **Expected Result**: Deployment respects VA minReplicas despite HPA minReplicas=0

**1.3. ConfigMap enableScaleToZero Changes**
- **Purpose**: Verify controller reacts to ConfigMap changes for scale-to-zero toggle
- **Steps**:
  - Update ConfigMap to disable scale-to-zero
  - Wait for controller to pick up change
  - Restore ConfigMap to enable scale-to-zero
- **Expected Result**: Controller successfully processes ConfigMap changes without conflicts

**1.4. VA minReplicas Enforcement**
- **Purpose**: Verify VA minReplicas is enforced even with HPA minReplicas=0
- **Steps**:
  - Monitor deployment for 2 minutes with HPA minReplicas=0
  - Verify VA status maintains minReplicas constraint
- **Expected Result**: Deployment maintains >= VA minReplicas throughout monitoring period

### 2. HPA Scale-to-Zero Comprehensive Lifecycle

This test suite validates the complete lifecycle of scale-to-zero behavior including retention period enforcement, scale-to-zero toggle behavior, traffic-based scaling, and VA minReplicas enforcement.

#### Test Cases

**2.1. Scale to Zero with Retention Period**
- **Purpose**: Verify scale-to-zero behavior with retention period after bootstrap
- **Steps**:
  - Enable scale-to-zero via ConfigMap
  - Set VA minReplicas=0
  - Delete and recreate VA to trigger bootstrap
  - Monitor deployment during 4-minute retention period
  - Verify deployment scales to 0 after retention period expires
- **Expected Result**:
  - Deployment maintains >= 1 replica during 4-minute retention period after bootstrap
  - Deployment scales to 0 after retention period expires

**2.2. Maintain Replicas When Scale-to-Zero Disabled**
- **Purpose**: Verify deployment maintains replicas when scale-to-zero is disabled
- **Steps**:
  - Disable scale-to-zero via ConfigMap
  - Wait for deployment to scale to >= 1 replica
  - Monitor deployment for 5+ minutes
- **Expected Result**: Deployment maintains >= 1 replica when scale-to-zero is disabled

**2.3. Scale with Traffic When Scale-to-Zero Enabled**
- **Purpose**: Verify deployment scales with traffic and respects retention period
- **Steps**:
  - Re-enable scale-to-zero via ConfigMap
  - Create load generation job (10 req/s, 1800 prompts = 3 minutes)
  - Verify deployment scales >= 1 during traffic
  - Wait for job to complete
  - Monitor deployment during 4-minute retention period
  - Verify deployment scales to 0 after retention period
- **Expected Result**:
  - Deployment scales >= 1 during traffic
  - Deployment maintains >= 1 replica during retention period after traffic ends
  - Deployment scales to 0 after retention period expires

**2.4. Enforce VA minReplicas=2**
- **Purpose**: Verify VA minReplicas=2 is enforced
- **Steps**:
  - Update VA minReplicas to 2
  - Wait for deployment to scale to 2 replicas
  - Monitor deployment for 2 minutes
  - Restore VA minReplicas to 0
- **Expected Result**: Deployment scales to and maintains 2 replicas while VA minReplicas=2

### 3. ShareGPT Scale-Up Test

The ShareGPT Scale-Up Test validates the autoscaling behavior under realistic load using the ShareGPT dataset.

#### Test Flow

The `sharegpt_scaleup_test.go` test performs the following steps:

**3.1. Initial State Verification**
- Records initial replica count
- Records initial VariantAutoscaling optimization state
- Sets VA minReplicas=1 for stable baseline
- Verifies HPA configuration
- Verifies external metrics API accessibility

**3.2. Load Generation**
- Creates a Kubernetes Job that runs vLLM benchmark with ShareGPT dataset
- Downloads ShareGPT dataset from HuggingFace
- Generates load at 20 requests/second with 6000 prompts (5 minutes of traffic)
- Verifies the job pod is running

**3.3. Scale-Up Detection**
- Monitors VariantAutoscaling for increased replica recommendation
- Verifies WVA detects the increased load
- Expects optimization to recommend at least 2 replicas

**3.4. HPA Scale-Up Trigger**
- Monitors HPA for metric processing
- Verifies HPA reads the updated `wva_desired_replicas` metric
- Confirms HPA desires more replicas

**3.5. Deployment Scaling**
- Monitors the vLLM deployment for actual scale-up
- Verifies at least 2 replicas become ready
- Confirms deployment maintains scaled state under load

**3.6. Job Completion**
- Waits for the load generation job to complete successfully
- Verifies all requests were processed

**3.7. Cleanup**
- Removes the load generation job
- Reports final scaling results

#### Key Features

- **Sustained Load Duration**: 6000 prompts at 20 req/s = 5 minutes of sustained traffic
  - This ensures HPA has sufficient time to detect and act on the increased load
  - HPA requires ~3 minutes to evaluate metrics and trigger scaling
- **VA minReplicas Baseline**: Sets VA minReplicas=1 before testing to establish stable baseline
  - Prevents scale-down from interfering with scale-up verification
- **Realistic Load Profile**: Uses ShareGPT dataset for realistic conversational AI workload

## Running the Tests

### Run All OpenShift E2E Tests

##### Default Arguments:

```bash
CONTROLLER_NAMESPACE = workload-variant-autoscaler-system
MONITORING_NAMESPACE = openshift-user-workload-monitoring
LLMD_NAMESPACE       = llm-d-inference-scheduling
GATEWAY_NAME         = infra-inference-scheduling-inference-gateway
MODEL_ID             = unsloth/Meta-Llama-3.1-8B
DEPLOYMENT           = ms-inference-scheduling-llm-d-modelservice-decode
REQUEST_RATE         = 20
NUM_PROMPTS          = 6000
```

**Note**: `NUM_PROMPTS` was increased from 3000 to 6000 to provide 5 minutes of sustained traffic, ensuring HPA has sufficient time (3+ minutes) to detect and act on load changes.


#### Example 1: Using Default Arguments


```bash
make test-e2e-openshift
```


#### Example 2: Using Custom Arguments

```bash
make test-e2e-openshift \
LLMD_NAMESPACE=llmd-stack \
DEPLOYMENT=unsloth--00171c6f-a-3-1-8b-decode \
GATEWAY_NAME=infra-llmd-inference-gateway \
REQUEST_RATE=20 \
NUM_PROMPTS=3000
```

#### Example 3: Using GO Directly Using Default Arguments
```bash
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 30m
```

#### Example 4: Using GO Directly Using Custom Arguments
```bash
export LLMD_NAMESPACE=llmd-stack
export DEPLOYMENT=unsloth--00171c6f-a-3-1-8b-decode
export GATEWAY_NAME=infra-llmd-inference-gateway
export REQUEST_RATE=8
export NUM_PROMPTS=2000
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 30m
```

### Run Specific Test Suite

#### HPA Scale-to-Zero Basic Integration (4 tests)
```bash
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 30m -ginkgo.focus="HPA Scale-to-Zero Basic Integration"
```

#### HPA Scale-to-Zero Comprehensive Lifecycle (4 tests)
```bash
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 60m -ginkgo.focus="HPA Scale-to-Zero Comprehensive Lifecycle"
```

**Note**: This test suite requires 60-minute timeout due to retention period monitoring (4 minutes × 3 tests = 12+ minutes) plus scale-up/scale-down cycles.

#### ShareGPT Scale-Up Test (7 tests)
```bash
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 30m -ginkgo.focus="ShareGPT Scale-Up Test"
```

### Run Specific Test Case

You can run a specific test case using the full test description:

```bash
# Run only the external metrics API test from Basic Integration
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 30m -ginkgo.focus="should verify external metrics API provides wva_desired_replicas"

# Run only the retention period test from Comprehensive Lifecycle
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 60m -ginkgo.focus="should scale to zero with scale-to-zero enabled and VA minReplicas=0"

# Run only the scale-up detection test from ShareGPT
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 30m -ginkgo.focus="should detect increased load and recommend scale-up"
```

### Run with Custom Timeouts

```bash
# Increase timeout for long-running tests
go test ./test/e2e-openshift/... -v -ginkgo.v -timeout 90m
```

## Test Parameters

### Environment Variables

The test suite uses the following environment variables (configured in `e2e_suite_test.go`):

| Variable | Default Value | Description |
|----------|---------------|-------------|
| `CONTROLLER_NAMESPACE` | `workload-variant-autoscaler-system` | Namespace where WVA controller is deployed |
| `MONITORING_NAMESPACE` | `openshift-user-workload-monitoring` | Namespace where Prometheus Adapter is deployed |
| `LLMD_NAMESPACE` | `llm-d-inference-scheduling` | Namespace where llm-d infrastructure is deployed |
| `GATEWAY_NAME` | `infra-inference-scheduling-inference-gateway-istio` | Name of the inference gateway service |
| `MODEL_ID` | `unsloth/Meta-Llama-3.1-8B` | Model ID for load generation |
| `DEPLOYMENT` | `ms-inference-scheduling-llm-d-modelservice-decode` | Name of the vLLM deployment to test |
| `REQUEST_RATE` | `20` | Request rate for load generation (requests/second) |
| `NUM_PROMPTS` | `6000` | Number of prompts for load generation (6000 @ 20 req/s = 5 minutes) |

### Load Generation Parameters

You can modify the load generation parameters for ShareGPT tests:

```go
job := createShareGPTJob(jobName, llmDNamespace, 20, 6000)
//                                               ^^  ^^^^
//                                               |    |
//                                               |    +--- Number of prompts
//                                               +-------- Request rate (req/s)
```

### Recommended Load Profiles

- **Light load** (should stay at 1 replica): `requestRate: 8, numPrompts: 2400` (5 minutes)
- **Medium load** (should scale to 2 replicas): `requestRate: 20, numPrompts: 6000` (5 minutes) - **DEFAULT**
- **Heavy load** (may scale to 3+ replicas): `requestRate: 40, numPrompts: 12000` (5 minutes)

**Important**: Ensure load duration is at least 5 minutes to allow HPA sufficient time (~3 minutes) to detect and act on metric changes.

## Expected Results

### Full Test Suite (15 tests)

A successful full test run should show:

```
Ran 15 of 15 Specs in [duration]
SUCCESS! -- 15 Passed | 0 Failed | 0 Pending | 0 Skipped

Test Suite Summary:
- HPA Scale-to-Zero Basic Integration: 4 Passed
- HPA Scale-to-Zero Comprehensive Lifecycle: 4 Passed
- ShareGPT Scale-Up Test: 7 Passed
```

### Individual Test Suite Results

#### HPA Scale-to-Zero Basic Integration (4 tests)

```
✓ should verify external metrics API provides wva_desired_replicas
✓ should maintain VA minReplicas constraint with HPA minReplicas=0
✓ should react to ConfigMap enableScaleToZero changes
✓ should enforce VA minReplicas even with HPA minReplicas=0
```

**Expected behavior**:
- External metrics API accessible and providing `wva_desired_replicas` metric
- VA minReplicas takes precedence over HPA minReplicas=0
- Controller successfully processes ConfigMap changes without HTTP 409 conflicts
- Deployment maintains >= VA minReplicas throughout monitoring periods

#### HPA Scale-to-Zero Comprehensive Lifecycle (4 tests)

```
✓ should scale to zero with scale-to-zero enabled and VA minReplicas=0
✓ should maintain replicas when scale-to-zero is disabled
✓ should scale with traffic when scale-to-zero is enabled
✓ should enforce VA minReplicas=2
```

**Expected behavior**:
- After bootstrap/traffic: Deployment maintains >= 1 replica during 4-minute retention period
- After retention period expires: Deployment scales to 0
- When scale-to-zero disabled: Deployment maintains >= 1 replica indefinitely
- When VA minReplicas=2: Deployment scales to and maintains 2 replicas

#### ShareGPT Scale-Up Test (7 tests)

```
Infrastructure verification complete
✓ should verify external metrics API is accessible
✓ should create and run ShareGPT load generation job
✓ should detect increased load and recommend scale-up
✓ should trigger HPA to scale up the deployment
✓ should scale deployment to match recommended replicas
✓ should maintain scaled state while load is active
✓ should complete the load generation job successfully
```

**Expected behavior**:
- Load generation job runs successfully (6000 prompts @ 20 req/s = 5 minutes)
- WVA detects load and recommends 2+ replicas (up from 1)
- HPA reads updated `wva_desired_replicas` metric (2.0)
- Deployment scales to 2+ replicas within 10 minutes
- Deployment maintains scaled state during 5-minute load period
- Test completes successfully: scaled from 1 to 2+ replicas

## Troubleshooting

### Test Fails: Infrastructure Not Ready

If the BeforeSuite fails, verify all infrastructure components are deployed:

```bash
# Check all namespaces
oc get pods -n workload-variant-autoscaler-system
oc get pods -n llm-d-inference-scheduling
oc get pods -n openshift-user-workload-monitoring | grep prometheus-adapter

# Check custom resources
oc get variantautoscaling -n llm-d-inference-scheduling
oc get hpa -n llm-d-inference-scheduling
```

### Test Fails: External Metrics Not Available

```bash
# Check Prometheus Adapter logs
oc logs -n openshift-user-workload-monitoring deployment/prometheus-adapter

# Query external metrics API directly
kubectl get --raw "/apis/external.metrics.k8s.io/v1beta1/namespaces/llm-d-inference-scheduling/wva_desired_replicas" | jq
```

### Test Fails: No Scale-Up Detected

```bash
# Check WVA controller logs
oc logs -n workload-variant-autoscaler-system deployment/workload-variant-autoscaler-controller-manager | grep inference-scheduling

# Check VariantAutoscaling status
oc get variantautoscaling -n llm-d-inference-scheduling -o yaml

# Check HPA status
oc describe hpa vllm-deployment-hpa -n llm-d-inference-scheduling
```

### Job Fails to Complete

```bash
# Check job status
oc get job vllm-bench-sharegpt-e2e -n llm-d-inference-scheduling

# Check job pod logs
oc logs -n llm-d-inference-scheduling job/vllm-bench-sharegpt-e2e

# Check if gateway is accessible
oc get svc -n llm-d-inference-scheduling | grep gateway
```

## Test Timeouts

The test suites use the following timeouts:

### Infrastructure Verification (BeforeSuite)
- Controller pod verification: 2 minutes
- vLLM deployment verification: 7 minutes (includes large model loading time)
- Prometheus Adapter verification: 2 minutes

### HPA Scale-to-Zero Basic Integration
- External metrics API query: 2 minutes
- VA minReplicas constraint monitoring: 2 minutes
- ConfigMap update processing: 15 seconds
- Overall suite timeout: **30 minutes**

### HPA Scale-to-Zero Comprehensive Lifecycle
- Retention period monitoring: 4 minutes (per test)
- Scale-to-zero detection: 3 minutes
- Scale-up detection: 10 minutes
- Load job completion: 10 minutes
- Overall suite timeout: **60 minutes** (includes multiple retention periods)

### ShareGPT Scale-Up Test
- Job pod startup: 3 minutes
- Scale-up detection: 5 minutes
- HPA trigger: 3 minutes
- Deployment scaling: 10 minutes
- Job completion: 10 minutes
- Overall suite timeout: **30 minutes**

### Recommended Timeouts by Test Run

- **Full suite** (15 tests): `timeout=90m`
- **HPA Scale-to-Zero Basic Integration only**: `timeout=30m`
- **HPA Scale-to-Zero Comprehensive Lifecycle only**: `timeout=60m`
- **ShareGPT Scale-Up Test only**: `timeout=30m`

## Contributing

When adding new tests:

1. Follow the Ginkgo/Gomega testing patterns
2. Use descriptive test names with `It("should ...")` format
3. Add appropriate timeouts with `Eventually` and `Consistently`
4. Clean up resources in `AfterAll` blocks
5. Log progress with `fmt.Fprintf(GinkgoWriter, ...)`
6. Document expected behavior and test parameters

