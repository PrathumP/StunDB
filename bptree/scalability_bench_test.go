package bptree

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkCurrentThroughput measures ops/sec at different concurrency levels
func BenchmarkCurrentThroughput(b *testing.B) {
	scenarios := []struct {
		name      string
		readers   int
		writers   int
		readRatio float64 // percentage of reads
	}{
		{"ReadHeavy_10R_1W", 10, 1, 0.91},
		{"ReadHeavy_50R_1W", 50, 1, 0.98},
		{"Mixed_10R_10W", 10, 10, 0.50},
		{"WriteHeavy_1R_10W", 1, 10, 0.09},
		{"WriteHeavy_1R_50W", 1, 50, 0.02},
		{"HighConcurrency_100R_100W", 100, 100, 0.50},
	}

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			tree := &Btree{}

			// Pre-populate with 10K keys
			for i := 0; i < 10000; i++ {
				key := fmt.Sprintf("key-%08d", i)
				tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", i)))
			}

			b.ResetTimer()

			var ops int64
			var wg sync.WaitGroup
			done := make(chan struct{})

			// Start readers
			for i := 0; i < sc.readers; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)))
					for {
						select {
						case <-done:
							return
						default:
							key := fmt.Sprintf("key-%08d", rng.Intn(10000))
							tree.Find(Keytype(key))
							atomic.AddInt64(&ops, 1)
						}
					}
				}(i)
			}

			// Start writers
			for i := 0; i < sc.writers; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					counter := 0
					for {
						select {
						case <-done:
							return
						default:
							key := fmt.Sprintf("writer-%d-key-%08d", id, counter)
							tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", counter)))
							counter++
							atomic.AddInt64(&ops, 1)
						}
					}
				}(i)
			}

			// Run for fixed duration
			time.Sleep(2 * time.Second)
			close(done)
			wg.Wait()

			totalOps := atomic.LoadInt64(&ops)
			opsPerSec := float64(totalOps) / 2.0
			b.ReportMetric(opsPerSec, "ops/sec")
			b.ReportMetric(float64(sc.readers), "readers")
			b.ReportMetric(float64(sc.writers), "writers")
		})
	}
}

// BenchmarkLatencyDistribution measures p50, p99, p999 latencies
func BenchmarkLatencyDistribution(b *testing.B) {
	tree := &Btree{}

	// Pre-populate
	for i := 0; i < 100000; i++ {
		key := fmt.Sprintf("key-%08d", i)
		tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", i)))
	}

	b.Run("ReadLatency", func(b *testing.B) {
		latencies := make([]time.Duration, b.N)
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%08d", rng.Intn(100000))
			start := time.Now()
			tree.Find(Keytype(key))
			latencies[i] = time.Since(start)
		}
		b.StopTimer()

		// Sort and report percentiles
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		if len(latencies) > 0 {
			b.ReportMetric(float64(latencies[len(latencies)*50/100].Nanoseconds()), "p50_ns")
			b.ReportMetric(float64(latencies[len(latencies)*99/100].Nanoseconds()), "p99_ns")
		}
	})

	b.Run("WriteLatency", func(b *testing.B) {
		latencies := make([]time.Duration, b.N)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("bench-key-%08d", i)
			start := time.Now()
			tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", i)))
			latencies[i] = time.Since(start)
		}
		b.StopTimer()

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		if len(latencies) > 0 {
			b.ReportMetric(float64(latencies[len(latencies)*50/100].Nanoseconds()), "p50_ns")
			b.ReportMetric(float64(latencies[len(latencies)*99/100].Nanoseconds()), "p99_ns")
		}
	})
}

// BenchmarkScalingLimits finds where performance degrades
func BenchmarkScalingLimits(b *testing.B) {
	treeSizes := []int{1000, 10000, 100000, 1000000}

	for _, size := range treeSizes {
		b.Run(fmt.Sprintf("TreeSize_%d", size), func(b *testing.B) {
			tree := &Btree{}

			// Populate
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%08d", i)
				tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", i)))
			}

			rng := rand.New(rand.NewSource(42))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key-%08d", rng.Intn(size))
				tree.Find(Keytype(key))
			}
		})
	}
}

// BenchmarkWriteContention measures write throughput under contention
func BenchmarkWriteContention(b *testing.B) {
	writerCounts := []int{1, 2, 4, 8, 16, 32, 64}

	for _, writers := range writerCounts {
		b.Run(fmt.Sprintf("Writers_%d", writers), func(b *testing.B) {
			tree := &Btree{}
			var wg sync.WaitGroup
			done := make(chan struct{})

			opsPerWriter := b.N / writers
			if opsPerWriter < 1 {
				opsPerWriter = 1
			}

			b.ResetTimer()

			for w := 0; w < writers; w++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for i := 0; i < opsPerWriter; i++ {
						select {
						case <-done:
							return
						default:
							key := fmt.Sprintf("w%d-key-%08d", id, i)
							tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", i)))
						}
					}
				}(w)
			}

			wg.Wait()
			close(done)
		})
	}
}
