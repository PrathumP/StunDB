//go:build !race

package btree

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// The tree's concurrency runs on optimistic reads, which the race detector
// reports by design (race_on.go). These stress tests run it in ordinary
// builds: every reader checks what it sees against what the writers have
// published, and the tree's invariants are checked at the end.

func workers() int { return max(4, runtime.GOMAXPROCS(0)) }

// valueFor ties a value to its key and a generation, so a reader can tell a
// torn or misplaced value from a real one.
func valueFor(k uint64, gen uint64) uint64 { return mix64(k)&^0xffff | gen&0xffff }
func valueOK(k, v uint64) bool             { return v&^0xffff == mix64(k)&^0xffff }

// TestConcurrentInsertGet: writers insert disjoint keys and publish how far
// they've got; readers look up published keys, single and batched, and must
// find each with its own value.
func TestConcurrentInsertGet(t *testing.T) {
	for _, dense := range []bool{false, true} {
		t.Run(fmt.Sprintf("dense=%v", dense), func(t *testing.T) {
			m := New[uint64, uint64]()
			w := workers() / 2
			const perWriter = 150_000
			key := func(writer, i int) uint64 {
				if dense {
					return uint64(i*w + writer)
				}
				return mix64(uint64(i*w + writer))
			}
			progress := make([]atomic.Int64, w)
			var done atomic.Bool
			var wg sync.WaitGroup
			for wr := range w {
				wg.Go(func() {
					for i := range perWriter {
						k := key(wr, i)
						m.Insert(k, valueFor(k, 0))
						progress[wr].Store(int64(i + 1))
					}
				})
			}
			errs := make(chan error, 64)
			var rg sync.WaitGroup
			for rd := range w {
				rg.Go(func() {
					r := rand.New(rand.NewPCG(uint64(rd), 1))
					keys := make([]uint64, 32)
					vals := make([]uint64, 32)
					found := make([]bool, 32)
					for !done.Load() {
						wr := r.IntN(w)
						p := int(progress[wr].Load())
						if p == 0 {
							continue
						}
						for j := range keys {
							keys[j] = key(wr, r.IntN(p))
						}
						m.GetMany(keys, vals, found)
						for j, k := range keys {
							if !found[j] || !valueOK(k, vals[j]) {
								errs <- fmt.Errorf("GetMany: published key %x: %x,%v", k, vals[j], found[j])
								return
							}
							if v, ok := m.Get(k); !ok || !valueOK(k, v) {
								errs <- fmt.Errorf("Get: published key %x: %x,%v", k, v, ok)
								return
							}
						}
					}
				})
			}
			wg.Wait()
			done.Store(true)
			rg.Wait()
			close(errs)
			for err := range errs {
				t.Fatal(err)
			}
			if n := checkMap(t, m); n != w*perWriter {
				t.Fatalf("holds %d, want %d", n, w*perWriter)
			}
		})
	}
}

// TestConcurrentMixed: each worker inserts, removes, updates and batch-inserts
// in its own interleaved key space against a model of its own, while others do
// the same on neighboring keys of the same leaves.
func TestConcurrentMixed(t *testing.T) {
	m := New[uint64, uint64]()
	w := workers()
	const ops = 200_000
	models := make([]map[uint64]uint64, w)
	var wg sync.WaitGroup
	errs := make(chan error, w)
	for id := range w {
		models[id] = map[uint64]uint64{}
		wg.Go(func() {
			ref := models[id]
			r := rand.New(rand.NewPCG(uint64(id), 2))
			key := func() uint64 { return r.Uint64N(20_000)*uint64(w) + uint64(id) }
			bk := make([]uint64, 8)
			bv := make([]uint64, 8)
			for i := range ops {
				k := key()
				switch op := r.IntN(10); {
				case op < 4:
					v := valueFor(k, uint64(i))
					old, replaced := m.Insert(k, v)
					if want, had := ref[k]; replaced != had || old != want {
						errs <- fmt.Errorf("worker %d: Insert(%d) = %x,%v; model %x,%v", id, k, old, replaced, want, had)
						return
					}
					ref[k] = v
				case op < 6:
					old, found := m.Remove(k)
					if want, had := ref[k]; found != had || old != want {
						errs <- fmt.Errorf("worker %d: Remove(%d) = %x,%v; model %x,%v", id, k, old, found, want, had)
						return
					}
					delete(ref, k)
				case op < 7:
					ok := m.Update(k, func(v *uint64) { *v = valueFor(k, *v+1) })
					if _, had := ref[k]; ok != had {
						errs <- fmt.Errorf("worker %d: Update(%d) = %v", id, k, ok)
						return
					} else if had {
						ref[k] = valueFor(k, ref[k]+1)
					}
				case op < 8:
					for j := range bk {
						bk[j] = key()
						bv[j] = valueFor(bk[j], uint64(i))
					}
					m.InsertMany(bk, bv)
					for j, k := range bk {
						ref[k] = bv[j]
					}
				default:
					v, found := m.Get(k)
					if want, had := ref[k]; found != had || v != want {
						errs <- fmt.Errorf("worker %d: Get(%d) = %x,%v; model %x,%v", id, k, v, found, want, had)
						return
					}
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	total := 0
	for id, ref := range models {
		total += len(ref)
		for k, v := range ref {
			if got, ok := m.Get(k); !ok || got != v {
				t.Fatalf("worker %d's key %d: %x,%v; model %x", id, k, got, ok, v)
			}
		}
	}
	if n := checkMap(t, m); n != total {
		t.Fatalf("holds %d, models %d", n, total)
	}
}

// TestConcurrentScans: steady keys are in the map throughout; other threads
// insert and remove keys between them. Every scan must visit each steady key
// exactly once, in order.
func TestConcurrentScans(t *testing.T) {
	for _, kind := range []string{"u64", "string"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "u64" {
				runConcurrentScans(t, func(i uint64) uint64 { return i })
			} else {
				runConcurrentScans(t, func(i uint64) string { return fmt.Sprintf("row/%012d", i) })
			}
		})
	}
}

func runConcurrentScans[K Key](t *testing.T, key func(uint64) K) {
	m := New[K, uint64]()
	const steady = 40_000
	for i := range uint64(steady) {
		m.Insert(key(i*4), i*4) // steady keys: multiples of 4
	}
	var done atomic.Bool
	var wg sync.WaitGroup
	for id := range workers() / 2 {
		wg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(id), 3))
			for !done.Load() {
				k := r.Uint64N(steady*4)*4 + 1 + r.Uint64N(3) // never a multiple of 4
				if r.IntN(2) == 0 {
					m.Insert(key(k), k)
				} else {
					m.Remove(key(k))
				}
			}
		})
	}
	errs := make(chan error, 16)
	var sg sync.WaitGroup
	for range workers() / 2 {
		sg.Go(func() {
			for range 30 {
				next := uint64(0)
				var prev K
				first := true
				m.All(func(k K, v uint64) bool {
					if !first && !keyLess(prev, k) {
						errs <- fmt.Errorf("scan out of order: %v then %v", prev, k)
						return false
					}
					prev, first = k, false
					if v%4 != 0 {
						return true
					}
					if v != next*4 || k != key(v) {
						errs <- fmt.Errorf("scan expected steady key %d, got %v=%d", next*4, k, v)
						return false
					}
					next++
					return true
				})
				if next != steady {
					errs <- fmt.Errorf("scan saw %d steady keys of %d", next, steady)
				}
			}
		})
	}
	sg.Wait()
	done.Store(true)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	checkMap(t, m)
}

// TestConcurrentStrings races inserts and lookups of string keys that tie on
// their inline keys, so lookups compare whole keys whose headers writers are
// moving.
func TestConcurrentStrings(t *testing.T) {
	m := New[string, uint64]()
	key := func(i int) string { return fmt.Sprintf("shared/prefix/%07d/tail", i) }
	w := workers() / 2
	const per = 60_000
	progress := make([]atomic.Int64, w)
	var done atomic.Bool
	var wg, rg sync.WaitGroup
	for wr := range w {
		wg.Go(func() {
			for i := range per {
				m.Insert(key(i*w+wr), uint64(i*w+wr))
				progress[wr].Store(int64(i + 1))
			}
		})
	}
	errs := make(chan error, 16)
	for rd := range w {
		rg.Go(func() {
			r := rand.New(rand.NewPCG(uint64(rd), 4))
			for !done.Load() {
				wr := r.IntN(w)
				p := int(progress[wr].Load())
				if p == 0 {
					continue
				}
				i := r.IntN(p)*w + wr
				if v, ok := m.Get(key(i)); !ok || v != uint64(i) {
					errs <- fmt.Errorf("Get(%q) = %d,%v", key(i), v, ok)
					return
				}
			}
		})
	}
	wg.Wait()
	done.Store(true)
	rg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n := checkMap(t, m); n != w*per {
		t.Fatalf("holds %d", n)
	}
}
