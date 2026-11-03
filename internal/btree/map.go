package btree

import "unsafe"

// Map is a concurrent ordered map: a B+-tree whose nodes are searched by
// inline keys, read optimistically and latched one at a time. Every method is
// safe for concurrent use; reads never block and never write to shared memory.
//
// The zero value is not usable; call New.
//
// Values are copied in and out. A value that refers to memory (a slice, a
// pointer) must not be changed in place once it is in the map: readers copy
// it without a latch, so replace it with Insert or Update instead.
type Map[K Key, V any] struct {
	// One of the two is set, by key type: 2-byte inline keys for keys that
	// compare in the node, 4-byte ones for strings. kindOf folds, so each
	// method calls one of them directly.
	fixed *tree[K, V, uint16]
	str   *tree[K, V, uint32]
	mu    raceLock
}

// New returns an empty map.
func New[K Key, V any]() *Map[K, V] {
	m := new(Map[K, V])
	if kindOf[K]() == kindStr {
		m.str = newTree[K, V, uint32]()
	} else {
		m.fixed = newTree[K, V, uint16]()
	}
	return m
}

func isStr[K Key]() bool { return kindOf[K]() == kindStr }

// Get returns the value stored under key.
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if isStr[K]() {
		return m.str.get(key)
	}
	return m.fixed.get(key)
}

// Contains reports whether key is in the map.
func (m *Map[K, V]) Contains(key K) bool {
	_, ok := m.Get(key)
	return ok
}

// Insert sets key to val, and returns the value it replaced, if any.
func (m *Map[K, V]) Insert(key K, val V) (old V, replaced bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isStr[K]() {
		return m.str.insert(key, val)
	}
	return m.fixed.insert(key, val)
}

// Update runs fn on key's value, in place, with the value's leaf latched, and
// reports whether the key was there. fn must not call into the map.
func (m *Map[K, V]) Update(key K, fn func(*V)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isStr[K]() {
		return m.str.update(key, fn)
	}
	return m.fixed.update(key, fn)
}

// Remove deletes key and returns its value.
func (m *Map[K, V]) Remove(key K) (V, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isStr[K]() {
		return m.str.remove(key)
	}
	return m.fixed.remove(key)
}

// GetMany looks up all of keys at once, overlapping their waits for memory,
// and stores keys[i]'s value in vals[i] and whether it was found in found[i].
// vals and found must be at least as long as keys.
func (m *Map[K, V]) GetMany(keys []K, vals []V, found []bool) {
	vals, found = vals[:len(keys)], found[:len(keys)]
	m.mu.RLock()
	defer m.mu.RUnlock()
	if isStr[K]() {
		m.str.getMany(keys, vals, found)
	} else {
		m.fixed.getMany(keys, vals, found)
	}
}

// InsertMany inserts keys[i] with vals[i], in order, overlapping their
// descents, and returns how many of the keys were not in the map before.
func (m *Map[K, V]) InsertMany(keys []K, vals []V) int {
	vals = vals[:len(keys)]
	m.mu.Lock()
	defer m.mu.Unlock()
	if isStr[K]() {
		return m.str.insertMany(keys, vals)
	}
	return m.fixed.insertMany(keys, vals)
}

// Ascend calls fn on each entry with a key >= start, in key order, until fn
// returns false. fn runs without any latch held and may call into the map.
// It is not a snapshot: an entry present for the whole scan is visited
// exactly once; one inserted or removed during it may or may not be.
func (m *Map[K, V]) Ascend(start K, fn func(K, V) bool) {
	m.ascend(start, true, fn)
}

// All calls fn on every entry in key order until fn returns false, like
// Ascend from the first key.
func (m *Map[K, V]) All(fn func(K, V) bool) {
	var zero K
	m.ascend(zero, false, fn)
}

func (m *Map[K, V]) ascend(start K, hasStart bool, fn func(K, V) bool) {
	if RaceSerialized {
		m.ascendSerialized(start, hasStart, fn)
		return
	}
	if isStr[K]() {
		m.str.ascend(start, hasStart, fn)
	} else {
		m.fixed.ascend(start, hasStart, fn)
	}
}

// ascendSerialized is the race-build scan: it copies a chunk of entries under
// the read lock, then runs fn on them with the lock released, so that fn may
// write to the map as it may in ordinary builds.
func (m *Map[K, V]) ascendSerialized(start K, hasStart bool, fn func(K, V) bool) {
	const chunk = 256
	keys := make([]K, 0, chunk)
	vals := make([]V, 0, chunk)
	skipStart := false // start was the last key of the previous chunk
	for {
		keys, vals = keys[:0], vals[:0]
		collect := func(k K, v V) bool {
			if skipStart && keyCompare(k, start, 0) == 0 {
				return true
			}
			keys, vals = append(keys, k), append(vals, v)
			return len(keys) < chunk
		}
		m.mu.RLock()
		if isStr[K]() {
			m.str.ascend(start, hasStart, collect)
		} else {
			m.fixed.ascend(start, hasStart, collect)
		}
		m.mu.RUnlock()
		for i := range keys {
			if !fn(keys[i], vals[i]) {
				return
			}
		}
		if len(keys) < chunk {
			return
		}
		start, hasStart, skipStart = keys[len(keys)-1], true, true
	}
}

// Range calls fn on each entry with start <= key < end, in key order, until
// fn returns false.
func (m *Map[K, V]) Range(start, end K, fn func(K, V) bool) {
	m.Ascend(start, func(k K, v V) bool {
		return keyLess(k, end) && fn(k, v)
	})
}

// Len counts the entries by walking the leaves: O(n). The map keeps no count,
// because a shared counter would be one cache line that every insert and
// remove on every core writes, the coherence traffic the tree is built to
// avoid. Callers that need the size often should count themselves.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if isStr[K]() {
		return m.str.count()
	}
	return m.fixed.count()
}

// Clear removes every entry. Operations running concurrently with it may
// complete on the old contents.
func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isStr[K]() {
		m.str.root.Store(&newLeaf[K, V, uint32]().header)
	} else {
		m.fixed.root.Store(&newLeaf[K, V, uint16]().header)
	}
}

// Height is the number of levels of the tree, leaves included.
func (m *Map[K, V]) Height() int {
	if isStr[K]() {
		return m.str.height()
	}
	return m.fixed.height()
}

// BytesKey views b as a string key without copying it, for lookups with a
// []byte in hand. The map must not keep the result: insert string(b) instead.
func BytesKey(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
