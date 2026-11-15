package btree

import (
	"runtime"
	"sync/atomic"
	"unsafe"
)

// Node sizing (the blog's "How does that apply to B-trees?"): a node holds a
// few dozen keys, few enough that one count over its inline keys finds the
// place, and its header and inline keys share the first cache lines. With
// 2-byte inline keys, the 16-byte header and 56 inline keys are exactly two
// lines, when the node starts on a line, which slab.go sees to. A
// uint64->uint64 leaf is 1016 bytes, and so is a uint64 internal node.
const (
	slots    = 56        // inline-key slots per node; the last is always padding
	leafCap  = slots - 1 // entries per leaf
	innerCap = slots - 1 // separators per internal node; children = innerCap+1

	lockedBit = 1
)

// header starts every node. Readers read every field but the version without
// synchronization, validated by the version afterwards (optimistic lock
// coupling); writers change a node only while holding its latch.
type header struct {
	// ver is the node's version lock: bit 0 is the exclusive latch, the rest
	// a version that every release bumps. A reader reads it, reads the node,
	// and reads it again; if it hasn't changed, what it read is consistent.
	ver   atomic.Uint64
	n     uint16 // keys in a leaf, separators in an internal node
	plen  uint8  // bytes every key under this node shares, from its fences
	level uint8  // 0 for leaves; a node's level never changes
	_     [4]byte
}

// leaf holds entries. Inline keys first, in the header's lines; then the
// keys, which a search reads only for the candidate the inline keys leave;
// then the values.
type leaf[K Key, V any, W word] struct {
	header
	ik   [slots]W
	next *leaf[K, V, W] // the leaf to the right, for scans
	keys [leafCap]K
	vals [leafCap]V
}

// inner routes. Its separators' inline keys come first, then the children, a
// line of which a descent reads, then the separators themselves, which it
// reads only when inline keys tie.
type inner[K Key, V any, W word] struct {
	header
	ik       [slots]W
	children [innerCap + 1]*header
	keys     [innerCap]K
}

func asLeaf[K Key, V any, W word](h *header) *leaf[K, V, W] {
	return (*leaf[K, V, W])(unsafe.Pointer(h))
}

func asInner[K Key, V any, W word](h *header) *inner[K, V, W] {
	return (*inner[K, V, W])(unsafe.Pointer(h))
}

// ---- the version lock -------------------------------------------------------

// stableVersion waits until the node is unlatched and returns its version.
func (h *header) stableVersion() uint64 {
	for spins := 0; ; spins++ {
		v := h.ver.Load()
		if v&lockedBit == 0 {
			return v
		}
		backoff(spins)
	}
}

// tryLock latches the node if it is still at version v.
func (h *header) tryLock(v uint64) bool { return h.ver.CompareAndSwap(v, v|lockedBit) }

// unlock releases the latch and bumps the version, so that every reader that
// overlapped the change fails its check.
func (h *header) unlock() { h.ver.Add(1) }

// unlockUnchanged releases a latch taken at version v without changing the
// node, restoring v so that concurrent readers need not restart.
func (h *header) unlockUnchanged(v uint64) { h.ver.Store(v) }

func (h *header) count(capacity int) int {
	n := int(h.n) // racy: clamp it, so that a torn read stays in bounds
	if n > capacity {
		n = capacity
	}
	return n
}

func backoff(spins int) {
	if spins < 16 {
		return
	}
	runtime.Gosched()
}

// ---- prefixes and inline keys ----------------------------------------------

// fences are the separators on either side of a node in its ancestors: every
// key under the node is >= lo and < hi. A node on the tree's border lacks one.
type fences[K Key] struct {
	lo, hi       K
	hasLo, hasHi bool
}

// prefixLen is how many bytes every key between the fences shares: the bytes
// lo and hi share (the blog's prefix_keys, Bayer and Unterauer's prefix
// B-trees). No node stores the prefix itself; a key that reaches the node has
// it, because it sorts between the fences.
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

func (f *fences[K]) split(sep K) (left, right fences[K]) {
	left = fences[K]{lo: f.lo, hasLo: f.hasLo, hi: sep, hasHi: true}
	right = fences[K]{lo: sep, hasLo: true, hi: f.hi, hasHi: f.hasHi}
	return
}

// inlineOf is the inline key of k in a node whose keys share plen bytes.
//
// It doesn't inline: Go's inliner charges the call in the non-uint64 branch
// even where that branch folds away. So the hot searches (leaf.seek,
// inner.route, getGroup) test isU64 themselves and call inlineU64, whose one
// shift inlines; only their other key types call out.
func inlineOf[K Key, W word](k K, plen uint8) W {
	if isU64[K]() {
		return inlineU64[W](asU64(&k), plen)
	}
	return inlineWindow[K, W](k, plen)
}

// inlineU64 is the inline key of a uint64 key. Go defines shifts past the
// width as 0, so a prefix of 8 or more bytes needs no test.
func inlineU64[W word](k uint64, plen uint8) W {
	return toInline[W](k << (8 * uint(plen)))
}

func inlineWindow[K Key, W word](k K, plen uint8) W {
	return toInline[W](keyWindow(k, int(plen)))
}

func rebuildInline[K Key, W word](ik *[slots]W, keys []K, plen uint8) {
	for i, k := range keys {
		ik[i] = inlineOf[K, W](k, plen)
	}
	pad := padInline[W]()
	for i := len(keys); i < slots; i++ {
		ik[i] = pad
	}
}

// ---- leaves -----------------------------------------------------------------

// seek finds where key belongs in a leaf of n entries: the first position
// whose key is >= key, and whether it is equal. The inline keys narrow the
// search to the run that ties with the key's own inline key, which is almost
// always one entry or none; only those keys are read.
//
// v is the version the caller read: for string keys, every key header is
// checked against it before its bytes are followed, since a racing writer may
// have torn it. ok is false if that check failed. Callers holding the latch
// pass the latched version and always get ok.
func (l *leaf[K, V, W]) seek(key K, n int, v uint64) (pos int, found, ok bool) {
	var t W
	if isU64[K]() {
		t = inlineU64[W](asU64(&key), l.plen)
	} else {
		t = inlineWindow[K, W](key, l.plen)
	}
	i := countLess(&l.ik, t)
	if i >= n || l.ik[i] != t {
		return min(i, n), false, true // no key ties: absent
	}
	if kindOf[K]() != kindStr && l.keys[i] == key {
		return i, true, true // the usual hit: one candidate, compared in the node
	}
	return l.seekTies(key, i, n, v)
}

// seekTies settles a seek among the keys from i on whose inline keys tie
// with key's.
func (l *leaf[K, V, W]) seekTies(key K, i, n int, v uint64) (pos int, found, ok bool) {
	t := l.ik[i]
	if i+4 < n && l.ik[i+4] == t {
		// A long run of ties: a node on the tree's border, whose prefix is
		// empty, holding dense keys. Binary search the keys themselves.
		return l.seekKeys(key, i, n, v)
	}
	plen := int(l.plen)
	for ; i < n && l.ik[i] == t; i++ {
		k := l.keys[i]
		if kindOf[K]() == kindStr && l.ver.Load() != v {
			return 0, false, false
		}
		if c := tiedCompare[K, W](k, key, plen); c >= 0 {
			return i, c == 0, true
		}
	}
	return i, false, true
}

// tiedCompare orders two keys of a node whose keys share plen bytes and whose
// inline keys tie. A string that ends within the inline key's window is
// settled by the lengths alone, without reading its bytes from the heap:
// the window showed every byte either has past the prefix, and where they
// differ in length the longer one's extra bytes are zeros, so the shorter one
// is its prefix (bbtree's D36, for keys that end in the inline key).
func tiedCompare[K Key, W word](k, key K, plen int) int {
	if kindOf[K]() == kindStr {
		lk, lt := len(asStr(&k)), len(asStr(&key))
		if limit := plen + int(wordBits[W]()/8); lk <= limit && lt <= limit {
			return cmpInt(lk, lt)
		}
	}
	return keyCompare(k, key, plen)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (l *leaf[K, V, W]) seekKeys(key K, lo, hi int, v uint64) (pos int, found, ok bool) {
	plen := int(l.plen)
	n := hi
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		k := l.keys[mid]
		if kindOf[K]() == kindStr && l.ver.Load() != v {
			return 0, false, false
		}
		if keyCompare(k, key, plen) < 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < n {
		k := l.keys[lo]
		if kindOf[K]() == kindStr && l.ver.Load() != v {
			return 0, false, false
		}
		return lo, keyCompare(k, key, plen) == 0, true
	}
	return lo, false, true
}

func (l *leaf[K, V, W]) insertAt(pos, n int, key K, val V) {
	copy(l.keys[pos+1:n+1], l.keys[pos:n])
	copy(l.vals[pos+1:n+1], l.vals[pos:n])
	copy(l.ik[pos+1:n+1], l.ik[pos:n])
	l.keys[pos] = key
	l.vals[pos] = val
	l.ik[pos] = inlineOf[K, W](key, l.plen)
	l.n = uint16(n + 1)
}

func (l *leaf[K, V, W]) removeAt(pos, n int) {
	copy(l.keys[pos:n-1], l.keys[pos+1:n])
	copy(l.vals[pos:n-1], l.vals[pos+1:n])
	copy(l.ik[pos:n-1], l.ik[pos+1:n])
	var zk K
	var zv V
	l.keys[n-1] = zk // let the collector have what they pointed to
	l.vals[n-1] = zv
	l.ik[n-1] = padInline[W]()
	l.n = uint16(n - 1)
}

// split moves the upper half of a full leaf into right, a new, zeroed leaf,
// and returns the separator, the right half's first key. Both halves get the
// prefix their new fences share, which is at least what the leaf had, and
// their inline keys are cut again at it. The caller holds the leaf's latch.
func (l *leaf[K, V, W]) split(f *fences[K], right *leaf[K, V, W]) (sep K) {
	n := int(l.n)
	mid := n / 2
	rn := copy(right.keys[:], l.keys[mid:n])
	copy(right.vals[:], l.vals[mid:n])
	clear(l.keys[mid:n])
	clear(l.vals[mid:n])
	sep = right.keys[0]
	lf, rf := f.split(sep)
	l.plen, right.plen = lf.prefixLen(), rf.prefixLen()
	l.n, right.n = uint16(mid), uint16(rn)
	rebuildInline(&l.ik, l.keys[:mid], l.plen)
	rebuildInline(&right.ik, right.keys[:rn], right.plen)
	right.next = l.next
	l.next = right // right is complete before anything can reach it
	return sep
}

// ---- internal nodes ---------------------------------------------------------

// route returns the child of an internal node of n separators that covers
// key: the number of separators <= key. Inline keys decide it but for a tie,
// which compares the tied separators themselves.
func (in *inner[K, V, W]) route(key K, n int, v uint64) (idx int, ok bool) {
	var t W
	if isU64[K]() {
		t = inlineU64[W](asU64(&key), in.plen)
	} else {
		t = inlineWindow[K, W](key, in.plen)
	}
	i := countLess(&in.ik, t)
	if i >= n || in.ik[i] != t {
		return min(i, n), true
	}
	return in.routeTies(key, i, n, v)
}

func (in *inner[K, V, W]) routeTies(key K, i, n int, v uint64) (idx int, ok bool) {
	t := in.ik[i]
	if i+4 < n && in.ik[i+4] == t {
		return in.routeKeys(key, i, n, v)
	}
	plen := int(in.plen)
	for ; i < n && in.ik[i] == t; i++ {
		k := in.keys[i]
		if kindOf[K]() == kindStr && in.ver.Load() != v {
			return 0, false
		}
		if tiedCompare[K, W](k, key, plen) > 0 {
			break
		}
	}
	return i, true
}

func (in *inner[K, V, W]) routeKeys(key K, lo, hi int, v uint64) (idx int, ok bool) {
	plen := int(in.plen)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		k := in.keys[mid]
		if kindOf[K]() == kindStr && in.ver.Load() != v {
			return 0, false
		}
		if keyCompare(k, key, plen) <= 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo, true
}

// insertChild puts separator sep and, right of it, child at child index
// idx+1: the node at idx has just split into itself and child.
func (in *inner[K, V, W]) insertChild(idx int, sep K, child *header) {
	n := int(in.n)
	copy(in.keys[idx+1:n+1], in.keys[idx:n])
	copy(in.ik[idx+1:n+1], in.ik[idx:n])
	copy(in.children[idx+2:n+2], in.children[idx+1:n+1])
	in.keys[idx] = sep
	in.ik[idx] = inlineOf[K, W](sep, in.plen)
	in.children[idx+1] = child
	in.n = uint16(n + 1)
}

// split moves the upper half of a full internal node into right, a new, zeroed
// internal node, and returns the separator that goes up, which neither half
// keeps.
func (in *inner[K, V, W]) split(f *fences[K], right *inner[K, V, W]) (sep K) {
	n := int(in.n)
	mid := n / 2
	sep = in.keys[mid]
	right.level = in.level
	rn := copy(right.keys[:], in.keys[mid+1:n])
	copy(right.children[:], in.children[mid+1:n+1])
	clear(in.keys[mid:n])
	clear(in.children[mid+1 : n+1])
	lf, rf := f.split(sep)
	in.plen, right.plen = lf.prefixLen(), rf.prefixLen()
	in.n, right.n = uint16(mid), uint16(rn)
	rebuildInline(&in.ik, in.keys[:mid], in.plen)
	rebuildInline(&right.ik, right.keys[:rn], right.plen)
	return sep
}

func newLeaf[K Key, V any, W word]() *leaf[K, V, W] {
	l := new(leaf[K, V, W])
	rebuildInline[K, W](&l.ik, nil, 0)
	return l
}
