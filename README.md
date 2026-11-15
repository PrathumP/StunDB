# StunDB

StunDB is an in-memory key-value store in Go. Its core is a concurrent B+-tree
built the way [bbtree](https://github.com/XiangpengHao/bbtree) is, following
its post [*bbtree: fastest ordered map*](https://bbtree.xiangpeng.systems/post/):

- **Inline keys behind a fence-derived prefix.** Each node keeps a dense
  array of 2-byte (integers, UUIDs) or 4-byte (strings) slices of its keys,
  taken after the bytes every key in the node shares. One cache line holds
  32 two-byte ones, and a search reads a key only for the one candidate they
  leave. Nodes are allocated so that they start on a cache line.
- **A branchless count instead of a binary search.** A node is searched by
  counting its inline keys below the target, in AVX2 (`VPCMPGTW`,
  `VPMOVMSKB`, `POPCNT`) through Go's `simd/archsimd`.
- **Optimistic lock coupling.** Every node has a version lock; readers
  validate versions and write nothing, so lookups scale across cores.
  Writers latch one node at a time and split full nodes on the way down.
- **Batched lookups and inserts** that overlap their cache misses, lanes
  moving down the tree in lockstep with software prefetches.

On top of it sit a byte-keyed store, secondary indexes and a write-ahead log.
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) explains each technique, how it
maps from Rust to Go, and where the Go version differs and why.

## Results

10M entries with `uint64` values: the post's workloads, measured by
[`bench/`](bench/) on a Ryzen 7 4800H laptop. The full tables are in
[bench/results/](bench/results/).

**Against the post, ratio for ratio.** Each cell is the speedup over the
baseline map on the same machine. For StunDB the baseline is google/btree,
Go's most used B-tree; for bbtree it is Rust's std `BTreeMap`, from the
post's own runs on a Ryzen 9 9900X. One thread, lookups half of which miss,
batched calls in brackets:

| keys | get | insert | update | 100-entry scan |
|---|---|---|---|---|
| `uint64` | **2.2× (3.4×)** · post 2.4× (3.6×) | 1.4× (2.3×) · post 1.5× (2.1×) | 1.9× · post 2.3× | 1.6× · post 2.7× |
| UUID | **3.6× (5.1×)** · post 2.8× (3.9×) | 2.3× (3.0×) · post 2.1× (2.7×) | 3.2× · post 2.5× | 2.2× · post 2.3× |
| Wikipedia titles | **1.9× (2.8×)** · post 1.7× (2.4×) | 1.7× (2.2×) · post 1.5× (1.9×) | 1.8× · post 1.6× | 1.9× · post 1.7× |
| JSON keys | **3.5× (4.6×)** · post 4.1× (6.5×) | 2.3× (3.0×) · post 2.3× (3.6×) | 3.2× · post 3.4× | 2.4× · post 2.0× |

Nanoseconds per operation, one thread:

| keys | stundb get (batched) | google/btree get | stundb insert (batched) | google/btree insert |
|---|--:|--:|--:|--:|
| `uint64` | 380 (253) | 853 | 666 (395) | 907 |
| UUID | 383 (269) | 1363 | 643 (492) | 1458 |
| Wikipedia titles | 1026 (671) | 1905 | 1298 (1023) | 2221 |
| JSON keys | 513 (394) | 1806 | 836 (651) | 1949 |

**Threads** (`uint64` keys, millions of operations per second):

| | 1 thread | 8 (all cores) | 16 (SMT) |
|---|--:|--:|--:|
| stundb get | 3.4 | 19.3 | **29.8** |
| stundb get, batched | 5.3 | 25.9 | 26.3 |
| skipmap get | 0.4 | 2.9 | 4.5 |
| RWMutex+google/btree get | 1.5 | 9.4 | 14.0 |
| stundb update | 2.9 | 14.7 | **20.9** |
| RWMutex+google/btree update | 1.5 | 1.3 | 1.3 |

At 16 threads stundb's lookups are 6.6× the lock-free skiplist's. The post
measures 5.1× against crossbeam-skiplist at 24 threads and 12× at 96 cores,
which this laptop doesn't have. Under writes, a tree-wide `RWMutex` stops
scaling at one core, while stundb's updates scale with cores: 16× its
throughput at 16 threads.

**Memory:** 27.5 bytes per `uint64` entry, close to bbtree's 27.1. Go's
B-trees store items more tightly than Rust's std, though: they take 24.7,
so StunDB is 11% above them, 14% on UUIDs, and 19–24% on strings (a string
leaf is 1,568 bytes, rounded up to Go's 1,792-byte size class). The skiplist
takes 88.4.

**Against StunDB before this rewrite** (4 keys a node, one tree-wide
`RWMutex`): 3,752 ns a lookup and 4,202 ns an insert at 10M, so lookups are
now 9.9× faster (14.8× batched) and inserts 6.3×.

**Why the nanoseconds aren't the post's.** The post's 9900X has 32 MB of L3
per core complex, which holds a 10M-entry tree's internal nodes, and faster
DDR5. This laptop has 4 MB of L3 per core complex, and a dependent DRAM miss
costs it 94 ns (121 ns without huge pages, measured by pointer chasing). So
the internal level just above the leaves misses too: a lookup here waits on
about 3.5 dependent misses where the post's waits on about 1.5, which is
380 ns against 109. The same hardware penalty hits the baselines, so the
ratios hold. The concurrent numbers are bounded by the laptop's two memory
channels (about 30M lookups a second × 3.5 lines from DRAM), not by
contention: no lookup writes to shared memory.

## Quick start

The tree's node search uses Go's experimental SIMD package. Build with it
and with x86-64-v3 (AVX2, BMI, POPCNT), the Go analogue of the post's
`-C target-cpu=native`:

```bash
export GOEXPERIMENT=simd GOAMD64=v3
go build ./...
go test ./...
go run ./cmd/stundb
```

Without `GOEXPERIMENT=simd` (or on a CPU without AVX2, or off amd64) the
tree builds and runs the same, searching nodes with a branchless binary
search instead.

## Usage

The packages live under `internal/`, for use inside this module.

The tree is a generic concurrent ordered map over `uint64`, `[16]byte` and
`string` keys (and types defined on them):

```go
m := btree.New[uint64, uint64]()

m.Insert(42, 1)                      // safe from any goroutine
v, ok := m.Get(42)                   // never blocks, writes nothing
m.Update(42, func(v *uint64) { *v++ }) // in place, under the leaf's latch
m.Ascend(40, func(k, v uint64) bool { // in key order from 40
	return k < 100
})

keys := []uint64{1, 2, 42}
vals := make([]uint64, len(keys))
found := make([]bool, len(keys))
m.GetMany(keys, vals, found) // overlaps the lookups' cache misses
```

The byte-keyed store, with copies in and out:

```go
s := kv.New()
s.Insert([]byte("user:1"), []byte("Alice"))
value, err := s.Find([]byte("user:1"))
keys, values, err := s.GetRange([]byte("user:0"), []byte("user:9")) // inclusive
```

Secondary indexes on top of it:

```go
db := index.NewIndexedBTree(index.IndexedConfig{})
err := db.CreateIndex("email", index.JSONFieldExtractor("email"), true)
err = db.Insert([]byte("user:1"), []byte(`{"email":"alice@example.com"}`))
primaryKey, err := db.FindByIndex("email", []byte("alice@example.com"))
```

A write-ahead log that replays mutations into memory on restart:

```go
db, err := wal.NewDurableBTree(wal.DurableConfig{WALPath: "data/stundb.wal"})
defer db.Close()
err = db.Insert([]byte("user:1"), []byte("Alice"))
```

## Testing

```bash
GOEXPERIMENT=simd go test ./...       # AVX2 search
go test ./...                          # portable search
go test -race ./...                    # see below
GOARCH=386 go test ./...               # 32-bit, where a string is 8 bytes
go test -fuzz FuzzU64 ./internal/btree # differential fuzzing against a Go map
go test -fuzz FuzzString ./internal/btree
```

- **Differential tests** run random operations on the tree and on a Go map
  side by side, for each key type and many key shapes: dense, strided,
  random, sharing 300-byte prefixes, binary with zero bytes. The tree's
  invariants are checked after each run: sorted keys, every inline key equal
  to its key's window at the node's prefix, each prefix exactly what the
  node's fences share, and the leaf chain.
- **Stress tests** race readers, writers, batches and scans, and check every
  value a reader sees against what the writers have published. They catch
  the classic optimistic-lock-coupling bug (a descent that doesn't recheck
  the parent's version) within a few runs.
- **Race builds.** Optimistic readers read nodes while writers change them,
  then throw away what a version check shows was torn. That is the design,
  and the race detector reports it. So under `-race` the map serializes
  writers against readers with a lock (`race_on.go`). The layers above are
  race-checked against the same API, and the stress tests cover the
  optimistic paths in ordinary builds.
- **Regression tests** (`clear_test.go`, `regression_test.go`,
  `crash_test.go`) reproduce the bugs an adversarial review found: `Clear`
  racing a root split, counts after `Clear`, stale and inconsistent
  secondary indexes, a torn WAL tail hiding later writes, and partial bulk
  inserts. `crash_test.go` kills a child process in the middle of a write and
  recovers its WAL.

## Benchmarks

`bench/` is a separate module, so the core has no dependencies. It runs the
post's workloads against other Go maps: 10M entries with `uint64` values;
`uint64`, UUID, Wikipedia-title and JSON-key keys; lookups half of which
miss, inserts, updates and 100-entry scans; on one thread and as threads
are added; and heap bytes per entry.

```bash
cd bench
./fetch-data.sh                         # Wikipedia titles, 110 MB
GOAMD64=v3 GOEXPERIMENT=simd go run . -mode single -keys u64,uuid
./run-all.sh                            # every cell, about an hour
```

Microbenchmarks of the tree alone: `go test -bench . ./internal/btree`.

## Limitations

- **No merges.** Removal never merges nodes, so a tree that shrinks keeps its
  nodes until `Clear`. Lookups and scans stay correct; memory is not handed
  back.
- **Key types.** `uint64`, `[16]byte` and `string` (and types defined on
  them) only. `[]byte` keys go through `string`. `kv.Store` does that for
  you, and `btree.BytesKey` looks one up without copying.
- **AVX2 only.** The SIMD search is AVX2. There is no AVX-512 path (the post's
  `_mm512_cmplt_epu16_mask`) and no NEON path, and `GOEXPERIMENT=simd` is
  experimental in Go 1.26.
- **No `Len` in O(1)**, by design (a shared counter is contention);
  `kv.Store.Count` keeps a striped count.
- **Scans are not snapshots.** An entry present for a whole scan is seen
  once; one written during it may or may not be. A write that races `Clear`
  either lands before it or is discarded with the old contents.
- The WAL records mutations and replays them; the tree itself is never
  written to disk.
