package bptree

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Sharded vs Single Tree Comparison Benchmarks
// ============================================================================

// BenchmarkShardedVsSingle compares throughput of sharded vs single tree
func BenchmarkShardedVsSingle(b *testing.B) {
	scenarios := []struct {
		name      string
		numShards int
		readers   int
		writers   int
	}{
		{"Single_10R_1W", 1, 10, 1},
		{"4Shards_10R_1W", 4, 10, 1},
		{"8Shards_10R_1W", 8, 10, 1},
		{"16Shards_10R_1W", 16, 10, 1},
		{"Single_50R_10W", 1, 50, 10},
		{"4Shards_50R_10W", 4, 50, 10},
		{"8Shards_50R_10W", 8, 50, 10},
		{"16Shards_50R_10W", 16, 50, 10},
	}

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			tree := NewShardedBTree(ShardConfig{NumShards: sc.numShards})

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

			// Run for 2 seconds
			time.Sleep(2 * time.Second)
			close(done)
			wg.Wait()

			totalOps := atomic.LoadInt64(&ops)
			opsPerSec := float64(totalOps) / 2.0
			b.ReportMetric(opsPerSec, "ops/sec")
			b.ReportMetric(float64(sc.numShards), "shards")
		})
	}
}

// BenchmarkShardScaling measures how throughput scales with shard count
func BenchmarkShardScaling(b *testing.B) {
	shardCounts := []int{1, 2, 4, 8, 16, 32}

	for _, numShards := range shardCounts {
		b.Run(fmt.Sprintf("Shards_%d", numShards), func(b *testing.B) {
			tree := NewShardedBTree(ShardConfig{NumShards: numShards})

			// Pre-populate
			for i := 0; i < 10000; i++ {
				tree.Insert(Keytype(fmt.Sprintf("key-%08d", i)), Valuetype("val"))
			}

			numWorkers := 32 // Fixed worker count to see scaling
			var ops int64
			var wg sync.WaitGroup
			done := make(chan struct{})

			b.ResetTimer()

			// Mix of reads and writes
			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)))
					counter := 0
					for {
						select {
						case <-done:
							return
						default:
							if rng.Intn(10) < 7 { // 70% reads
								key := fmt.Sprintf("key-%08d", rng.Intn(10000))
								tree.Find(Keytype(key))
							} else { // 30% writes
								key := fmt.Sprintf("w%d-key-%08d", id, counter)
								tree.Insert(Keytype(key), Valuetype("val"))
								counter++
							}
							atomic.AddInt64(&ops, 1)
						}
					}
				}(w)
			}

			time.Sleep(2 * time.Second)
			close(done)
			wg.Wait()

			totalOps := atomic.LoadInt64(&ops)
			opsPerSec := float64(totalOps) / 2.0
			b.ReportMetric(opsPerSec, "ops/sec")
		})
	}
}

// BenchmarkShardedWriteOnly measures pure write throughput
func BenchmarkShardedWriteOnly(b *testing.B) {
	shardCounts := []int{1, 2, 4, 8, 16}

	for _, numShards := range shardCounts {
		b.Run(fmt.Sprintf("Shards_%d", numShards), func(b *testing.B) {
			tree := NewShardedBTree(ShardConfig{NumShards: numShards})

			numWriters := 16
			var ops int64
			var wg sync.WaitGroup
			done := make(chan struct{})

			b.ResetTimer()

			for w := 0; w < numWriters; w++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					counter := 0
					for {
						select {
						case <-done:
							return
						default:
							key := fmt.Sprintf("w%d-key-%08d", id, counter)
							tree.Insert(Keytype(key), Valuetype("val"))
							counter++
							atomic.AddInt64(&ops, 1)
						}
					}
				}(w)
			}

			time.Sleep(2 * time.Second)
			close(done)
			wg.Wait()

			totalOps := atomic.LoadInt64(&ops)
			opsPerSec := float64(totalOps) / 2.0
			b.ReportMetric(opsPerSec, "ops/sec")
		})
	}
}

// BenchmarkShardedReadOnly measures pure read throughput
func BenchmarkShardedReadOnly(b *testing.B) {
	shardCounts := []int{1, 2, 4, 8, 16}

	for _, numShards := range shardCounts {
		b.Run(fmt.Sprintf("Shards_%d", numShards), func(b *testing.B) {
			tree := NewShardedBTree(ShardConfig{NumShards: numShards})

			// Pre-populate
			for i := 0; i < 100000; i++ {
				tree.Insert(Keytype(fmt.Sprintf("key-%08d", i)), Valuetype("val"))
			}

			numReaders := 32
			var ops int64
			var wg sync.WaitGroup
			done := make(chan struct{})

			b.ResetTimer()

			for r := 0; r < numReaders; r++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)))
					for {
						select {
						case <-done:
							return
						default:
							key := fmt.Sprintf("key-%08d", rng.Intn(100000))
							tree.Find(Keytype(key))
							atomic.AddInt64(&ops, 1)
						}
					}
				}(r)
			}

			time.Sleep(2 * time.Second)
			close(done)
			wg.Wait()

			totalOps := atomic.LoadInt64(&ops)
			opsPerSec := float64(totalOps) / 2.0
			b.ReportMetric(opsPerSec, "ops/sec")
		})
	}
}

// BenchmarkShardedRangeQuery measures range query performance
func BenchmarkShardedRangeQuery(b *testing.B) {
	shardCounts := []int{1, 4, 8, 16}

	for _, numShards := range shardCounts {
		b.Run(fmt.Sprintf("Shards_%d", numShards), func(b *testing.B) {
			tree := NewShardedBTree(ShardConfig{NumShards: numShards})

			// Pre-populate
			for i := 0; i < 10000; i++ {
				tree.Insert(Keytype(fmt.Sprintf("key-%08d", i)), Valuetype("val"))
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				start := i % 9000
				end := start + 100
				startKey := fmt.Sprintf("key-%08d", start)
				endKey := fmt.Sprintf("key-%08d", end)
				tree.GetRange([]byte(startKey), []byte(endKey))
			}
		})
	}
}

// BenchmarkShardedBulkInsert measures bulk insert performance
func BenchmarkShardedBulkInsert(b *testing.B) {
	shardCounts := []int{1, 4, 8, 16}
	batchSize := 1000

	for _, numShards := range shardCounts {
		b.Run(fmt.Sprintf("Shards_%d", numShards), func(b *testing.B) {
			keys := make([]Keytype, batchSize)
			values := make([]Valuetype, batchSize)
			for i := 0; i < batchSize; i++ {
				keys[i] = Keytype(fmt.Sprintf("key-%08d", i))
				values[i] = Valuetype("val")
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				tree := NewShardedBTree(ShardConfig{NumShards: numShards})
				tree.BulkInsert(keys, values)
			}
		})
	}
}

// ============================================================================
// Latency Benchmarks
// ============================================================================

// BenchmarkShardedLatency measures operation latencies
func BenchmarkShardedLatency(b *testing.B) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	// Pre-populate
	for i := 0; i < 100000; i++ {
		tree.Insert(Keytype(fmt.Sprintf("key-%08d", i)), Valuetype("val"))
	}

	b.Run("Insert", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("new-key-%08d", i)
			tree.Insert(Keytype(key), Valuetype("val"))
		}
	})

	b.Run("Find", func(b *testing.B) {
		rng := rand.New(rand.NewSource(42))
		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key-%08d", rng.Intn(100000))
			tree.Find(Keytype(key))
		}
	})

	b.Run("Delete", func(b *testing.B) {
		// Re-populate for delete test
		for i := 0; i < b.N; i++ {
			tree.Insert(Keytype(fmt.Sprintf("del-key-%08d", i)), Valuetype("val"))
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tree.Delete(Keytype(fmt.Sprintf("del-key-%08d", i)))
		}
	})
}

// ============================================================================
// Contention Benchmarks
// ============================================================================

// BenchmarkShardContention measures performance under high contention
func BenchmarkShardContention(b *testing.B) {
	scenarios := []struct {
		name      string
		numShards int
		hotKeys   int // Number of "hot" keys all goroutines access
	}{
		{"1Shard_HighContention", 1, 10},
		{"8Shards_HighContention", 8, 10},
		{"1Shard_LowContention", 1, 10000},
		{"8Shards_LowContention", 8, 10000},
	}

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			tree := NewShardedBTree(ShardConfig{NumShards: sc.numShards})

			// Pre-populate with hot keys
			for i := 0; i < sc.hotKeys; i++ {
				tree.Insert(Keytype(fmt.Sprintf("hot-%08d", i)), Valuetype("val"))
			}

			numWorkers := 16
			var ops int64
			var wg sync.WaitGroup
			done := make(chan struct{})

			b.ResetTimer()

			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)))
					for {
						select {
						case <-done:
							return
						default:
							key := fmt.Sprintf("hot-%08d", rng.Intn(sc.hotKeys))
							if rng.Intn(10) < 8 { // 80% reads
								tree.Find(Keytype(key))
							} else { // 20% writes
								tree.Insert(Keytype(key), Valuetype("updated"))
							}
							atomic.AddInt64(&ops, 1)
						}
					}
				}(w)
			}

			time.Sleep(2 * time.Second)
			close(done)
			wg.Wait()

			totalOps := atomic.LoadInt64(&ops)
			opsPerSec := float64(totalOps) / 2.0
			b.ReportMetric(opsPerSec, "ops/sec")
		})
	}
}

// ============================================================================
// Distribution Quality Benchmark
// ============================================================================

// BenchmarkHashDistribution verifies hash function distribution
func BenchmarkHashDistribution(b *testing.B) {
	numShards := 16
	numKeys := 1000000

	b.Run("FNV32a", func(b *testing.B) {
		counts := make([]int64, numShards)

		for i := 0; i < numKeys; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			hash := fnv32a(key)
			shard := hash % uint32(numShards)
			counts[shard]++
		}

		// Calculate skew
		expected := float64(numKeys) / float64(numShards)
		var maxDiff float64
		for _, count := range counts {
			diff := float64(count) - expected
			if diff < 0 {
				diff = -diff
			}
			if diff > maxDiff {
				maxDiff = diff
			}
		}

		skewPercent := (maxDiff / expected) * 100
		b.ReportMetric(skewPercent, "max_skew_%")
	})
}
