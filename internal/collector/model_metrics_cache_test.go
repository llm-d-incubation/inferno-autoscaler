package collector

import (
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ModelMetricsCache", func() {
	var cache *ModelMetricsCache

	BeforeEach(func() {
		cache = NewModelMetricsCache()
	})

	Context("Basic operations", func() {
		It("should create a new empty cache", func() {
			Expect(cache).NotTo(BeNil())
			Expect(cache.metrics).NotTo(BeNil())
			Expect(cache.metrics).To(BeEmpty())
		})

		It("should set and get metrics successfully", func() {
			modelID := "test-model"
			metrics := &ModelMetrics{
				TotalRequestsOverRetentionPeriod: 100.0,
				RetentionPeriod:                  10 * time.Minute,
			}

			cache.Set(modelID, metrics)

			retrieved, exists := cache.Get(modelID)
			Expect(exists).To(BeTrue())
			Expect(retrieved).NotTo(BeNil())
			Expect(retrieved.TotalRequestsOverRetentionPeriod).To(Equal(100.0))
			Expect(retrieved.RetentionPeriod).To(Equal(10 * time.Minute))
			Expect(retrieved.LastUpdated).NotTo(BeZero())
		})

		It("should return false for non-existent model", func() {
			retrieved, exists := cache.Get("non-existent")
			Expect(exists).To(BeFalse())
			Expect(retrieved).To(BeNil())
		})

		It("should update existing metrics", func() {
			modelID := "test-model"
			metrics1 := &ModelMetrics{
				TotalRequestsOverRetentionPeriod: 100.0,
				RetentionPeriod:                  10 * time.Minute,
			}
			cache.Set(modelID, metrics1)

			time.Sleep(10 * time.Millisecond)

			metrics2 := &ModelMetrics{
				TotalRequestsOverRetentionPeriod: 200.0,
				RetentionPeriod:                  15 * time.Minute,
			}
			cache.Set(modelID, metrics2)

			retrieved, exists := cache.Get(modelID)
			Expect(exists).To(BeTrue())
			Expect(retrieved.TotalRequestsOverRetentionPeriod).To(Equal(200.0))
			Expect(retrieved.RetentionPeriod).To(Equal(15 * time.Minute))
		})

		It("should delete metrics successfully", func() {
			modelID := "test-model"
			metrics := &ModelMetrics{
				TotalRequestsOverRetentionPeriod: 100.0,
				RetentionPeriod:                  10 * time.Minute,
			}
			cache.Set(modelID, metrics)

			cache.Delete(modelID)

			retrieved, exists := cache.Get(modelID)
			Expect(exists).To(BeFalse())
			Expect(retrieved).To(BeNil())
		})

		It("should handle deleting non-existent model gracefully", func() {
			Expect(func() {
				cache.Delete("non-existent")
			}).NotTo(Panic())
		})

		It("should clear all metrics", func() {
			cache.Set("model1", &ModelMetrics{TotalRequestsOverRetentionPeriod: 100.0})
			cache.Set("model2", &ModelMetrics{TotalRequestsOverRetentionPeriod: 200.0})

			cache.Clear()

			_, exists1 := cache.Get("model1")
			_, exists2 := cache.Get("model2")
			Expect(exists1).To(BeFalse())
			Expect(exists2).To(BeFalse())
		})
	})

	Context("Nil handling", func() {
		It("should handle nil metrics in Set gracefully", func() {
			Expect(func() {
				cache.Set("test-model", nil)
			}).NotTo(Panic())

			_, exists := cache.Get("test-model")
			Expect(exists).To(BeFalse())
		})
	})

	Context("Copy semantics", func() {
		It("should return a copy in Get, not the internal pointer", func() {
			modelID := "test-model"
			metrics := &ModelMetrics{
				TotalRequestsOverRetentionPeriod: 100.0,
				RetentionPeriod:                  10 * time.Minute,
			}
			cache.Set(modelID, metrics)

			retrieved1, _ := cache.Get(modelID)
			retrieved2, _ := cache.Get(modelID)

			// Modifying retrieved1 should not affect retrieved2
			retrieved1.TotalRequestsOverRetentionPeriod = 999.0

			Expect(retrieved2.TotalRequestsOverRetentionPeriod).To(Equal(100.0))
		})

		It("should not modify caller's struct in Set", func() {
			modelID := "test-model"
			metrics := &ModelMetrics{
				TotalRequestsOverRetentionPeriod: 100.0,
				RetentionPeriod:                  10 * time.Minute,
			}
			originalLastUpdated := metrics.LastUpdated // Should be zero

			cache.Set(modelID, metrics)

			// Original struct should not be modified
			Expect(metrics.LastUpdated).To(Equal(originalLastUpdated))
		})

		It("should return independent copies in GetAll", func() {
			cache.Set("model1", &ModelMetrics{TotalRequestsOverRetentionPeriod: 100.0})
			cache.Set("model2", &ModelMetrics{TotalRequestsOverRetentionPeriod: 200.0})

			allMetrics := cache.GetAll()

			// Modify one of the returned metrics
			allMetrics["model1"].TotalRequestsOverRetentionPeriod = 999.0

			// Original should be unchanged
			retrieved, _ := cache.Get("model1")
			Expect(retrieved.TotalRequestsOverRetentionPeriod).To(Equal(100.0))
		})
	})

	Context("GetAll operations", func() {
		It("should return all metrics", func() {
			cache.Set("model1", &ModelMetrics{TotalRequestsOverRetentionPeriod: 100.0})
			cache.Set("model2", &ModelMetrics{TotalRequestsOverRetentionPeriod: 200.0})
			cache.Set("model3", &ModelMetrics{TotalRequestsOverRetentionPeriod: 300.0})

			allMetrics := cache.GetAll()

			Expect(allMetrics).To(HaveLen(3))
			Expect(allMetrics["model1"].TotalRequestsOverRetentionPeriod).To(Equal(100.0))
			Expect(allMetrics["model2"].TotalRequestsOverRetentionPeriod).To(Equal(200.0))
			Expect(allMetrics["model3"].TotalRequestsOverRetentionPeriod).To(Equal(300.0))
		})

		It("should return empty map for empty cache", func() {
			allMetrics := cache.GetAll()
			Expect(allMetrics).To(BeEmpty())
		})
	})

	Context("Concurrent access", func() {
		It("should handle concurrent Set operations", func() {
			const numGoroutines = 10
			const numIterations = 100

			var wg sync.WaitGroup
			wg.Add(numGoroutines)

			for i := 0; i < numGoroutines; i++ {
				go func(goroutineID int) {
					defer wg.Done()
					for j := 0; j < numIterations; j++ {
						modelID := "model" + string(rune(goroutineID))
						cache.Set(modelID, &ModelMetrics{
							TotalRequestsOverRetentionPeriod: float64(j),
							RetentionPeriod:                  time.Duration(j) * time.Second,
						})
					}
				}(i)
			}

			wg.Wait()

			// Verify we can still get metrics
			allMetrics := cache.GetAll()
			Expect(allMetrics).To(HaveLen(numGoroutines))
		})

		It("should handle concurrent Get operations", func() {
			// Pre-populate cache
			for i := 0; i < 10; i++ {
				modelID := "model" + string(rune(i))
				cache.Set(modelID, &ModelMetrics{
					TotalRequestsOverRetentionPeriod: float64(i * 100),
					RetentionPeriod:                  time.Duration(i) * time.Minute,
				})
			}

			const numGoroutines = 20
			const numIterations = 100

			var wg sync.WaitGroup
			wg.Add(numGoroutines)

			for i := 0; i < numGoroutines; i++ {
				go func() {
					defer wg.Done()
					for j := 0; j < numIterations; j++ {
						modelID := "model" + string(rune(j%10))
						retrieved, exists := cache.Get(modelID)
						Expect(exists).To(BeTrue())
						Expect(retrieved).NotTo(BeNil())
					}
				}()
			}

			wg.Wait()
		})

		It("should handle concurrent Set and Get operations", func() {
			const numGoroutines = 10
			const numIterations = 100

			var wg sync.WaitGroup
			wg.Add(numGoroutines * 2)

			// Writers
			for i := 0; i < numGoroutines; i++ {
				go func(goroutineID int) {
					defer wg.Done()
					for j := 0; j < numIterations; j++ {
						modelID := "model" + string(rune(goroutineID%5))
						cache.Set(modelID, &ModelMetrics{
							TotalRequestsOverRetentionPeriod: float64(j),
							RetentionPeriod:                  time.Duration(j) * time.Second,
						})
					}
				}(i)
			}

			// Readers
			for i := 0; i < numGoroutines; i++ {
				go func(goroutineID int) {
					defer wg.Done()
					for j := 0; j < numIterations; j++ {
						modelID := "model" + string(rune(goroutineID%5))
						cache.Get(modelID)
					}
				}(i)
			}

			wg.Wait()
		})

		It("should handle concurrent GetAll operations", func() {
			// Pre-populate cache
			cache.Set("model1", &ModelMetrics{TotalRequestsOverRetentionPeriod: 100.0})
			cache.Set("model2", &ModelMetrics{TotalRequestsOverRetentionPeriod: 200.0})

			const numGoroutines = 10
			var wg sync.WaitGroup
			wg.Add(numGoroutines)

			for i := 0; i < numGoroutines; i++ {
				go func() {
					defer wg.Done()
					allMetrics := cache.GetAll()
					Expect(allMetrics).To(HaveLen(2))
				}()
			}

			wg.Wait()
		})

		It("should handle concurrent Delete operations", func() {
			// Pre-populate cache
			for i := 0; i < 10; i++ {
				modelID := "model" + string(rune(i))
				cache.Set(modelID, &ModelMetrics{TotalRequestsOverRetentionPeriod: float64(i * 100)})
			}

			const numGoroutines = 10
			var wg sync.WaitGroup
			wg.Add(numGoroutines)

			for i := 0; i < numGoroutines; i++ {
				go func(goroutineID int) {
					defer wg.Done()
					modelID := "model" + string(rune(goroutineID))
					cache.Delete(modelID)
				}(i)
			}

			wg.Wait()

			allMetrics := cache.GetAll()
			Expect(allMetrics).To(BeEmpty())
		})
	})

	Context("Edge cases", func() {
		It("should handle empty modelID", func() {
			metrics := &ModelMetrics{TotalRequestsOverRetentionPeriod: 100.0}
			cache.Set("", metrics)

			retrieved, exists := cache.Get("")
			Expect(exists).To(BeTrue())
			Expect(retrieved).NotTo(BeNil())
		})

		It("should handle zero values", func() {
			modelID := "test-model"
			metrics := &ModelMetrics{
				TotalRequestsOverRetentionPeriod: 0.0,
				RetentionPeriod:                  0,
			}
			cache.Set(modelID, metrics)

			retrieved, exists := cache.Get(modelID)
			Expect(exists).To(BeTrue())
			Expect(retrieved.TotalRequestsOverRetentionPeriod).To(Equal(0.0))
			Expect(retrieved.RetentionPeriod).To(Equal(time.Duration(0)))
		})

		It("should handle very large values", func() {
			modelID := "test-model"
			metrics := &ModelMetrics{
				TotalRequestsOverRetentionPeriod: 1e15,
				RetentionPeriod:                  24 * 365 * time.Hour, // 1 year
			}
			cache.Set(modelID, metrics)

			retrieved, exists := cache.Get(modelID)
			Expect(exists).To(BeTrue())
			Expect(retrieved.TotalRequestsOverRetentionPeriod).To(Equal(1e15))
		})
	})
})

var _ = Describe("formatPrometheusDuration", func() {
	Context("Duration formatting", func() {
		It("should format whole hours correctly", func() {
			Expect(formatPrometheusDuration(1 * time.Hour)).To(Equal("1h"))
			Expect(formatPrometheusDuration(24 * time.Hour)).To(Equal("24h"))
		})

		It("should format whole minutes correctly", func() {
			Expect(formatPrometheusDuration(1 * time.Minute)).To(Equal("1m"))
			Expect(formatPrometheusDuration(10 * time.Minute)).To(Equal("10m"))
			Expect(formatPrometheusDuration(60 * time.Minute)).To(Equal("1h")) // Should prefer hours
		})

		It("should format seconds correctly", func() {
			Expect(formatPrometheusDuration(1 * time.Second)).To(Equal("1s"))
			Expect(formatPrometheusDuration(30 * time.Second)).To(Equal("30s"))
			Expect(formatPrometheusDuration(90 * time.Second)).To(Equal("90s")) // Not 1.5m
		})

		It("should prefer larger units when possible", func() {
			Expect(formatPrometheusDuration(60 * time.Second)).To(Equal("1m"))
			Expect(formatPrometheusDuration(3600 * time.Second)).To(Equal("1h"))
		})

		It("should handle zero duration", func() {
			Expect(formatPrometheusDuration(0)).To(Equal("0s"))
		})

		It("should not lose precision for sub-minute periods", func() {
			Expect(formatPrometheusDuration(45 * time.Second)).To(Equal("45s"))
			Expect(formatPrometheusDuration(15 * time.Second)).To(Equal("15s"))
		})
	})
})
