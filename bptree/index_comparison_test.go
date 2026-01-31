package bptree

import (
	"bytes"
	"fmt"
	"testing"
	"time"
)

// This file contains benchmarks comparing indexed vs non-indexed lookups
// to demonstrate the real-world performance benefits of secondary indexes.

// BenchmarkComparisonFullScan measures finding a record by scanning ALL records
// This is what you'd have to do WITHOUT a secondary index
func BenchmarkComparisonFullScan(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size%d", size), func(b *testing.B) {
			tree := NewShardedBTree(ShardConfig{NumShards: 16})

			// Insert records with JSON values
			for i := 0; i < size; i++ {
				key := Keytype(fmt.Sprintf("user:%06d", i))
				value := Valuetype(fmt.Sprintf(`{"id":%d,"email":"user%d@example.com","age":%d}`, i, i, 20+(i%50)))
				tree.Insert(key, value)
			}

			// Search for a specific email (in the middle of the dataset)
			targetEmail := []byte(fmt.Sprintf("user%d@example.com", size/2))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Full scan: iterate through ALL records to find matching email
				found := false
				tree.ForEach(func(key Keytype, value Valuetype) bool {
					if bytes.Contains(value, targetEmail) {
						found = true
						return false // stop iteration
					}
					return true // continue
				})
				if !found {
					b.Fatal("Record not found")
				}
			}
		})
	}
}

// BenchmarkComparisonIndexedLookup measures finding a record using secondary index
// This is the O(log n) lookup WITH a secondary index
func BenchmarkComparisonIndexedLookup(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size%d", size), func(b *testing.B) {
			tree := NewIndexedBTree(IndexedConfig{NumShards: 16})

			// Create email index
			tree.CreateIndex("email", JSONFieldExtractor("email"), true)

			// Insert records with JSON values
			for i := 0; i < size; i++ {
				key := Keytype(fmt.Sprintf("user:%06d", i))
				value := Valuetype(fmt.Sprintf(`{"id":%d,"email":"user%d@example.com","age":%d}`, i, i, 20+(i%50)))
				tree.Insert(key, value)
			}

			// Search for a specific email (in the middle of the dataset)
			targetEmail := []byte(fmt.Sprintf("user%d@example.com", size/2))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Indexed lookup: O(log n) to find primary key, O(log n) to get value
				pk, err := tree.FindByIndex("email", targetEmail)
				if err != nil || pk == nil {
					b.Fatal("Record not found")
				}
				// Optionally fetch the actual value
				_, _ = tree.Get(pk)
			}
		})
	}
}

// BenchmarkComparisonRangeScan measures range queries without index
func BenchmarkComparisonRangeScan(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size%d", size), func(b *testing.B) {
			tree := NewShardedBTree(ShardConfig{NumShards: 16})

			// Insert records
			for i := 0; i < size; i++ {
				key := Keytype(fmt.Sprintf("user:%06d", i))
				value := Valuetype(fmt.Sprintf(`{"id":%d,"age":%d}`, i, 20+(i%50)))
				tree.Insert(key, value)
			}

			// Find all users with age 25-30 (about 12% of records)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				results := make([]Keytype, 0)
				tree.ForEach(func(key Keytype, value Valuetype) bool {
					// Parse age from JSON (simplified)
					if bytes.Contains(value, []byte(`"age":25`)) ||
						bytes.Contains(value, []byte(`"age":26`)) ||
						bytes.Contains(value, []byte(`"age":27`)) ||
						bytes.Contains(value, []byte(`"age":28`)) ||
						bytes.Contains(value, []byte(`"age":29`)) ||
						bytes.Contains(value, []byte(`"age":30`)) {
						results = append(results, key)
					}
					return true
				})
				_ = results
			}
		})
	}
}

// BenchmarkComparisonIndexedRange measures range queries WITH index
func BenchmarkComparisonIndexedRange(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size%d", size), func(b *testing.B) {
			tree := NewIndexedBTree(IndexedConfig{NumShards: 16})

			// Create age index
			tree.CreateIndex("age", JSONFieldExtractor("age"), false)

			// Insert records
			for i := 0; i < size; i++ {
				key := Keytype(fmt.Sprintf("user:%06d", i))
				value := Valuetype(fmt.Sprintf(`{"id":%d,"age":%d}`, i, 20+(i%50)))
				tree.Insert(key, value)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Use index range query
				results, _ := tree.FindRangeByIndex("age", []byte("25"), []byte("31"))
				_ = results
			}
		})
	}
}

// TestComparisonDemo runs a simple comparison and prints results
func TestComparisonDemo(t *testing.T) {
	sizes := []int{1000, 10000, 50000}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("Size%d", size), func(t *testing.T) {
			// Setup without index
			treeNoIndex := NewShardedBTree(ShardConfig{NumShards: 16})
			for i := 0; i < size; i++ {
				key := Keytype(fmt.Sprintf("user:%06d", i))
				value := Valuetype(fmt.Sprintf(`{"id":%d,"email":"user%d@example.com"}`, i, i))
				treeNoIndex.Insert(key, value)
			}

			// Setup with index
			treeWithIndex := NewIndexedBTree(IndexedConfig{NumShards: 16})
			treeWithIndex.CreateIndex("email", JSONFieldExtractor("email"), true)
			for i := 0; i < size; i++ {
				key := Keytype(fmt.Sprintf("user:%06d", i))
				value := Valuetype(fmt.Sprintf(`{"id":%d,"email":"user%d@example.com"}`, i, i))
				treeWithIndex.Insert(key, value)
			}

			targetEmail := []byte(fmt.Sprintf("user%d@example.com", size/2))

			// Time full scan
			iterations := 100
			scanStart := time.Now()
			for i := 0; i < iterations; i++ {
				treeNoIndex.ForEach(func(key Keytype, value Valuetype) bool {
					if bytes.Contains(value, targetEmail) {
						return false
					}
					return true
				})
			}
			scanDuration := time.Since(scanStart)

			// Time indexed lookup
			indexStart := time.Now()
			for i := 0; i < iterations; i++ {
				pk, _ := treeWithIndex.FindByIndex("email", targetEmail)
				_, _ = treeWithIndex.Get(pk)
			}
			indexDuration := time.Since(indexStart)

			t.Logf("Dataset size: %d records", size)
			t.Logf("Full scan avg: %v", scanDuration/time.Duration(iterations))
			t.Logf("Indexed avg:   %v", indexDuration/time.Duration(iterations))
			t.Logf("Speedup:       %.1fx faster", float64(scanDuration)/float64(indexDuration))
		})
	}
}
