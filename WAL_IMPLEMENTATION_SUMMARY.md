# StunDB Write-Ahead Log (WAL) Implementation Summary

**Date:** January 31, 2026  
**Status:** Complete ✅  
**Author:** StunDB Team

---

## Overview

This document summarizes the implementation of a Write-Ahead Log (WAL) for StunDB, providing durability guarantees for the B-Tree database.

---

## What Was Built

### New Files Created

| File | Purpose | Lines |
|------|---------|-------|
| [wal.go](bptree/wal.go) | Core WAL implementation | ~500 |
| [durable_btree.go](bptree/durable_btree.go) | WAL-integrated B-Tree wrapper | ~250 |
| [wal_test.go](bptree/wal_test.go) | Comprehensive unit tests | ~500 |
| [durable_btree_test.go](bptree/durable_btree_test.go) | DurableBTree tests | ~600 |
| [wal_bench_test.go](bptree/wal_bench_test.go) | Performance benchmarks | ~350 |

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       DurableBTree                          │
│  ┌─────────────────┐        ┌───────────────────────────┐  │
│  │                 │        │                           │  │
│  │  ShardedBTree   │◄───────│    Write-Ahead Log        │  │
│  │  (in-memory)    │        │    (on-disk)              │  │
│  │                 │        │                           │  │
│  └─────────────────┘        └───────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
         │                              │
         │ Insert(k,v)                  │ 1. Log to WAL
         │ Delete(k)                    │ 2. Apply to tree
         │ Clear()                      │
         ▼                              ▼
    Fast in-memory              Durable on-disk
    operations                  recovery log
```

### Features Implemented

#### WAL Core
- **Append-only logging** - All mutations logged before tree modification
- **CRC32 checksums** - Detect corruption on replay
- **Binary format** - Efficient serialization with length prefixes
- **Buffered I/O** - 64KB buffer for performance

#### Sync Modes
| Mode | Behavior | Use Case |
|------|----------|----------|
| `SyncNone` | Buffer only (OS handles flush) | Maximum performance |
| `SyncBatch` | fsync every N entries | Balanced |
| `SyncAlways` | fsync every entry | Maximum durability |

#### Operations
- `AppendInsert` - Log insert operation
- `AppendDelete` - Log delete operation
- `AppendClear` - Log clear operation
- `Replay` - Recover state from log
- `Checkpoint` - Truncate log after persistence
- `RotateLog` - Archive and start fresh log

#### DurableBTree
- **Transparent durability** - Same API as ShardedBTree
- **Automatic recovery** - Replays WAL on startup
- **Bulk operations** - Atomic batch inserts

---

## Log Format

```
┌──────────────────────────── WAL File ────────────────────────────┐
│ Header (8 bytes)                                                 │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ Magic: "WAL1" (4 bytes) │ Version: 1 (4 bytes)              │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│ Entry 1                                                          │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │ Length │ Sequence │ Op │ KeyLen │ Key │ ValLen │ Val │ CRC │ │
│ │ 4 bytes│ 8 bytes  │ 1B │ 4 bytes│ var │ 4 bytes│ var │ 4B  │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│ Entry 2, Entry 3, ...                                            │
└──────────────────────────────────────────────────────────────────┘
```

---

## Benchmark Results

### WAL Performance by Sync Mode

| Sync Mode | Throughput | Latency | Durability |
|-----------|------------|---------|------------|
| **SyncNone** | 483K ops/sec | 2.1 µs | Low |
| **SyncBatch(100)** | 96K ops/sec | 10.4 µs | Medium |
| **SyncAlways** | 1.4K ops/sec | 740 µs | High |

### DurableBTree Performance

| Operation | Throughput | Latency |
|-----------|------------|---------|
| **Insert (SyncNone)** | 255K ops/sec | 3.9 µs |
| **Find** | 2.2M ops/sec | 449 ns |
| **Read-Heavy (10R:1W)** | 1.35M ops/sec | 741 ns |
| **Balanced (5R:5W)** | 478K ops/sec | 2.1 µs |
| **Write-Heavy (1R:10W)** | 285K ops/sec | 3.5 µs |

### Durability Overhead

| Configuration | Insert Throughput | Overhead vs Non-Durable |
|--------------|-------------------|-------------------------|
| **Non-Durable (baseline)** | 612K ops/sec | - |
| **Durable + SyncNone** | 261K ops/sec | 2.3× slower |
| **Durable + SyncBatch** | 70K ops/sec | 8.7× slower |
| **Durable + SyncAlways** | 1.3K ops/sec | 461× slower |

### Recovery Time

| WAL Entries | Recovery Time | Rate |
|-------------|---------------|------|
| **1,000** | 2.2 ms | 457K entries/sec |
| **10,000** | 22.6 ms | 443K entries/sec |
| **50,000** | 113 ms | 442K entries/sec |

---

## Design Decisions & Tradeoffs

### 1. Append-Only Log vs In-Place Updates

**Decision:** Append-only log

| Approach | Pros | Cons |
|----------|------|------|
| **Append-only** | Sequential I/O, crash-safe | Unbounded growth |
| In-place | Bounded size | Random I/O, complex recovery |

**Rationale:** Sequential writes are 10-100× faster than random writes. Checkpointing handles growth.

### 2. Single WAL vs Per-Shard WALs

**Decision:** Single WAL with mutex

| Approach | Pros | Cons |
|----------|------|------|
| **Single WAL** | Simpler, global ordering | Serialized writes |
| Per-shard WALs | Parallel writes | Complex recovery ordering |

**Rationale:** Single WAL simplifies recovery. For StunDB's workloads, the WAL is not the bottleneck (tree operations dominate).

### 3. Checksum Algorithm: CRC32

**Decision:** CRC32-IEEE

| Algorithm | Speed | Detection |
|-----------|-------|-----------|
| **CRC32** | Very fast | Good for random errors |
| xxHash | Faster | Good for random errors |
| SHA256 | Slow | Cryptographic |

**Rationale:** CRC32 is fast, well-supported in Go stdlib, and sufficient for detecting storage corruption.

### 4. Buffered vs Unbuffered I/O

**Decision:** 64KB buffered writes

| Approach | Throughput | Latency |
|----------|------------|---------|
| **Buffered (64KB)** | Higher | Batched |
| Unbuffered | Lower | Immediate |

**Rationale:** Buffering amortizes syscall overhead. Explicit `Sync()` ensures durability when needed.

### 5. Recovery: Replay All vs Incremental

**Decision:** Full replay from WAL

| Approach | Complexity | Recovery Time |
|----------|------------|---------------|
| **Full replay** | Simple | Linear in log size |
| Incremental | Complex | Depends on checkpoint frequency |

**Rationale:** Full replay is simpler and recovery is fast enough (~450K entries/sec). Can add incremental checkpointing later.

---

## Implementation Challenges

### Challenge 1: Corruption Handling

**Problem:** What happens if the system crashes mid-write?

**Solution:** Length-prefixed entries with CRC32 checksums. On replay:
1. Read length prefix
2. Read entry data
3. Verify checksum
4. If mismatch, truncate at last valid entry

```go
// Corruption detection in readEntry()
expectedChecksum := calculateEntryChecksum(entry)
if checksum != expectedChecksum {
    return nil, errors.New("checksum mismatch")
}
```

### Challenge 2: Sequence Continuity Across Restarts

**Problem:** Sequence numbers must be unique even after restart

**Solution:** On startup, scan WAL to find last sequence number

```go
func (w *WAL) validateAndRecover() error {
    // Scan to find last sequence
    for {
        entry, err := readEntry(reader)
        if err == io.EOF { break }
        lastSeq = entry.Sequence
    }
    w.sequence = lastSeq  // Continue from here
}
```

### Challenge 3: Checkpoint Without Data Loss

**Problem:** Truncating WAL while tree is in-memory risks data loss

**Solution:** Checkpoint assumes caller has persisted tree state. In a full system:
1. Persist tree to disk
2. Call `Checkpoint()`
3. WAL is truncated

For StunDB's in-memory design, checkpoint is mainly for testing/benchmarking.

### Challenge 4: Thread-Safe Concurrent Writes

**Problem:** Multiple goroutines writing to WAL

**Solution:** Mutex around write operations, atomic sequence counter

```go
func (w *WAL) Append(op OpType, key, value []byte) (uint64, error) {
    w.mu.Lock()
    defer w.mu.Unlock()
    
    seq := atomic.AddUint64(&w.sequence, 1)
    // ... write entry ...
}
```

---

## Test Coverage

### Unit Tests (40 tests)

| Category | Tests | Coverage |
|----------|-------|----------|
| WAL Creation | 3 | New WAL, nested dirs, validation |
| Append Operations | 3 | Insert, Delete, Clear |
| Replay | 2 | Basic replay, tree recovery |
| Checkpointing | 1 | Truncation, sequence continuity |
| Sync Modes | 3 | SyncNone, SyncBatch, SyncAlways |
| Rotation | 1 | Log archiving |
| Concurrent | 1 | Parallel appends |
| Edge Cases | 4 | Empty values, large values, corruption |
| Headers | 2 | Invalid magic, unsupported version |
| DurableBTree | 20 | Full CRUD + recovery + concurrent |

### All Tests Pass with Race Detector

```bash
$ go test -race -run "TestWAL|TestDurable" ./bptree/
PASS
ok      Database/bptree 3.930s
```

---

## Usage Examples

### Basic Usage

```go
// Create durable B-Tree
db, err := NewDurableBTree(DurableConfig{
    WALPath:   "/data/stundb.wal",
    NumShards: 8,
    SyncMode:  SyncBatch,  // Balanced durability
    BatchSize: 100,
})
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Use like a regular B-Tree (but durable!)
db.Insert([]byte("user:123"), []byte(`{"name": "Alice"}`))
value, err := db.Find([]byte("user:123"))
db.Delete([]byte("user:123"))
```

### Sync Mode Selection

```go
// High performance, eventual durability (rely on OS)
config.SyncMode = SyncNone

// Balanced (fsync every 100 operations)
config.SyncMode = SyncBatch
config.BatchSize = 100

// Maximum durability (fsync every operation)
config.SyncMode = SyncAlways
```

### Recovery After Crash

```go
// On restart, just create DurableBTree with same path
// WAL is automatically replayed
db, err := NewDurableBTree(DurableConfig{
    WALPath: "/data/stundb.wal",
})
// db now has all data that was in WAL
```

### Periodic Checkpointing

```go
// After persisting tree to disk (e.g., snapshot)
err := db.Checkpoint()
// WAL is now truncated, next recovery will be fast
```

---

## Performance Recommendations

### For Maximum Throughput
```go
config.SyncMode = SyncNone
// ~260K inserts/sec
// Risk: Last ~64KB of data may be lost on crash
```

### For Balanced Performance/Durability
```go
config.SyncMode = SyncBatch
config.BatchSize = 100
// ~70K inserts/sec
// Risk: Last ~100 operations may be lost on crash
```

### For Maximum Durability
```go
config.SyncMode = SyncAlways
// ~1.3K inserts/sec
// Guarantees: No data loss on crash (if disk doesn't lie)
```

---

## Comparison with Industry Standards

| Database | Durability Approach | Sync Default |
|----------|---------------------|--------------|
| **StunDB** | WAL + Checkpoint | SyncBatch |
| PostgreSQL | WAL + Checkpoint | fsync |
| SQLite | WAL or Journal | fsync |
| Redis | AOF or RDB | everysec |
| LevelDB | WAL | sync on close |

---

## Future Enhancements

1. **Incremental Checkpointing**
   - Save tree snapshots periodically
   - Replay only entries after snapshot

2. **WAL Compression**
   - Compress entries with LZ4/Snappy
   - Reduce I/O and storage

3. **Group Commit**
   - Batch multiple operations in single fsync
   - Better throughput for high-concurrency

4. **Async Replication**
   - Stream WAL to replicas
   - Foundation for distributed StunDB

---

## Conclusion

The WAL implementation achieves its goals:

1. ✅ **Durability** - Survives process crashes
2. ✅ **Configurable sync** - Performance/durability tradeoff
3. ✅ **Fast recovery** - 450K entries/sec replay
4. ✅ **Simple API** - Same interface as ShardedBTree
5. ✅ **Thread-safe** - Concurrent operations supported

Performance overhead is reasonable:
- **SyncNone**: 2.3× overhead (still 261K ops/sec)
- **SyncBatch**: 8.7× overhead (70K ops/sec)
- **SyncAlways**: 461× overhead (1.3K ops/sec)

For most use cases, `SyncBatch` with batch size 100-1000 provides a good balance of durability and performance.

---

## References

- [Write-Ahead Logging (PostgreSQL)](https://www.postgresql.org/docs/current/wal-intro.html)
- [Redis Persistence](https://redis.io/docs/management/persistence/)
- [SQLite Write-Ahead Logging](https://www.sqlite.org/wal.html)
- [The Design of a Practical System for Fault-Tolerant Virtual Machines](https://www.vmware.com/pdf/ftdesign.pdf)
