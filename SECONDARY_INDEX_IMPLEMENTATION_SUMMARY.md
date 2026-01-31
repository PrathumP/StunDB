# Secondary Index Implementation Summary

## Overview

This document summarizes the secondary index implementation for StunDB, enabling efficient queries by non-primary key fields. Secondary indexes provide O(log n) lookups on arbitrary fields, dramatically improving query flexibility without sacrificing performance.

## The Theory Behind Secondary Indexes

### The Problem: Why Do We Need Indexes?

In a key-value store like StunDB, data is organized by **primary key**:

```
user:001 → {"email":"alice@example.com","name":"Alice","dept":"Eng"}
user:002 → {"email":"bob@example.com","name":"Bob","dept":"Sales"}
user:003 → {"email":"carol@example.com","name":"Carol","dept":"Eng"}
...
user:N   → {"email":"...","name":"...","dept":"..."}
```

**Finding by primary key** is fast: **O(log n)** using B-Tree traversal.

But what if you want to find a user by email? Without an index:

```
Query: Find user where email = "carol@example.com"

Without Index (Full Table Scan):
  1. Read user:001 → check email → not a match
  2. Read user:002 → check email → not a match  
  3. Read user:003 → check email → MATCH! ✓
  ...potentially check ALL N records

Time Complexity: O(n) - LINEAR scan through all data
```

### The Solution: Secondary Indexes

A **secondary index** is a separate data structure that maps **field values → primary keys**:

```
Email Index (Unique):
  "alice@example.com"  → user:001
  "bob@example.com"    → user:002
  "carol@example.com"  → user:003

Department Index (Non-Unique):
  "Engineering" → [user:001, user:003]
  "Sales"       → [user:002]
```

Now finding by email becomes:

```
Query: Find user where email = "carol@example.com"

With Index:
  1. index.Find("carol@example.com") → user:003  [O(log n)]
  2. tree.Get("user:003") → full record          [O(log n)]

Time Complexity: O(log n) + O(log n) = O(log n) - LOGARITHMIC!
```

### Time Complexity Comparison

| Dataset Size | Full Scan O(n) | Indexed O(log n) | Speedup |
|--------------|----------------|------------------|---------|
| 1,000 | 1,000 ops | ~10 ops | **100×** |
| 10,000 | 10,000 ops | ~13 ops | **770×** |
| 100,000 | 100,000 ops | ~17 ops | **5,900×** |
| 1,000,000 | 1,000,000 ops | ~20 ops | **50,000×** |

The larger your dataset, the more dramatic the speedup!

### How B-Tree Indexes Work

StunDB's secondary indexes use B-Trees (same as the primary storage), which provide:

```
B-Tree Index Structure:
                    ┌─────────────────┐
                    │ "m" comparator  │
                    └────────┬────────┘
              ┌──────────────┴──────────────┐
              ▼                              ▼
     ┌────────────────┐             ┌────────────────┐
     │ "d" | "h" | "k"│             │ "p" | "s" | "w"│
     └──┬────┬────┬───┘             └──┬────┬────┬───┘
        ▼    ▼    ▼                    ▼    ▼    ▼
      leaves with actual key→pk mappings

Search for "s":
  1. Root: "s" > "m" → go right
  2. Node: "p" < "s" < "w" → middle child  
  3. Leaf: Found "s" → return primary key

Only 3 node accesses regardless of tree size!
```

**B-Tree Properties:**
- **Balanced**: All leaves at same depth
- **Sorted**: Keys in order for range queries
- **Fan-out**: Each node holds many keys (cache-friendly)
- **Self-balancing**: Automatically rebalances on insert/delete

### Unique vs Non-Unique Indexes

**Unique Index** (email, SSN, username):
```
One-to-one mapping:
  "alice@example.com" → user:001
  "bob@example.com"   → user:002
  
Storage: Simple key→value in B-Tree
Lookup:  Single B-Tree find = O(log n)
```

**Non-Unique Index** (department, status, category):
```
One-to-many mapping:
  "Engineering" → [user:001, user:003, user:007, user:012, ...]
  
Storage: Encoded list of primary keys
Lookup:  B-Tree find + decode list = O(log n + k) where k = matches
```

### The Trade-offs

Secondary indexes aren't free. Here's what you pay:

| Benefit | Cost |
|---------|------|
| Fast queries by field | Extra memory for index storage |
| O(log n) lookups | O(log n) extra work on each insert |
| Range queries on field | O(log n) extra work on each update |
| Unique constraints | O(log n) extra work on each delete |

**Rule of thumb**: Create indexes on fields you query frequently. Don't index fields you rarely search by.

## Real-World Performance Comparison

We measured actual performance comparing full table scans vs indexed lookups:

### Point Query: Find One Record by Email

```
┌───────────────────────────────────────────────────────────────────┐
│         Point Query Performance: Full Scan vs Index               │
├─────────────┬───────────────┬─────────────┬──────────────────────┤
│ Dataset     │ Full Scan     │ Indexed     │ Speedup              │
├─────────────┼───────────────┼─────────────┼──────────────────────┤
│ 1,000       │ 30,263 ns     │ 269 ns      │ 112× faster          │
│ 10,000      │ 378,559 ns    │ 293 ns      │ 1,292× faster        │
│ 100,000     │ 9,252,562 ns  │ 344 ns      │ 26,897× faster       │
└─────────────┴───────────────┴─────────────┴──────────────────────┘
```

**Key Insight**: Indexed lookup stays nearly constant (~300 ns) regardless of dataset size, while full scan grows linearly.

### Live Test Results

```
Dataset size: 1,000 records
  Full scan avg: 23.888µs
  Indexed avg:   379ns
  Speedup:       63× faster

Dataset size: 10,000 records
  Full scan avg: 533.351µs  
  Indexed avg:   413ns
  Speedup:       1,290× faster

Dataset size: 50,000 records
  Full scan avg: 5.636ms
  Indexed avg:   457ns
  Speedup:       12,321× faster
```

### Range Query: Find Records in Age Range 25-30

```
┌───────────────────────────────────────────────────────────────────┐
│         Range Query Performance: Full Scan vs Index               │
├─────────────┬───────────────┬──────────────┬─────────────────────┤
│ Dataset     │ Full Scan     │ Indexed      │ Speedup             │
├─────────────┼───────────────┼──────────────┼─────────────────────┤
│ 1,000       │ 55,435 ns     │ 21,833 ns    │ 2.5× faster         │
│ 10,000      │ 737,884 ns    │ 63,306 ns    │ 11.7× faster        │
│ 100,000     │ 20,962,817 ns │ 401,669 ns   │ 52× faster          │
└─────────────┴───────────────┴──────────────┴─────────────────────┘
```

**Key Insight**: Range queries also benefit significantly, especially at scale.

### Visual: Scaling Behavior

```
Time (log scale)
    │
10s │                                              ╱ Full Scan
    │                                           ╱   O(n)
 1s │                                        ╱
    │                                     ╱
100ms│                                  ╱
    │                               ╱
10ms│                            ╱
    │                         ╱
 1ms│                      ╱
    │                   ╱
100µs│                ╱
    │             ╱
10µs│          ╱
    │       ╱
 1µs│    ╱───────────────────────────────── Indexed O(log n)
    │ ╱
    └──────────────────────────────────────────────────────
         1K      10K      100K      1M       10M     Dataset Size
```

The indexed lookup stays flat (logarithmic growth is nearly invisible at this scale), while full scan grows linearly.

### When to Use Secondary Indexes

| Use Index When... | Avoid Index When... |
|-------------------|---------------------|
| Field is queried frequently | Field is rarely searched |
| Dataset is large (>1000 records) | Dataset is tiny (<100 records) |
| Selectivity is high (few matches) | Every query returns most records |
| Point lookups or small ranges | Always need full table data |
| Unique constraints required | Write-heavy, read-light workload |

### Index Overhead Analysis

Each index adds overhead to write operations:

```
Insert Performance with Varying Index Count:
┌────────────┬─────────────┬────────────────────────────────┐
│ # Indexes  │ Insert Time │ Overhead vs No Index           │
├────────────┼─────────────┼────────────────────────────────┤
│ 0          │ 1,398 ns    │ baseline                       │
│ 1          │ 2,940 ns    │ +110% (2.1× slower)            │
│ 3          │ 326,729 ns  │ +23,270% (much slower)*        │
└────────────┴─────────────┴────────────────────────────────┘
* Note: 3-index benchmark includes non-unique indexes with high fan-out
```

**Recommendation**: Keep index count minimal. 1-3 well-chosen indexes usually sufficient.

## Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────────┐
│                      IndexedBTree                           │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              Primary B-Tree (ShardedBTree)            │  │
│  │     key → value (full record)                         │  │
│  └──────────────────────────────────────────────────────┘  │
│                           │                                  │
│  ┌─────────┬─────────┬────┴────┬──────────┬─────────────┐  │
│  │ Index 1 │ Index 2 │ Index 3 │    ...    │  Index N    │  │
│  │ (email) │ (name)  │ (age)   │           │  (custom)   │  │
│  └────┬────┴────┬────┴────┬────┴──────────┴─────────────┘  │
│       ▼         ▼         ▼                                  │
│  ┌─────────────────────────────────────────────────────────┐│
│  │           SecondaryIndex (ShardedBTree each)            ││
│  │     extracted_value → primary_key(s)                     ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

```
INSERT Operation:
   user:001 → {"email":"alice@example.com","name":"Alice","age":25}
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
    email_index          name_index          age_index
   "alice@..." → 001     "Alice" → 001       "25" → 001

QUERY by email:
   email = "alice@example.com"
          │
          ▼
   email_index.FindOne("alice@example.com")
          │
          ▼
   primary_key = "user:001"
          │
          ▼
   primary_btree.Get("user:001")
          │
          ▼
   {"email":"alice@example.com","name":"Alice","age":25}
```

## Implementation Details

### SecondaryIndex (`secondary_index.go`)

The `SecondaryIndex` struct wraps a `ShardedBTree` to map extracted field values to primary keys:

```go
type SecondaryIndex struct {
    tree      *ShardedBTree  // Index storage
    extractor KeyExtractor   // Extracts indexed field from value
    unique    bool           // Whether index enforces uniqueness
    mu        sync.RWMutex   // Concurrency control
}
```

**Key Features:**
- **Unique Indexes**: Direct mapping (indexKey → primaryKey)
- **Non-Unique Indexes**: List mapping (indexKey → [pk1, pk2, ...])
- **Automatic Extraction**: `KeyExtractor` functions pull indexed fields from records
- **Thread-Safe**: Full concurrency support with RWMutex

### IndexedBTree (`indexed_btree.go`)

The `IndexedBTree` provides a high-level API with automatic index maintenance:

```go
type IndexedBTree struct {
    tree    *ShardedBTree
    indexes map[string]*SecondaryIndex
    mu      sync.RWMutex
}
```

**Operations:**
- `CreateIndex(name, extractor, unique)` - Create new secondary index
- `CreateIndexWithRebuild(...)` - Create index and populate from existing data
- `DropIndex(name)` - Remove an index
- `Insert(key, value)` - Insert with automatic index updates
- `Update(key, value)` - Update with old/new index maintenance
- `Delete(key)` - Delete with index cleanup
- `FindByIndex(name, key)` - Query by secondary index
- `FindAllByIndex(name, key)` - Find all matches (non-unique)
- `FindRangeByIndex(name, start, end)` - Range queries on index

### Key Extractors

Built-in extractors for common use cases:

| Extractor | Purpose | Example |
|-----------|---------|---------|
| `JSONFieldExtractor(field)` | Extract JSON field | `{"email":"a@b.com"}` → `a@b.com` |
| `PrefixExtractor(n)` | First N bytes | `hello` → `hel` (n=3) |
| `OffsetExtractor(off, len)` | Substring at offset | `abcdef` → `cd` (off=2, len=2) |
| `CompositeExtractor(extractors...)` | Combine multiple | Multi-field composite keys |

### Non-Unique Index Encoding

For non-unique indexes, multiple primary keys are stored per index key:

```
Format: [count:4][len1:4][pk1:len1][len2:4][pk2:len2]...

Example: "Engineering" → [user:001, user:003, user:007]
Encoded: [3][8][user:001][8][user:003][8][user:007]
```

## Benchmark Results

### Raw Performance (AMD Ryzen 7 4800H, 16 threads)

| Operation | Time | Memory | Allocs |
|-----------|------|--------|--------|
| **Secondary Index Operations** ||||
| Insert (Unique) | **1,516 ns/op** | 827 B | 14 |
| Insert (Non-Unique) | 234,260 ns/op | 359 KB | 3,543 |
| Find (Unique) | **364 ns/op** | 47 B | 2 |
| Find (Non-Unique) | 92,250 ns/op | 156 KB | 2,003 |
| **IndexedBTree Operations** ||||
| Insert (No Indexes) | **1,398 ns/op** | 868 B | 13 |
| Insert (1 Index) | 2,940 ns/op | 1.5 KB | 22 |
| Insert (3 Indexes) | 326,729 ns/op | 491 KB | 5,032 |
| Find by Primary Key | **361 ns/op** | 55 B | 2 |
| Find by Index | **405 ns/op** | 47 B | 2 |
| Find by Index + Primary | 716 ns/op | 110 B | 3 |
| Update (w/ index maintenance) | 531,772 ns/op | 773 KB | 8,357 |
| Delete (w/ index cleanup) | 738,081 ns/op | 61 KB | 3,151 |
| **Concurrent Operations** ||||
| Concurrent Insert | **1,373 ns/op** | 296 B | 11 |
| Concurrent Find | **104 ns/op** | 47 B | 2 |
| Concurrent Mixed | 4,344 ns/op | 3.3 KB | 62 |

### Performance Analysis

1. **Unique Index Lookups**: ~364 ns - nearly as fast as primary key lookups (361 ns)
2. **Index Overhead on Insert**: ~2x with 1 index, scales with index count
3. **Concurrent Reads**: Excellent scaling at 104 ns/op with 16 threads
4. **Non-Unique Performance**: Higher due to encoding/decoding multiple keys

### Throughput Estimates

| Operation | Ops/Second |
|-----------|------------|
| Unique Index Insert | ~660,000 |
| Unique Index Find | ~2,750,000 |
| Concurrent Find | ~9,600,000 |
| Primary + Index Lookup | ~1,400,000 |

## API Usage Examples

### Creating an IndexedBTree with Indexes

```go
// Create indexed B-tree
ibt := NewIndexedBTree(64, 16) // order=64, shards=16

// Create unique email index
ibt.CreateIndex("email", JSONFieldExtractor("email"), true)

// Create non-unique department index
ibt.CreateIndex("department", JSONFieldExtractor("department"), false)

// Create composite index (multi-field)
ibt.CreateIndex("name_dept", CompositeExtractor(
    JSONFieldExtractor("name"),
    JSONFieldExtractor("department"),
), false)
```

### Basic Operations

```go
// Insert with automatic index maintenance
err := ibt.Insert(
    Keytype("user:001"),
    Valuetype(`{"email":"alice@example.com","name":"Alice","department":"Engineering"}`),
)

// Query by email (unique index)
value, found := ibt.FindByIndex("email", []byte("alice@example.com"))

// Query by department (non-unique index)  
primaryKeys := ibt.FindAllByIndex("department", []byte("Engineering"))

// Range query on index
results := ibt.FindRangeByIndex("name", []byte("A"), []byte("M"))
```

### Building Index on Existing Data

```go
// Create index and rebuild from existing data
err := ibt.CreateIndexWithRebuild("status", JSONFieldExtractor("status"), false)
```

## Design Decisions

### 1. Separate Index Storage
**Choice**: Each secondary index uses its own `ShardedBTree`.
**Rationale**: 
- Independent scaling per index
- Isolated failure domains
- Can drop index without affecting primary data

### 2. Key Extractor Pattern
**Choice**: Function-based extraction (`KeyExtractor` type).
**Rationale**:
- Maximum flexibility for custom logic
- Composable via `CompositeExtractor`
- No schema dependency

### 3. Encoded Multi-Key Storage (Non-Unique)
**Choice**: Encode all primary keys in single value.
**Rationale**:
- Atomic updates
- No secondary data structures
- Efficient for moderate fan-out

**Trade-off**: High fan-out indexes (1000+ matches per key) will have higher memory/latency.

### 4. Automatic Index Maintenance
**Choice**: Indexes updated automatically on Insert/Update/Delete.
**Rationale**:
- Simpler API
- Guaranteed consistency
- No manual sync required

## Limitations & Future Work

### Current Limitations

1. **Memory**: Each index is a separate B-tree in memory
2. **Fan-out**: Non-unique indexes with many matches per key are slower
3. **Partial Updates**: Must read old value to update indexes correctly
4. **No Index-Only Scans**: Always fetches full record from primary tree

### Future Improvements

1. **Index-Only Queries**: Store covering data in index to avoid primary lookup
2. **Bloom Filters**: Quick negative lookups for sparse indexes
3. **Index Compression**: Prefix compression for sorted index keys
4. **Async Index Rebuild**: Background population for large datasets
5. **Index Statistics**: Cardinality estimation for query planning

## Test Coverage

### Unit Tests (37 tests)

| Category | Tests | Coverage |
|----------|-------|----------|
| SecondaryIndex Creation | 2 | Construction, nil extractor |
| Insert Operations | 4 | Unique, non-unique, duplicates |
| Remove Operations | 3 | Unique, non-unique, missing |
| Update Operations | 2 | Value changes, key changes |
| Find Operations | 4 | FindOne, FindAll, FindRange |
| Extractors | 5 | JSON, Prefix, Offset, Composite |
| Encoding | 3 | Primary key encode/decode |
| IndexedBTree | 15 | Full CRUD with index maintenance |
| Concurrency | 2 | Parallel inserts, mixed ops |

### Benchmark Coverage

- Insert performance (unique vs non-unique)
- Find performance (single vs multi-result)
- IndexedBTree overhead scaling
- Concurrent operation throughput

## Files Added

| File | Lines | Purpose |
|------|-------|---------|
| `secondary_index.go` | ~440 | Core index implementation |
| `indexed_btree.go` | ~320 | High-level wrapper with auto-maintenance |
| `secondary_index_test.go` | ~850 | Comprehensive unit tests |
| `secondary_index_bench_test.go` | ~300 | Performance benchmarks |

**Total**: ~1,910 lines of new code

## Conclusion

The secondary index implementation provides:

- **Sub-microsecond lookups** (~364 ns for unique indexes)
- **Flexible indexing** via KeyExtractor pattern
- **Thread-safe operations** with full concurrency support
- **Automatic maintenance** - no manual index sync required
- **Scalable architecture** - each index independently sharded

This enables StunDB to support efficient queries on any field, not just primary keys, opening up use cases like:
- User lookup by email, username, or phone
- Order search by status, customer, or date
- Product filtering by category, price range, or availability
