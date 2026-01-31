# B-Link Tree Implementation - Quick Reference

## Usage Guide

### Original Functions (Backward Compatible)
These functions work as before and are fully stable:

```go
// Original Insert (with serialization through insertLock)
tree.Insert(key, value)

// Original Find
value, err := tree.Find(key)

// Original Delete  
deleted := tree.Delete(key)
```

### New B-Link Functions
These functions implement the B-Link tree algorithm:

```go
// New Get() with lock coupling and right-link traversal
// Handles concurrent splits transparently
value, err := tree.Get(key)

// New Put() with pessimistic locking
// Updates rightSibling pointers on splits
tree.Put(key, value)
```

## Key Differences

| Aspect | Insert | Put |
|--------|--------|-----|
| **Lock Strategy** | insertLock mutex | Pessimistic (per-node) |
| **Concurrency** | Fully serialized | Partial (early unlock) |
| **B-Link Aware** | No | Yes |
| **RightSibling** | Not updated | Updated |
| **Use Case** | Stable, simple | Enhanced concurrency |

| Aspect | Find | Get |
|--------|------|-----|
| **Lock Coupling** | Yes | Yes |
| **Right-Link Follow** | No | Yes |
| **Split Handling** | Fixed path | Adaptive |
| **Restart on Split** | Yes | No |

## Node Structure

```go
type Node struct {
	keys         []Keytype       // Sorted keys
	values       []Valuetype     // Associated values
	children     []*Node         // Child pointers (internal nodes)
	isleaf       bool            // True if leaf node
	mu           sync.RWMutex    // Per-node read-write lock
	rightSibling *Node           // NEW: Horizontal link for B-Link
}
```

## Lock Coupling Pattern

Both Get() and Find() use lock coupling:

```
1. Lock current node (RLock)
2. Check if key is in current node
   - If yes: return value, unlock, done
3. If internal node:
   - Lock child node (RLock)
   - Unlock current node
   - Move to child, repeat
```

## B-Link Right-Sibling Traversal

Get() detects concurrent splits:

```
1. If key > node's highKey (max key):
   - Node was split while reading
   - Follow rightSibling to find key
2. This allows readers to adapt to concurrent modifications
```

## Split Behavior (Put Only)

When a node is full during Put():

```
Node A [a, b, c, d, e] → needs split
Mid = 2 (key 'c')

After split:
  Left:   [a, b]
  Right:  [d, e]  
  Up:     promote 'c' to parent
  Link:   A.rightSibling = Right
```

## Performance Characteristics

### Get()
- **Best case**: O(log n) with read locks only
- **Traversal overhead**: 1-2 extra checks per level (right-sibling check)
- **Concurrency**: High - readers never block writers (except at node level)

### Put()  
- **Best case**: O(log n) with early node unlocking
- **Worst case**: O(log n) with full split propagation
- **Lock contention**: Low on non-full path, splits are serialized

### Insert()
- **Best case**: O(log n) but fully serialized
- **Lock contention**: Highest - single global insertLock
- **Stability**: Highest - proven, tested extensively

## Thread Safety Notes

✅ **Safe for Concurrent Use:**
- Multiple readers with Get()/Find()
- Single writer with Insert() (serialized)
- Insert() and Get()/Find() can work together

⚠️ **Known Limitations:**
- Put() has race conditions with concurrent writers
- Multiple concurrent Put() calls may lose updates
- Use Insert() for multi-threaded writes currently

## Recommended Usage Patterns

### Read-Heavy Workload (Multiple Readers)
```go
// Use Get() for better concurrency
value, err := tree.Get(key)
```

### Single-Threaded or Serialized Writes
```go
// Use Insert() for stability and simplicity
tree.Insert(key, value)
value, err := tree.Find(key)  // or tree.Get(key)
```

### Transitional (Mixed Operations)
```go
// Keep using Insert() until Put() is hardened
tree.Insert(key, value)
value, err := tree.Get(key)  // Safe to use Get()
```

## Testing

Run tests with:
```bash
# All tests (including skipped ones)
go test -v ./bptree

# Only passing tests
go test -v ./bptree -run "Insert|Delete|Get|Put|BasicConcurrency" 

# Benchmark
go test -v ./bptree -bench=.
```

## Future Work

1. **Harden Put() for concurrent writers**
   - Fix race conditions
   - Add proper synchronization
   - Enable true concurrent inserts

2. **Implement optimistic Put()**
   - Detect conflicts
   - Retry with pessimistic locking
   - Better performance for contention-free case

3. **Performance tuning**
   - Profile Get() vs Find()
   - Optimize lock granularity
   - Benchmark concurrent workloads

## References

- Implementation: Lanin & Shasha (1986)
- Lock Coupling: Standard B-Tree technique
- Right-Link Chains: Enables concurrent structure modifications
- Pessimistic Locking: Ensures safe splits without deadlock

## Questions?

See [BLINK_TREE_REFACTOR_SUMMARY.md](BLINK_TREE_REFACTOR_SUMMARY.md) for detailed architecture.
