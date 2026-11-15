# Porting bbtree to Go

*How StunDB's B+-tree went from 3,752 ns to 380 ns a lookup, and what Go made
easy and hard along the way.*

StunDB is a small in-memory key-value store in Go, with a write-ahead log and
secondary indexes. Until this rewrite its core was a textbook B+-tree behind
one lock. Xiangpeng Hao's post
[*bbtree: fastest ordered map*](https://bbtree.xiangpeng.systems/post/)
describes a Rust B-tree that beats std's `BTreeMap` by 1.5 to 6.5 times, and
it justifies every design choice by three costs. The goal of this project was
to rebuild StunDB's tree the way bbtree is built and land near the post's
numbers.

This post walks through the rewrite in the order it happened. It covers what
I deleted, the node layout, the node search, the concurrency protocol,
batches, scans, testing, and the benchmark harness. Each section names the
trade-off I made. Where the first attempt failed, it says what failed and why.

The short version: on a Ryzen 7 4800H laptop, at 10M entries, StunDB's tree
now looks up a `uint64` key in 380 ns and inserts one in 666 ns. The old tree
took 3,752 ns and 4,202 ns. Against google/btree on the same machine, lookups
are 1.9 to 3.6 times faster depending on the key type, and the post measures
bbtree at 1.7 to 4.1 times std's `BTreeMap`. The ratios match. The
nanoseconds don't, and a section near the end explains why.

## The starting point was ten levels of pointer chasing

Here is the old node, from `bptree/node.go`:

```go
type Node struct {
	keys         []Keytype // Keytype is []byte
	values       []Valuetype
	children     []*Node
	isleaf       bool
	mu           sync.RWMutex
	rightSibling *Node
}

const (
	MaxKeys = 4
	MinKeys = MaxKeys / 2
)
```

Read it with the post's first cost in mind, the number of cache lines a lookup
must load one after another:

- **Four keys a node.** A 10M-entry tree with at most five children a node is
  at least 10 levels deep.
- **Three dependent loads a level.** Comparing against one key reads the node
  struct, then the `keys` slice's backing array, then the key's bytes. Each
  load needs the address the previous one returned.
- **One tree-wide `sync.RWMutex`.** Every `RLock` writes the mutex's reader
  count. Every lookup on every core writes the same cache line, so reads
  scale only until that line's transfers between cores saturate.

The repository also had a `ShardedBTree` that hashed keys with FNV-1a across
`runtime.NumCPU()` such trees, to get write concurrency out of a single-lock
design. It worked for point operations. `GetRange` had to query every shard and
sort the merged results, which makes an ordered map's main advantage the
slowest thing it does.

On the benchmark workload described below, the old tree took 3,752 ns a lookup
and 4,202 ns an insert at 10M entries.

## The post ranks costs, and the ranking decides everything

The post orders a B-tree's costs:

1. Chained cache lines: about 100 ns each when they come from DRAM.
2. Instructions: about 0.3 ns each.
3. Cache coherence traffic between cores.

The ranking matters more than the list. A change that saves twenty
instructions but adds one dependent miss is a loss by a factor of fifteen. I
used it as a rule for every decision below, and it settled most of them before
any benchmark ran.

The second decision was what "the same numbers" could mean. The post's
machine is a Ryzen 9 9900X. Mine is a Zen 2 laptop with an eighth of the L3
cache per core complex and slower memory. Absolute nanoseconds were never going
to match. So every result here is a ratio against a baseline on the same
machine, compared with the post's ratio against its baseline:

| the post's map (Rust) | the stand-in here (Go) |
|---|---|
| std `BTreeMap` | google/btree, also tidwall/btree |
| `RwLock<BTreeMap>` | `sync.RWMutex` + google/btree |
| crossbeam-skiplist | zhangyunhao116/skipmap |
| `HashMap`, dashmap | Go's `map`, xsync.Map |

google/btree is Go's most used B-tree. Like std's `BTreeMap`, it is a
single-threaded B-tree with no SIMD and no prefix compression, so it carries
the same costs the post set out to remove.

## Delete before building

The rewrite began by deciding what to keep.

The old tree went. Nothing in it survives a design with fixed-size nodes and
optimistic reads. The sharding layer went too. Sharding existed to give a
single-lock tree write concurrency, and the new tree gets that per node,
without hashing keys apart. Deleting it gave back in-order range scans.

The write-ahead log and the secondary indexes stayed. They were sound, they
had test suites, and they only needed a byte-keyed store underneath. That
store is new: `internal/kv`, a thin layer over the generic tree.

```text
internal/btree   the concurrent B+-tree: Map[K, V]
      |
internal/kv      byte keys and values over btree.Map[string, []byte]
      |      \
internal/wal   internal/index
(redo log)     (secondary indexes)
```

`kv.Store` keeps the old API (`Insert`, `Find`, `GetRange`, `DeleteRange`,
`BulkInsert`, `Count`), so the WAL and index tests pass with one change: their
configs no longer take a `NumShards` field.

## Node layout comes first

A node is a struct of fixed-size arrays, not slices. A slice header is a
pointer, and following it is a dependent load. Here is the leaf, from
`internal/btree/node.go`:

```go
const (
	slots    = 56        // inline-key slots per node; the last is always padding
	leafCap  = slots - 1 // entries per leaf
	innerCap = slots - 1 // separators per internal node
)

type header struct {
	ver   atomic.Uint64 // version lock: bit 0 latched, the rest a version
	n     uint16        // keys in a leaf, separators in an internal node
	plen  uint8         // bytes every key under this node shares
	level uint8         // 0 for leaves
	_     [4]byte
}

type leaf[K Key, V any, W word] struct {
	header
	ik   [slots]W // inline keys, 2 or 4 bytes each
	next *leaf[K, V, W]
	keys [leafCap]K
	vals [leafCap]V
}
```

The numbers fit together on purpose:

- The 16-byte header and 56 two-byte inline keys are exactly 128 bytes, two
  cache lines. A search reads those two lines and, for a hit, the one key it
  lands on.
- A `uint64 -> uint64` leaf is 1,016 bytes, inside the post's 1 KiB.
- The last inline-key slot is always padding, so the search never needs to
  know the node's count. More on that below.

The first bullet was false for the first version of this tree, and I didn't
know it until an adversarial review measured it (see the section on the
review below). Two cache lines means the node starts on a line. A node
allocated with `new` doesn't: Go puts an 8-byte malloc header in front of
every object over 512 bytes that holds pointers. Every node started 8 bytes
past a line, and its 128 hot bytes spanned three lines. The 1,016-byte leaf
fit Go's 1,024-byte size class only because 1,016 plus the 8-byte header is
exactly 1,024.

The fix is a slab allocator per tree, in `slab.go`. Objects over 32 KiB get
their own span, start on a page boundary, and carry no malloc header, because
the collector keeps their type in the span. So the slab allocates chunks of
at least 64 KiB and carves nodes out of them, each padded to a multiple of 64
bytes. Go generics can't size a padding array by a type parameter, so the
slab builds the padded node type at run time with `reflect.StructOf`, which
keeps the collector's pointer map correct. A tree's first 64 nodes of each
kind are still allocated one by one, so a small map doesn't hold a 64 KiB
chunk.

At 10M `uint64` keys, aligned nodes made lookups 5% faster and batched
lookups 13% faster, the same direction in every round of an alternating A/B
run. Padding to 64 bytes also beats size-class rounding for the other key
types: a string leaf of 1,568 bytes takes 1,600 bytes in a slab, against
1,792 from the allocator.

The tree is generic over the key: `~uint64`, `~[16]byte` (UUIDs, hashes), or
`~string`. That rules out `[]byte` keys in the tree's API, and it is a real
trade-off. `kv.Store` copies a `[]byte` key into a string on insert. Lookups
convert without copying through `btree.BytesKey`, so the cost falls only on
writes, which copy the key anyway to own it.

Go has no specialization, but it compiles a generic function once per GC
shape, and the three key kinds are three shapes. Inside each instantiation,
`unsafe.Sizeof` of the key is a constant, so a `switch kindOf[K]()` folds to
one arm at compile time. Each key type gets its own node code for free. That
"for free" comes with a catch, covered under inlining below.

## Inline keys keep a search inside two cache lines

Every node keeps, beside its full keys, a dense array of **inline keys**. An
inline key is a 2-byte (integers, UUIDs) or 4-byte (strings) window of the key's
big-endian byte encoding. A node search compares the target's window against
the inline keys and touches the full keys only for the one candidate they
leave. For string keys that means it doesn't follow a pointer into the heap
for every comparison, only for the last one.

Two bytes out of an eight-byte key sound like too few. They aren't, because the
window doesn't start at byte zero. It starts after the bytes every key in the
node shares, the node's **prefix**. In a 10M-entry tree of random `uint64`s,
one leaf covers about 2^64 / 2^18, or 2^46, of the key space. Three leaves in
four sit inside one 2^48 block, so their keys share their first two bytes,
and the next two bytes usually tell them apart. The fourth leaf crosses a 2^48
boundary and shares only one byte. Its 16-bit window then spreads about 38 keys
over roughly 64 values, and ties are common. Measured over the whole tree,
11.5% of hits and 11.9% of misses tie on the inline key and fall back to a
full-key compare. For `uint64` keys that compare is in the node and cheap.

The prefix comes from the node's fences, the separator keys on either side of
it in its ancestors. This is Bayer and Unterauer's prefix B-tree, and the post
does the same:

```go
// prefixLen is how many bytes every key between the fences shares.
// No node stores the prefix itself; a key that reaches the node has it,
// because it sorts between the fences.
func (f *fences[K]) prefixLen() uint8 {
	if !f.hasLo || !f.hasHi {
		return 0
	}
	p := keyCPL(f.lo, f.hi)
	if p > 255 {
		p = 255 // any shorter prefix is shared too
	}
	return uint8(p)
}
```

No node stores its prefix bytes, only the length. A split computes the length
for both halves from their new fences. Inserts never change it. And because
nodes never merge (a trade-off covered later), a node's fences only narrow
and its prefix only grows, so an inline key computed once stays correct for
the node's lifetime.

A leaf hit then costs one count, one compare of inline keys, and one compare of
full keys:

```go
func (l *leaf[K, V, W]) seek(key K, n int, v uint64) (pos int, found, ok bool) {
	t := inlineOf[K, W](key, l.plen)
	i := countLess(&l.ik, t)
	if i >= n || l.ik[i] != t {
		return min(i, n), false, true // no key ties: absent
	}
	if kindOf[K]() != kindStr && l.keys[i] == key {
		return i, true, true // the usual hit: one candidate, compared in the node
	}
	return l.seekTies(key, i, n, v)
}
```

A miss whose inline key matches nothing never reads a full key at all. The
benchmark's lookups are half misses, so that path matters.

The inline keys are not free, and three costs came with them:

- **Ties cost a key read.** When two keys share their window, the search falls
  back to the full keys. For strings each one is a heap pointer to follow.
  bbtree's design note D36 removes most of these: a string that ends inside
  the window is settled by its length alone, without reading its bytes.
  `tiedCompare` does the same.
- **Border nodes have no prefix.** The leftmost and rightmost nodes at each
  level have one open fence, so their prefix is empty. Dense keys there produce
  long runs of equal inline keys. When a run is longer than four, `seekTies`
  switches to a binary search over the full keys instead of walking them.
- **Memory.** A `uint64` entry costs 27.5 bytes in StunDB against 24.7 in
  google/btree, 11% more. bbtree's figure is 27.1, but std's `BTreeMap` is
  less compact than Go's B-trees, so the post comes out ahead of its baseline
  where this port doesn't. String leaves are worse: 1,568 bytes, which Go
  rounds to the 1,792-byte class, putting string keys 19 to 24% above
  google/btree.

## A node search is a count, not a binary search

A binary search over 56 keys takes six steps, and each step's branch depends on
the data, so the CPU guesses wrong about half the time. Each step also loads
from an address the previous step chose.

The post replaces it with a count. The number of inline keys smaller than the
target is exactly the index to follow in an internal node, or the first
candidate in a leaf. Slots past the node's count hold the largest possible
value, so the count runs over all 56 slots with no loop bound, no early exit,
and no branch. With `GOEXPERIMENT=simd` on amd64, it runs in AVX2 through Go
1.26's experimental `simd/archsimd` package:

```go
q := (*[slots]int16)(p)
tv := archsimd.BroadcastInt16x16(int16(t))
m0 := tv.Greater(archsimd.LoadInt16x16((*[16]int16)(q[0:16]))).ToInt16x16()
m1 := tv.Greater(archsimd.LoadInt16x16((*[16]int16)(q[16:32]))).ToInt16x16()
m2 := tv.Greater(archsimd.LoadInt16x16((*[16]int16)(q[32:48]))).ToInt16x16()
m3 := archsimd.BroadcastInt16x8(int16(t)).Greater(archsimd.LoadInt16x8((*[8]int16)(q[48:56]))).ToInt16x8()
pair := m0.And(lowBytes16).Or(m1.AndNot(lowBytes16)) // a bit per lane of m0 and of m1
c01 := bits.OnesCount32(pair.AsInt8x32().Less(zeroBytes32).ToBits())
c2 := bits.OnesCount32(m2.AsInt8x32().Less(zeroBytes32).ToBits()) // two bits per lane
c3 := bits.OnesCount16(m3.AsInt8x16().Less(zeroBytes16).ToBits()) // two bits per lane
archsimd.ClearAVXUpperBits()
return c01 + (c2+c3)>>1
```

That is four `VPCMPGTW` compares, then three byte-sign steps (a `VPCMPGTB`
against zero and a `VPMOVMSKB` byte movemask each), and three `POPCNT`s. AVX2 only compares signed integers, so inline keys are stored with
their sign bit flipped (`toInline` in `inline.go`), and a signed compare of two
stored keys orders them as unsigned windows.

Three things about this code were learned the hard way.

**`Mask16x16.ToBits` is AVX-512.** The natural way to turn a 16-bit compare
mask into an integer is `ToBits`, and on a mask of 16-bit lanes archsimd
compiles it to an AVX-512 mask-move instruction. Zen 2 has no AVX-512, and
the first run died with `SIGILL: illegal instruction`. AVX2 only has a byte
movemask. The fix is the `pair` line above: it takes the low byte of each lane
from `m0` and the high byte from `m1`, so one byte movemask yields one bit per
key of both masks. For the other two masks every key sets two bits, and the
count is halved.

**Without `VZEROUPPER`, batched lookups ran three times slower.** Go's own
generated code uses legacy SSE encodings. The ABI zeroes `X15` with `XORPS`,
for example. On Zen 2, running legacy SSE instructions while the upper halves
of the YMM registers are dirty costs a state transition. Single lookups hid
it. Batched lookups, which interleave many counts with ordinary Go code, did
not. `archsimd.ClearAVXUpperBits()` at the end of each count emits
`VZEROUPPER` and removed the penalty.

**Go's inliner charges for code it will delete.** A helper that switches on
`kindOf[K]()` folds to one arm after inlining, but the inliner decides
before folding and counts the cost of every arm. A generic helper that handles
three key types is never inlined, and an uninlined call on every level of
every search is measurable. The fix was structural. `countLess` is a single
call, and the common leaf hit uses `==`, which Go compiles natively for each
shape. The first version also had `inlineOf` handle the `uint64` window with
one shift and call out only for the other kinds, and I wrote here that it
inlined. The review showed it doesn't: the call in the dead branch alone puts
it at 129 against a budget of 80. So `leaf.seek`, `inner.route`, and the
batch's leaf pass now test for `uint64` themselves and do the shift in place.
`go tool objdump` confirms that their only call on the `uint64` path is
`countSlots`.

Builds without `GOEXPERIMENT=simd`, or without AVX2, use a branchless binary
search (as in Khuong and Morin's study of array layouts) instead. I also wrote a SWAR
count that packs four 16-bit compares into one 64-bit word, and dropped it in
favor of the branchless search. The scalar count stays in `inline.go` as the
reference that tests check both faster versions against.

The cost of this choice is the build. The fast path needs
`GOEXPERIMENT=simd GOAMD64=v3`. That experiment is not covered by Go's
compatibility promise, and there is no AVX-512 or ARM NEON path. I accepted
that because a plain build still works and is still correct.

## Readers that write nothing

The post's third cost is coherence traffic. A lock that readers take, even a
reader-writer lock, is a cache line every reader writes. So the tree uses
**optimistic lock coupling**, from Leis et al.'s *The ART of Practical
Synchronization*, which is also what bbtree uses.

Every node has a 64-bit version word. Bit 0 is a latch. Releasing the latch
adds one, which clears the bit and bumps the version in one atomic add. A
reader reads the version, reads the node, and reads the version again. If the
version is unchanged and wasn't latched, what it read is consistent. If not,
it starts over from the root.

The descent is the part to get right:

```go
for n.level != 0 {
	in := asInner[K, V, W](n)
	idx, ok := in.route(key, in.count(innerCap), v)
	if !ok {
		continue restart
	}
	child := in.children[idx]
	if child == nil {
		continue restart
	}
	cv := child.ver.Load()
	if in.ver.Load() != v || cv&lockedBit != 0 {
		continue restart
	}
	n, v = child, cv
}
```

The order matters. The reader loads the child's version first, then checks the
parent's. If the parent is unchanged at that point, the child was still the
parent's child when its version was read. Checking the parent before reading
the child's version leaves a window where a split moves the key to a new right
sibling and the reader searches the wrong node. That is the classic bug in
optimistic lock coupling, and the stress tests described below catch it within
a few runs.

At the leaf, `get` copies the value out and validates afterward, as a seqlock
does:

```go
if hit {
	val = l.vals[pos] // copied out, then validated: a seqlock read
}
if l.ver.Load() == v {
	return val, hit
}
```

This is where Go turned out easier than Rust. bbtree only allows this pattern
for `Copy` values and keeps freed nodes on free lists of type-stable pages, so
that a slow reader holding a stale pointer still reads memory shaped like a
node. In Go a reader that holds a pointer keeps the node alive, and a torn
copy of a value with pointers is discarded unused. The garbage collector does
the work of bbtree's whole reclamation scheme. There are no epochs, no guards,
and no free lists.

Writers latch only what they change, by compare-and-swap from the version their
descent read. An insert latches its leaf. Full internal nodes split on the way
down, with their parent latched, as in Leis et al.'s B-tree. So a full leaf's
parent always has room for the new separator, and a split never climbs back up
the tree. A writer that latches a node and then changes nothing restores the
old version (`unlockUnchanged`), so concurrent readers don't restart for
nothing.

Three trade-offs came with this design.

- **No merges.** Removing entries never merges nodes. Merges would make fences
  widen and prefixes shrink, which breaks the "computed once, correct forever"
  property of inline keys, and they complicate the concurrency protocol. The
  cost is that a tree that shrinks keeps its nodes until `Clear`. Lookups and
  scans stay correct.
- **No O(1) `Len`.** A shared counter is one cache line that every insert on
  every core writes, the exact traffic the design avoids. `Map.Len` walks the
  leaves. `kv.Store.Count` keeps a striped counter, because the store's
  callers expected a cheap count.
- **The race detector can't check it.** Optimistic readers read non-atomic
  fields while writers change them, then discard what the version check shows
  was torn. That is a data race by design, and `go test -race` reports it.
  Under `-race`, the map serializes writers against readers with a
  `sync.RWMutex` (`race_on.go`), so the layers above are still race-checked
  against the same API. The lock-free paths are tested by stress tests in
  ordinary builds instead. I'm comfortable with that split, but it means the
  most subtle code has no tool checking it, only tests.

On threads, the protocol does what the post says it does. At 16 threads on 8
cores, lookups reach 29.8M a second, 6.6 times zhangyunhao116/skipmap. Updates
reach 20.9M a second, while an `RWMutex` around google/btree stays at 1.3M no
matter how many threads write.

## Batches without async

A lookup's cache misses are chained. It can't fetch the next node before it
has searched this one. A batch hides that latency by running many lookups at
once: each one prefetches the line it needs next and steps aside while the
line arrives.

bbtree writes each lookup as an async function, so the Rust compiler builds a
small state machine per lookup, and polls the lanes with its own executor. Go
has no zero-cost async. Goroutines are far too heavy for this, so my first
version wrote the state machine by hand: per-lane state and a `switch` on it
every step. It cost more in branches than it hid in misses.

The fix came from noticing a property of B+-trees. Every leaf is at the same
depth, so the lanes don't need independent state machines. They can move down
the tree in lockstep, one level at a time. This is group prefetching, from
Chen et al. (SIGMOD 2004). Each level is two passes over the lanes:

1. Search each lane's node and prefetch the child pointer it chose.
2. Follow each pointer and prefetch the child's header and inline keys.

By the time pass 1 comes back to a lane on the next level, its lines have
arrived. A lane whose version check fails sets a bit in a `failed` mask and
is retried alone at the end. No lane holds a latch between passes, so lanes
never wait on one another.

Some numbers in `batch.go` are tuned to the hardware. `batchLanes` is 24,
roughly the number of L1 miss buffers on a Zen 2 core, so each pass keeps about
that many misses in flight. `minBatchHeight` is 3, because trees with fewer
levels fit in cache and the passes cost more than they hide. Go has no
prefetch intrinsic, so `prefetch_amd64.s` is three tiny assembly functions
that issue `PREFETCHT0` on one, two, or four lines.

`InsertMany` descends the same way, then latches each lane's leaf in order.
When two lanes share a leaf, the first insert bumps the leaf's version, so the
second lane's latch attempt would fail. The code updates the version it
expects for later lanes on the same leaf instead of sending them back to the
root.

bbtree's design note D46 forbids prefetching outside batches, and I followed
it. Prefetching a single lookup's next level speculatively trades memory
bandwidth for latency, and it hides how good the layout is. The post's
single-lookup numbers don't use it, so mine don't either.

The result: batched `uint64` lookups take 253 ns against 380 ns one at a time,
and batched inserts take 395 ns against 666 ns.

## Scans copy leaves out

Leaves are linked through `next`, so a scan walks right without returning to
the root. For each leaf, `ascend` copies the keys and values out under the
leaf's version, checks the version, and only then hands the copies to the
callback. The callback runs with no latch held, so it can call back into the
map. If the version check fails, the scan restarts from the root at the last
key it handed out. An entry present for the whole scan is visited exactly
once, but a scan is not a snapshot.

One small trick: the scan reads the next leaf's version before copying the
current one, so that probable miss is in flight during the copy. I also tried
interleaving the copy with the callbacks, and it was slower (about 1,605 ns
against 1,450 ns for a 100-entry scan), so I reverted it.

Scans are the one place the port clearly trails the post. A 100-entry
`uint64` scan is 1.6 times google/btree, where the post measures 2.7 times
std's `BTreeMap`. String keys are on par with or ahead of the post. I haven't
pinned down the `uint64` gap. My first suspect is the callback, an indirect
call per entry that Go doesn't inline, which costs proportionally more when
each entry is only 16 bytes.

## Testing a tree whose reads are races

With the race detector out of the picture for the core, the tests had to carry
more weight. There are four kinds:

- **Differential tests** run random operations on the tree and on a Go `map`
  side by side, for each key type, over many key shapes. The shapes include
  dense, strided, random, strings sharing 300-byte prefixes, and binary keys
  with zero bytes.
- **Invariant checks** (`invariants_test.go`) walk the tree after each run.
  They check that keys are sorted, that every inline key equals its key's
  window at the node's prefix, that each prefix is exactly what the node's
  fences share, and that the leaf chain is complete.
- **Fuzzing** with `FuzzU64` and `FuzzString` feeds arbitrary operation
  sequences through the same differential check.
- **Stress tests** (`concurrent_test.go`) race readers, writers, batches, and
  scans. Every value a reader sees is checked against what writers have
  published.

Every test runs in three builds: AVX2, portable, and `-race`.

## The benchmark harness had bugs too

`bench/` is a separate Go module, so the core keeps zero dependencies. It runs
the post's workloads: 10M entries with `uint64` values, four key sets
(`uint64`, UUIDs, Wikipedia titles, and JSON-style keys), lookups half of which
miss, inserts, updates, and 100-entry scans, on one thread and on 1 to 16.

Two harness bugs produced wrong numbers before they produced right ones.

**Huge pages silently weren't used.** The post's runs have transparent huge
pages, and a 10M-entry tree spread over 4 KB pages pays a TLB miss on most
node visits. This machine's THP setting is `madvise`, so the harness asks for
huge pages with `MADV_COLLAPSE` on the heap after building each map. The first
version matched nothing, because Go 1.26 names its heap mappings
`[anon: Go: heap]` in `/proc/self/maps`, not `[heap]`. Measured by pointer
chasing, a dependent DRAM miss costs 94 ns with huge pages and 121 ns without.
Every map, baselines included, gets the same treatment.

**Garbage collection leaked into timed runs.** Building a 10M-entry map
allocates a lot, and the collector's mark phase was still running when the
timed reads started. The symptom was absurd: batched JSON-key lookups at 16
threads read 5.5M a second. The fix is one line, a `runtime.GC()` after each
build. The `uint64` threaded tables were rerun with it. The UUID and JSON
threaded tables predate the fix and say so in `bench/results/threads-str.md`.

One constraint shaped the profiling. `perf` was unavailable on this machine
(`perf_event_paranoid` is 4), so every profile came from pprof, which sees
functions and lines but not cache misses. Cache behavior was reasoned about
from the layout and checked against timings, not measured directly.

## Results

All numbers in this section are from the first version of the rewrite, at
10M entries, one thread, on the 4800H, from
`bench/results/ryzen-4800h-20261103.md`. The section after the review gives
the current code's numbers. Each ratio is the speedup over the
baseline on the same machine: google/btree here, std's `BTreeMap` for the post.
Batched calls are in brackets.

| keys | get, StunDB | get, post | insert, StunDB | insert, post | update, StunDB | update, post | scan, StunDB | scan, post |
|---|---|---|---|---|---|---|---|---|
| `uint64` | 2.2× (3.4×) | 2.4× (3.6×) | 1.4× (2.3×) | 1.5× (2.1×) | 1.9× | 2.3× | 1.6× | 2.7× |
| UUID | 3.6× (5.1×) | 2.8× (3.9×) | 2.3× (3.0×) | 2.1× (2.7×) | 3.2× | 2.5× | 2.2× | 2.3× |
| Wikipedia titles | 1.9× (2.8×) | 1.7× (2.4×) | 1.7× (2.2×) | 1.5× (1.9×) | 1.8× | 1.6× | 1.9× | 1.7× |
| JSON keys | 3.5× (4.6×) | 4.1× (6.5×) | 2.3× (3.0×) | 2.3× (3.6×) | 3.2× | 3.4× | 2.4× | 2.0× |

Nineteen of the 24 cells land within 20% of the post's ratio. UUID keys do
better than the post, and I don't have a verified reason. The baseline isn't
handicapped: the harness compares UUIDs in google/btree as two 64-bit
integers. Batched JSON-key lookups and `uint64` scans do worse.

Against the old StunDB tree, lookups are 9.9 times faster (14.8 times batched)
and inserts 6.3 times faster.

## Why 380 ns and not 109

The post's `uint64` lookup takes 109 ns. Mine takes 380. The difference is the
memory system, not the code.

The 9900X has 32 MB of L3 per core complex. That holds every internal node of
a 10M-entry tree, so a lookup misses to DRAM about 1.5 times: the leaf, and
sometimes its parent. The 4800H has 4 MB of L3 per core complex. Only the top
of the tree fits, and the level just above the leaves misses too. A lookup
here waits on about 3.5 dependent DRAM misses at 94 ns each, roughly 330 ns,
and the search code fills the rest.

The baselines pay the same penalty, which is why the ratios hold while the
nanoseconds don't.

The first version of this post said the same arithmetic bounds the threaded
numbers: 30M lookups a second at 3.5 lines each, against the laptop's two
memory channels. The review measured random line reads from DRAM on this
machine at about 200M lines a second with 8 or more threads, twice what that
arithmetic needs. So bandwidth is not shown to be the limit. Without `perf` I
can't count the lines a lookup really fetches, and I don't know what stops
16-thread lookups at their current rate. Contention is still ruled out by
design, because no lookup writes shared memory.

## What an adversarial review broke

After the first version shipped, I ran an adversarial review. Four agents each
took one part of the system, with one rule: a finding counts only if a run
proves it, with a failing test, a repro program, or command output. Each
worked in its own git worktree. I then reran every finding myself and kept
only the ones that reproduced. All of them did.

| area | agent's scope |
|---|---|
| tree concurrency | the lock-free reads, splits, batches, scans, and `Clear` |
| keys and search | key encoding, prefixes, and the AVX2 and portable counts |
| storage layers | `kv.Store`, the WAL, and the secondary indexes |
| performance claims | the benchmark harness and every number in the docs |

### Bugs

- **`Clear` racing a root split undid the `Clear`.** `growRoot` checked that
  the old root was still the root, split it, then stored the new root without
  checking again. A `Clear` in between was overwritten: the cleared entries
  came back, and a key inserted after `Clear` returned disappeared. The
  repro failed in 81 to 88% of its trials. `growRoot` now installs the new root with a
  compare-and-swap from the old one, so a split that lost the race stays in
  the discarded tree.
- **`kv.Store.Count` drifted after `Clear`, and could go negative.** `Clear`
  swapped the tree, then reset the striped counter, and writers that
  straddled the two left the count permanently wrong. Now the tree and its
  count live in one generation behind an atomic pointer, and `Clear` installs
  a new generation. A writer that races `Clear` lands, count and all, in the
  generation that `Clear` discarded.
- **A torn WAL tail hid every later write.** Recovery stopped at a torn entry
  but left it in the file, and the file is opened for appending. Writes
  acknowledged after the restart, fsync and all, went in behind the torn
  entry, and the next restart never reached them. A test that kills a child
  process with `SIGKILL` in the middle of a write reproduced it. Opening the
  WAL now truncates the file after the last whole entry. It also checks every
  length field against the rest of the file, so a damaged length can't make
  recovery allocate gigabytes.
- **`DurableBTree.BulkInsert` was atomic only in its comment.** A crash in
  the middle of a 2M-key batch recovered 78K of its keys, and readers saw
  batches half applied. A batch is now one WAL entry with one checksum, so
  recovery replays all of it or none. A sequence counter, odd while a batch
  applies, lets `Find`, `GetRange`, and `Count` retry until they see all of
  a batch or none of it. That keeps reads free of writes to shared memory.
- **The secondary indexes went stale.** Overwriting a record with `Put` left
  the old record's index entries behind. Re-putting a record with its own
  unique value was rejected as a duplicate of itself. Concurrent writers left
  index entries pointing at missing records, and a rejected `Put` could delete
  the record that was already there. These bugs predate the rewrite. Writes
  to `IndexedBTree` now go through one mutex, as the WAL layer's already did,
  because a write changes several trees that have to agree. Each write checks
  every unique constraint first, ignoring the record's own entries, then
  replaces the old record's index entries with the new ones.
- **`ForEach` and `Scan` handed out the stored value slices.** A caller that
  wrote to one changed the store, and in `DurableBTree` the in-memory data
  then disagreed with what recovery would rebuild. They now hand out copies.
- **String keys were broken on 32-bit platforms.** `kindOf` told a `uint64`
  key from a string by its size. On `GOARCH=386` a string header is 8 bytes,
  the same size and alignment as a `uint64`, so string maps compared string
  headers as integers. On 32-bit platforms, `key_kind32.go` now asks
  `reflect` for the key's kind instead. 64-bit builds still fold the size test
  at compile time. The tree's tests didn't catch this because they didn't
  compile on 386. They do now, and the README lists a 386 test run.
- **Nodes weren't line-aligned, and `inlineOf` wasn't inlined.** The node
  layout section above covers both.

### False claims and harness flaws

- The README said one cache line holds 32 inline keys. That is true only for
  2-byte keys. A line holds 16 of the 4-byte string keys.
- This post listed the AVX2 count's instructions and left out three
  `VPCMPGTB`s.
- The header of each results file read `GOAMD64` from the environment at run
  time, not from the build. It now reads the build settings with
  `debug.ReadBuildInfo`.
- The `-thp` flag claimed to back every map with huge pages before timing it.
  It collapses each map's heap after the timed build, so insert cells run on
  ordinary pages for every map. That is even between maps, but unlike the
  post's runs.
- `BenchmarkInsertU64` grew a map from empty, so its result depended on
  `b.N`, from 120 to 456 ns. It now inserts into a map that starts at 10M
  entries.
- The prefix and bandwidth claims, corrected in their sections above.

### What held up

- **Concurrency.** A 60-second stress test ran 74M mixed operations over all
  three key types with no wrong value. Another ran 362K scans against batch
  writers, and every key present for a whole scan was seen exactly once, in
  order.
- **Key handling.** New fuzzers and differential tests over adversarial
  string shapes passed in both builds: zero and `0xff` runs, and shared
  prefixes of 248 to 259 bytes, around the prefix clamp at 255. So did
  `uint64` and UUID keys at the extremes. All three counts, AVX2, branchless,
  and scalar, agreed on every 16-bit target at every node size.
- **Benchmark fairness.** Every map gets the same keys, the same probes, and
  the same huge-page treatment, with a 49.96% hit rate for both StunDB and
  google/btree. The comparator wrappers given to the baselines cost them at
  most 5%. The memory figures reproduced exactly.
- **`VZEROUPPER`.** Removing it made batched lookups about three times
  slower, as this post says.

Every bug above now has a regression test: `clear_test.go`,
`regression_test.go` in `kv`, `wal`, and `index`, and `crash_test.go`, which
kills a child process mid-write and recovers its WAL.

## Results after the review, on server CPUs

The laptop kept dying before the benchmark rerun could finish: twice it lost
power partway through an hour-long run. The first time, the whole run had been
on battery, which caps a laptop's CPU, so its numbers were thrown out. The
harness now records the power source in every results header and warns on
battery.

So the current code was measured on GitHub Actions instead. Pushing a
`bench-*` tag runs `bench/run-all.sh` on two standard runners at once and
publishes the tables as a prerelease. The two VMs landed on different
processors, an AMD EPYC 9V74 (Zen 4) and an Intel Xeon 6975P-C, each a
4-vCPU slice. All the maps in one run share a VM, so the ratios are fair. The
nanoseconds belong to a shared server and compare with nothing else.

| keys | get: EPYC · Xeon · post | insert: EPYC · Xeon · post | update: EPYC · Xeon · post | scan: EPYC · Xeon · post |
|---|---|---|---|---|
| `uint64` | 1.9× (5.8×) · 2.4× (7.6×) · 2.4× (3.6×) | 1.6× (2.5×) · 1.6× (3.0×) · 1.5× (2.1×) | 1.7× · 1.9× · 2.3× | 1.7× · 1.5× · 2.7× |
| UUID | 3.7× (8.3×) · 3.2× (9.3×) · 2.8× (3.9×) | 2.6× (3.9×) · 2.1× (3.6×) · 2.1× (2.7×) | 3.1× · 2.7× · 2.5× | 2.3× · 1.7× · 2.3× |
| Wikipedia titles | 2.2× (3.0×) · 2.0× (2.9×) · 1.7× (2.4×) | 1.8× (2.4×) · 1.7× (2.5×) · 1.5× (1.9×) | 2.1× · 2.0× · 1.6× | 2.5× · 1.9× · 1.7× |
| JSON keys | 3.8× (6.6×) · 3.7× (6.9×) · 4.1× (6.5×) | 2.4× (3.7×) · 2.5× (3.7×) · 2.3× (3.6×) | 3.3× · 3.3× · 3.4× | 3.0× · 2.3× · 2.0× |

Single lookups land where they did on the laptop and where the post's do.
Batched lookups of `uint64` and UUID keys are the surprise: 5.8 to 9.3 times
google/btree, against 3.4 and 5.1 on the laptop and 3.6 and 3.9 in the post.
I haven't measured why. My guess is that these server cores keep more memory
requests in flight, which is exactly what group prefetching feeds on.

At 4 threads, the most these runners have, `uint64` lookups reach 10.8M and
11.2M a second, 7.6 to 8.8 times the skiplist. Updates reach 9.1M and 9.3M,
7.5 to 7.9 times a tree behind an `RWMutex`. Memory came out the same on both
VMs: 27.5 bytes per `uint64` entry, and 10 to 14% above google/btree across
the four key sets, down from 11 to 24% before the slab allocator.

## Where the port falls short

- `uint64` scans and updates trail the post's ratios: 1.5 to 1.7 against 2.7
  times, and 1.7 to 1.9 against 2.3 times.
- Nodes never merge, so deleting entries returns no memory until `Clear`.
- The SIMD search is AVX2 only. The post's AVX-512 compare
  (`_mm512_cmplt_epu16_mask`) has no counterpart here, and neither does NEON.
- The fast path depends on `GOEXPERIMENT=simd`, which is experimental.
- The race detector doesn't check the optimistic paths, only the stress tests do.
- Memory per entry is 10 to 14% above Go's B-trees, where bbtree is close to
  its baseline.
- The current code has no results past 4 threads. The 8- and 16-thread
  figures come from the first version, on the laptop.

## What carried over and what didn't

The post's ranking of costs carried over to Go unchanged. Every decision it
settled in Rust, it settled the same way here: fixed nodes sized to cache
lines and size classes, inline keys behind a fence-derived prefix, a branchless
count, and readers that write nothing. The ratios came out where the post's
did, which I take as evidence that the gains come from the layout, not from
Rust.

The Go-specific work split cleanly into one large gift and several taxes. The
gift is the garbage collector, which replaces bbtree's type-stable pages,
free lists, and the reasoning that goes with them. It also lets the seqlock
read work for any value type. The taxes all came from the toolchain. There is
no zero-cost async, so batches had to become lockstep group prefetching. SIMD
sits behind an experiment flag, with an API where the obvious mask call
compiles to an instruction my CPU doesn't have. The legacy SSE encodings in
Go's own code need a `VZEROUPPER` after every AVX2 block. And the inliner
charges for switch arms it is about to delete, which shaped the structure of
every hot helper.

If I did it again, I'd start with the benchmark harness, not the tree. Both
harness bugs produced numbers that looked plausible enough to tune against,
and the THP bug alone changed the cost of every miss by more than a fifth.

## Reproduce the numbers

```bash
export GOEXPERIMENT=simd GOAMD64=v3
go test ./...                            # AVX2 search
GOEXPERIMENT= go test ./...              # portable search
go test -race ./...                      # race build, map serialized
GOARCH=386 go test ./...                 # 32-bit, where a string is 8 bytes
go test -fuzz FuzzU64 ./internal/btree

cd bench
./fetch-data.sh                          # Wikipedia titles, 110 MB
go run . -mode single -keys u64,uuid     # one cell group
./run-all.sh                             # every table, about an hour

# or on GitHub's runners, publishing the tables as a prerelease
git tag bench-$(date +%Y%m%d) && git push origin bench-$(date +%Y%m%d)
```

The design is documented piece by piece in
[ARCHITECTURE.md](ARCHITECTURE.md), and the full result tables are in
[bench/results/](../bench/results/).
