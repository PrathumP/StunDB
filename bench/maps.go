package main

import (
	"cmp"
	"sync"

	gbtree "github.com/google/btree"
	"github.com/prathump/stundb/internal/btree"
	"github.com/puzpuzpuz/xsync/v4"
	tbtree "github.com/tidwall/btree"
	"github.com/zhangyunhao116/skipmap"
)

// Map is what a workload needs of a map, with uint64 values. Every map is
// given its cheapest way to do each: an update is one descent that overwrites
// the value of a key the map holds.
type Map[K comparable] interface {
	Insert(k K, v uint64)
	Get(k K) (uint64, bool)
	Update(k K, v uint64)
	// Scan visits up to n entries from the first key >= k and returns how
	// many it visited.
	Scan(k K, n int) int
}

// Batcher is a map with batch calls.
type Batcher[K comparable] interface {
	GetMany(keys []K, vals []uint64, found []bool)
	InsertMany(keys []K, vals []uint64)
}

type mapKind struct {
	name       string
	concurrent bool // safe to share between goroutines
	ordered    bool // scans
	batched    bool // use its batch calls
	unordered  bool // a hash map, for reference
}

var (
	kindStunDB        = mapKind{name: "stundb", concurrent: true, ordered: true}
	kindStunDBBatched = mapKind{name: "stundb (batched)", concurrent: true, ordered: true, batched: true}
	kindGoogle        = mapKind{name: "google/btree", ordered: true}
	kindTidwall       = mapKind{name: "tidwall/btree", ordered: true}
	kindSkipmap       = mapKind{name: "skipmap", concurrent: true}
	kindRWGoogle      = mapKind{name: "RWMutex+google/btree", concurrent: true, ordered: true}
	kindGoMap         = mapKind{name: "Go map", unordered: true}
	kindXsync         = mapKind{name: "xsync.Map", concurrent: true, unordered: true}
)

// newMap makes a map of the kind for keys ordered by less.
func newMap[K comparable](kind mapKind, less func(a, b K) bool, bare any) Map[K] {
	switch kind {
	case kindStunDB, kindStunDBBatched:
		return newStunDB[K](bare)
	case kindGoogle:
		return newGoogle(less)
	case kindRWGoogle:
		return &rwMap[K]{m: newGoogle(less)}
	case kindTidwall:
		return newTidwall(less)
	case kindSkipmap:
		return newSkip(less)
	case kindGoMap:
		return goMap[K]{}
	case kindXsync:
		return xsyncMap[K]{xsync.NewMap[K, uint64]()}
	}
	panic(kind.name)
}

// ---- stundb -----------------------------------------------------------------

type stundbMap[K btree.Key] struct{ m *btree.Map[K, uint64] }

// newStunDB builds the map for K, which must be one of btree's key types; bare
// is a value of K, there to tell the type.
func newStunDB[K comparable](bare any) Map[K] {
	switch bare.(type) {
	case uint64:
		return any(stundbMap[uint64]{btree.New[uint64, uint64]()}).(Map[K])
	case [16]byte:
		return any(stundbMap[[16]byte]{btree.New[[16]byte, uint64]()}).(Map[K])
	case string:
		return any(stundbMap[string]{btree.New[string, uint64]()}).(Map[K])
	}
	panic("key type")
}

func (s stundbMap[K]) Insert(k K, v uint64)                { s.m.Insert(k, v) }
func (s stundbMap[K]) Get(k K) (uint64, bool)              { return s.m.Get(k) }
func (s stundbMap[K]) Update(k K, v uint64)                { s.m.Update(k, func(p *uint64) { *p = v }) }
func (s stundbMap[K]) GetMany(k []K, v []uint64, f []bool) { s.m.GetMany(k, v, f) }
func (s stundbMap[K]) InsertMany(k []K, v []uint64)        { s.m.InsertMany(k, v) }
func (s stundbMap[K]) Scan(k K, n int) int {
	c := 0
	s.m.Ascend(k, func(K, uint64) bool { c++; return c < n })
	return c
}

// ---- google/btree -----------------------------------------------------------

type item[K any] struct {
	k K
	v uint64
}

type googleMap[K comparable] struct{ t *gbtree.BTreeG[item[K]] }

// googleDegree: nodes of 2*32-1 items, the library's suggested degree, and
// the fastest of 8, 16, 32 and 64 at 10M uint64 keys on the machine the
// results were taken on.
const googleDegree = 32

func newGoogle[K comparable](less func(a, b K) bool) *googleMap[K] {
	return &googleMap[K]{gbtree.NewG(googleDegree, func(a, b item[K]) bool { return less(a.k, b.k) })}
}

func (g *googleMap[K]) Insert(k K, v uint64) { g.t.ReplaceOrInsert(item[K]{k, v}) }
func (g *googleMap[K]) Update(k K, v uint64) { g.t.ReplaceOrInsert(item[K]{k, v}) }
func (g *googleMap[K]) Get(k K) (uint64, bool) {
	it, ok := g.t.Get(item[K]{k: k})
	return it.v, ok
}
func (g *googleMap[K]) Scan(k K, n int) int {
	c := 0
	g.t.AscendGreaterOrEqual(item[K]{k: k}, func(item[K]) bool { c++; return c < n })
	return c
}

// rwMap shares a single-thread map behind a sync.RWMutex: the blog's
// RwLock<BTreeMap>.
type rwMap[K comparable] struct {
	mu sync.RWMutex
	m  Map[K]
}

func (r *rwMap[K]) Insert(k K, v uint64) { r.mu.Lock(); r.m.Insert(k, v); r.mu.Unlock() }
func (r *rwMap[K]) Update(k K, v uint64) { r.mu.Lock(); r.m.Update(k, v); r.mu.Unlock() }
func (r *rwMap[K]) Get(k K) (uint64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m.Get(k)
}
func (r *rwMap[K]) Scan(k K, n int) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m.Scan(k, n)
}

// ---- tidwall/btree ----------------------------------------------------------

type tidwallMap[K comparable] struct{ t *tbtree.BTreeG[item[K]] }

func newTidwall[K comparable](less func(a, b K) bool) *tidwallMap[K] {
	return &tidwallMap[K]{tbtree.NewBTreeGOptions(func(a, b item[K]) bool { return less(a.k, b.k) },
		tbtree.Options{NoLocks: true})}
}

func (t *tidwallMap[K]) Insert(k K, v uint64) { t.t.Set(item[K]{k, v}) }
func (t *tidwallMap[K]) Update(k K, v uint64) { t.t.Set(item[K]{k, v}) }
func (t *tidwallMap[K]) Get(k K) (uint64, bool) {
	it, ok := t.t.Get(item[K]{k: k})
	return it.v, ok
}
func (t *tidwallMap[K]) Scan(k K, n int) int {
	c := 0
	t.t.Ascend(item[K]{k: k}, func(item[K]) bool { c++; return c < n })
	return c
}

// ---- skipmap ----------------------------------------------------------------

// skipMap is zhangyunhao116/skipmap, a lock-free concurrent skiplist: the
// blog's crossbeam-skiplist. It has no seek, so no scans from a key.
type skipMap[K comparable] struct{ s *skipmap.FuncMap[K, uint64] }

func newSkip[K comparable](less func(a, b K) bool) *skipMap[K] {
	return &skipMap[K]{skipmap.NewFunc[K, uint64](less)}
}

func (s *skipMap[K]) Insert(k K, v uint64)   { s.s.Store(k, v) }
func (s *skipMap[K]) Update(k K, v uint64)   { s.s.Store(k, v) }
func (s *skipMap[K]) Get(k K) (uint64, bool) { return s.s.Load(k) }
func (s *skipMap[K]) Scan(K, int) int        { return -1 }

// ---- hash maps, for reference -----------------------------------------------

type goMap[K comparable] map[K]uint64

func (g goMap[K]) Insert(k K, v uint64)   { g[k] = v }
func (g goMap[K]) Update(k K, v uint64)   { g[k] = v }
func (g goMap[K]) Get(k K) (uint64, bool) { v, ok := g[k]; return v, ok }
func (g goMap[K]) Scan(K, int) int        { return -1 }

type xsyncMap[K comparable] struct{ m *xsync.Map[K, uint64] }

func (x xsyncMap[K]) Insert(k K, v uint64)   { x.m.Store(k, v) }
func (x xsyncMap[K]) Update(k K, v uint64)   { x.m.Store(k, v) }
func (x xsyncMap[K]) Get(k K) (uint64, bool) { return x.m.Load(k) }
func (x xsyncMap[K]) Scan(K, int) int        { return -1 }

// ---- comparators --------------------------------------------------------------

func lessOrdered[K cmp.Ordered](a, b K) bool { return a < b }

func lessUUID(a, b [16]byte) bool {
	x, y := be64(a[0:8]), be64(b[0:8])
	if x != y {
		return x < y
	}
	return be64(a[8:16]) < be64(b[8:16])
}

func be64(b []byte) uint64 {
	_ = b[7]
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}
