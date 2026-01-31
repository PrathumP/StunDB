package bptree

import (
	"bytes"
	"errors"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
)

// ShardedBTree distributes data across multiple B-Trees for linear scaling.
//
// DESIGN:
// - Keys are hashed to determine which shard they belong to
// - Each shard is an independent B-Tree with its own lock
// - Operations on different shards can proceed in parallel
//
// PERFORMANCE:
// - N shards → ~N× throughput (linear scaling)
// - Single-key operations: O(1) shard lookup + O(log n) tree operation
// - Range queries: O(shards) parallel queries + O(n log n) merge
//
// TRADEOFFS:
// - Pros: Simple, linear scaling, isolated failures
// - Cons: Range queries touch all shards, no cross-shard transactions
type ShardedBTree struct {
	shards    []*Btree
	numShards uint32

	// Statistics (atomic for lock-free reads)
	totalInserts uint64
	totalDeletes uint64
	totalFinds   uint64
}

// ShardConfig configures the sharded B-Tree.
type ShardConfig struct {
	// NumShards is the number of shards. Default: runtime.NumCPU()
	// Power of 2 recommended for faster modulo operation.
	NumShards int
}

// ShardStats provides statistics about shard distribution.
type ShardStats struct {
	NumShards    int
	TotalKeys    int64
	KeysPerShard []int64
	MinShardKeys int64
	MaxShardKeys int64
	Skew         float64 // Coefficient of variation (stddev/mean)
	TotalInserts uint64
	TotalDeletes uint64
	TotalFinds   uint64
}

// NewShardedBTree creates a new sharded B-Tree with the given configuration.
func NewShardedBTree(config ShardConfig) *ShardedBTree {
	numShards := config.NumShards
	if numShards <= 0 {
		numShards = runtime.NumCPU()
	}

	s := &ShardedBTree{
		shards:    make([]*Btree, numShards),
		numShards: uint32(numShards),
	}

	for i := 0; i < numShards; i++ {
		s.shards[i] = &Btree{}
	}

	return s
}

// NewShardedBTreeDefault creates a sharded B-Tree with default settings.
func NewShardedBTreeDefault() *ShardedBTree {
	return NewShardedBTree(ShardConfig{})
}

// fnv32a implements FNV-1a hash for byte slices.
// FNV-1a is fast and has good distribution properties.
func fnv32a(key []byte) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)

	hash := uint32(offset32)
	for _, b := range key {
		hash ^= uint32(b)
		hash *= prime32
	}
	return hash
}

// getShard returns the shard for a given key.
func (s *ShardedBTree) getShard(key Keytype) *Btree {
	hash := fnv32a(key)
	// Use bitwise AND if numShards is power of 2, otherwise modulo
	// For simplicity, we always use modulo (compiler optimizes power of 2)
	return s.shards[hash%s.numShards]
}

// getShardIndex returns the shard index for a given key.
func (s *ShardedBTree) getShardIndex(key Keytype) int {
	hash := fnv32a(key)
	return int(hash % s.numShards)
}

// Insert inserts a key-value pair into the appropriate shard.
// Thread-safe: each shard has its own lock.
func (s *ShardedBTree) Insert(key Keytype, value Valuetype) {
	shard := s.getShard(key)
	shard.Insert(key, value)
	atomic.AddUint64(&s.totalInserts, 1)
}

// Put is an alias for Insert.
func (s *ShardedBTree) Put(key Keytype, value Valuetype) {
	s.Insert(key, value)
}

// Find searches for a key in the appropriate shard.
// Returns the value and nil error if found, nil and error otherwise.
// Thread-safe: uses read lock on the shard.
func (s *ShardedBTree) Find(key Keytype) (Valuetype, error) {
	shard := s.getShard(key)
	atomic.AddUint64(&s.totalFinds, 1)
	return shard.Find(key)
}

// Get is an alias for Find.
func (s *ShardedBTree) Get(key Keytype) (Valuetype, error) {
	return s.Find(key)
}

// Delete removes a key from the appropriate shard.
// Returns true if the key was found and deleted, false otherwise.
// Thread-safe: uses write lock on the shard.
func (s *ShardedBTree) Delete(key Keytype) bool {
	shard := s.getShard(key)
	deleted := shard.Delete(key)
	if deleted {
		atomic.AddUint64(&s.totalDeletes, 1)
	}
	return deleted
}

// keyValuePair holds a key-value pair for sorting.
type keyValuePair struct {
	key   Keytype
	value Valuetype
}

// GetRange returns all key-value pairs in the range [startKey, endKey].
// Queries all shards in parallel and merges results.
// Thread-safe: each shard uses its own read lock.
func (s *ShardedBTree) GetRange(startKey, endKey []byte) ([]Keytype, []Valuetype, error) {
	if bytes.Compare(startKey, endKey) > 0 {
		return nil, nil, errors.New("invalid range: startKey is greater than endKey")
	}

	// Query all shards in parallel
	type shardResult struct {
		keys   []Keytype
		values []Valuetype
		err    error
	}

	results := make([]shardResult, len(s.shards))
	var wg sync.WaitGroup

	for i, shard := range s.shards {
		wg.Add(1)
		go func(idx int, sh *Btree) {
			defer wg.Done()
			keys, values, err := sh.GetRange(startKey, endKey)
			results[idx] = shardResult{keys: keys, values: values, err: err}
		}(i, shard)
	}

	wg.Wait()

	// Check for errors and collect results
	var pairs []keyValuePair
	for _, r := range results {
		if r.err != nil {
			return nil, nil, r.err
		}
		for i := range r.keys {
			pairs = append(pairs, keyValuePair{key: r.keys[i], value: r.values[i]})
		}
	}

	// Sort by key for consistent ordering
	sort.Slice(pairs, func(i, j int) bool {
		return bytes.Compare(pairs[i].key, pairs[j].key) < 0
	})

	// Extract sorted keys and values
	keys := make([]Keytype, len(pairs))
	values := make([]Valuetype, len(pairs))
	for i, p := range pairs {
		keys[i] = p.key
		values[i] = p.value
	}

	return keys, values, nil
}

// DeleteRange deletes all keys in the range [startKey, endKey].
// Returns the number of keys deleted.
// Thread-safe: queries then deletes (not atomic across the range).
func (s *ShardedBTree) DeleteRange(startKey, endKey []byte) (int, error) {
	keys, _, err := s.GetRange(startKey, endKey)
	if err != nil {
		return 0, err
	}

	deletedCount := 0
	for _, key := range keys {
		if s.Delete(key) {
			deletedCount++
		}
	}
	return deletedCount, nil
}

// Count returns the total number of keys across all shards.
// Note: This is an O(n) operation that traverses all trees.
func (s *ShardedBTree) Count() int64 {
	var total int64
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, shard := range s.shards {
		wg.Add(1)
		go func(sh *Btree) {
			defer wg.Done()
			count := sh.countKeys()
			mu.Lock()
			total += count
			mu.Unlock()
		}(shard)
	}

	wg.Wait()
	return total
}

// countKeys counts keys in a single B-Tree.
func (t *Btree) countKeys() int64 {
	t.treeLock.RLock()
	defer t.treeLock.RUnlock()

	if t.root == nil {
		return 0
	}
	return t.root.countKeys()
}

// countKeys counts keys in a node and its children recursively.
func (n *Node) countKeys() int64 {
	count := int64(len(n.keys))
	if !n.isleaf {
		for _, child := range n.children {
			if child != nil {
				count += child.countKeys()
			}
		}
	}
	return count
}

// Stats returns statistics about shard distribution.
func (s *ShardedBTree) Stats() ShardStats {
	stats := ShardStats{
		NumShards:    len(s.shards),
		KeysPerShard: make([]int64, len(s.shards)),
		TotalInserts: atomic.LoadUint64(&s.totalInserts),
		TotalDeletes: atomic.LoadUint64(&s.totalDeletes),
		TotalFinds:   atomic.LoadUint64(&s.totalFinds),
	}

	// Count keys per shard in parallel
	var wg sync.WaitGroup
	for i, shard := range s.shards {
		wg.Add(1)
		go func(idx int, sh *Btree) {
			defer wg.Done()
			stats.KeysPerShard[idx] = sh.countKeys()
		}(i, shard)
	}
	wg.Wait()

	// Calculate statistics
	stats.MinShardKeys = stats.KeysPerShard[0]
	stats.MaxShardKeys = stats.KeysPerShard[0]

	for _, count := range stats.KeysPerShard {
		stats.TotalKeys += count
		if count < stats.MinShardKeys {
			stats.MinShardKeys = count
		}
		if count > stats.MaxShardKeys {
			stats.MaxShardKeys = count
		}
	}

	// Calculate skew (coefficient of variation)
	if stats.TotalKeys > 0 && len(s.shards) > 1 {
		mean := float64(stats.TotalKeys) / float64(len(s.shards))
		var variance float64
		for _, count := range stats.KeysPerShard {
			diff := float64(count) - mean
			variance += diff * diff
		}
		variance /= float64(len(s.shards))
		stddev := sqrt(variance)
		if mean > 0 {
			stats.Skew = stddev / mean
		}
	}

	return stats
}

// sqrt computes square root using Newton's method.
// Avoids importing math package for this single function.
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

// NumShards returns the number of shards.
func (s *ShardedBTree) NumShards() int {
	return int(s.numShards)
}

// GetShard returns a specific shard by index (for testing/debugging).
func (s *ShardedBTree) GetShard(index int) *Btree {
	if index < 0 || index >= len(s.shards) {
		return nil
	}
	return s.shards[index]
}

// BulkInsert inserts multiple key-value pairs efficiently.
// Groups keys by shard to minimize lock acquisition overhead.
func (s *ShardedBTree) BulkInsert(keys []Keytype, values []Valuetype) error {
	if len(keys) != len(values) {
		return errors.New("keys and values must have the same length")
	}

	// Group by shard
	shardGroups := make(map[int][]int) // shard index -> key indices
	for i, key := range keys {
		shardIdx := s.getShardIndex(key)
		shardGroups[shardIdx] = append(shardGroups[shardIdx], i)
	}

	// Insert into each shard in parallel
	var wg sync.WaitGroup
	errChan := make(chan error, len(s.shards))

	for shardIdx, keyIndices := range shardGroups {
		wg.Add(1)
		go func(idx int, indices []int) {
			defer wg.Done()
			shard := s.shards[idx]
			for _, keyIdx := range indices {
				shard.Insert(keys[keyIdx], values[keyIdx])
				atomic.AddUint64(&s.totalInserts, 1)
			}
		}(shardIdx, keyIndices)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

// ForEach iterates over all key-value pairs in the tree.
// The callback is called for each key-value pair.
// Order is not guaranteed (depends on shard iteration order).
// Thread-safe: each shard is locked during iteration.
func (s *ShardedBTree) ForEach(callback func(key Keytype, value Valuetype) bool) {
	for _, shard := range s.shards {
		shard.treeLock.RLock()
		if shard.root != nil {
			if !shard.root.forEach(callback) {
				shard.treeLock.RUnlock()
				return
			}
		}
		shard.treeLock.RUnlock()
	}
}

// forEach iterates over all key-value pairs in a node.
func (n *Node) forEach(callback func(key Keytype, value Valuetype) bool) bool {
	if n.isleaf {
		for i := range n.keys {
			if !callback(n.keys[i], n.values[i]) {
				return false
			}
		}
		return true
	}

	// Internal node: traverse children and keys
	for i := 0; i < len(n.children); i++ {
		if n.children[i] != nil {
			if !n.children[i].forEach(callback) {
				return false
			}
		}
		if i < len(n.keys) {
			if !callback(n.keys[i], n.values[i]) {
				return false
			}
		}
	}
	return true
}

// Clear removes all data from all shards.
func (s *ShardedBTree) Clear() {
	for i := range s.shards {
		s.shards[i] = &Btree{}
	}
	atomic.StoreUint64(&s.totalInserts, 0)
	atomic.StoreUint64(&s.totalDeletes, 0)
	atomic.StoreUint64(&s.totalFinds, 0)
}
