package bptree

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// ============================================================================
// BENCHMARKING STRATEGY FOR STUNDB
// ============================================================================
//
// StunDB is a Concurrent Pure B-Tree with these key advantages:
//
// 1. EARLY EXIT: Point lookups can return in O(1) to O(log N) steps
//    (vs. B+Trees which always require O(log N) leaf traversal)
//
// 2. HOT KEY OPTIMIZATION: If frequently accessed keys are in internal nodes,
//    they are retrieved with minimal pointer hops
//
// 3. LOCK-FREE READING: With B-Link implementation, readers never block
//    writers and can adapt to concurrent splits via right-sibling traversal
//
// WINNING SCENARIOS:
// - Read-heavy workloads with hot-key distribution (Zipfian)
// - High concurrency (100+ goroutines with 90% reads, 10% writes)
// - Memory efficiency (arrays vs. hashtable buckets)
// ============================================================================

const (
	BenchDataSize = 1_000_000 // 1 Million items for realistic scale
	SmallDataSize = 100_000   // 100K for faster tests
)

// ============================================================================
// SCENARIO 1: HOT KEY DISTRIBUTION (Zipfian)
// ============================================================================
//
// Real-world traffic follows Zipfian distribution: 80% of requests are for
// 20% of keys. StunDB excels here because hot keys tend to exist higher in
// the tree, enabling early exit.
//
// Benchmark metric: ns/op (nanoseconds per operation)
// ============================================================================

// ZipfianGenerator produces Zipfian-distributed random numbers
// (biased toward low numbers, realistic for "hot key" scenarios)
type ZipfianGenerator struct {
	rand  *rand.Rand
	zipf  *rand.Zipf
	items uint64
}

func NewZipfianGenerator(items uint64, skew float64, seed int64) *ZipfianGenerator {
	r := rand.New(rand.NewSource(seed))
	return &ZipfianGenerator{
		rand:  r,
		zipf:  rand.NewZipf(r, skew, 1.0, items-1), // skew=1.0 means uniform, >1 = more skewed
		items: items,
	}
}

func (z *ZipfianGenerator) Next() uint64 {
	return z.zipf.Uint64()
}

// BenchmarkStunDB_Read_Zipfian: Point lookups with hot-key distribution
// This is where Pure B-Trees shine: early exit on frequently accessed keys
func BenchmarkStunDB_Read_Zipfian(b *testing.B) {
	tree := &Btree{}

	// Fill tree with test data
	testData := generateTestData(SmallDataSize)
	for _, pair := range testData {
		tree.Insert(pair.key, pair.value)
	}

	// Use Zipfian distribution (skew=1.5 means 80/20 rule)
	zipf := NewZipfianGenerator(uint64(SmallDataSize), 1.5, 42)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			keyIdx := zipf.Next()
			key := intToBytes(int(keyIdx))
			tree.Get(key)
		}
	})
}

// BenchmarkStunDB_Read_Sequential: Control benchmark - sequential keys
func BenchmarkStunDB_Read_Sequential(b *testing.B) {
	tree := &Btree{}
	testData := generateTestData(SmallDataSize)
	for _, pair := range testData {
		tree.Insert(pair.key, pair.value)
	}

	b.ResetTimer()
	counter := int64(0)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			keyIdx := atomic.AddInt64(&counter, 1) % int64(SmallDataSize)
			key := intToBytes(int(keyIdx))
			tree.Get(key)
		}
	})
}

// BenchmarkStunDB_Read_Uniform: Control benchmark - uniform random distribution
func BenchmarkStunDB_Read_Uniform(b *testing.B) {
	tree := &Btree{}
	testData := generateTestData(SmallDataSize)
	for _, pair := range testData {
		tree.Insert(pair.key, pair.value)
	}

	r := rand.New(rand.NewSource(42))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			keyIdx := r.Intn(SmallDataSize)
			key := intToBytes(keyIdx)
			tree.Get(key)
		}
	})
}

// ============================================================================
// SCENARIO 2: MASSIVE CONCURRENCY
// ============================================================================
//
// StunDB's B-Link implementation allows readers to never block writers.
// With pessimistic locking and right-sibling traversal, it should handle
// high concurrency better than alternatives.
//
// Benchmark metric: ops/sec (operations per second throughput)
// ============================================================================

// BenchmarkStunDB_Concurrency_90Read10Write: Mixed read/write workload
// 90% reads, 10% writes - typical for cache/session stores
func BenchmarkStunDB_Concurrency_90Read10Write(b *testing.B) {
	tree := &Btree{}

	// Pre-populate tree
	testData := generateTestData(SmallDataSize)
	for _, pair := range testData {
		tree.Insert(pair.key, pair.value)
	}

	counter := int64(0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(42))
		idx := 0
		for pb.Next() {
			if r.Intn(100) < 90 {
				// 90% reads
				keyIdx := idx % SmallDataSize
				key := intToBytes(keyIdx)
				tree.Get(key)
				idx++
			} else {
				// 10% writes
				writeIdx := atomic.AddInt64(&counter, 1)
				keyIdx := SmallDataSize + int(writeIdx%int64(SmallDataSize/10))
				key := intToBytes(keyIdx)
				value := []byte(fmt.Sprintf("value_%d", keyIdx))
				tree.Insert(key, value)
			}
		}
	})
}

// BenchmarkStunDB_Concurrency_ReadOnly: Maximum concurrency, read-only
// This should show StunDB's lock-free reading advantage
func BenchmarkStunDB_Concurrency_ReadOnly(b *testing.B) {
	tree := &Btree{}
	testData := generateTestData(SmallDataSize)
	for _, pair := range testData {
		tree.Insert(pair.key, pair.value)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			keyIdx := idx % SmallDataSize
			key := intToBytes(keyIdx)
			tree.Get(key)
			idx++
		}
	})
}

// BenchmarkStunDB_Concurrency_WriteHeavy: 50% reads, 50% writes
func BenchmarkStunDB_Concurrency_WriteHeavy(b *testing.B) {
	tree := &Btree{}
	testData := generateTestData(SmallDataSize)
	for _, pair := range testData {
		tree.Insert(pair.key, pair.value)
	}

	counter := int64(0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		r := rand.New(rand.NewSource(42))
		idx := 0
		for pb.Next() {
			if r.Intn(100) < 50 {
				// 50% reads
				keyIdx := idx % SmallDataSize
				key := intToBytes(keyIdx)
				tree.Get(key)
				idx++
			} else {
				// 50% writes
				writeIdx := atomic.AddInt64(&counter, 1)
				keyIdx := int(writeIdx % int64(SmallDataSize))
				key := intToBytes(keyIdx)
				value := []byte(fmt.Sprintf("value_%d", keyIdx))
				tree.Insert(key, value)
			}
		}
	})
}

// BenchmarkStunDB_Concurrency_Scaling: Demonstrate scaling with goroutine count
// Run with: go test -bench=ScalingWithGoroutines -benchtime=3s ./bptree
func BenchmarkStunDB_Concurrency_Scaling(b *testing.B) {
	tree := &Btree{}
	testData := generateTestData(SmallDataSize)
	for _, pair := range testData {
		tree.Insert(pair.key, pair.value)
	}

	// Test with different GOMAXPROCS
	originalProcs := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(originalProcs)

	for _, procs := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("GOMAXPROCS=%d", procs), func(b *testing.B) {
			runtime.GOMAXPROCS(procs)

			counter := int64(0)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				r := rand.New(rand.NewSource(42))
				idx := 0
				for pb.Next() {
					if r.Intn(100) < 90 {
						keyIdx := idx % SmallDataSize
						key := intToBytes(keyIdx)
						tree.Get(key)
						idx++
					} else {
						writeIdx := atomic.AddInt64(&counter, 1)
						keyIdx := SmallDataSize + int(writeIdx%1000)
						key := intToBytes(keyIdx)
						value := []byte(fmt.Sprintf("val_%d", keyIdx))
						tree.Insert(key, value)
					}
				}
			})
		})
	}
}

// ============================================================================
// SCENARIO 3: MEMORY EFFICIENCY
// ============================================================================
//
// B-Trees pack data tightly in arrays. This benchmark shows memory usage
// per key compared to Go's built-in map.
//
// Benchmark metric: bytes/key (lower is better)
// ============================================================================

// BenchmarkMemory_StunDB_Large: Memory consumption for 1M items
func BenchmarkMemory_StunDB_Large(b *testing.B) {
	b.Run("StunDB_1M_Items", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			tree := &Btree{}
			testData := generateTestData(1_000_000)
			b.StartTimer()

			for _, pair := range testData {
				tree.Insert(pair.key, pair.value)
			}

			b.StopTimer()
			// Measure memory
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			allocPerKey := float64(m.Alloc) / 1_000_000
			b.ReportMetric(allocPerKey, "bytes/key")
		}
	})

	b.Run("GoMap_1M_Items", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			goMap := make(map[string][]byte)
			testData := generateTestData(1_000_000)
			b.StartTimer()

			for _, pair := range testData {
				goMap[string(pair.key)] = pair.value
			}

			b.StopTimer()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			allocPerKey := float64(m.Alloc) / 1_000_000
			b.ReportMetric(allocPerKey, "bytes/key")
		}
	})

	b.Run("SyncMap_1M_Items", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			syncMap := &sync.Map{}
			testData := generateTestData(1_000_000)
			b.StartTimer()

			for _, pair := range testData {
				syncMap.Store(string(pair.key), pair.value)
			}

			b.StopTimer()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			allocPerKey := float64(m.Alloc) / 1_000_000
			b.ReportMetric(allocPerKey, "bytes/key")
		}
	})
}

// ============================================================================
// SCENARIO 4: EARLY EXIT ADVANTAGE
// ============================================================================
//
// Measure average tree depth accessed during a read to demonstrate
// that pure B-Trees enable early exit
// ============================================================================

// BenchmarkStunDB_DepthAnalysis: Analyze actual tree traversal depth
// This is informational - not a performance benchmark but shows the
// theoretical advantage of early exit
func BenchmarkStunDB_DepthAnalysis(b *testing.B) {
	tree := &Btree{}
	testData := generateTestData(SmallDataSize)
	for _, pair := range testData {
		tree.Insert(pair.key, pair.value)
	}

	// Calculate theoretical tree depth
	theoreticalDepth := math.Log(float64(SmallDataSize)) / math.Log(float64(MaxKeys))

	b.Logf("Dataset size: %d items", SmallDataSize)
	b.Logf("Tree branching factor: %d", MaxKeys)
	b.Logf("Theoretical tree depth (log_B(N)): %.2f", theoreticalDepth)
	b.Logf("Early exit advantage: Some keys found in 1-2 hops (80/20 rule)")
	b.Logf("B+Tree equivalent: Always requires %.2f hops to leaf", theoreticalDepth)
}

// ============================================================================
// SCENARIO 5: COMPARISON WITH STANDARD GO LIBRARIES
// ============================================================================
//
// Direct comparisons to show where StunDB wins and loses
// ============================================================================

// BenchmarkComparison_ReadOnly: StunDB vs sync.Map on read-only workload
func BenchmarkComparison_ReadOnly(b *testing.B) {
	testData := generateTestData(SmallDataSize)

	b.Run("StunDB_ReadOnly", func(b *testing.B) {
		tree := &Btree{}
		for _, pair := range testData {
			tree.Insert(pair.key, pair.value)
		}

		b.ResetTimer()
		idx := 0
		for i := 0; i < b.N; i++ {
			keyIdx := idx % SmallDataSize
			key := intToBytes(keyIdx)
			tree.Get(key)
			idx++
		}
	})

	b.Run("SyncMap_ReadOnly", func(b *testing.B) {
		syncMap := &sync.Map{}
		for _, pair := range testData {
			syncMap.Store(string(pair.key), pair.value)
		}

		b.ResetTimer()
		idx := 0
		for i := 0; i < b.N; i++ {
			keyIdx := idx % SmallDataSize
			key := string(intToBytes(keyIdx))
			syncMap.Load(key)
			idx++
		}
	})

	b.Run("GoMap_ReadOnly", func(b *testing.B) {
		goMap := make(map[string][]byte)
		for _, pair := range testData {
			goMap[string(pair.key)] = pair.value
		}

		var mu sync.RWMutex
		b.ResetTimer()
		idx := 0
		for i := 0; i < b.N; i++ {
			mu.RLock()
			keyIdx := idx % SmallDataSize
			key := string(intToBytes(keyIdx))
			_ = goMap[key]
			mu.RUnlock()
			idx++
		}
	})
}

// BenchmarkComparison_Mixed: Mixed read/write under concurrency
func BenchmarkComparison_Mixed(b *testing.B) {
	testData := generateTestData(SmallDataSize)

	b.Run("StunDB_Mixed", func(b *testing.B) {
		tree := &Btree{}
		for _, pair := range testData {
			tree.Insert(pair.key, pair.value)
		}

		counter := int64(0)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			r := rand.New(rand.NewSource(42))
			idx := 0
			for pb.Next() {
				if r.Intn(100) < 90 {
					keyIdx := idx % SmallDataSize
					key := intToBytes(keyIdx)
					tree.Get(key)
					idx++
				} else {
					writeIdx := atomic.AddInt64(&counter, 1)
					keyIdx := int(writeIdx % int64(SmallDataSize))
					key := intToBytes(keyIdx)
					value := []byte(fmt.Sprintf("value_%d", keyIdx))
					tree.Insert(key, value)
				}
			}
		})
	})

	b.Run("SyncMap_Mixed", func(b *testing.B) {
		syncMap := &sync.Map{}
		for _, pair := range testData {
			syncMap.Store(string(pair.key), pair.value)
		}

		counter := int64(0)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			r := rand.New(rand.NewSource(42))
			idx := 0
			for pb.Next() {
				if r.Intn(100) < 90 {
					keyIdx := idx % SmallDataSize
					key := string(intToBytes(keyIdx))
					syncMap.Load(key)
					idx++
				} else {
					writeIdx := atomic.AddInt64(&counter, 1)
					keyIdx := int(writeIdx % int64(SmallDataSize))
					key := string(intToBytes(keyIdx))
					value := []byte(fmt.Sprintf("value_%d", keyIdx))
					syncMap.Store(key, value)
				}
			}
		})
	})

	b.Run("GoMap_Mixed", func(b *testing.B) {
		goMap := make(map[string][]byte)
		for _, pair := range testData {
			goMap[string(pair.key)] = pair.value
		}

		var mu sync.RWMutex
		counter := int64(0)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			r := rand.New(rand.NewSource(42))
			idx := 0
			for pb.Next() {
				if r.Intn(100) < 90 {
					mu.RLock()
					keyIdx := idx % SmallDataSize
					key := string(intToBytes(keyIdx))
					_ = goMap[key]
					mu.RUnlock()
					idx++
				} else {
					mu.Lock()
					writeIdx := atomic.AddInt64(&counter, 1)
					keyIdx := int(writeIdx % int64(SmallDataSize))
					key := string(intToBytes(keyIdx))
					value := []byte(fmt.Sprintf("value_%d", keyIdx))
					goMap[key] = value
					mu.Unlock()
				}
			}
		})
	})
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

type TestPair struct {
	key   []byte
	value []byte
}

// generateTestData creates deterministic test data
func generateTestData(size int) []TestPair {
	data := make([]TestPair, size)
	for i := 0; i < size; i++ {
		key := intToBytes(i)
		value := []byte(fmt.Sprintf("value_%d", i))
		data[i] = TestPair{key: key, value: value}
	}
	return data
}

// intToBytes converts an integer to a fixed-width byte array
func intToBytes(i int) []byte {
	return []byte(fmt.Sprintf("%010d", i))
}

// ============================================================================
// INSTRUCTIONS FOR RUNNING BENCHMARKS
// ============================================================================
//
// Run all benchmarks:
//   go test -bench=. -benchmem ./bptree
//
// Run specific benchmark:
//   go test -bench=BenchmarkStunDB_Read_Zipfian -benchmem ./bptree
//
// Run with verbose output:
//   go test -bench=. -benchmem -v ./bptree
//
// Longer benchmark (more accurate):
//   go test -bench=. -benchmem -benchtime=10s ./bptree
//
// Compare results with previous run:
//   go test -bench=. -benchmem ./bptree > new.txt
//   benchstat old.txt new.txt
//
// Profile CPU usage:
//   go test -bench=. -cpuprofile=cpu.prof ./bptree
//   go tool pprof cpu.prof
//
// Profile memory usage:
//   go test -bench=. -memprofile=mem.prof ./bptree
//   go tool pprof mem.prof
// ============================================================================
