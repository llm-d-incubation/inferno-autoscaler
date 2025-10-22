# Model Metrics Caching

## Overview

The Workload-Variant-Autoscaler implements a thread-safe, TTL-based cache for model-level metrics to reduce redundant Prometheus queries and improve controller performance. This is particularly beneficial in multi-variant scenarios where multiple `VariantAutoscaling` resources reference the same model.

## Architecture

### Cache Design

The `ModelMetricsCache` provides:
- **Thread-safe operations** using `sync.RWMutex` for concurrent access
- **TTL-based expiration** (default 30 seconds, configurable)
- **Namespace isolation** - same model in different namespaces is cached separately
- **Failed query caching** - prevents thundering herd on Prometheus failures
- **Automatic cleanup** - expired entries can be removed on-demand

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                  Controller Reconciliation Loop             │
└───────────────────────────┬─────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│         CollectAggregateMetricsWithCache()                  │
│                                                             │
│  1. Check cache for modelID:namespace                       │
│  2. If found & valid: return cached metrics                 │
│  3. If not found: query Prometheus                          │
│  4. Cache result (even if query failed)                     │
└───────────────────────────┬─────────────────────────────────┘
                            │
         ┌──────────────────┴──────────────────┐
         │                                     │
         ▼                                     ▼
┌─────────────────┐                 ┌──────────────────────┐
│ ModelMetrics    │                 │  Prometheus API      │
│ Cache           │                 │                      │
│                 │                 │  - Load metrics      │
│ - Get()         │                 │  - TTFT average      │
│ - Set()         │                 │  - ITL average       │
│ - Invalidate()  │                 │                      │
│ - Cleanup()     │                 └──────────────────────┘
└─────────────────┘
```

## Implementation Details

### Cache Structure

```go
type ModelMetricsCache struct {
    metrics map[string]*ModelMetrics // key: "modelID:namespace"
    mu      sync.RWMutex             // protects metrics map
    ttl     time.Duration            // time-to-live for cached entries
}

type ModelMetrics struct {
    ModelID     string          // Unique identifier for the model
    Namespace   string          // Kubernetes namespace
    Load        LoadProfile     // Workload characteristics
    TTFTAverage string          // Time to first token (ms)
    ITLAverage  string          // Inter-token latency (ms)
    LastUpdated time.Time       // When metrics were cached
    Valid       bool            // Whether query succeeded
}
```

### Cache Key Format

Cache keys follow the pattern: `modelID:namespace`

**Examples:**
- `meta-llama/Llama-2-7b:production`
- `meta-llama/Llama-2-7b:staging`
- `ibm/granite-13b:default`

This ensures namespace isolation - the same model in different namespaces is cached separately.

### TTL and Expiration

**Default TTL:** 30 seconds

The TTL balances:
- **Freshness** - Metrics reflect recent workload changes
- **Performance** - Reduces Prometheus query load by ~90% in multi-variant scenarios
- **Accuracy** - Sufficient for autoscaling decisions (typical reconciliation interval: 60s)

**Expiration Logic:**
```go
func (c *ModelMetricsCache) Get(modelID, namespace string) (*ModelMetrics, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    metrics, exists := c.metrics[key]
    if !exists {
        return nil, false // Cache miss
    }

    if time.Since(metrics.LastUpdated) > c.ttl {
        return nil, false // Expired
    }

    return metrics, true // Cache hit
}
```

### Failed Query Caching

When Prometheus queries fail, the cache stores the failed result with `Valid: false`:

```go
// Update cache even on error (mark as invalid) to prevent thundering herd
if cache != nil {
    cache.Set(modelName, namespace, load, ttftAvg, itlAvg, err == nil)
}
```

**Benefits:**
- Prevents retry storms when Prometheus is temporarily unavailable
- Reduces controller reconciliation failures
- Failed queries respect the same TTL, allowing retry after expiration

## Usage

### Controller Integration

The cache is initialized in the controller's `SetupWithManager()`:

```go
func (r *VariantAutoscalingReconciler) SetupWithManager(mgr ctrl.Manager) error {
    // ... Prometheus client setup ...

    // Initialize model metrics cache with 30-second TTL
    cacheTTL := 30 * time.Second
    r.MetricsCache = collector.NewModelMetricsCache(cacheTTL)
    logger.Log.Info("Model metrics cache initialized", "ttl", cacheTTL.String())

    return ctrl.NewControllerManagedBy(mgr).
        For(&llmdVariantAutoscalingV1alpha1.VariantAutoscaling{}).
        Complete(r)
}
```

### Collecting Metrics with Cache

```go
// In reconciliation loop
load, ttftAvg, itlAvg, err := collector.CollectAggregateMetricsWithCache(
    ctx,
    modelName,
    deploy.Namespace,
    r.PromAPI,
    r.MetricsCache,
)
```

## Performance Impact

### Metrics Query Reduction

**Scenario:** 10 VariantAutoscalings referencing the same model

**Without cache:**
- 10 Prometheus queries per reconciliation cycle
- Each query executes 4 separate Prometheus API calls
- Total: 40 API calls per cycle

**With cache (30s TTL):**
- First VA: 1 Prometheus query (cache miss) → 4 API calls
- Next 9 VAs: Return from cache → 0 API calls
- Total: 4 API calls per cycle

**Reduction:** 90% fewer Prometheus API calls

### Memory Overhead

Each cached entry: ~200 bytes

**Example:** 100 unique model-namespace combinations = ~20 KB

## Configuration

### TTL Configuration

Currently, TTL is hardcoded to 30 seconds. To change:

1. Modify `internal/controller/variantautoscaling_controller.go`:
   ```go
   cacheTTL := 60 * time.Second // Example: 60 seconds
   r.MetricsCache = collector.NewModelMetricsCache(cacheTTL)
   ```

2. Rebuild and redeploy the controller

**Future:** TTL will be configurable via ConfigMap

### Disabling the Cache

To disable caching (for debugging):

```go
// Pass nil cache to bypass caching
load, ttftAvg, itlAvg, err := collector.CollectAggregateMetricsWithCache(
    ctx, modelName, namespace, promAPI, nil,
)
```

## Monitoring and Debugging

### Cache Hit/Miss Logging

Debug logs show cache behavior:

```
DEBUG Using cached metrics for model  model=meta-llama/Llama-2-7b namespace=production
DEBUG Querying Prometheus for model metrics  model=ibm/granite-13b namespace=staging
```

Enable debug logging:
```bash
kubectl set env deployment/wva-controller \
  -n workload-variant-autoscaler-system \
  LOG_LEVEL=debug
```

### Cache Statistics

Programmatically access cache stats:

```go
size := r.MetricsCache.Size()
all := r.MetricsCache.GetAll()
logger.Log.Info("Cache stats", "entries", size)
```

### Manual Cache Invalidation

Force cache refresh for a specific model:

```go
r.MetricsCache.Invalidate(modelID, namespace)
```

Clear entire cache:

```go
r.MetricsCache.Clear()
```

## Thread Safety

All cache operations are thread-safe:

- **Read operations** (`Get`, `GetAll`, `Size`): Use `RLock()` - multiple concurrent reads allowed
- **Write operations** (`Set`, `Invalidate`, `Clear`, `Cleanup`): Use `Lock()` - exclusive access

**Tested:** Concurrent access with 100 goroutines in `model_metrics_cache_test.go`

## Testing

### Unit Tests

**Cache functionality:** `internal/collector/model_metrics_cache_test.go`
- Set/Get operations
- TTL expiration
- Namespace isolation
- Concurrent access
- Invalid TTL handling
- Cleanup operations

**Integration tests:** `internal/collector/collector_cache_test.go`
- Cache miss/hit scenarios
- Multiple models
- Failed query caching
- Nil cache behavior

### Running Tests

```bash
# All collector tests (includes cache tests)
go test ./internal/collector/... -v

# Specific cache tests
go test ./internal/collector/... -v -run TestModelMetricsCache
go test ./internal/collector/... -v -run TestCollectAggregateMetricsWithCache
```

## Limitations and Future Improvements

### Current Limitations

1. **Fixed TTL** - Not configurable at runtime
2. **No eviction policy** - Cache grows unbounded (though TTL provides implicit bounds)
3. **No metrics** - Cache hit/miss rates not exposed as Prometheus metrics

### Planned Improvements

1. **ConfigMap-based TTL** - Make TTL configurable via controller ConfigMap
2. **Cache metrics** - Expose cache hit/miss rates, size, eviction count
3. **LRU eviction** - Limit cache size with LRU policy for long-running controllers
4. **Prometheus query batching** - Batch multiple model queries into single API call
5. **Cache warming** - Pre-populate cache on controller startup

## API Reference

### ModelMetricsCache Methods

```go
// NewModelMetricsCache creates a cache with specified TTL
func NewModelMetricsCache(ttl time.Duration) *ModelMetricsCache

// Get retrieves cached metrics (returns nil if expired or not found)
func (c *ModelMetricsCache) Get(modelID, namespace string) (*ModelMetrics, bool)

// Set stores metrics in cache with current timestamp
func (c *ModelMetricsCache) Set(modelID, namespace string, load LoadProfile,
    ttftAvg, itlAvg string, valid bool)

// Invalidate removes specific model metrics from cache
func (c *ModelMetricsCache) Invalidate(modelID, namespace string)

// Clear removes all cached metrics
func (c *ModelMetricsCache) Clear()

// Size returns number of cached entries
func (c *ModelMetricsCache) Size() int

// GetAll returns snapshot of all cached metrics
func (c *ModelMetricsCache) GetAll() map[string]*ModelMetrics

// Cleanup removes expired entries, returns count removed
func (c *ModelMetricsCache) Cleanup() int
```

### Collector Functions

```go
// CollectAggregateMetricsWithCache queries with cache support
func CollectAggregateMetricsWithCache(ctx context.Context,
    modelName string, namespace string,
    promAPI promv1.API, cache *ModelMetricsCache) (LoadProfile, string, string, error)

// CollectAggregateMetrics queries without cache (legacy)
func CollectAggregateMetrics(ctx context.Context,
    modelName string, namespace string,
    promAPI promv1.API) (LoadProfile, string, string, error)
```

## See Also

- [Prometheus Integration](../integrations/prometheus.md)
- [Controller Architecture](development.md#project-structure)
- [Performance Testing](../tutorials/performance-testing.md)
