package bptree

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// Basic Functionality Tests
// ============================================================================

func TestShardedBTreeCreation(t *testing.T) {
	tests := []struct {
		name       string
		numShards  int
		wantShards int
	}{
		{"default shards", 0, 0}, // Will use runtime.NumCPU()
		{"1 shard", 1, 1},
		{"4 shards", 4, 4},
		{"8 shards", 8, 8},
		{"16 shards", 16, 16},
		{"32 shards", 32, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := NewShardedBTree(ShardConfig{NumShards: tt.numShards})
			if tt.wantShards == 0 {
				// Default case - just check it's positive
				if tree.NumShards() <= 0 {
					t.Errorf("Expected positive shard count, got %d", tree.NumShards())
				}
			} else if tree.NumShards() != tt.wantShards {
				t.Errorf("NumShards() = %d, want %d", tree.NumShards(), tt.wantShards)
			}
		})
	}
}

func TestShardedBTreeInsertAndFind(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Insert some keys
	testData := map[string]string{
		"key1":   "value1",
		"key2":   "value2",
		"key3":   "value3",
		"apple":  "fruit",
		"banana": "yellow",
		"cherry": "red",
	}

	for k, v := range testData {
		tree.Insert(Keytype(k), Valuetype(v))
	}

	// Verify all keys can be found
	for k, expectedV := range testData {
		v, err := tree.Find(Keytype(k))
		if err != nil {
			t.Errorf("Key %q not found: %v", k, err)
			continue
		}
		if string(v) != expectedV {
			t.Errorf("Key %q: got value %q, want %q", k, string(v), expectedV)
		}
	}

	// Verify non-existent key returns error
	_, err := tree.Find(Keytype("nonexistent"))
	if err == nil {
		t.Error("Expected non-existent key to return error")
	}
}

func TestShardedBTreePutAndGet(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Test Put (alias for Insert)
	tree.Put(Keytype("test"), Valuetype("value"))

	// Test Get (alias for Find)
	v, err := tree.Get(Keytype("test"))
	if err != nil {
		t.Errorf("Get should find inserted key: %v", err)
	}
	if string(v) != "value" {
		t.Errorf("Get returned %q, want %q", string(v), "value")
	}
}

func TestShardedBTreeDelete(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Insert keys
	keys := []string{"a", "b", "c", "d", "e"}
	for _, k := range keys {
		tree.Insert(Keytype(k), Valuetype("val-"+k))
	}

	// Delete some keys
	deleted := tree.Delete(Keytype("b"))
	if !deleted {
		t.Error("Delete should return true for existing key")
	}

	deleted = tree.Delete(Keytype("d"))
	if !deleted {
		t.Error("Delete should return true for existing key")
	}

	// Verify deleted keys are gone
	if _, err := tree.Find(Keytype("b")); err == nil {
		t.Error("Key 'b' should not be found after delete")
	}
	if _, err := tree.Find(Keytype("d")); err == nil {
		t.Error("Key 'd' should not be found after delete")
	}

	// Verify other keys still exist
	for _, k := range []string{"a", "c", "e"} {
		if _, err := tree.Find(Keytype(k)); err != nil {
			t.Errorf("Key %q should still exist: %v", k, err)
		}
	}

	// Delete non-existent key
	deleted = tree.Delete(Keytype("nonexistent"))
	if deleted {
		t.Error("Delete should return false for non-existent key")
	}
}

func TestShardedBTreeGetRange(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Insert keys
	testKeys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	for _, k := range testKeys {
		tree.Insert(Keytype(k), Valuetype("val-"+k))
	}

	tests := []struct {
		name     string
		start    string
		end      string
		wantKeys []string
	}{
		{"full range", "a", "j", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}},
		{"partial range", "c", "f", []string{"c", "d", "e", "f"}},
		{"single key", "d", "d", []string{"d"}},
		{"start of range", "a", "c", []string{"a", "b", "c"}},
		{"end of range", "h", "j", []string{"h", "i", "j"}},
		{"beyond range", "x", "z", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, values, err := tree.GetRange([]byte(tt.start), []byte(tt.end))
			if err != nil {
				t.Fatalf("GetRange error: %v", err)
			}

			if len(keys) != len(tt.wantKeys) {
				t.Errorf("GetRange returned %d keys, want %d", len(keys), len(tt.wantKeys))
				return
			}

			// Check keys are in sorted order
			for i := 0; i < len(keys); i++ {
				if string(keys[i]) != tt.wantKeys[i] {
					t.Errorf("keys[%d] = %q, want %q", i, string(keys[i]), tt.wantKeys[i])
				}
				expectedVal := "val-" + tt.wantKeys[i]
				if string(values[i]) != expectedVal {
					t.Errorf("values[%d] = %q, want %q", i, string(values[i]), expectedVal)
				}
			}
		})
	}
}

func TestShardedBTreeGetRangeInvalidRange(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	_, _, err := tree.GetRange([]byte("z"), []byte("a"))
	if err == nil {
		t.Error("GetRange should return error for invalid range (start > end)")
	}
}

func TestShardedBTreeDeleteRange(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Insert keys
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key-%02d", i)
		tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", i)))
	}

	// Delete a range
	deleted, err := tree.DeleteRange([]byte("key-05"), []byte("key-14"))
	if err != nil {
		t.Fatalf("DeleteRange error: %v", err)
	}

	expectedDeleted := 10 // key-05 through key-14
	if deleted != expectedDeleted {
		t.Errorf("DeleteRange returned %d, want %d", deleted, expectedDeleted)
	}

	// Verify deleted keys are gone
	for i := 5; i <= 14; i++ {
		key := fmt.Sprintf("key-%02d", i)
		if _, err := tree.Find(Keytype(key)); err == nil {
			t.Errorf("Key %q should have been deleted", key)
		}
	}

	// Verify remaining keys still exist
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key-%02d", i)
		if _, err := tree.Find(Keytype(key)); err != nil {
			t.Errorf("Key %q should still exist: %v", key, err)
		}
	}
	for i := 15; i < 20; i++ {
		key := fmt.Sprintf("key-%02d", i)
		if _, err := tree.Find(Keytype(key)); err != nil {
			t.Errorf("Key %q should still exist: %v", key, err)
		}
	}
}

// ============================================================================
// Shard Distribution Tests
// ============================================================================

func TestShardDistribution(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	// Insert many keys
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key-%08d", i)
		tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", i)))
	}

	stats := tree.Stats()

	// Check total keys
	if stats.TotalKeys != int64(numKeys) {
		t.Errorf("TotalKeys = %d, want %d", stats.TotalKeys, numKeys)
	}

	// Check distribution is reasonably even (within 50% of mean)
	expectedPerShard := float64(numKeys) / float64(stats.NumShards)
	for i, count := range stats.KeysPerShard {
		ratio := float64(count) / expectedPerShard
		if ratio < 0.5 || ratio > 1.5 {
			t.Errorf("Shard %d has %d keys (%.2f%% of expected), distribution too skewed",
				i, count, ratio*100)
		}
	}

	// Check skew is reasonable (< 0.3 coefficient of variation)
	if stats.Skew > 0.3 {
		t.Errorf("Skew = %.3f, want < 0.3", stats.Skew)
	}

	t.Logf("Distribution stats: total=%d, min=%d, max=%d, skew=%.3f",
		stats.TotalKeys, stats.MinShardKeys, stats.MaxShardKeys, stats.Skew)
}

func TestSameKeyAlwaysSameShard(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	key := Keytype("consistent-key")
	expectedShard := tree.getShardIndex(key)

	// Verify the same key always maps to the same shard
	for i := 0; i < 1000; i++ {
		shard := tree.getShardIndex(key)
		if shard != expectedShard {
			t.Fatalf("Key mapped to different shards: %d vs %d", shard, expectedShard)
		}
	}
}

func TestHashFunctionDistribution(t *testing.T) {
	numShards := 16
	numKeys := 100000
	counts := make([]int, numShards)

	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		hash := fnv32a(key)
		shard := hash % uint32(numShards)
		counts[shard]++
	}

	expected := numKeys / numShards
	for i, count := range counts {
		ratio := float64(count) / float64(expected)
		if ratio < 0.8 || ratio > 1.2 {
			t.Errorf("Shard %d: got %d keys (%.1f%% of expected), hash distribution issue",
				i, count, ratio*100)
		}
	}
}

// ============================================================================
// Statistics Tests
// ============================================================================

func TestShardedBTreeStats(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Insert and delete some keys
	for i := 0; i < 100; i++ {
		tree.Insert(Keytype(fmt.Sprintf("key-%d", i)), Valuetype("value"))
	}

	for i := 0; i < 50; i++ {
		tree.Find(Keytype(fmt.Sprintf("key-%d", i)))
	}

	for i := 0; i < 10; i++ {
		tree.Delete(Keytype(fmt.Sprintf("key-%d", i)))
	}

	stats := tree.Stats()

	if stats.TotalInserts != 100 {
		t.Errorf("TotalInserts = %d, want 100", stats.TotalInserts)
	}

	if stats.TotalFinds != 50 {
		t.Errorf("TotalFinds = %d, want 50", stats.TotalFinds)
	}

	if stats.TotalDeletes != 10 {
		t.Errorf("TotalDeletes = %d, want 10", stats.TotalDeletes)
	}

	if stats.TotalKeys != 90 {
		t.Errorf("TotalKeys = %d, want 90", stats.TotalKeys)
	}
}

func TestShardedBTreeCount(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Empty tree
	if tree.Count() != 0 {
		t.Errorf("Empty tree Count() = %d, want 0", tree.Count())
	}

	// Insert keys
	for i := 0; i < 1000; i++ {
		tree.Insert(Keytype(fmt.Sprintf("key-%d", i)), Valuetype("value"))
	}

	if tree.Count() != 1000 {
		t.Errorf("Count() = %d, want 1000", tree.Count())
	}

	// Delete some
	for i := 0; i < 100; i++ {
		tree.Delete(Keytype(fmt.Sprintf("key-%d", i)))
	}

	if tree.Count() != 900 {
		t.Errorf("After delete Count() = %d, want 900", tree.Count())
	}
}

// ============================================================================
// Bulk Operations Tests
// ============================================================================

func TestShardedBTreeBulkInsert(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	numKeys := 10000
	keys := make([]Keytype, numKeys)
	values := make([]Valuetype, numKeys)

	for i := 0; i < numKeys; i++ {
		keys[i] = Keytype(fmt.Sprintf("key-%08d", i))
		values[i] = Valuetype(fmt.Sprintf("val-%d", i))
	}

	err := tree.BulkInsert(keys, values)
	if err != nil {
		t.Fatalf("BulkInsert error: %v", err)
	}

	// Verify all keys exist
	for i := 0; i < numKeys; i++ {
		v, err := tree.Find(keys[i])
		if err != nil {
			t.Errorf("Key %d not found after bulk insert: %v", i, err)
			continue
		}
		if !bytes.Equal(v, values[i]) {
			t.Errorf("Key %d: value mismatch", i)
		}
	}

	if tree.Count() != int64(numKeys) {
		t.Errorf("Count() = %d, want %d", tree.Count(), numKeys)
	}
}

func TestShardedBTreeBulkInsertMismatchedLengths(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	keys := []Keytype{Keytype("a"), Keytype("b")}
	values := []Valuetype{Valuetype("1")}

	err := tree.BulkInsert(keys, values)
	if err == nil {
		t.Error("BulkInsert should return error for mismatched lengths")
	}
}

// ============================================================================
// ForEach and Clear Tests
// ============================================================================

func TestShardedBTreeForEach(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	expected := make(map[string]string)
	for i := 0; i < 100; i++ {
		k := fmt.Sprintf("key-%d", i)
		v := fmt.Sprintf("val-%d", i)
		tree.Insert(Keytype(k), Valuetype(v))
		expected[k] = v
	}

	visited := make(map[string]string)
	tree.ForEach(func(key Keytype, value Valuetype) bool {
		visited[string(key)] = string(value)
		return true
	})

	if len(visited) != len(expected) {
		t.Errorf("ForEach visited %d keys, want %d", len(visited), len(expected))
	}

	for k, v := range expected {
		if visited[k] != v {
			t.Errorf("Key %q: got %q, want %q", k, visited[k], v)
		}
	}
}

func TestShardedBTreeForEachEarlyTermination(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	for i := 0; i < 100; i++ {
		tree.Insert(Keytype(fmt.Sprintf("key-%d", i)), Valuetype("value"))
	}

	count := 0
	tree.ForEach(func(key Keytype, value Valuetype) bool {
		count++
		return count < 10 // Stop after 10 iterations
	})

	if count != 10 {
		t.Errorf("ForEach should have stopped after 10 iterations, got %d", count)
	}
}

func TestShardedBTreeClear(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	for i := 0; i < 100; i++ {
		tree.Insert(Keytype(fmt.Sprintf("key-%d", i)), Valuetype("value"))
	}

	if tree.Count() != 100 {
		t.Fatalf("Before clear: Count() = %d, want 100", tree.Count())
	}

	tree.Clear()

	if tree.Count() != 0 {
		t.Errorf("After clear: Count() = %d, want 0", tree.Count())
	}

	stats := tree.Stats()
	if stats.TotalInserts != 0 || stats.TotalDeletes != 0 || stats.TotalFinds != 0 {
		t.Error("Stats should be reset after Clear()")
	}
}

// ============================================================================
// Concurrent Operation Tests
// ============================================================================

func TestShardedBTreeConcurrentInsert(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	numGoroutines := 50
	keysPerGoroutine := 1000
	var wg sync.WaitGroup

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < keysPerGoroutine; i++ {
				key := fmt.Sprintf("g%d-key-%d", goroutineID, i)
				value := fmt.Sprintf("value-%d", i)
				tree.Insert(Keytype(key), Valuetype(value))
			}
		}(g)
	}

	wg.Wait()

	expectedTotal := int64(numGoroutines * keysPerGoroutine)
	if tree.Count() != expectedTotal {
		t.Errorf("Count() = %d, want %d", tree.Count(), expectedTotal)
	}
}

func TestShardedBTreeConcurrentFind(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	// Pre-populate
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		tree.Insert(Keytype(fmt.Sprintf("key-%08d", i)), Valuetype(fmt.Sprintf("val-%d", i)))
	}

	numGoroutines := 50
	findsPerGoroutine := 1000
	var wg sync.WaitGroup
	var foundCount int64
	var notFoundCount int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(goroutineID)))
			for i := 0; i < findsPerGoroutine; i++ {
				key := fmt.Sprintf("key-%08d", rng.Intn(numKeys*2)) // 50% hit rate
				_, err := tree.Find(Keytype(key))
				if err == nil {
					atomic.AddInt64(&foundCount, 1)
				} else {
					atomic.AddInt64(&notFoundCount, 1)
				}
			}
		}(g)
	}

	wg.Wait()

	totalFinds := foundCount + notFoundCount
	expectedTotal := int64(numGoroutines * findsPerGoroutine)
	if totalFinds != expectedTotal {
		t.Errorf("Total finds = %d, want %d", totalFinds, expectedTotal)
	}

	t.Logf("Found: %d, Not found: %d (%.1f%% hit rate)",
		foundCount, notFoundCount, float64(foundCount)/float64(totalFinds)*100)
}

func TestShardedBTreeConcurrentMixed(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	// Pre-populate
	for i := 0; i < 5000; i++ {
		tree.Insert(Keytype(fmt.Sprintf("key-%08d", i)), Valuetype(fmt.Sprintf("val-%d", i)))
	}

	numGoroutines := 30
	opsPerGoroutine := 500
	var wg sync.WaitGroup

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(goroutineID)))

			for i := 0; i < opsPerGoroutine; i++ {
				op := rng.Intn(100)
				keyNum := rng.Intn(10000)
				key := fmt.Sprintf("key-%08d", keyNum)

				switch {
				case op < 50: // 50% reads
					tree.Find(Keytype(key))
				case op < 80: // 30% inserts
					tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-new-%d", i)))
				default: // 20% deletes
					tree.Delete(Keytype(key))
				}
			}
		}(g)
	}

	wg.Wait()

	// Just verify tree is still consistent
	count := tree.Count()
	t.Logf("Final count: %d", count)

	stats := tree.Stats()
	t.Logf("Stats: inserts=%d, finds=%d, deletes=%d",
		stats.TotalInserts, stats.TotalFinds, stats.TotalDeletes)
}

func TestShardedBTreeConcurrentRangeQuery(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	// Pre-populate with sorted keys
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("key-%04d", i)
		tree.Insert(Keytype(key), Valuetype(fmt.Sprintf("val-%d", i)))
	}

	numGoroutines := 20
	var wg sync.WaitGroup
	var errors int64

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(goroutineID)))

			for i := 0; i < 50; i++ {
				start := rng.Intn(900)
				end := start + rng.Intn(100) + 1

				startKey := fmt.Sprintf("key-%04d", start)
				endKey := fmt.Sprintf("key-%04d", end)

				keys, values, err := tree.GetRange([]byte(startKey), []byte(endKey))
				if err != nil {
					atomic.AddInt64(&errors, 1)
					continue
				}

				// Verify keys are sorted
				for j := 1; j < len(keys); j++ {
					if bytes.Compare(keys[j-1], keys[j]) > 0 {
						atomic.AddInt64(&errors, 1)
						break
					}
				}

				// Verify key-value pairs match
				if len(keys) != len(values) {
					atomic.AddInt64(&errors, 1)
				}
			}
		}(g)
	}

	wg.Wait()

	if errors > 0 {
		t.Errorf("Had %d errors during concurrent range queries", errors)
	}
}

func TestShardedBTreeConcurrentInsertSameKey(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	numGoroutines := 100
	var wg sync.WaitGroup

	// All goroutines try to insert the same key
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			tree.Insert(Keytype("shared-key"), Valuetype(fmt.Sprintf("value-%d", goroutineID)))
		}(g)
	}

	wg.Wait()

	// Key should exist with some value
	v, err := tree.Find(Keytype("shared-key"))
	if err != nil {
		t.Errorf("Shared key should exist: %v", err)
	}
	t.Logf("Final value: %s", string(v))
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestShardedBTreeEmptyTree(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Find on empty
	_, err := tree.Find(Keytype("key"))
	if err == nil {
		t.Error("Find on empty tree should return error")
	}

	// Delete on empty
	deleted := tree.Delete(Keytype("key"))
	if deleted {
		t.Error("Delete on empty tree should return false")
	}

	// Range on empty
	keys, values, err := tree.GetRange([]byte("a"), []byte("z"))
	if err != nil {
		t.Errorf("GetRange on empty tree should not error: %v", err)
	}
	if len(keys) != 0 || len(values) != 0 {
		t.Error("GetRange on empty tree should return empty slices")
	}

	// Count on empty
	if tree.Count() != 0 {
		t.Error("Count on empty tree should be 0")
	}
}

func TestShardedBTreeSingleShard(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 1})

	// Basic operations should still work with single shard
	for i := 0; i < 100; i++ {
		tree.Insert(Keytype(fmt.Sprintf("key-%d", i)), Valuetype("value"))
	}

	if tree.Count() != 100 {
		t.Errorf("Count() = %d, want 100", tree.Count())
	}

	stats := tree.Stats()
	if stats.KeysPerShard[0] != 100 {
		t.Errorf("Single shard should have all keys: got %d", stats.KeysPerShard[0])
	}
}

func TestShardedBTreeLargeKeys(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Insert keys of various sizes
	for size := 1; size <= 1000; size *= 10 {
		key := make([]byte, size)
		for i := range key {
			key[i] = byte('a' + (i % 26))
		}
		value := []byte(fmt.Sprintf("value-size-%d", size))

		tree.Insert(key, value)

		v, err := tree.Find(key)
		if err != nil {
			t.Errorf("Key of size %d not found: %v", size, err)
			continue
		}
		if !bytes.Equal(v, value) {
			t.Errorf("Key of size %d: value mismatch", size)
		}
	}
}

func TestShardedBTreeEmptyKey(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Empty key
	tree.Insert(Keytype(""), Valuetype("empty-key-value"))
	v, err := tree.Find(Keytype(""))
	if err != nil {
		t.Errorf("Empty key should be found: %v", err)
	}
	if string(v) != "empty-key-value" {
		t.Errorf("Empty key value = %q, want %q", string(v), "empty-key-value")
	}
}

func TestShardedBTreeDuplicateKeys(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Insert same key multiple times
	tree.Insert(Keytype("dup"), Valuetype("value1"))
	tree.Insert(Keytype("dup"), Valuetype("value2"))
	tree.Insert(Keytype("dup"), Valuetype("value3"))

	v, err := tree.Find(Keytype("dup"))
	if err != nil {
		t.Errorf("Duplicate key should be found: %v", err)
	}
	// Value should be one of the inserted values (implementation-dependent)
	t.Logf("Duplicate key value: %s", string(v))
}

// ============================================================================
// GetShard Test (for debugging)
// ============================================================================

func TestGetShard(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	// Insert into specific shard via key
	tree.Insert(Keytype("test-key"), Valuetype("test-value"))

	// Find which shard it went to
	shardIdx := tree.getShardIndex(Keytype("test-key"))
	shard := tree.GetShard(shardIdx)

	if shard == nil {
		t.Fatal("GetShard returned nil")
	}

	// Verify the value is in that shard
	v, err := shard.Find(Keytype("test-key"))
	if err != nil {
		t.Errorf("Key should be in the calculated shard: %v", err)
	}
	if string(v) != "test-value" {
		t.Errorf("Value mismatch: got %q", string(v))
	}

	// GetShard with invalid index
	if tree.GetShard(-1) != nil {
		t.Error("GetShard(-1) should return nil")
	}
	if tree.GetShard(100) != nil {
		t.Error("GetShard(100) should return nil for 4-shard tree")
	}
}

// ============================================================================
// Ordering Tests
// ============================================================================

func TestShardedBTreeRangeQueryOrdering(t *testing.T) {
	tree := NewShardedBTree(ShardConfig{NumShards: 8})

	// Insert in random order
	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%04d", i)
	}
	rand.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })

	for _, k := range keys {
		tree.Insert(Keytype(k), Valuetype("val"))
	}

	// Get full range
	resultKeys, _, err := tree.GetRange([]byte("key-0000"), []byte("key-9999"))
	if err != nil {
		t.Fatalf("GetRange error: %v", err)
	}

	// Verify sorted order
	sorted := sort.SliceIsSorted(resultKeys, func(i, j int) bool {
		return bytes.Compare(resultKeys[i], resultKeys[j]) < 0
	})
	if !sorted {
		t.Error("GetRange results should be sorted")
	}
}
