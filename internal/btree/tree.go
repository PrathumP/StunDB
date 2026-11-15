package btree

import "sync/atomic"

// tree is the concurrent B+-tree behind Map, for one key type K, value type V
// and inline-key width W.
//
// Concurrency is optimistic lock coupling (Leis et al., "The ART of Practical
// Synchronization"; the blog's version lock). A reader takes no latch and
// writes nothing: at each node it reads the version, reads the node, and checks
// the version again before trusting what it read. So readers cause no cache
// coherence traffic, and lookups scale with cores.
//
// Writers latch only the nodes they change, by compare-and-swap from the
// version their descent read. Full nodes split on the way down (Leis's B-tree),
// so a split never has to climb back up the tree.
//
// Memory reclamation is the garbage collector's: a reader that holds a pointer
// to a node keeps the node alive, so the blog's free lists of type-stable pages
// are not needed here. Nodes never merge, so a node's key range only ever
// shrinks, when it splits.
type tree[K Key, V any, W word] struct {
	root   atomic.Pointer[header]
	leaves slab[leaf[K, V, W]]
	inners slab[inner[K, V, W]]
}

func newTree[K Key, V any, W word]() *tree[K, V, W] {
	t := new(tree[K, V, W])
	t.root.Store(&newLeaf[K, V, W]().header)
	return t
}

// readRoot returns the root and its version, once the version is unlatched
// and read while the node was still the root.
func (t *tree[K, V, W]) readRoot() (*header, uint64) {
	for spins := 0; ; spins++ {
		n := t.root.Load()
		v := n.ver.Load()
		if v&lockedBit == 0 && n == t.root.Load() {
			return n, v
		}
		backoff(spins)
	}
}

// findLeaf descends to the leaf that covers key, or the first leaf if first
// is set, and returns it with the version that vouches for it: when that
// version was read, the leaf's parent was still at the version under which it
// pointed to the leaf.
func (t *tree[K, V, W]) findLeaf(key K, first bool) (*leaf[K, V, W], uint64) {
restart:
	for {
		n, v := t.readRoot()
		for n.level != 0 {
			in := asInner[K, V, W](n)
			idx := 0
			if !first {
				var ok bool
				if idx, ok = in.route(key, in.count(innerCap), v); !ok {
					continue restart
				}
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
		return asLeaf[K, V, W](n), v
	}
}

func (t *tree[K, V, W]) get(key K) (val V, found bool) {
	for {
		l, v := t.findLeaf(key, false)
		pos, hit, ok := l.seek(key, l.count(leafCap), v)
		if !ok {
			continue
		}
		if hit {
			val = l.vals[pos] // copied out, then validated: a seqlock read
		}
		if l.ver.Load() == v {
			return val, hit
		}
		var zero V
		val = zero
	}
}

// lockLeaf descends to key's leaf and latches it.
func (t *tree[K, V, W]) lockLeaf(key K) (*leaf[K, V, W], uint64) {
	for spins := 0; ; spins++ {
		l, v := t.findLeaf(key, false)
		if l.tryLock(v) {
			return l, v
		}
		backoff(spins)
	}
}

func (t *tree[K, V, W]) update(key K, fn func(*V)) bool {
	l, v := t.lockLeaf(key)
	pos, hit, _ := l.seek(key, int(l.n), v|lockedBit)
	if !hit {
		l.unlockUnchanged(v)
		return false
	}
	defer l.unlock() // fn is user code; release the latch even if it panics
	fn(&l.vals[pos])
	return true
}

func (t *tree[K, V, W]) remove(key K) (old V, found bool) {
	l, v := t.lockLeaf(key)
	n := int(l.n)
	pos, hit, _ := l.seek(key, n, v|lockedBit)
	if !hit {
		l.unlockUnchanged(v)
		return old, false
	}
	old = l.vals[pos]
	l.removeAt(pos, n)
	l.unlock()
	return old, true
}

// insert puts key and val in the tree and returns the value it replaced.
func (t *tree[K, V, W]) insert(key K, val V) (old V, replaced bool) {
	for spins := 0; ; spins++ {
		if spins > 0 {
			backoff(spins)
		}
		if old, replaced, done := t.tryInsert(key, val); done {
			return old, replaced
		}
	}
}

// tryInsert makes one attempt, from the root, and reports whether it got
// through. It follows Leis et al.'s B-tree: a full internal node met on the
// way down is split there and then, with its parent latched, and the attempt
// restarts. So when the leaf is full, its parent has room for the separator.
func (t *tree[K, V, W]) tryInsert(key K, val V) (old V, replaced, done bool) {
	n, v := t.readRoot()
	var (
		parent *inner[K, V, W]
		pv     uint64
		pidx   int
		f      fences[K]
	)
	for n.level != 0 {
		in := asInner[K, V, W](n)
		cnt := in.count(innerCap)
		if cnt == innerCap {
			t.splitInner(in, v, parent, pv, pidx, &f)
			return old, false, false
		}
		idx, ok := in.route(key, cnt, v)
		if !ok {
			return old, false, false
		}
		child := in.children[idx]
		if child == nil {
			return old, false, false
		}
		// The child's fences, copied racily: the version check below vouches
		// for them along with the child pointer.
		if idx > 0 {
			f.lo, f.hasLo = in.keys[idx-1], true
		}
		if idx < cnt {
			f.hi, f.hasHi = in.keys[idx], true
		}
		cv := child.ver.Load()
		if in.ver.Load() != v || cv&lockedBit != 0 {
			return old, false, false
		}
		parent, pv, pidx = in, v, idx
		n, v = child, cv
	}

	l := asLeaf[K, V, W](n)
	if !l.tryLock(v) {
		return old, false, false
	}
	cnt := int(l.n)
	pos, hit, _ := l.seek(key, cnt, v|lockedBit)
	if hit {
		old = l.vals[pos]
		l.vals[pos] = val
		l.unlock()
		return old, true, true
	}
	if cnt < leafCap {
		l.insertAt(pos, cnt, key, val)
		l.unlock()
		return old, false, true
	}

	// The leaf is full: split it, with its parent latched for the separator.
	if parent != nil {
		if !parent.tryLock(pv) {
			l.unlockUnchanged(v)
			return old, false, false
		}
	} else if n != t.root.Load() {
		l.unlockUnchanged(v)
		return old, false, false
	}
	right := t.leaves.alloc()
	sep := l.split(&f, right)
	if keyLess(key, sep) {
		p, _, _ := l.seek(key, int(l.n), v|lockedBit)
		l.insertAt(p, int(l.n), key, val)
	} else {
		p, _, _ := right.seek(key, int(right.n), 0)
		right.insertAt(p, int(right.n), key, val)
	}
	if parent != nil {
		parent.insertChild(pidx, sep, &right.header)
		parent.unlock()
	} else {
		t.growRoot(n, sep, &right.header)
	}
	l.unlock()
	return old, false, true
}

// splitInner splits a full internal node met on the way down, if it and its
// parent are still at the versions the descent read.
func (t *tree[K, V, W]) splitInner(in *inner[K, V, W], v uint64, parent *inner[K, V, W], pv uint64, pidx int, f *fences[K]) {
	if parent != nil && !parent.tryLock(pv) {
		return
	}
	if !in.tryLock(v) {
		if parent != nil {
			parent.unlockUnchanged(pv)
		}
		return
	}
	if parent == nil && &in.header != t.root.Load() {
		in.unlockUnchanged(v)
		return
	}
	right := t.inners.alloc()
	sep := in.split(f, right)
	if parent != nil {
		parent.insertChild(pidx, sep, &right.header)
		parent.unlock()
	} else {
		t.growRoot(&in.header, sep, &right.header)
	}
	in.unlock()
}

// growRoot puts a new root above the old one, which has just split into
// itself and right. The caller holds the old root's latch, so readers that
// reached it before the new root went up fail their check and start over.
//
// The swap is a compare-and-swap from old: Clear may have replaced the root
// since the caller checked it, and then the split belongs to the discarded
// tree. Storing unconditionally would put the cleared tree back and lose
// whatever was written to the new one.
func (t *tree[K, V, W]) growRoot(old *header, sep K, right *header) {
	r := t.inners.alloc()
	r.level = old.level + 1
	r.n = 1
	r.keys[0] = sep
	r.children[0], r.children[1] = old, right
	rebuildInline(&r.ik, r.keys[:1], 0)
	t.root.CompareAndSwap(old, &r.header)
}

// height is the number of levels, leaves included.
func (t *tree[K, V, W]) height() int { return int(t.root.Load().level) + 1 }

// ---- scans ------------------------------------------------------------------

// ascend calls fn on every entry from start (or the first, if !hasStart) in
// key order until fn returns false. Each leaf is copied out under its version,
// then handed to fn, so fn runs with no latch held and may call back into the
// map. A scan is not a snapshot: an entry present for the whole scan is visited
// exactly once; one inserted or removed meanwhile may or may not be.
func (t *tree[K, V, W]) ascend(start K, hasStart bool, fn func(K, V) bool) {
	var kb [leafCap]K
	var vb [leafCap]V
	from, inclusive := start, true
	first := !hasStart
restart:
	for {
		l, v := t.findLeaf(from, first)
		pos := 0
		if !first {
			p, hit, ok := l.seek(from, l.count(leafCap), v)
			if !ok {
				continue restart
			}
			pos = p
			if hit && !inclusive {
				pos++
			}
		}
		for {
			// Read the next leaf's version first, before this leaf's entries:
			// its line, most likely a miss, is then on its way while this
			// leaf is copied and handed out, instead of after. next is a
			// racy read, but always nil or a leaf; if this leaf's version
			// holds below, it was this leaf's neighbor throughout, and nv a
			// version it had.
			next := l.next
			var nv uint64
			if next != nil {
				nv = next.ver.Load()
			}
			n := l.count(leafCap)
			m := 0
			if pos < n {
				m = copy(kb[:], l.keys[pos:n])
				copy(vb[:], l.vals[pos:n])
			}
			if l.ver.Load() != v {
				continue restart // resume after the last key handed out
			}
			for i := 0; i < m; i++ {
				if !fn(kb[i], vb[i]) {
					return
				}
				from, inclusive, first = kb[i], false, false
			}
			if next == nil {
				return
			}
			if nv&lockedBit != 0 {
				nv = next.stableVersion()
			}
			l, v, pos = next, nv, 0
		}
	}
}

// count walks the leaves and counts their entries.
func (t *tree[K, V, W]) count() int {
	total := 0
	var zero K
restart:
	for {
		total = 0
		l, v := t.findLeaf(zero, true)
		for {
			n := l.count(leafCap)
			next := l.next
			if l.ver.Load() != v {
				continue restart
			}
			total += n
			if next == nil {
				return total
			}
			l, v = next, next.stableVersion()
		}
	}
}
