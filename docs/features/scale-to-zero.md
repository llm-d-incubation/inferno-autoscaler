# Scale-to-Zero Feature

## Overview

The scale-to-zero feature allows model deployments to automatically scale down to zero replicas when not in use, reducing infrastructure costs while maintaining the ability to quickly scale up when requests arrive. This feature is particularly valuable for models with intermittent or unpredictable traffic patterns.

## Key Features

- **Per-Model Configuration**: Scale-to-zero settings are configured per model (e.g., `meta/llama-3.1-8b`), allowing all variants of the same model across different accelerators to share the same behavior.
- **Namespace-Aware**: Support for namespace-specific configurations, allowing the same model in different namespaces to have different scale-to-zero settings.
- **Configurable Retention Period**: Control how long to wait after the last request before scaling to zero (default: 10 minutes).
- **Global Fallback**: Models not explicitly configured in the ConfigMap fall back to a global environment variable setting.
- **Zero-Downtime Scaling**: When scale-to-zero is disabled, models maintain a minimum of 1 replica to ensure availability.

## Architecture

### Configuration Hierarchy

The scale-to-zero feature uses a five-tier configuration hierarchy:

1. **Namespace-Specific Model Entry** (Highest Priority): Model settings for a specific namespace (namespace + modelID match)
2. **Global Model Entry**: Model settings that apply to all namespaces (modelID match, no namespace specified)
3. **Global Defaults in ConfigMap**: Default settings for all models using the special `__defaults__` key
4. **Global Environment Variable**: The `WVA_SCALE_TO_ZERO` environment variable
5. **System Default** (Fallback): Scale-to-zero disabled with 10-minute retention period

### Configuration Location

Scale-to-zero configuration is managed via a Kubernetes ConfigMap named `model-scale-to-zero-config` in the `workload-variant-autoscaler-system` namespace. This follows the project's pattern of storing system-level configuration in the controller's namespace.

### How It Works

1. **Reconciliation Loop**: The controller reads the scale-to-zero ConfigMap once per reconciliation cycle
2. **Configuration Parsing**: The ConfigMap data is parsed into a `ScaleToZeroConfigData` structure
3. **Model Lookup**: For each VariantAutoscaling resource, the controller looks up the corresponding model's configuration
4. **Replica Calculation**: Based on the configuration, the controller sets the minimum number of replicas:
   - **Scale-to-zero enabled**: Minimum 0 replicas
   - **Scale-to-zero disabled**: Minimum 1 replica

## Configuration

### ConfigMap Structure

Create a ConfigMap in the `workload-variant-autoscaler-system` namespace using the **prefixed-key format with YAML values**:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: model-scale-to-zero-config
  namespace: workload-variant-autoscaler-system
data:
  # Global defaults for all models (optional, special key)
  __defaults__: |
    enableScaleToZero: true
    retentionPeriod: "15m"

  # Per-model configurations
  # Key format: "model.<safe-key>" where safe-key uses dots instead of slashes
  # Value: YAML configuration that includes the original modelID
  # This format allows independent editing of each model's configuration

  # Example 1: Namespace-specific configuration (production)
  # Same model in production namespace - scale-to-zero DISABLED
  model.prod.meta.llama-3.1-8b: |
    namespace: "production"
    modelID: "meta/llama-3.1-8b"
    enableScaleToZero: false

  # Example 2: Namespace-specific configuration (development)
  # Same model in development namespace - scale-to-zero ENABLED with 5min retention
  model.dev.meta.llama-3.1-8b: |
    namespace: "development"
    modelID: "meta/llama-3.1-8b"
    enableScaleToZero: true
    retentionPeriod: "5m"

  # Example 3: Global model configuration (no namespace)
  # Applies to all namespaces unless overridden by namespace-specific entry
  model.meta.llama-3.1-70b: |
    modelID: "meta/llama-3.1-70b"
    enableScaleToZero: true
    retentionPeriod: "15m"

  # Example 4: Model ID with colon (vllm prefix) in specific namespace
  model.test.vllm.meta.llama-3.1-8b: |
    namespace: "test"
    modelID: "vllm:meta/llama-3.1-8b"
    enableScaleToZero: true
    retentionPeriod: "1m"

  # Example 5: Use global defaults by not specifying this model
  # "meta/llama-2-7b" - will inherit enableScaleToZero=true, retentionPeriod="15m"
```

**Key Format Benefits**:
- ✅ **Independently editable** - Use `kubectl patch` to update single models
- ✅ **Better Git diffs** - Only changed models show in version control
- ✅ **No collision risk** - Original modelID preserved in YAML value
- ✅ **Human-readable** - Keys like `model.meta.llama-3.1-8b` are recognizable

### Configuration Fields

#### `namespace` (string, optional)
- Kubernetes namespace this configuration applies to
- If specified, configuration only applies to models in that namespace
- If omitted (empty), configuration applies globally to all namespaces
- Namespace-specific configurations take priority over global configurations

#### `modelID` (string, required for per-model entries)
- Original model identifier with any characters (/, :, @, etc.)
- Must match the `modelID` in VariantAutoscaling spec
- Not required for `__defaults__` entry

#### `enableScaleToZero` (boolean, optional)
- **true**: Allows the model to scale down to 0 replicas when idle
- **false**: Maintains a minimum of 1 replica at all times
- If omitted, inherits from `__defaults__` or environment variable

#### `retentionPeriod` (string, optional)
- Duration to wait after the last request before scaling to zero
- Format: Go duration string (e.g., "5m", "1h", "30s")
- Default: "10m" (10 minutes)
- If omitted, inherits from `__defaults__` or system default
- Examples:
  - "30s" - 30 seconds
  - "5m" - 5 minutes
  - "1h" - 1 hour
  - "1h30m" - 1 hour and 30 minutes

### Global Defaults in ConfigMap

You can set default scale-to-zero behavior for all models using the special `__defaults__` key in the ConfigMap. This is **recommended over using the environment variable** as it keeps all configuration in one place and allows setting both enable/disable and retention period defaults.

```yaml
data:
  __defaults__: |
    enableScaleToZero: true
    retentionPeriod: "20m"
```

Models not explicitly configured will inherit these defaults. Per-model configurations always override global defaults.

**Benefits of Global Defaults:**
- Centralized configuration in ConfigMap
- Can set default retention period (not possible with environment variable)
- Easier to update without restarting controller
- Higher priority than environment variable

### Partial Overrides

**Important**: Individual model configurations support partial overrides. A model entry can specify only one field and inherit the other from global defaults.

```yaml
data:
  __defaults__: |
    enableScaleToZero: true
    retentionPeriod: "15m"

  # Override ONLY retention period - inherits enableScaleToZero=true from defaults
  model.meta.llama-3.1-8b: |
    modelID: "meta/llama-3.1-8b"
    retentionPeriod: "5m"

  # Override ONLY enableScaleToZero - inherits retentionPeriod="15m" from defaults
  model.meta.llama-3.1-70b: |
    modelID: "meta/llama-3.1-70b"
    enableScaleToZero: false
```

**Behavior**:
- If `enableScaleToZero` is omitted → inherits from global defaults
- If `retentionPeriod` is omitted → inherits from global defaults
- If both are omitted → inherits everything from global defaults
- Explicitly set values always override defaults

This allows maximum flexibility with minimal configuration - only specify what differs from your organization's defaults.

### Global Environment Variable

Set the `WVA_SCALE_TO_ZERO` environment variable for the controller to enable or disable scale-to-zero globally for models not listed in the ConfigMap and without global defaults:

```yaml
env:
  - name: WVA_SCALE_TO_ZERO
    value: "true"  # or "false"
```

**Note**: Global defaults in ConfigMap take priority over this environment variable. Use the ConfigMap `__defaults__` key for more flexible configuration.

## Usage Examples

### Example 1: Multiple Variants, Same Model

Two variants of the same model with different accelerators share the same scale-to-zero configuration:

```yaml
---
apiVersion: llmd.ai/v1alpha1
kind: VariantAutoscaling
metadata:
  name: llama-8b-a100
  namespace: default
spec:
  scaleTargetRef:
    kind: Deployment
    name: llama-8b-deployment
  modelID: "meta/llama-3.1-8b"
  # ... other config ...

---
apiVersion: llmd.ai/v1alpha1
kind: VariantAutoscaling
metadata:
  name: llama-8b-l40s
  namespace: default
spec:
  scaleTargetRef:
    kind: Deployment
    name: llama-8b-deployment  # Can target same deployment (conflict resolution applies)
  modelID: "meta/llama-3.1-8b"  # Same modelID
  # ... other config ...
```

Both variants will use the configuration for `meta/llama-3.1-8b` from the ConfigMap.

### Example 2: Mixed Configuration

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: model-scale-to-zero-config
  namespace: workload-variant-autoscaler-system
data:
  # Development models: aggressive scale-to-zero (5 minutes)
  model.meta.llama-3.1-8b: |
    modelID: "meta/llama-3.1-8b"
    enableScaleToZero: true
    retentionPeriod: "5m"

  # Production models: conservative scale-to-zero (30 minutes)
  model.meta.llama-3.1-70b: |
    modelID: "meta/llama-3.1-70b"
    enableScaleToZero: true
    retentionPeriod: "30m"

  # Critical models: no scale-to-zero (always available)
  model.meta.llama-3.1-405b: |
    modelID: "meta/llama-3.1-405b"
    enableScaleToZero: false
```

### Example 3: Using Global Defaults in ConfigMap

Set defaults for all models while allowing specific models to override:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: model-scale-to-zero-config
  namespace: workload-variant-autoscaler-system
data:
  # Set defaults for all models
  __defaults__: |
    enableScaleToZero: true
    retentionPeriod: "20m"

  # Override defaults for specific models
  model.meta.llama-3.1-8b: |
    modelID: "meta/llama-3.1-8b"
    retentionPeriod: "5m"  # Shorter retention for dev model

  model.meta.llama-3.1-405b: |
    modelID: "meta/llama-3.1-405b"
    enableScaleToZero: false  # Critical model, always available
```

In this configuration:
- **meta/llama-3.1-8b**: Uses custom 5-minute retention
- **meta/llama-3.1-405b**: Always maintains 1 replica
- **All other models**: Inherit defaults (scale-to-zero enabled, 20-minute retention)

### Example 4: Using Global Environment Variable

For simpler configurations without retention period customization:

```yaml
# In controller deployment
env:
  - name: WVA_SCALE_TO_ZERO
    value: "true"
```

Any model not explicitly configured in the ConfigMap (and without global defaults) will use:
- Scale-to-zero: enabled
- Retention period: 10 minutes (system default)

## Best Practices

### 1. Choose Appropriate Retention Periods

Consider your use case when setting retention periods:

- **Development/Testing**: 5-10 minutes (faster cost savings)
- **Production with predictable traffic**: 15-30 minutes (balance between cost and availability)
- **Production with unpredictable traffic**: 30-60 minutes (prioritize availability)
- **Critical services**: Disable scale-to-zero (always maintain 1 replica)

### 2. Use Global Defaults

**Recommended**: Set organization-wide defaults using `__defaults__` and only override for specific models:

```yaml
data:
  # Set sensible defaults for most models
  __defaults__: |
    enableScaleToZero: true
    retentionPeriod: "15m"

  # Only specify exceptions
  model.meta.llama-3.1-405b: |
    modelID: "meta/llama-3.1-405b"
    enableScaleToZero: false  # Critical model

  model.meta.llama-3.1-8b: |
    modelID: "meta/llama-3.1-8b"
    retentionPeriod: "5m"  # Dev model, shorter retention
```

**Benefits:**
- Less configuration duplication
- Easier to change default policy
- Explicit exceptions are clearly visible

### 3. Model Categorization

Group models by usage patterns with global defaults as baseline:

```yaml
data:
  # Global baseline for most models
  __defaults__: |
    enableScaleToZero: true
    retentionPeriod: "20m"

  # High-traffic models (override to disable)
  model.meta.llama-3.1-8b: |
    modelID: "meta/llama-3.1-8b"
    enableScaleToZero: false

  # Low-traffic models (override for aggressive retention)
  model.mistralai.Mistral-7B-v0.1: |
    modelID: "mistralai/Mistral-7B-v0.1"
    retentionPeriod: "5m"

  # Medium-traffic models inherit defaults (20m retention)
```

### 4. Monitor and Adjust

- Monitor metrics to understand actual traffic patterns
- Adjust retention periods based on observed behavior
- Consider time-of-day variations in traffic

### 5. ConfigMap Updates

ConfigMap changes are automatically detected by the controller through a Kubernetes watch. Changes will take effect in the next reconciliation cycle (typically within seconds).

## Integration with Other Features

### Optimization

Scale-to-zero works seamlessly with the workload optimization engine:

1. The optimizer calculates the ideal number of replicas based on current load
2. The scale-to-zero configuration sets the minimum allowed replicas (0 or 1)
3. The final replica count respects both the optimization result and the scale-to-zero constraint

### Metrics

The controller monitors these metrics to determine when to scale:

- **Request rate**: Number of requests per second
- **Last request timestamp**: Time since the last request
- **Retention period**: Configured wait time before scaling to zero

## Troubleshooting

### Model Not Scaling to Zero

**Symptoms**: Model stays at 1 replica even when idle

**Possible Causes**:
1. Scale-to-zero is disabled in ConfigMap
2. Global `WVA_SCALE_TO_ZERO` is set to "false"
3. Retention period hasn't elapsed yet
4. Active requests are preventing scale-down

**Solutions**:
- Check ConfigMap configuration
- Verify environment variable
- Wait for retention period to elapse
- Check metrics for active requests

### ConfigMap Changes Not Applied

**Symptoms**: Changes to ConfigMap don't affect behavior

**Possible Causes**:
1. ConfigMap in wrong namespace
2. JSON syntax error in configuration
3. Controller hasn't reconciled yet

**Solutions**:
- Verify ConfigMap is in `workload-variant-autoscaler-system` namespace
- Validate YAML syntax
- Check controller logs for parsing errors
- Trigger reconciliation by updating a VariantAutoscaling resource

### Invalid YAML in ConfigMap

**Symptoms**: Some models work, others don't

**Behavior**: The controller skips entries with invalid YAML and logs a warning. Other valid entries are still processed.

**Solution**: Check controller logs for YAML parsing errors and fix the configuration.

## Implementation Details

### Code Structure

The scale-to-zero feature is implemented across several components:

#### Utility Functions (`internal/utils/utils.go`)

- `ScaleToZeroConfigData`: Type alias for configuration map
- `IsScaleToZeroEnabled()`: Checks if scale-to-zero is enabled for a model
- `GetScaleToZeroRetentionPeriod()`: Gets retention period for a model
- `GetMinNumReplicas()`: Returns minimum replicas (0 or 1) based on configuration

#### Controller (`internal/controller/variantautoscaling_controller.go`)

- `readScaleToZeroConfig()`: Reads and parses the ConfigMap using prefixed-key format with YAML values
- **Deterministic parsing**: Sorts keys lexicographically before processing
- **Duplicate detection**: Warns if same modelID appears in multiple keys (first key wins)
- ConfigMap watch: Automatically triggers reconciliation on changes
- Integration with `prepareVariantAutoscalings()`: Applies configuration during optimization

### Configuration Reading Pattern

The controller follows the project's established pattern for reading ConfigMaps:

1. **Read once per reconcile**: ConfigMap is read once at the start of reconciliation
2. **Pass data down**: Parsed configuration is passed as parameter to functions
3. **No context in structs**: Context is passed as function parameter, not stored
4. **Exponential backoff**: Uses `GetConfigMapWithBackoff()` for resilient reads

## API Reference

### ScaleToZeroConfigData

```go
type ScaleToZeroConfigData map[string]ModelScaleToZeroConfig
```

Map of modelID to scale-to-zero configuration.

### ModelScaleToZeroConfig

```go
type ModelScaleToZeroConfig struct {
    ModelID           string `yaml:"modelID,omitempty" json:"modelID,omitempty"`
    EnableScaleToZero *bool  `yaml:"enableScaleToZero,omitempty" json:"enableScaleToZero,omitempty"`
    RetentionPeriod   string `yaml:"retentionPeriod,omitempty" json:"retentionPeriod,omitempty"`
}
```

Configuration for a specific model:
- `ModelID`: Original model identifier (required for per-model entries)
- `EnableScaleToZero`: Whether to allow scaling to zero (pointer to support partial overrides)
- `RetentionPeriod`: How long to wait before scaling to zero (optional)

### Functions

#### IsScaleToZeroEnabled

```go
func IsScaleToZeroEnabled(configData ScaleToZeroConfigData, modelID string) bool
```

Determines if scale-to-zero is enabled for a model. Checks ConfigMap first, then falls back to global environment variable.

#### GetScaleToZeroRetentionPeriod

```go
func GetScaleToZeroRetentionPeriod(configData ScaleToZeroConfigData, modelID string) time.Duration
```

Returns the retention period for a model. Returns default (10 minutes) if not configured or if parsing fails.

#### GetMinNumReplicas

```go
func GetMinNumReplicas(configData ScaleToZeroConfigData, modelID string) int
```

Returns the minimum number of replicas for a model:
- Returns 0 if scale-to-zero is enabled
- Returns 1 if scale-to-zero is disabled

## See Also

- [CRD Reference](../user-guide/crd-reference.md) - VariantAutoscaling API documentation
- [Configuration Guide](../user-guide/configuration.md) - General configuration options
- [KEDA Integration](../integrations/keda-integration.md) - Integration with KEDA for scale-to-zero
