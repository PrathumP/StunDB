package btree

import "unsafe"

// Batches (the blog's "batch mode using Rust's async state machines").
//
// A B-tree's memory accesses are chained: a lookup can't fetch the next node
// before it has searched this one. A batch hides that latency by running many
// lookups at once: each works until it needs a line that may be in memory,
// prefetches it, and steps aside for the next; by the time the round comes
// back, the line has arrived. Database research calls this interleaving
// (Kocberber et al., VLDB 2015; Psaropoulos et al., VLDB 2017).
//
// bbtree writes each lookup as an async function, a state machine the
// compiler builds. Go has no zero-cost async, and a hand-written state
// machine per lane costs a branch on its state every step. But every leaf of a
// B+-tree is at the same depth, so the lanes can move in lockstep instead
// (group prefetching, Chen et al., SIGMOD 2004): each level is two passes over
// the lanes, one that searches each lane's node and prefetches the child
// pointer it chose, one that follows those pointers and prefetches the
// children. No goroutines, no allocation, no state switch.
//
// A lane holds no latch between passes, so lanes never wait on one another.
// Each pass validates what it read against node versions, as a single lookup
// does; a lane whose check fails drops out and is retried alone at the end.

// batchLanes is the number of lookups in flight. Each pass keeps about this
// many misses outstanding, against the ~22 L1 miss buffers of a core.
const batchLanes = 24

// minBatchHeight: below three levels the leaves are few enough to sit in
// cache, and the passes cost more than they hide (the blog: "smaller trees
// look their keys up one at a time").
const minBatchHeight = 3

// group is the state of up to batchLanes lanes moving down the tree together.
type group struct {
	node   [batchLanes]*header
	ver    [batchLanes]uint64
	parent [batchLanes]*header
	pver   [batchLanes]uint64
	pos    [batchLanes]int
	failed uint32 // a bit per lane that has to be retried alone
}

// prefetchNode fetches the lines a search reads first: the header and the
// inline keys.
func prefetchNode[W word](h *header) {
	if wordBits[W]() == 16 {
		prefetch2(unsafe.Pointer(h)) // 16 + 56*2 = 128 bytes
	} else {
		prefetch4(unsafe.Pointer(h)) // 16 + 56*4 = 240 bytes
	}
}

// descendGroup moves every lane of g from the root to the leaf that covers its
// key, with each leaf's version read and its parent validated after.
func (t *tree[K, V, W]) descendGroup(g *group, keys []K) {
	n := len(keys)
	root, rv := t.readRoot()
	for j := range n {
		g.node[j], g.ver[j], g.parent[j] = root, rv, nil
	}
	g.failed = 0
	for level := int(root.level); level > 0; level-- {
		// Pass 1: search each lane's node, prefetch the chosen edge's line.
		for j := range n {
			if g.failed&(1<<j) != 0 {
				continue
			}
			h := g.node[j]
			if p := g.parent[j]; p != nil {
				v := h.ver.Load()
				if v&lockedBit != 0 || p.ver.Load() != g.pver[j] {
					g.failed |= 1 << j
					continue
				}
				g.ver[j] = v
			}
			in := asInner[K, V, W](h)
			idx, ok := in.route(keys[j], in.count(innerCap), g.ver[j])
			if !ok {
				g.failed |= 1 << j
				continue
			}
			g.pos[j] = idx
			prefetch(unsafe.Pointer(&in.children[idx]))
		}
		// Pass 2: follow each edge, prefetch the child's header and inline keys.
		for j := range n {
			if g.failed&(1<<j) != 0 {
				continue
			}
			in := asInner[K, V, W](g.node[j])
			child := in.children[g.pos[j]]
			if child == nil || in.ver.Load() != g.ver[j] {
				g.failed |= 1 << j
				continue
			}
			g.parent[j], g.pver[j] = g.node[j], g.ver[j]
			g.node[j] = child
			prefetchNode[W](child)
		}
	}
	// At the leaves: read each version, then check the parent still pointed
	// there.
	for j := range n {
		if g.failed&(1<<j) != 0 || g.parent[j] == nil {
			continue
		}
		v := g.node[j].ver.Load()
		if v&lockedBit != 0 || g.parent[j].ver.Load() != g.pver[j] {
			g.failed |= 1 << j
			continue
		}
		g.ver[j] = v
	}
}

// getMany looks up keys[i] into vals[i] and found[i].
func (t *tree[K, V, W]) getMany(keys []K, vals []V, found []bool) {
	if t.height() < minBatchHeight {
		for i, k := range keys {
			vals[i], found[i] = t.get(k)
		}
		return
	}
	var g group
	for base := 0; base < len(keys); base += batchLanes {
		end := min(base+batchLanes, len(keys))
		t.getGroup(&g, keys[base:end], vals[base:end], found[base:end])
	}
}

func (t *tree[K, V, W]) getGroup(g *group, keys []K, vals []V, found []bool) {
	n := len(keys)
	t.descendGroup(g, keys)
	// Pass 1 at the leaves: the inline keys rule a key out, or leave a
	// candidate, whose key and value lines are prefetched.
	for j := range n {
		if g.failed&(1<<j) != 0 {
			continue
		}
		l := asLeaf[K, V, W](g.node[j])
		c := l.count(leafCap)
		var tk W
		if isU64[K]() {
			tk = inlineU64[W](asU64(&keys[j]), l.plen)
		} else {
			tk = inlineWindow[K, W](keys[j], l.plen)
		}
		i := countLess(&l.ik, tk)
		if i < c && l.ik[i] == tk {
			g.pos[j] = i
			prefetch(unsafe.Pointer(&l.keys[i]))
			prefetch(unsafe.Pointer(&l.vals[i]))
			continue
		}
		g.pos[j] = -1 // absent, if the version holds
		if l.ver.Load() != g.ver[j] {
			g.failed |= 1 << j
			continue
		}
		var zero V
		vals[j], found[j] = zero, false
	}
	// Pass 2: settle the candidates.
	for j := range n {
		if g.failed&(1<<j) != 0 || g.pos[j] < 0 {
			continue
		}
		l := asLeaf[K, V, W](g.node[j])
		pos, hit, ok := l.seek(keys[j], l.count(leafCap), g.ver[j])
		var val V
		if hit {
			val = l.vals[pos]
		}
		if !ok || l.ver.Load() != g.ver[j] {
			g.failed |= 1 << j
			continue
		}
		vals[j], found[j] = val, hit
	}
	for j := range n {
		if g.failed&(1<<j) != 0 {
			vals[j], found[j] = t.get(keys[j])
		}
	}
}

// insertMany inserts keys[i], vals[i] in order, and returns how many keys
// were new. The lanes find each entry's leaf together, prefetching the lines
// the insert will shift; then the entries go in one at a time, each into the
// leaf its lane found if that leaf is still at the version the lane read. An
// earlier entry of the batch moves its leaf's version on, and the later lanes
// that found the same leaf with it. A leaf that changed otherwise, or is
// full, sends its entry through a single insert.
func (t *tree[K, V, W]) insertMany(keys []K, vals []V) int {
	added := 0
	if t.height() < minBatchHeight {
		for i, k := range keys {
			if _, replaced := t.insert(k, vals[i]); !replaced {
				added++
			}
		}
		return added
	}
	var g group
	for base := 0; base < len(keys); base += batchLanes {
		end := min(base+batchLanes, len(keys))
		ks, vs := keys[base:end], vals[base:end]
		n := len(ks)
		t.descendGroup(&g, ks)
		for j := range n {
			if g.failed&(1<<j) != 0 {
				continue
			}
			l := asLeaf[K, V, W](g.node[j])
			tk := inlineOf[K, W](ks[j], l.plen)
			p := min(countLess(&l.ik, tk), leafCap-1)
			prefetch(unsafe.Pointer(&l.keys[p]))
			prefetch(unsafe.Pointer(&l.vals[p]))
		}
		for j := range n {
			k, v := ks[j], vs[j]
			if g.failed&(1<<j) == 0 {
				l := asLeaf[K, V, W](g.node[j])
				if l.tryLock(g.ver[j]) {
					c := int(l.n)
					pos, hit, _ := l.seek(k, c, g.ver[j]|lockedBit)
					if hit || c < leafCap {
						if hit {
							l.vals[pos] = v
						} else {
							l.insertAt(pos, c, k, v)
							added++
						}
						l.unlock()
						for r := j + 1; r < n; r++ {
							if g.node[r] == g.node[j] && g.ver[r] == g.ver[j] {
								g.ver[r] += 2
							}
						}
						continue
					}
					l.unlockUnchanged(g.ver[j])
				}
			}
			if _, replaced := t.insert(k, v); !replaced {
				added++
			}
		}
	}
	return added
}
