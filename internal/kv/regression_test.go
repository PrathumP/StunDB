package kv

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"

	"github.com/prathump/stundb/internal/btree"
)

// rounds scales a stress test's repetitions down under -race, where the
// tree serializes every operation behind a lock: the race detector checks
// this package's own synchronization in a few rounds, and the tree's
// lock-free paths only run in ordinary builds anyway.
func rounds(n int) int {
	if btree.RaceSerialized {
		return max(1, n/10)
	}
	return n
}

// countByWalk counts entries by iterating the tree.
func countByWalk(s *Store) int64 {
	var n int64
	s.ForEach(func(_, _ []byte) bool { n++; return true })
	return n
}

// TestAdvModel drives Store with random single-threaded operations and
// checks every result against a map model: Count, Find, GetRange (exact,
// inclusive, ordered), DeleteRange counts, BulkInsert with duplicates, Clear.
func TestAdvModel(t *testing.T) {
	for seed := uint64(1); seed <= 2; seed++ {
		r := rand.New(rand.NewPCG(seed, seed*7))
		s := New()
		model := map[string]string{}
		// keys share long prefixes so inline-key windows and node prefixes matter
		key := func() []byte {
			switch r.IntN(4) {
			case 0:
				return []byte(fmt.Sprintf("user:%06d", r.IntN(30000)))
			case 1:
				return []byte(fmt.Sprintf("user:%06d:x", r.IntN(30000)))
			case 2:
				return []byte(fmt.Sprintf("u%d", r.IntN(300)))
			default:
				b := make([]byte, r.IntN(12))
				for i := range b {
					b[i] = byte(r.IntN(4)) * 0x55
				}
				return b
			}
		}
		check := func(step int, what string) {
			if got, want := s.Count(), int64(len(model)); got != want {
				t.Fatalf("seed %d step %d after %s: Count = %d, model = %d (walk %d)", seed, step, what, got, want, countByWalk(s))
			}
		}
		for step := 0; step < rounds(40000); step++ {
			switch op := r.IntN(100); {
			case op < 45:
				k, v := key(), []byte(fmt.Sprint(r.Int()))
				s.Insert(k, v)
				model[string(k)] = string(v)
				check(step, "Insert")
			case op < 60:
				k := key()
				_, had := model[string(k)]
				if got := s.Delete(k); got != had {
					t.Fatalf("seed %d step %d: Delete(%q) = %v, want %v", seed, step, k, got, had)
				}
				delete(model, string(k))
				check(step, "Delete")
			case op < 72:
				k := key()
				v, err := s.Find(k)
				mv, had := model[string(k)]
				if had != (err == nil) || (had && string(v) != mv) {
					t.Fatalf("seed %d step %d: Find(%q) = %q,%v want %q,%v", seed, step, k, v, err, mv, had)
				}
			case op < 82:
				a, b := key(), key()
				if bytes.Compare(a, b) > 0 {
					a, b = b, a
				}
				ks, vs, err := s.GetRange(a, b)
				if err != nil {
					t.Fatal(err)
				}
				var want []string
				for k := range model {
					if k >= string(a) && k <= string(b) {
						want = append(want, k)
					}
				}
				slices.Sort(want)
				if len(ks) != len(want) {
					t.Fatalf("seed %d step %d: GetRange(%q,%q) len %d want %d", seed, step, a, b, len(ks), len(want))
				}
				for i := range ks {
					if string(ks[i]) != want[i] || string(vs[i]) != model[want[i]] {
						t.Fatalf("seed %d step %d: GetRange[%d] = %q want %q", seed, step, i, ks[i], want[i])
					}
				}
			case op < 85:
				a := key()
				b := append(bytes.Clone(a), 0xff)
				if r.IntN(10) == 0 {
					b = key()
					if bytes.Compare(a, b) > 0 {
						a, b = b, a
					}
				}
				want := 0
				for k := range model {
					if k >= string(a) && k <= string(b) {
						want++
						delete(model, k)
					}
				}
				n, err := s.DeleteRange(a, b)
				if err != nil || n != want {
					t.Fatalf("seed %d step %d: DeleteRange = %d,%v want %d", seed, step, n, err, want)
				}
				check(step, "DeleteRange")
			case op < 99:
				n := r.IntN(80)
				ks := make([][]byte, n)
				vs := make([][]byte, n)
				for i := range ks {
					if i > 0 && r.IntN(4) == 0 {
						ks[i] = ks[r.IntN(i)] // duplicate within the batch
					} else {
						ks[i] = key()
					}
					vs[i] = []byte(fmt.Sprint(r.Int()))
				}
				if err := s.BulkInsert(ks, vs); err != nil {
					t.Fatal(err)
				}
				for i := range ks {
					model[string(ks[i])] = string(vs[i])
				}
				check(step, "BulkInsert")
			default:
				if r.IntN(300) == 0 {
					s.Clear()
					clear(model)
					check(step, "Clear")
				}
			}
		}
		if w := countByWalk(s); w != int64(len(model)) {
			t.Fatalf("seed %d: walk %d model %d", seed, w, len(model))
		}
		t.Logf("seed %d ok: %d keys, height %d", seed, len(model), s.Stats().Height)
	}
}

// TestAdvForEachMutatesStore: the value slice ForEach/Scan hand to the
// callback is the stored one, so writing to it changes what Find returns.
func TestAdvForEachMutatesStore(t *testing.T) {
	s := New()
	s.Insert([]byte("k"), []byte("original"))
	s.ForEach(func(k, v []byte) bool { copy(v, "MUTATED!"); return true })
	got, _ := s.Find([]byte("k"))
	t.Logf("after ForEach wrote to its value: Find = %q", got)
	s.Scan(nil, func(k, v []byte) bool { v[0] = 'Z'; return true })
	got, _ = s.Find([]byte("k"))
	t.Logf("after Scan wrote to its value:    Find = %q", got)
	if string(got) != "original" {
		t.Errorf("stored bytes changed through a callback slice: %q, want %q", got, "original")
	}
}

// TestAdvClearConcurrentCount: writers insert distinct new keys while Clear
// runs. Once everyone is done, Count must equal the number of keys.
func TestAdvClearConcurrentCount(t *testing.T) {
	for round := 0; round < rounds(200); round++ {
		s := New()
		for i := 0; i < 2000; i++ {
			s.Insert([]byte(fmt.Sprintf("pre%05d", i)), []byte("v"))
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < 500; i++ {
					s.Insert([]byte(fmt.Sprintf("g%d-%05d", g, i)), []byte("v"))
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s.Clear()
		}()
		close(start)
		wg.Wait()
		if c, w := s.Count(), countByWalk(s); c != w {
			t.Fatalf("round %d: Count() = %d, keys in store = %d", round, c, w)
		}
	}
}

// TestAdvClearConcurrentDeleteCount: Deletes of existing keys race a Clear.
// Afterwards the store is empty, so Count must be 0.
func TestAdvClearConcurrentDeleteCount(t *testing.T) {
	for round := 0; round < rounds(500); round++ {
		s := New()
		for i := 0; i < 4000; i++ {
			s.Insert([]byte(fmt.Sprintf("pre%05d", i)), []byte("v"))
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := g; i < 4000; i += 4 {
					s.Delete([]byte(fmt.Sprintf("pre%05d", i)))
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s.Clear()
		}()
		close(start)
		wg.Wait()
		if c, w := s.Count(), countByWalk(s); c != w {
			t.Fatalf("round %d: Count() = %d, keys in store = %d", round, c, w)
		}
	}
}

// TestAdvConcurrentCount: concurrent Insert/Delete/BulkInsert/DeleteRange on a
// small key space; once they finish, Count must equal the number of keys.
func TestAdvConcurrentCount(t *testing.T) {
	for round := 0; round < rounds(20); round++ {
		s := New()
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := rand.New(rand.NewPCG(uint64(g), uint64(round)))
				for i := 0; i < 20000; i++ {
					k := []byte(fmt.Sprintf("key%04d", r.IntN(3000)))
					switch r.IntN(10) {
					case 0, 1, 2, 3:
						s.Insert(k, []byte("v"))
					case 4, 5, 6:
						s.Delete(k)
					case 7, 8:
						ks := [][]byte{k, []byte(fmt.Sprintf("key%04d", r.IntN(3000))), k}
						s.BulkInsert(ks, [][]byte{[]byte("a"), []byte("b"), []byte("c")})
					default:
						e := []byte(fmt.Sprintf("key%04d", r.IntN(3000)))
						if bytes.Compare(k, e) > 0 {
							k, e = e, k
						}
						s.DeleteRange(k, e)
					}
				}
			}()
		}
		wg.Wait()
		if c, w := s.Count(), countByWalk(s); c != w {
			t.Fatalf("round %d: Count() = %d, keys in store = %d", round, c, w)
		}
	}
}

// TestAdvConcurrentRanges: stable keys are never written; writers insert and
// delete other keys (forcing splits) and BulkInsert. Readers' GetRange must be
// strictly ascending, inside [a, b], include every stable key in range with
// its value, and Find must always see stable keys.
func TestAdvConcurrentRanges(t *testing.T) {
	prefix := "tenant/0000000000000000000000000000/orders/"
	mk := func(i int, suffix string) []byte { return []byte(fmt.Sprintf("%s%07d%s", prefix, i, suffix)) }
	const nStable = 20000
	s := New()
	for i := 0; i < nStable; i++ {
		s.Insert(mk(i*2, ""), mk(i*2, "=v"))
	}
	stable := func(k []byte) (int, bool) {
		var i int
		var rest string
		if !bytes.HasPrefix(k, []byte(prefix)) {
			return 0, false
		}
		n, _ := fmt.Sscanf(string(k[len(prefix):]), "%07d%s", &i, &rest)
		return i, n == 1 && i%2 == 0
	}
	var stop sync.WaitGroup
	done := make(chan struct{})
	for g := 0; g < 4; g++ {
		stop.Add(1)
		go func() {
			defer stop.Done()
			r := rand.New(rand.NewPCG(uint64(g), 99))
			for {
				select {
				case <-done:
					return
				default:
				}
				i := r.IntN(nStable*2) | 1
				sfx := []string{"", "/a", "/aa", "/ab/long/suffix/xxxxxxxxxxxxxxxx"}[r.IntN(4)]
				switch r.IntN(3) {
				case 0:
					s.Insert(mk(i, sfx), []byte("x"))
				case 1:
					s.Delete(mk(i, sfx))
				default:
					ks := make([][]byte, 30)
					vs := make([][]byte, 30)
					for j := range ks {
						ks[j], vs[j] = mk((i+2*j)%(nStable*2)|1, sfx), []byte("y")
					}
					s.BulkInsert(ks, vs)
				}
			}
		}()
	}
	var failures []string
	var mu sync.Mutex
	var rd sync.WaitGroup
	for g := 0; g < 4; g++ {
		rd.Add(1)
		go func() {
			defer rd.Done()
			r := rand.New(rand.NewPCG(uint64(g), 7))
			for iter := 0; iter < 3000; iter++ {
				a := r.IntN(nStable * 2)
				b := a + r.IntN(400)
				lo, hi := mk(a, ""), mk(b, "/zz")
				ks, vs, err := s.GetRange(lo, hi)
				fail := func(f string, args ...any) {
					mu.Lock()
					if len(failures) < 10 {
						failures = append(failures, fmt.Sprintf(f, args...))
					}
					mu.Unlock()
				}
				if err != nil {
					fail("GetRange err %v", err)
					continue
				}
				seen := 0
				for j := range ks {
					if bytes.Compare(ks[j], lo) < 0 || bytes.Compare(ks[j], hi) > 0 {
						fail("key %q outside [%q,%q]", ks[j], lo, hi)
					}
					if j > 0 && bytes.Compare(ks[j-1], ks[j]) >= 0 {
						fail("out of order: %q then %q", ks[j-1], ks[j])
					}
					if _, ok := stable(ks[j]); ok {
						seen++
						if !bytes.Equal(vs[j], append(bytes.Clone(ks[j]), "=v"...)) {
							fail("stable key %q has value %q", ks[j], vs[j])
						}
					}
				}
				want := 0
				for i := a; i <= b && i < nStable*2; i++ {
					if i%2 == 0 {
						want++
					}
				}
				if seen != want {
					fail("range [%d,%d]: saw %d stable keys, want %d", a, b, seen, want)
				}
				k := mk(r.IntN(nStable)*2, "")
				if v, err := s.Find(k); err != nil || !bytes.Equal(v, append(bytes.Clone(k), "=v"...)) {
					fail("Find(%q) = %q, %v", k, v, err)
				}
			}
		}()
	}
	rd.Wait()
	close(done)
	stop.Wait()
	for _, f := range failures {
		t.Error(f)
	}
	if c, w := s.Count(), countByWalk(s); c != w {
		t.Errorf("Count %d walk %d", c, w)
	}
}

// TestAdvConcurrentCountTall: like TestAdvConcurrentCount, but on a tree of
// height >= 3 so that BulkInsert takes the batched InsertMany path, with
// batches full of duplicates racing Deletes of the same keys.
func TestAdvConcurrentCountTall(t *testing.T) {
	const space = 40000
	for round := 0; round < rounds(6); round++ {
		s := New()
		for i := 0; i < space; i += 2 {
			s.Insert([]byte(fmt.Sprintf("key%06d", i)), []byte("v"))
		}
		if h := s.Stats().Height; h < 3 {
			t.Fatalf("height %d", h)
		}
		var wg sync.WaitGroup
		for g := 0; g < 8; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := rand.New(rand.NewPCG(uint64(g), uint64(round)))
				for i := 0; i < 20000; i++ {
					base := r.IntN(space)
					switch r.IntN(4) {
					case 0, 1:
						n := 1 + r.IntN(48)
						ks := make([][]byte, n)
						vs := make([][]byte, n)
						for j := range ks {
							ks[j] = []byte(fmt.Sprintf("key%06d", (base+r.IntN(40))%space))
							vs[j] = []byte("b")
						}
						s.BulkInsert(ks, vs)
					case 2:
						for j := 0; j < 8; j++ {
							s.Delete([]byte(fmt.Sprintf("key%06d", (base+r.IntN(40))%space)))
						}
					default:
						s.Insert([]byte(fmt.Sprintf("key%06d", base)), []byte("i"))
					}
				}
			}()
		}
		wg.Wait()
		if c, w := s.Count(), countByWalk(s); c != w {
			t.Fatalf("round %d: Count() = %d, keys in store = %d", round, c, w)
		}
	}
}
