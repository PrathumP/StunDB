//go:build !race

package btree

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestAdversaryClearVsRootSplit races Clear against an insert that splits the
// root. Both root-splitting paths (tryInsert on a full root leaf, splitInner
// on a full internal root) check that the node is still the root, then split
// it and call growRoot, which stores the new root unconditionally. A Clear
// that lands between the check and the store is overwritten: the cleared
// contents come back, and whatever was written to the cleared map in the
// meantime is lost.
//
// Goroutine A: Insert(splitKey), which splits the full root.
// Goroutine B: Clear(), then Insert(laterKey) once Clear has returned.
//
// Allowed outcomes: Clear first, or A completes on the old contents; either
// way the map ends as {laterKey} or {splitKey, laterKey}. Never the entries
// Clear removed, and never without laterKey, which was inserted after Clear
// returned and is never removed.
func TestAdversaryClearVsRootSplit(t *testing.T) {
	t.Run("root=leaf", func(t *testing.T) {
		raceClearWithRootSplit(t, 20000, func(m *Map[uint64, uint64]) {
			for i := range leafCap { // a full root leaf
				m.Insert(uint64(i), 0)
			}
		})
	})
	t.Run("root=inner", func(t *testing.T) {
		raceClearWithRootSplit(t, 4000, func(m *Map[uint64, uint64]) {
			for i := uint64(0); ; i++ { // ascending until the root holds innerCap separators
				m.Insert(i, 0)
				if r := m.fixed.root.Load(); r.level == 1 && int(r.n) == innerCap {
					return
				}
			}
		})
	})
}

func raceClearWithRootSplit(t *testing.T, trials int, fill func(*Map[uint64, uint64])) {
	const splitKey, laterKey = 1 << 40, 1 << 41
	resurrected, lost := 0, 0
	var before, firstLen int
	for trial := range trials {
		m := New[uint64, uint64]()
		fill(m)
		before = m.Len()
		var gate atomic.Int32
		var wg sync.WaitGroup
		delay := trial % 64
		wg.Go(func() {
			gate.Add(1)
			for gate.Load() < 2 {
			}
			m.Insert(splitKey, 1) // splits the root
		})
		wg.Go(func() {
			gate.Add(1)
			for gate.Load() < 2 {
			}
			for i := 0; i < delay; i++ {
				gate.Load()
			}
			m.Clear()
			m.Insert(laterKey, 2) // strictly after Clear returned
		})
		wg.Wait()
		n := m.Len()
		_, hasLater := m.Get(laterKey)
		if n > 2 {
			if resurrected == 0 {
				firstLen = n
			}
			resurrected++
		}
		if !hasLater {
			lost++
		}
	}
	if resurrected > 0 || lost > 0 {
		t.Errorf("%d entries before Clear; of %d trials: %d ended holding the cleared entries (first: Len()=%d, want <= 2); "+
			"%d lost laterKey, inserted after Clear returned and never removed (want 0)",
			before, trials, resurrected, firstLen, lost)
	}
}
