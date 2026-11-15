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
[`bench/`](bench/). The current code was measured on two GitHub Actions
runners (4 vCPUs each) by [the benchmark workflow](.github/workflows/bench.yml):
an AMD EPYC 9V74 (Zen 4) and an Intel Xeon 6975P-C. Each map runs on the same
VM in the same job, so ratios between maps hold; nanoseconds on shared VMs
don't compare with other machines. The full tables are in
[bench/results/](bench/results/).

**Against the post, ratio for ratio.** Each cell is the speedup over the
baseline map on the same machine. For StunDB the baseline is google/btree,
Go's most used B-tree; for bbtree it is Rust's std `BTreeMap`, from the
post's own runs on a Ryzen 9 9900X. One thread, lookups half of which miss,
batched calls in brackets:

| keys | get: EPYC · Xeon · post | insert: EPYC · Xeon · post |
|---|---|---|
| `uint64` | 1.9× (5.8×) · 2.4× (7.6×) · 2.4× (3.6×) | 1.6× (2.5×) · 1.6× (3.0×) · 1.5× (2.1×) |
| UUID | 3.7× (8.3×) · 3.2× (9.3×) · 2.8× (3.9×) | 2.6× (3.9×) · 2.1× (3.6×) · 2.1× (2.7×) |
| Wikipedia titles | 2.2× (3.0×) · 2.0× (2.9×) · 1.7× (2.4×) | 1.8× (2.4×) · 1.7× (2.5×) · 1.5× (1.9×) |
| JSON keys | 3.8× (6.6×) · 3.7× (6.9×) · 4.1× (6.5×) | 2.4× (3.7×) · 2.5× (3.7×) · 2.3× (3.6×) |

| keys | update: EPYC · Xeon · post | 100-entry scan: EPYC · Xeon · post |
|---|---|---|
| `uint64` | 1.7× · 1.9× · 2.3× | 1.7× · 1.5× · 2.7× |
| UUID | 3.1× · 2.7× · 2.5× | 2.3× · 1.7× · 2.3× |
| Wikipedia titles | 2.1× · 2.0× · 1.6× | 2.5× · 1.9× · 1.7× |
| JSON keys | 3.3× · 3.3× · 3.4× | 3.0× · 2.3× · 2.0× |

Batched lookups of fixed-size keys gain far more on these server CPUs than on
the laptop below: 5.8 to 9.3 times google/btree for `uint64` and UUID keys,
against 3.4 and 5.1 on the laptop. I haven't measured why; more memory
requests in flight per core is the likely reason.

Nanoseconds per operation, one thread (EPYC / Xeon):

| keys | stundb get (batched) | google/btree get | stundb insert (batched) | google/btree insert |
|---|--:|--:|--:|--:|
| `uint64` | 362 (119) / 302 (95) | 690 / 723 | 410 (258) / 377 (202) | 648 / 604 |
| UUID | 345 (152) / 353 (120) | 1265 / 1124 | 500 (330) / 490 (292) | 1276 / 1048 |
| Wikipedia titles | 797 (593) / 678 (466) | 1749 / 1357 | 1037 (782) / 893 (612) | 1844 / 1518 |
| JSON keys | 478 (274) / 414 (223) | 1807 / 1535 | 718 (478) / 610 (418) | 1742 / 1526 |

**Threads** (`uint64` keys, millions of operations per second, EPYC / Xeon):

| | 1 thread | 2 | 4 |
|---|--:|--:|--:|
| stundb get | 2.1 / 3.2 | 5.0 / 6.3 | **10.8 / 11.2** |
| stundb get, batched | 8.1 / 10.0 | 16.3 / 20.5 | 21.9 / 26.0 |
| skipmap get | 0.3 / 0.3 | 0.7 / 0.7 | 1.4 / 1.3 |
| RWMutex+google/btree get | 1.3 / 1.3 | 2.6 / 2.5 | 5.4 / 4.7 |
| stundb update | 1.7 / 2.9 | 4.0 / 5.9 | **9.1 / 9.3** |
| RWMutex+google/btree update | 1.2 / 1.4 | 1.2 / 1.3 | 1.2 / 1.2 |

At 4 threads stundb's lookups are 7.6 to 8.8 times the lock-free skiplist's,
and its updates 7.5 to 7.9 times a tree behind an `RWMutex`, which doesn't
scale past one writer.

**On 16 threads**, from the first version of the rewrite on a Ryzen 7 4800H
laptop (8 cores, before the fixes from the adversarial review, in
[ryzen-4800h-20261103.md](bench/results/ryzen-4800h-20261103.md)): 29.8M
lookups a second, 6.6 times the skiplist, and updates at 20.9M a second, 16
times the `RWMutex` tree. The post measures 5.1 times crossbeam-skiplist at 24
threads. On that laptop StunDB looked up a `uint64` in 380 ns against 3,752 ns
for StunDB before the rewrite (4 keys a node, one tree-wide `RWMutex`), 9.9
times faster.

**Memory:** 27.5 bytes per `uint64` entry, close to bbtree's 27.1. Go's
B-trees store items more tightly than Rust's std, though: google/btree takes
24.7, so StunDB is 10 to 14% above it across the four key sets (39.6 against
36.1 bytes on UUIDs, 68.0 against 61.6 on Wikipedia titles, 53.4 against 47.0
on JSON keys). The skiplist takes 88.4.

**Why the nanoseconds aren't the post's.** A lookup's time is mostly dependent
cache misses to DRAM, so it tracks the memory system more than the code. The
post's 9900X has 32 MB of L3 per core complex, which holds a 10M-entry tree's
internal nodes, so a lookup misses about 1.5 times: 109 ns. The 4800H laptop
has 4 MB per core complex and waits on about 3.5 misses at 94 ns: 380 ns. A
4-vCPU slice of a shared server sees a share of its cache and server memory
latency. The baselines pay the same penalty, so the ratios hold.

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

To run every table on GitHub's runners instead, push a tag; the
[workflow](.github/workflows/bench.yml) runs on two VMs and publishes the
tables as a prerelease:

```bash
git tag bench-$(date +%Y%m%d) && git push origin bench-$(date +%Y%m%d)
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
