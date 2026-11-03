package btree

import (
	"fmt"
	"testing"
)

// checkMap verifies every structural invariant of m's tree and returns its
// number of entries. The map must be quiescent.
func checkMap[K Key, V any](tb testing.TB, m *Map[K, V]) int {
	tb.Helper()
	if isStr[K]() {
		return checkTree(tb, m.str)
	}
	return checkTree(tb, m.fixed)
}

func checkTree[K Key, V any, W word](tb testing.TB, t *tree[K, V, W]) int {
	tb.Helper()
	root := t.root.Load()
	var leaves []*leaf[K, V, W]
	if err := checkNode(root, fences[K]{}, int(root.level), true, &leaves); err != nil {
		tb.Fatalf("invariant: %v", err)
	}
	total := 0
	for i, l := range leaves {
		total += int(l.n)
		var want *leaf[K, V, W]
		if i+1 < len(leaves) {
			want = leaves[i+1]
		}
		if l.next != want {
			tb.Fatalf("invariant: leaf %d of %d: next pointer is not the next leaf", i, len(leaves))
		}
	}
	return total
}

func checkNode[K Key, V any, W word](h *header, f fences[K], level int, isRoot bool, leaves *[]*leaf[K, V, W]) error {
	if h == nil {
		return fmt.Errorf("nil node at level %d", level)
	}
	if int(h.level) != level {
		return fmt.Errorf("node at level %d says %d", level, h.level)
	}
	if h.ver.Load()&lockedBit != 0 {
		return fmt.Errorf("node left latched")
	}
	if h.plen != f.prefixLen() {
		return fmt.Errorf("level %d: prefix %d, fences share %d", level, h.plen, f.prefixLen())
	}
	inFences := func(k K) error {
		if f.hasLo && keyLess(k, f.lo) {
			return fmt.Errorf("level %d: key %v below lower fence %v", level, k, f.lo)
		}
		if f.hasHi && !keyLess(k, f.hi) {
			return fmt.Errorf("level %d: key %v not below upper fence %v", level, k, f.hi)
		}
		return nil
	}
	checkKeys := func(keys []K, ik *[slots]W) error {
		for i, k := range keys {
			if i > 0 && !keyLess(keys[i-1], k) {
				return fmt.Errorf("level %d: keys out of order at %d: %v, %v", level, i, keys[i-1], k)
			}
			if err := inFences(k); err != nil {
				return err
			}
			if want := inlineOf[K, W](k, h.plen); ik[i] != want {
				return fmt.Errorf("level %d: inline key %d is %#x, want %#x", level, i, ik[i], want)
			}
		}
		for i := len(keys); i < slots; i++ {
			if ik[i] != padInline[W]() {
				return fmt.Errorf("level %d: slot %d past the count is not padding", level, i)
			}
		}
		return nil
	}

	if level == 0 {
		l := asLeaf[K, V, W](h)
		n := int(l.n)
		if n > leafCap {
			return fmt.Errorf("leaf holds %d", n)
		}
		*leaves = append(*leaves, l)
		return checkKeys(l.keys[:n], &l.ik)
	}

	in := asInner[K, V, W](h)
	n := int(in.n)
	if n < 1 || n > innerCap {
		return fmt.Errorf("level %d: internal node holds %d separators", level, n)
	}
	if err := checkKeys(in.keys[:n], &in.ik); err != nil {
		return err
	}
	for i := n + 1; i <= innerCap; i++ {
		if in.children[i] != nil {
			return fmt.Errorf("level %d: child %d past the count is set", level, i)
		}
	}
	for i := 0; i <= n; i++ {
		cf := f
		if i > 0 {
			cf.lo, cf.hasLo = in.keys[i-1], true
		}
		if i < n {
			cf.hi, cf.hasHi = in.keys[i], true
		}
		if err := checkNode(in.children[i], cf, level-1, false, leaves); err != nil {
			return err
		}
	}
	return nil
}
