# StunDB Sharding Design Document

**Date:** January 31, 2026  
**Status:** Design Phase  
**Author:** StunDB Team

---

## Overview

This document outlines the design for adding sharding support to StunDB's B-Tree implementation. Sharding partitions data across multiple independent B-Trees, allowing linear scaling of throughput.

---

## Goals

1. **Linear throughput scaling** - N shards → ~N× throughput
2. **Backward compatibility** - existing API should work unchanged
3. **Transparent routing** - clients don't need to know about shards
4. **Minimal complexity** - simpler than B-link tree or distributed systems

---

## Design

### Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        ShardedBTree                               │
│                                                                   │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │                    Shard Router                              │ │
│  │         shardIndex = hash(key) % numShards                   │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                              │                                    │
│         ┌────────────────────┼────────────────────┐              │
│         │                    │                    │              │
│         ▼                    ▼                    ▼              │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────┐        │
│  │   Shard 0   │     │   Shard 1   │     │   Shard N   │        │
│  │  ┌───────┐  │     │  ┌───────┐  │     │  ┌───────┐  │        │
│  │  │BTree  │  │     │  │BTree  │  │     │  │BTree  │  │        │
│  │  │+Lock  │  │     │  │+Lock  │  │     │  │+Lock  │  │        │
│  │  └───────┘  │     │  └───────┘  │     │  └───────┘  │        │
│  └─────────────┘     └─────────────┘     └─────────────┘        │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

### Data Structures

```go
// ShardedBTree distributes data across multiple B-Trees
type ShardedBTree struct {
    shards    []*Btree    // Individual B-Trees
    numShards uint32      // Number of shards (power of 2 recommended)
    hashFunc  HashFunc    // Key hashing function
}

// HashFunc computes shard index from key
type HashFunc func(key []byte) uint32

// Shard configuration
type ShardConfig struct {
    NumShards     int       // Number of shards (default: runtime.NumCPU())
    HashFunction  HashFunc  // Custom hash (default: fnv32a)
}
```

### API Design

```go
// Constructor
func NewShardedBTree(config ShardConfig) *ShardedBTree

// Same interface as Btree - drop-in replacement
func (s *ShardedBTree) Insert(key Keytype, value Valuetype)
func (s *ShardedBTree) Find(key Keytype) (Valuetype, bool)
func (s *ShardedBTree) Delete(key Keytype) bool
func (s *ShardedBTree) Get(key Keytype) (Valuetype, bool)
func (s *ShardedBTree) Put(key Keytype, value Valuetype)

// Range queries (more complex - see below)
func (s *ShardedBTree) GetRange(startKey, endKey []byte) ([]Keytype, []Valuetype, error)

// Shard statistics
func (s *ShardedBTree) Stats() ShardStats
```

---

## Implementation Plan

### Phase 1: Basic Sharding

```go
type ShardedBTree struct {
    shards    []*Btree
    numShards uint32
}

func NewShardedBTree(numShards int) *ShardedBTree {
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

func (s *ShardedBTree) getShard(key Keytype) *Btree {
    hash := fnv32a(key)
    return s.shards[hash % s.numShards]
}

func (s *ShardedBTree) Insert(key Keytype, value Valuetype) {
    s.getShard(key).Insert(key, value)
}

func (s *ShardedBTree) Find(key Keytype) (Valuetype, bool) {
    return s.getShard(key).Find(key)
}
```

### Phase 2: Range Queries

Range queries across shards are complex because data is distributed by hash, not by key order.

**Option A: Query All Shards (Simple)**
```go
func (s *ShardedBTree) GetRange(startKey, endKey []byte) ([]Keytype, []Valuetype, error) {
    var allKeys []Keytype
    var allValues []Valuetype
    
    // Query all shards in parallel
    var wg sync.WaitGroup
    var mu sync.Mutex
    
    for _, shard := range s.shards {
        wg.Add(1)
        go func(shard *Btree) {
            defer wg.Done()
            keys, values, _ := shard.GetRange(startKey, endKey)
            
            mu.Lock()
            allKeys = append(allKeys, keys...)
            allValues = append(allValues, values...)
            mu.Unlock()
        }(shard)
    }
    
    wg.Wait()
    
    // Sort results by key
    sortByKey(allKeys, allValues)
    return allKeys, allValues, nil
}
```

**Option B: Range-Based Sharding (Complex)**
- Partition by key range instead of hash
- Ranges are contiguous: shard 0 = A-F, shard 1 = G-L, etc.
- Range queries only touch relevant shards
- Requires rebalancing when data is skewed

### Phase 3: Statistics & Monitoring

```go
type ShardStats struct {
    TotalKeys    int64
    KeysPerShard []int64
    Skew         float64  // Standard deviation of keys across shards
}

func (s *ShardedBTree) Stats() ShardStats {
    stats := ShardStats{
        KeysPerShard: make([]int64, len(s.shards)),
    }
    
    for i, shard := range s.shards {
        count := shard.Count()  // Need to implement
        stats.KeysPerShard[i] = count
        stats.TotalKeys += count
    }
    
    stats.Skew = calculateSkew(stats.KeysPerShard)
    return stats
}
```

---

## Tradeoffs & Decisions

### Hash Function Choice

| Function | Speed | Distribution | Collision Rate |
|----------|-------|--------------|----------------|
| FNV-1a | Fast | Good | Low |
| xxHash | Very Fast | Excellent | Very Low |
| CRC32 | Fast | Moderate | Moderate |
| SHA256 | Slow | Perfect | Zero |

**Decision:** Use FNV-1a for simplicity, xxHash if we need more performance.

### Number of Shards

| Shards | Pros | Cons |
|--------|------|------|
| CPU count | Matches parallelism | May not be power of 2 |
| Power of 2 | Fast modulo (bitwise AND) | May over/under-provision |
| Fixed (e.g., 256) | Simple | Wastes memory if small dataset |

**Decision:** Default to `runtime.NumCPU()`, allow override.

### Range Queries

| Approach | Pros | Cons |
|----------|------|------|
| Hash-based + scan all | Simple | O(N) shards per query |
| Range-based partition | Efficient queries | Complex rebalancing |
| Hybrid | Best of both | Very complex |

**Decision:** Start with hash-based (scan all), add range-based later if needed.

---

## Issues & Challenges

### 1. Hot Spots

**Problem:** If certain key patterns are more common, some shards get more traffic.

**Example:** 
```
Keys: user_1, user_2, user_3, ...  → all hash to similar values
```

**Solutions:**
- Use better hash function
- Add salt/prefix to keys
- Monitor shard distribution
- Dynamic rebalancing (complex)

### 2. Cross-Shard Operations

**Problem:** Operations spanning multiple keys may touch multiple shards.

**Examples:**
- Range queries
- Transactions (not yet implemented)
- Bulk inserts

**Solutions:**
- Parallel execution with result merging
- Accept higher latency for cross-shard ops
- Consider range-based sharding for range-heavy workloads

### 3. Shard Count Changes

**Problem:** Changing shard count requires rehashing all keys.

**Solutions:**
- Consistent hashing (minimizes data movement)
- Offline migration
- Virtual shards (fixed large count, map to physical)

### 4. Memory Overhead

**Problem:** Each shard has its own tree structure overhead.

**Analysis:**
- 16 shards × ~1KB overhead = ~16KB total
- Negligible for most use cases
- May matter for very small datasets

---

## Testing Strategy

### Unit Tests

```go
func TestShardedInsertFind(t *testing.T)      // Basic operations
func TestShardedConcurrent(t *testing.T)       // Concurrent access
func TestShardDistribution(t *testing.T)       // Even key distribution
func TestShardedRangeQuery(t *testing.T)       // Cross-shard ranges
```

### Benchmark Tests

```go
func BenchmarkShardedThroughput(b *testing.B)  // Compare vs single tree
func BenchmarkShardScaling(b *testing.B)       // 1, 2, 4, 8, 16 shards
func BenchmarkShardedLatency(b *testing.B)     // p50, p99 latencies
```

### Expected Results

| Shards | Expected Throughput | Notes |
|--------|---------------------|-------|
| 1 | ~2M ops/sec | Baseline (same as current) |
| 2 | ~4M ops/sec | 2× scaling |
| 4 | ~8M ops/sec | 4× scaling |
| 8 | ~12M ops/sec | Sub-linear (lock contention) |
| 16 | ~20M ops/sec | Diminishing returns |

---

## Migration Path

### From Single Btree to ShardedBTree

```go
// Before
tree := &Btree{}
tree.Insert(key, value)
val, found := tree.Find(key)

// After (drop-in replacement)
tree := NewShardedBTree(ShardConfig{NumShards: 8})
tree.Insert(key, value)
val, found := tree.Find(key)
```

### Data Migration

For existing data:
1. Create new ShardedBTree
2. Iterate old tree with GetRange
3. Insert all keys into new structure
4. Swap pointers atomically

---

## Future Considerations

### 1. Persistent Sharding
- Each shard maps to separate file
- Parallel I/O for reads/writes
- Independent recovery per shard

### 2. Dynamic Resharding
- Monitor shard load
- Split hot shards
- Merge cold shards
- Requires consistent hashing

### 3. Shard-Level Snapshots
- Snapshot individual shards
- Parallel backup
- Point-in-time recovery per shard

---

## Timeline

| Phase | Task | Estimated Time |
|-------|------|----------------|
| 1 | Basic sharding (Insert/Find/Delete) | 2-3 hours |
| 2 | Range queries | 1-2 hours |
| 3 | Statistics & monitoring | 1 hour |
| 4 | Benchmarks & optimization | 2 hours |
| 5 | Documentation | 1 hour |

**Total:** ~8 hours for complete implementation

---

## References

- "Consistent Hashing and Random Trees" - Karger et al. (1997)
- "Dynamo: Amazon's Highly Available Key-value Store" (2007)
- "The Google File System" - Ghemawat et al. (2003)
