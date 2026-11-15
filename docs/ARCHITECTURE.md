# Architecture

StunDB is an in-memory key-value store whose core is a concurrent B+-tree
built after [bbtree](https://github.com/XiangpengHao/bbtree) and its post,
[*bbtree: fastest ordered map*](https://bbtree.xiangpeng.systems/post/).
The post orders a B-tree's costs by three principles, and the tree follows
them in that order:

1. the number of **chained cache lines** a lookup touches (~100 ns each);
2. the number of **instructions** it executes (~0.3 ns each);
3. the **cache coherence traffic** it causes between cores.

```text
internal/btree   the concurrent B+-tree: Map[K, V]
      |
internal/kv      byte keys and values over btree.Map[string, []byte]
      |      \
internal/wal   internal/index
(redo log)     (secondary indexes)
```

## The tree (`internal/btree`)

### Nodes

Nodes are fixed-size arrays, sized so that a search touches as few lines as
possible ([node.go](../internal/btree/node.go)):

```text
leaf:   | header 16 B | 56 inline keys | next | 55 keys | 55 values |
inner:  | header 16 B | 56 inline keys | 56 children   | 55 separator keys |
```

For `uint64` keys, the header and the 2-byte inline keys are 128 bytes, and
a `uint64 -> uint64` leaf is 1016 bytes (the post's 1 KiB leaves). The keys
sit after the inline keys: a search reads them only for the one candidate the
inline keys leave. Internal nodes keep their separators' full keys at the
end, read only on a tie.

Those 128 bytes are two cache lines only if the node starts on a line, and a
node allocated with `new` doesn't: Go puts an 8-byte malloc header in front
of every object over 512 bytes that holds pointers, so such a node starts 8
bytes past a line, and its first 128 bytes span three lines. So each tree
allocates nodes from a slab ([slab.go](../internal/btree/slab.go)): chunks
of at least 64 KiB, which Go page-aligns and gives no malloc header, with
each node padded to a multiple of 64 bytes. At 10M `uint64` keys that made
lookups 5% faster and batched lookups 13% faster. Padding to 64 bytes also
replaces Go's size-class rounding: a string leaf takes 1,600 bytes in a slab
against 1,792 on its own. A tree's first 64 nodes of each kind are allocated
one by one, so small trees don't each hold a chunk.

### Inline keys and prefixes (principle 1)

Every node keeps, beside its keys, a dense array of **inline keys**: a small
fixed-width slice of each key's order-preserving byte encoding
([key.go](../internal/btree/key.go), [inline.go](../internal/btree/inline.go)).
One cache line holds 32 two-byte or 16 four-byte inline keys, so a node is
searched without touching its keys, and string keys without following a pointer to the heap. A tie (equal
inline keys) falls back to the full keys, and a string that ends within the
window is settled by its length alone (bbtree's D36).

| key type | encoding | inline key |
|---|---|---|
| `~uint64` | 8 bytes, big-endian | 2 bytes |
| `~[16]byte` (UUIDs, hashes) | the bytes | 2 bytes |
| `~string` | the bytes | 4 bytes |

The window starts after the bytes every key in the node shares: the node's
**prefix**, the common prefix of the separators on either side of it in its
ancestors (Bayer and Unterauer's prefix B-trees). No node stores the prefix:
a key that reaches the node sorts between those separators, so it has the
prefix too. A node only stores its length, set when the node is created by a
split. Nodes never merge, so a node's fences only narrow and its prefix only
grows.

With the prefix stripped, 2 bytes usually distinguish the keys of a leaf of
random `uint64`s. A leaf of a 10M-entry tree covers about 2^46 of the key
space. Three leaves in four sit inside one 2^48 block and share their first
two bytes. The fourth crosses a 2^48 boundary, shares only one byte, and its
16-bit window spreads its ~38 keys over about 64 values. Measured at 10M
keys, 11.5% of hits and 11.9% of misses tie on the inline key and read full
keys; for `uint64`s that is one compare in the node.

Go has no specialization, but it compiles a generic function once per GC
shape, and `uint64`, `[16]byte` and `string` are three shapes. Within each
instantiation `unsafe.Sizeof` of the key is a constant, so a
`switch kindOf[K]()` folds away and each key type gets its own node code at
no run-time cost. On 32-bit platforms a string is 8 bytes, the size and
alignment of a `uint64`, so there `key_kind32.go` asks `reflect` for the
key's kind instead: correct, and a call per operation, on platforms the tree
is not tuned for.

### Searching a node (principle 2)

The search counts the inline keys below the target over all 56 slots, padded
with the greatest value past the node's count: no early exit, no branch, no
dependent load. The count is the child to follow, or the first candidate in a
leaf. With `GOEXPERIMENT=simd` on amd64 it runs in AVX2 through
`simd/archsimd`: `VPCMPGTW` compares 16 inline keys at once, `VPMOVMSKB`
packs the masks, `POPCNT` counts them. AVX2 has no 16-bit movemask, so two
masks are merged byte by byte first. Inline keys are stored with their sign
bit flipped, because AVX2 only compares signed integers.

Two Go-specific findings, in the spirit of the post's *the compiler is not
your friend*:

- **VZEROUPPER.** Each count ends with `VZEROUPPER`. Go's own code uses
  legacy SSE encodings (the ABI zeroes `X15` with `XORPS`), and on Zen 2
  mixing them with dirty upper YMM halves cost a state transition that made
  batched lookups three times slower.
- **Inlining.** Go's inliner charges every arm of a `switch` on the key type
  before constant folding, so a generic helper that handles three key types
  is never inlined. Even `inlineOf`, which is one shift for `uint64`, costs
  129 against a budget of 80 because of the call in its other branch. So
  the hot searches (`leaf.seek`, `inner.route`, the batch's leaf pass) test
  for `uint64` themselves and do the shift in place; their only call on the
  `uint64` path is the count. The leaf's common hit is decided by `==`,
  which Go compiles natively for each shape.

### Concurrency (principle 3)

**Optimistic lock coupling** (Leis et al., *The ART of Practical
Synchronization*; the post's version lock): every node has a 64-bit word, bit
0 a latch and the rest a version that every release bumps. A reader reads the
version, reads the node, and checks the version again before trusting what it
read, so **readers write nothing**: no reader count, no shared cache line,
nothing for cores to fight over. A read that overlapped a change starts over
from the root.

- **Lookups** descend optimistically, and at each level read the child's
  version and then check the parent's, so that the child was still the
  parent's child. At the leaf they copy the value out and check the version
  after, like a seqlock. In Go that works for *any* value type, not only
  `Copy` ones as in Rust: a torn copy is thrown away unused, and memory it
  points to can't have been freed, because the garbage collector keeps it.
- **Writers** latch only the nodes they change, by compare-and-swap from the
  version their descent read. An insert latches its leaf. Full internal
  nodes are split on the way down (Leis et al.'s B-tree), so a split latches
  the node and its parent and never climbs back up.
- **Clear** swaps in an empty root. A root split that was already under way
  installs its new root by compare-and-swap from the old root, so a split of
  the discarded tree never puts it back.
- **Memory reclamation** is the garbage collector's. bbtree keeps freed
  nodes on free lists so that a late reader still reads a node (type-stable
  pages). In Go a reader that holds a pointer keeps the node alive, so there
  is nothing to reclaim by hand: no epochs, no guards, no free lists.
- **Scans** copy each leaf out under its version and hand the copies to the
  callback, which runs with no latch held. Leaves are linked, so a scan moves
  right without returning to the root. It reads the next leaf's version
  before copying the current leaf, so that line is already on its way.

The map keeps no length: a shared counter would be one line that every insert
on every core writes. `Len` walks the leaves; `kv.Store` keeps a striped
count.

Under the race detector (`-race`), optimistic reads are reported as data
races, by design: they race, then they validate. Race builds therefore
serialize the map behind a `sync.RWMutex` (`race_on.go`), so the layers
above are race-checked against a map with the same API. The tree's own
concurrency is tested by stress tests in ordinary builds
([concurrent_test.go](../internal/btree/concurrent_test.go)), whose readers
check every value against what writers have published. Those tests catch the
classic optimistic-lock-coupling bug (a descent that doesn't recheck the
parent's version) within a few runs.

### Batches

`GetMany` and `InsertMany` hide the cache misses of one lookup behind those
of others (the post's *batch mode*). bbtree writes each lookup as an async
function and polls the lanes with its own executor. Go has no zero-cost
async, and an explicit per-lane state machine cost more in branches than it
hid. But every leaf of a B+-tree is at the same depth, so the lanes move in
lockstep instead (group prefetching, Chen et al.): each level is two passes
over up to 24 lanes, one that searches each lane's node and prefetches the
child pointer it chose, and one that follows the pointers and prefetches the
children's headers and inline keys. A lane whose version check fails is
retried alone at the end. Trees under three levels fit in cache and go one
key at a time.

Like bbtree (D46), nothing but the batches prefetches. Prefetching single
lookups trades memory bandwidth for latency and hides what the layout does.

## Byte store (`internal/kv`)

`kv.Store` is `btree.Map[string, []byte]` with copies in and out, inclusive
ranges and a striped entry count. Lookups view the caller's `[]byte` as a
string without copying it (`btree.BytesKey`). Inserts copy key and value, and
every read, scans included, hands out copies, so no caller can change stored
bytes.

The map and its count live together in a generation, and `Clear` installs a
new one. A writer works on the generation it loaded, so one that races `Clear`
lands, count and all, in the discarded generation. Resetting a shared counter
instead would leave it permanently off by the writes that straddled the
reset.

It replaces the old hash-sharded wrapper. Sharding was there to give a
single-lock tree write concurrency, and it cost ordered scans: a range had to
be gathered from every shard and sorted. The new tree latches one node at a
time and its readers take no latch at all, so one tree scales across cores
by itself, and ranges walk it in order.

## Secondary indexes (`internal/index`)

`internal/index` keeps one `kv.Store` per secondary index beside the primary
store. Unique indexes store `indexKey -> primaryKey`; non-unique ones store
`indexKey -> encoded list of primary keys`. Index keys come from
`KeyExtractor` functions. The built-in `JSONFieldExtractor` is a small byte
scanner, not a JSON parser.

A write changes the primary store and every index, and those changes have to
agree, so `IndexedBTree` serializes writers with one mutex, as the WAL layer
does. Each write reads the record it replaces, checks every unique constraint
before changing anything, and then replaces the old record's index entries
with the new one's. Readers take no write lock, so a reader can see a write
applied to the primary store before an index, or the other way round.

## Write-ahead log (`internal/wal`)

`internal/wal` logs every mutation before applying it to the in-memory store
(see [DURABILITY.md](DURABILITY.md)). Writers are serialized, so the log's
order is the order the store sees, and replay rebuilds the same state.
Readers take no lock. A `BulkInsert` is one log entry, so recovery restores
all of it or none, and point reads, ranges and counts see all of it or none.
