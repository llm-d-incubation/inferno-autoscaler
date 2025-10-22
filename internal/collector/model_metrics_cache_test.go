package collector

import (
	"sync"
	"testing"
	"time"

	llmdVariantAutoscalingV1alpha1 "github.com/llm-d-incubation/workload-variant-autoscaler/api/v1alpha1"
)

func TestNewModelMetricsCache(t *testing.T) {
	ttl := 5 * time.Second
	cache := NewModelMetricsCache(ttl)

	if cache == nil {
		t.Fatal("NewModelMetricsCache returned nil")
	}

	if cache.ttl != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, cache.ttl)
	}

	if cache.metrics == nil {
		t.Error("Cache metrics map is nil")
	}

	if cache.Size() != 0 {
		t.Errorf("Expected empty cache, got size %d", cache.Size())
	}
}

func TestModelMetricsCache_SetAndGet(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	// Set metrics
	cache.Set("model1", "namespace1", load, "50.0", "25.0", true)

	// Get metrics
	metrics, found := cache.Get("model1", "namespace1")
	if !found {
		t.Fatal("Expected to find cached metrics")
	}

	if metrics.ModelID != "model1" {
		t.Errorf("Expected ModelID model1, got %s", metrics.ModelID)
	}

	if metrics.Namespace != "namespace1" {
		t.Errorf("Expected namespace namespace1, got %s", metrics.Namespace)
	}

	if metrics.TTFTAverage != "50.0" {
		t.Errorf("Expected TTFT 50.0, got %s", metrics.TTFTAverage)
	}

	if metrics.ITLAverage != "25.0" {
		t.Errorf("Expected ITL 25.0, got %s", metrics.ITLAverage)
	}

	if !metrics.Valid {
		t.Error("Expected metrics to be valid")
	}
}

func TestModelMetricsCache_GetNonExistent(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	metrics, found := cache.Get("nonexistent", "namespace1")
	if found {
		t.Error("Expected not to find non-existent metrics")
	}

	if metrics != nil {
		t.Error("Expected nil metrics for non-existent entry")
	}
}

func TestModelMetricsCache_TTLExpiration(t *testing.T) {
	// Use very short TTL for testing
	cache := NewModelMetricsCache(100 * time.Millisecond)

	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	cache.Set("model1", "namespace1", load, "50.0", "25.0", true)

	// Should find immediately
	_, found := cache.Get("model1", "namespace1")
	if !found {
		t.Error("Expected to find metrics immediately after setting")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should not find after expiration
	_, found = cache.Get("model1", "namespace1")
	if found {
		t.Error("Expected metrics to be expired after TTL")
	}
}

func TestModelMetricsCache_Invalidate(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	cache.Set("model1", "namespace1", load, "50.0", "25.0", true)

	// Verify it's there
	_, found := cache.Get("model1", "namespace1")
	if !found {
		t.Fatal("Expected to find metrics before invalidation")
	}

	// Invalidate
	cache.Invalidate("model1", "namespace1")

	// Should not find after invalidation
	_, found = cache.Get("model1", "namespace1")
	if found {
		t.Error("Expected metrics to be invalidated")
	}
}

func TestModelMetricsCache_Clear(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	// Add multiple entries
	cache.Set("model1", "namespace1", load, "50.0", "25.0", true)
	cache.Set("model2", "namespace1", load, "60.0", "30.0", true)
	cache.Set("model3", "namespace2", load, "70.0", "35.0", true)

	if cache.Size() != 3 {
		t.Errorf("Expected cache size 3, got %d", cache.Size())
	}

	// Clear cache
	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Expected empty cache after Clear, got size %d", cache.Size())
	}

	// Verify entries are gone
	_, found := cache.Get("model1", "namespace1")
	if found {
		t.Error("Expected model1 to be cleared")
	}
}

func TestModelMetricsCache_Size(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	if cache.Size() != 0 {
		t.Errorf("Expected initial size 0, got %d", cache.Size())
	}

	cache.Set("model1", "namespace1", load, "50.0", "25.0", true)
	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}

	cache.Set("model2", "namespace1", load, "60.0", "30.0", true)
	if cache.Size() != 2 {
		t.Errorf("Expected size 2, got %d", cache.Size())
	}

	// Setting same key should not increase size
	cache.Set("model1", "namespace1", load, "55.0", "28.0", true)
	if cache.Size() != 2 {
		t.Errorf("Expected size to remain 2, got %d", cache.Size())
	}
}

func TestModelMetricsCache_GetAll(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	load1 := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	load2 := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "20.5",
		AvgInputTokens:  "150",
		AvgOutputTokens: "250",
	}

	cache.Set("model1", "namespace1", load1, "50.0", "25.0", true)
	cache.Set("model2", "namespace1", load2, "60.0", "30.0", true)

	all := cache.GetAll()

	if len(all) != 2 {
		t.Errorf("Expected GetAll to return 2 entries, got %d", len(all))
	}

	// Verify keys exist
	key1 := cache.cacheKey("model1", "namespace1")
	key2 := cache.cacheKey("model2", "namespace1")

	if _, exists := all[key1]; !exists {
		t.Error("Expected to find model1 in GetAll result")
	}

	if _, exists := all[key2]; !exists {
		t.Error("Expected to find model2 in GetAll result")
	}

	// Verify returned map is a copy (modifying it doesn't affect cache)
	delete(all, key1)
	if cache.Size() != 2 {
		t.Error("Modifying GetAll result should not affect cache")
	}
}

func TestModelMetricsCache_Cleanup(t *testing.T) {
	// Use very short TTL
	cache := NewModelMetricsCache(50 * time.Millisecond)

	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	// Add entries
	cache.Set("model1", "namespace1", load, "50.0", "25.0", true)
	cache.Set("model2", "namespace1", load, "60.0", "30.0", true)
	cache.Set("model3", "namespace2", load, "70.0", "35.0", true)

	if cache.Size() != 3 {
		t.Fatalf("Expected size 3, got %d", cache.Size())
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Run cleanup
	removed := cache.Cleanup()

	if removed != 3 {
		t.Errorf("Expected Cleanup to remove 3 entries, removed %d", removed)
	}

	if cache.Size() != 0 {
		t.Errorf("Expected cache to be empty after cleanup, got size %d", cache.Size())
	}
}

func TestModelMetricsCache_ConcurrentAccess(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	// Run concurrent operations
	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent writes
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			modelID := "model" + string(rune('A'+id%26))
			cache.Set(modelID, "namespace1", load, "50.0", "25.0", true)
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			modelID := "model" + string(rune('A'+id%26))
			cache.Get(modelID, "namespace1")
		}(i)
	}
	wg.Wait()

	// Should not panic or deadlock
	t.Log("Concurrent access test passed")
}

func TestModelMetricsCache_InvalidMetrics(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	load := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	// Set invalid metrics (valid=false)
	cache.Set("model1", "namespace1", load, "50.0", "25.0", false)

	metrics, found := cache.Get("model1", "namespace1")
	if !found {
		t.Fatal("Expected to find metrics even if invalid")
	}

	if metrics.Valid {
		t.Error("Expected metrics to be invalid")
	}
}

func TestModelMetricsCache_NamespaceIsolation(t *testing.T) {
	cache := NewModelMetricsCache(10 * time.Second)

	load1 := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "10.5",
		AvgInputTokens:  "100",
		AvgOutputTokens: "200",
	}

	load2 := llmdVariantAutoscalingV1alpha1.LoadProfile{
		ArrivalRate:     "20.5",
		AvgInputTokens:  "150",
		AvgOutputTokens: "250",
	}

	// Same model ID, different namespaces
	cache.Set("model1", "namespace1", load1, "50.0", "25.0", true)
	cache.Set("model1", "namespace2", load2, "60.0", "30.0", true)

	// Should be separate entries
	if cache.Size() != 2 {
		t.Errorf("Expected 2 separate entries for different namespaces, got %d", cache.Size())
	}

	metrics1, _ := cache.Get("model1", "namespace1")
	metrics2, _ := cache.Get("model1", "namespace2")

	if metrics1.Load.ArrivalRate != "10.5" {
		t.Error("Namespace isolation failed for namespace1")
	}

	if metrics2.Load.ArrivalRate != "20.5" {
		t.Error("Namespace isolation failed for namespace2")
	}
}
