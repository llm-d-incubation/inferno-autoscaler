# Custom Metrics Documentation

The WVA exposes a focused set of custom metrics that provide insights into the autoscaling behavior and optimization performance. These metrics are exposed via Prometheus and can be used for monitoring, alerting, and dashboard creation.

## Metrics Overview

All custom metrics are prefixed with `wva_` and include labels for `target_name`, `namespace`, and other relevant dimensions to enable detailed analysis and filtering.

**Important**: The `target_name` label contains the **deployment name** from the VariantAutoscaling's `scaleTargetRef.Name` field, **not** the VariantAutoscaling resource name. This is required for HPA integration via Prometheus Adapter. See [Metrics Labeling Architecture](../architecture/metrics-labeling.md) for details.

## Optimization Metrics

*No optimization metrics are currently exposed. Optimization timing is logged at DEBUG level.*

## Replica Management Metrics

### `wva_current_replicas`
- **Type**: Gauge
- **Description**: Current number of replicas for each scale target
- **Labels**:
  - `target_name`: Deployment name from `scaleTargetRef.Name`
  - `target_kind`: Resource kind from `scaleTargetRef.Kind` (e.g., "Deployment")
  - `namespace`: Kubernetes namespace
  - `accelerator_type`: Type of accelerator being used
- **Use Case**: Monitor current number of replicas per scale target

### `wva_desired_replicas`
- **Type**: Gauge
- **Description**: Desired number of replicas for each scale target (used by HPA for external metrics)
- **Labels**:
  - `target_name`: Deployment name from `scaleTargetRef.Name`
  - `target_kind`: Resource kind from `scaleTargetRef.Kind` (e.g., "Deployment")
  - `namespace`: Kubernetes namespace
  - `accelerator_type`: Type of accelerator being used
- **Use Case**: Expose the desired optimized number of replicas per scale target; consumed by HPA via Prometheus Adapter

### `wva_desired_ratio`
- **Type**: Gauge
- **Description**: Ratio of the desired number of replicas and the current number of replicas for each scale target
- **Labels**:
  - `target_name`: Deployment name from `scaleTargetRef.Name`
  - `target_kind`: Resource kind from `scaleTargetRef.Kind` (e.g., "Deployment")
  - `namespace`: Kubernetes namespace
  - `accelerator_type`: Type of accelerator being used
- **Use Case**: Compare the desired and current number of replicas per scale target, for scaling purposes

### `wva_replica_scaling_total`
- **Type**: Counter
- **Description**: Total number of replica scaling operations
- **Labels**:
  - `target_name`: Deployment name from `scaleTargetRef.Name`
  - `target_kind`: Resource kind from `scaleTargetRef.Kind` (e.g., "Deployment")
  - `namespace`: Kubernetes namespace
  - `direction`: Direction of scaling (up, down)
  - `reason`: Reason for scaling
  - `accelerator_type`: Type of accelerator being used
- **Use Case**: Track scaling frequency and reasons

## Configuration

### Metrics Endpoint
The metrics are exposed at the `/metrics` endpoint on port 8080 (HTTP).

### ServiceMonitor Configuration
```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: workload-variant-autoscaler
  namespace: workload-variant-autoscaler-system
  labels:
    release: kube-prometheus-stack
spec:
  selector:
    matchLabels:
      control-plane: controller-manager
  endpoints:
  - port: http
    scheme: http
    interval: 30s
    path: /metrics
```

## Example Queries

### Basic Queries
```promql
# Current replicas by variant
wva_current_replicas

# Scaling frequency
rate(wva_replica_scaling_total[5m])

# Desired replicas by variant
wva_desired_replicas
```

### Advanced Queries
```promql
# Scaling frequency by direction
rate(wva_replica_scaling_total{direction="scale_up"}[5m])

# Replica count mismatch
abs(wva_desired_replicas - wva_current_replicas)

# Scaling frequency by reason
rate(wva_replica_scaling_total[5m]) by (reason)
```