# B-Tree Concurrency Bug Fixes

**Date:** January 31, 2026  
**Status:** Fixed and verified with race detector

---

## Executive Summary

The B-Tree implementation had several critical concurrency bugs that caused deadlocks, data races, and undefined behavior. After analyzing the original complex multi-lock design, we **simplified to a single RWMutex model** which is correct by construction and much easier to reason about.

---

## Original Issues Found

### 1. 🔴 CRITICAL: `unlockPath()` Used Wrong Unlock Type

**Symptom:** Panics with `sync: RUnlock of unlocked RWMutex` or deadlocks

**Root Cause:**  
The `Put()` method acquired **write locks** (`mu.Lock()`) during tree descent, but `unlockPath()` was calling `mu.RUnlock()` instead of `mu.Unlock()`.

### 2. 🔴 CRITICAL: `propagateSplit()` Double-Locked Nodes

**Symptom:** Deadlock during node splits

**Root Cause:**  
During `Put()`, nodes were locked while descending. Nodes that were "unsafe" (full) remained locked in the `path` slice. However, `propagateSplit()` attempted to `Lock()` these already-locked nodes again.

### 3. 🔴 CRITICAL: `Delete()` Had No Internal Node Locking

**Symptom:** Data races, corrupted tree structure

**Root Cause:**  
The `Delete()` method only locked the root node, but the recursive `delete()` helper traversed and modified child nodes without any locking.

### 4. 🔴 CRITICAL: `GetRange()` Had No Locking

**Symptom:** Data races, inconsistent reads

**Root Cause:**  
The `GetRange()` method traversed the entire tree without acquiring any locks, racing with concurrent inserts/deletes.

---

## Solution: Simplified Single-Lock Model

Rather than trying to fix the complex multi-lock design, we replaced it with a **single `treeLock sync.RWMutex`** at the tree level:

```go
type Btree struct {
    root     *Node
    treeLock sync.RWMutex  // Single lock for all operations
}
```

### Write Operations (Insert, Put, Delete)

```go
func (t *Btree) Insert(key Keytype, value Valuetype) {
    t.treeLock.Lock()
    defer t.treeLock.Unlock()
    // ... all tree modifications happen under exclusive lock
}

func (t *Btree) Delete(key Keytype) bool {
    t.treeLock.Lock()
    defer t.treeLock.Unlock()
    // ... delete runs under exclusive lock
}
```

### Read Operations (Find, Get, GetRange)

```go
func (t *Btree) Find(key Keytype) (Valuetype, bool) {
    t.treeLock.RLock()
    defer t.treeLock.RUnlock()
    // ... read traversal under shared lock
}

func (t *Btree) GetRange(startKey, endKey []byte) (...) {
    t.treeLock.RLock()
    defer t.treeLock.RUnlock()
    // ... range scan under shared lock
}
```

---

## Additional Fix: Slice Capacity Bug in `insertAt()`

During stress testing, we discovered a bug in `node.go` where `insertAt()` used an unsafe slice insertion pattern:

```go
// BEFORE (BUG - could panic on slice bounds):
func (node *Node) insertAt(index int, key Keytype, value Valuetype) {
    node.keys = append(node.keys[:index+1], node.keys[index:]...)
    // ❌ Fails if slice has limited capacity
}

// AFTER (FIXED):
func (node *Node) insertAt(index int, key Keytype, value Valuetype) {
    node.keys = append(node.keys, nil)  // Grow first
    copy(node.keys[index+1:], node.keys[index:len(node.keys)-1])  // Shift
    node.keys[index] = key  // Insert
}
```

The same fix was applied to `insertChildAt()`.

---

## Design Tradeoffs

### Why Single Lock?

| Aspect | Multi-Lock (Original) | Single RWMutex (Current) |
|--------|----------------------|--------------------------|
| Correctness | Had 4+ critical bugs | Correct by construction |
| Complexity | High (lock ordering, coupling) | Low |
| Read Concurrency | Possible but buggy | ✅ Multiple readers |
| Write Concurrency | One writer (in theory) | One writer |
| Maintenance | Hard | Easy |

The single-lock model provides:
- **Multiple concurrent readers** - RLock allows parallel reads
- **Exclusive writes** - One writer at a time, no races
- **Simple reasoning** - No lock ordering issues, no deadlocks

### When to Consider Multi-Lock

Multi-lock designs (like B-link trees) make sense when:
1. Write contention is the bottleneck (not read contention)
2. You have very long-running transactions
3. You need to support lock-free reads during splits

For most use cases, the single RWMutex is sufficient and much safer.

---

## Current Concurrency Model

```
┌─────────────────────────────────────────────────────────────┐
│                     Btree Concurrency                        │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  WRITES (Insert, Put, Delete):                              │
│  ┌─────────────────────────────────────────────────────┐   │
│  │            treeLock.Lock()                          │   │
│  │  ┌───────────────────────────────────────────────┐  │   │
│  │  │  Exclusive access to entire tree              │  │   │
│  │  │  - Only one writer at a time                  │  │   │
│  │  │  - No readers during write                    │  │   │
│  │  └───────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                              │
│  READS (Find, Get, GetRange):                               │
│  ┌─────────────────────────────────────────────────────┐   │
│  │            treeLock.RLock()                         │   │
│  │  ┌───────────────────────────────────────────────┐  │   │
│  │  │  Shared access for all readers                │  │   │
│  │  │  - Multiple readers concurrently              │  │   │
│  │  │  - Blocked while writer holds lock            │  │   │
│  │  └───────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

---

## Verification

All tests pass with Go's race detector enabled:

```bash
$ go test -race -count=1 ./bptree/
ok      Database/bptree 2.122s
```

Stress tests run 5 times in succession without failure:

```bash
$ for i in {1..5}; do go test -race -count=1 -run "TestConcurrent|TestGranular" ./bptree/; done
ok      Database/bptree 2.096s
ok      Database/bptree 2.122s
ok      Database/bptree 2.152s
ok      Database/bptree 2.111s
ok      Database/bptree 2.105s
```

Concurrent tests now pass:
- `TestConcurrentOperations` - 100 workers, 500 ops each
- `TestConcurrentStress` - 50 workers, 200 ops each  
- `TestConcurrentDeleteAndInsert` - 500 pairs
- `TestGranularConcurrency` - 50 workers, 100 ops each
- `TestConcurrentInsertBasic` - 100 goroutines, 500 keys each

---

## Future Improvements

1. **B-Link Tree Protocol:** For higher write concurrency, implement B-link tree with link pointers for lock-free reads during splits.

2. **Partitioned Tree:** Shard data across multiple trees by key range for write parallelism.

3. **Copy-on-Write:** Use immutable nodes with atomic pointer swaps for better read concurrency.

---

## References

- Lehman & Yao, "Efficient Locking for Concurrent Operations on B-Trees" (1981)
- Graefe, "A Survey of B-Tree Locking Techniques" (2010)
- Go sync package documentation: https://pkg.go.dev/sync
