package controller

import (
	"sync"
	"time"
)

// ModelMetrics holds internal metrics for a specific model that are not exposed in the CRD
type ModelMetrics struct {
	// TotalRequestsOverRetentionPeriod is the total number of requests received over
	// the scale-to-zero retention period. This is used internally for scale-to-zero decisions.
	TotalRequestsOverRetentionPeriod float64

	// RetentionPeriod is the configured retention period for this model
	RetentionPeriod time.Duration

	// LastUpdated is the timestamp when these metrics were last updated
	LastUpdated time.Time
}

// ModelMetricsCache is a thread-safe cache for storing per-model internal metrics
type ModelMetricsCache struct {
	mu      sync.RWMutex
	metrics map[string]*ModelMetrics // modelID -> metrics
}

// NewModelMetricsCache creates a new ModelMetricsCache
func NewModelMetricsCache() *ModelMetricsCache {
	return &ModelMetricsCache{
		metrics: make(map[string]*ModelMetrics),
	}
}

// Set stores or updates metrics for a model
func (c *ModelMetricsCache) Set(modelID string, metrics *ModelMetrics) {
	if metrics == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Create a copy to avoid side effects on caller's struct
	metricsCopy := *metrics
	metricsCopy.LastUpdated = time.Now()
	c.metrics[modelID] = &metricsCopy
}

// Get retrieves a copy of metrics for a model
func (c *ModelMetricsCache) Get(modelID string) (*ModelMetrics, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	metrics, exists := c.metrics[modelID]
	if !exists {
		return nil, false
	}
	// Return a copy to prevent race conditions
	metricsCopy := *metrics
	return &metricsCopy, true
}

// GetAll returns a copy of all metrics
func (c *ModelMetricsCache) GetAll() map[string]*ModelMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*ModelMetrics, len(c.metrics))
	for k, v := range c.metrics {
		// Create a copy to avoid race conditions
		metricsCopy := *v
		result[k] = &metricsCopy
	}
	return result
}

// Delete removes metrics for a model
func (c *ModelMetricsCache) Delete(modelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.metrics, modelID)
}

// Clear removes all cached metrics
func (c *ModelMetricsCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = make(map[string]*ModelMetrics)
}
