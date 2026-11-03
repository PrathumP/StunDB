package btree

import (
	"encoding/binary"
	"testing"
)

// Differential fuzzing, as bbtree's fuzz targets do: the input is a stream of
// operations, run on a Map and on a Go map side by side; every answer must
// match, and the tree's invariants must hold at the end.
//
//	go test -fuzz FuzzU64 ./internal/btree
//	go test -fuzz FuzzString ./internal/btree

func FuzzU64(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add(make([]byte, 600))
	f.Fuzz(func(t *testing.T, ops []byte) {
		m := New[uint64, uint64]()
		ref := map[uint64]uint64{}
		for i := 0; i+3 <= len(ops); i += 3 {
			op, k := ops[i]%8, uint64(binary.BigEndian.Uint16(ops[i+1:]))
			// Spread keys over the whole range, and repeat them often.
			k = k<<48 | k%7
			fuzzStep(t, m, ref, op, k, uint64(i))
		}
		fuzzCheck(t, m, ref)
	})
}

func FuzzString(f *testing.F) {
	f.Add([]byte("\x00ab\x01abc\x02a\x00\x00b\x03"))
	f.Fuzz(func(t *testing.T, ops []byte) {
		m := New[string, uint64]()
		ref := map[string]uint64{}
		for i := 0; i+2 <= len(ops); {
			op, n := ops[i]%8, int(ops[i+1]%24)
			i += 2
			n = min(n, len(ops)-i)
			// Keys share a long prefix so that nodes strip it, and tie on
			// their inline keys.
			k := "prefix/shared/by/every/key/" + string(ops[i:i+n])
			i += n
			fuzzStep(t, m, ref, op, k, uint64(i))
		}
		fuzzCheck(t, m, ref)
	})
}

func fuzzStep[K Key](t *testing.T, m *Map[K, uint64], ref map[K]uint64, op byte, k K, v uint64) {
	switch {
	case op < 4:
		old, replaced := m.Insert(k, v)
		if want, had := ref[k]; replaced != had || old != want {
			t.Fatalf("Insert(%v) = %v,%v; want %v,%v", k, old, replaced, want, had)
		}
		ref[k] = v
	case op < 6:
		old, found := m.Remove(k)
		if want, had := ref[k]; found != had || old != want {
			t.Fatalf("Remove(%v) = %v,%v; want %v,%v", k, old, found, want, had)
		}
		delete(ref, k)
	case op < 7:
		ks := []K{k, k, k}
		vs := []uint64{v, v + 1, v + 2}
		m.InsertMany(ks, vs)
		ref[k] = v + 2
	default:
		got, found := m.Get(k)
		if want, had := ref[k]; found != had || got != want {
			t.Fatalf("Get(%v) = %v,%v; want %v,%v", k, got, found, want, had)
		}
	}
}

func fuzzCheck[K Key](t *testing.T, m *Map[K, uint64], ref map[K]uint64) {
	if n := checkMap(t, m); n != len(ref) {
		t.Fatalf("tree holds %d, model %d", n, len(ref))
	}
	var prev K
	first := true
	seen := 0
	m.All(func(k K, v uint64) bool {
		if !first && !keyLess(prev, k) {
			t.Fatalf("scan out of order: %v then %v", prev, k)
		}
		if want, ok := ref[k]; !ok || want != v {
			t.Fatalf("scan found %v=%v, model %v,%v", k, v, want, ok)
		}
		prev, first = k, false
		seen++
		return true
	})
	if seen != len(ref) {
		t.Fatalf("scan saw %d, model %d", seen, len(ref))
	}
}
