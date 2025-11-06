# Metrics Labeling Architecture

## Overview

This document explains how the WVA controller emits Prometheus metrics using `target_name` and `target_kind` labels to identify the scale target (deployment) being managed.

## Background: VA Name vs Scale Target

The VariantAutoscaling (VA) CRD is designed to allow **VA name independence** from scale target names:

```yaml
apiVersion: llmd.ai/v1alpha1
kind: VariantAutoscaling
metadata:
  name: ms-inference-scheduling-a100  # VA resource name includes accelerator
  namespace: llm-d-inference-scheduling
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: ms-inference-scheduling-decode  # Deployment name is different!
  accelerator: A100
  # ...
```

**Why different names?**
- **VA name** (e.g., `ms-inference-scheduling-a100`) includes the accelerator type for uniqueness
- **Scale target name** (e.g., `ms-inference-scheduling-decode`) represents the workload being scaled
- This allows **multiple VAs** (for different accelerators) to target the **same deployment** in multi-variant scenarios

## Metrics Emission

When the controller emits metrics, it uses labels that identify the **scale target**, not the VA resource:

```go
// internal/metrics/metrics.go
baseLabels := prometheus.Labels{
    constants.LabelTargetName:      sanitizeLabel(va.Spec.ScaleTargetRef.Name), // ✓ Deployment name
    constants.LabelTargetKind:      sanitizeLabel(va.Spec.ScaleTargetRef.Kind), // ✓ "Deployment"
    constants.LabelNamespace:       sanitizeLabel(va.Namespace),
    constants.LabelAcceleratorType: sanitizeLabel(acceleratorType),
}
```

**Emitted metric example:**
```prometheus
wva_desired_replicas{
  target_name="ms-inference-scheduling-decode",      # Deployment name
  target_kind="Deployment",                         # Resource kind
  namespace="llm-d-inference-scheduling",
  accelerator_type="A100"
} 2
```

## Why Use Scale Target Labels?

### 1. HPA Integration

HorizontalPodAutoscaler (HPA) targets **deployments**, not VAs:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: vllm-deployment-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: ms-inference-scheduling-decode  # HPA scales the deployment
  metrics:
  - type: External
    external:
      metric:
        name: wva_desired_replicas
        selector:
          matchLabels:
            target_name: ms-inference-scheduling-decode  # Must match deployment!
            target_kind: Deployment
```

HPA queries the external metrics API for metrics **associated with the deployment** it's scaling. Using `target_name` and `target_kind` makes this relationship explicit.

### 2. Prometheus Adapter Resource Mapping

Prometheus Adapter maps Prometheus metric labels to Kubernetes resources:

```yaml
rules:
  external:
  - seriesQuery: 'wva_desired_replicas{target_name!="",exported_namespace!=""}'
    resources:
      overrides:
        exported_namespace: {resource: "namespace"}
        target_name: {resource: "deployment"}  # Maps to deployment resource
```

This configuration tells Prometheus Adapter:
- `target_name` label → deployment resource dimension
- When HPA queries for a deployment's metrics, Adapter filters by `target_name=<deployment-name>`

### 3. Semantic Clarity

The new label names are more semantically clear:
- `target_name`: The name of the resource being scaled
- `target_kind`: The kind of resource ("Deployment", "StatefulSet", etc.)
- This makes it obvious that metrics track **scale targets**, not internal VA resources

### 4. Multi-Variant Architecture Support

Multiple VAs can target the same deployment with different accelerators:

```yaml
# VA for A100 accelerator
apiVersion: llmd.ai/v1alpha1
kind: VariantAutoscaling
metadata:
  name: model-service-a100
spec:
  scaleTargetRef:
    name: model-service-decode
    kind: Deployment
  accelerator: A100
---
# VA for MI300X accelerator
apiVersion: llmd.ai/v1alpha1
kind: VariantAutoscaling
metadata:
  name: model-service-mi300x
spec:
  scaleTargetRef:
    name: model-service-decode  # Same deployment!
    kind: Deployment
  accelerator: MI300X
```

Both VAs emit metrics with:
```prometheus
wva_desired_replicas{target_name="model-service-decode", target_kind="Deployment", accelerator_type="A100"} 2
wva_desired_replicas{target_name="model-service-decode", target_kind="Deployment", accelerator_type="MI300X"} 3
```

The HPA can query for all metrics with `target_name=model-service-decode` and `accelerator_type` distinguishes between variants.

## Complete Flow

1. **Controller emits metrics**:
   ```
   target_name: va.Spec.ScaleTargetRef.Name (deployment name)
   target_kind: va.Spec.ScaleTargetRef.Kind ("Deployment")
   ```

2. **Prometheus scrapes and relabels**:
   ```prometheus
   wva_desired_replicas{
     target_name="ms-inference-scheduling-decode",
     target_kind="Deployment",
     namespace="llm-d-inference-scheduling",  # Relabeled to exported_namespace
     accelerator_type="A100"
   }
   ```

3. **Prometheus Adapter maps to resources**:
   - `target_name` → deployment resource
   - `exported_namespace` → namespace resource

4. **HPA queries external metrics**:
   ```
   GET /apis/external.metrics.k8s.io/v1beta1/namespaces/llm-d-inference-scheduling/wva_desired_replicas
   ?labelSelector=target_name=ms-inference-scheduling-decode,target_kind=Deployment
   ```

5. **Prometheus Adapter returns**:
   ```json
   {
     "items": [{
       "metricName": "wva_desired_replicas",
       "metricLabels": {
         "target_name": "ms-inference-scheduling-decode",
         "target_kind": "Deployment",
         "exported_namespace": "llm-d-inference-scheduling",
         "accelerator_type": "A100"
       },
       "value": "2"
     }]
   }
   ```

6. **HPA scales deployment** to match desired replicas

## Summary

| Concept | Value | Purpose |
|---------|-------|---------|
| **VA Name** | `ms-inference-scheduling-a100` | Uniquely identifies the VA resource (includes accelerator) |
| **Scale Target Name** | `ms-inference-scheduling-decode` | Identifies the workload being scaled |
| **`target_name` Label** | `ms-inference-scheduling-decode` | **MUST** be scale target name for HPA/Prometheus Adapter integration |
| **`target_kind` Label** | `Deployment` | Identifies the type of resource being scaled |
| **`accelerator_type` Label** | `A100` | Distinguishes between accelerator variants |

## Key Takeaway

**The `target_name` and `target_kind` metric labels MUST identify the scale target from `scaleTargetRef`, not the VA resource.** This is required for HPA to successfully query metrics via Prometheus Adapter's resource mapping and provides semantic clarity about what is being scaled.
