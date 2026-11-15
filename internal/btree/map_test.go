package btree

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// keyGen makes keys of one shape for the differential tests.
type keyGen[K Key] struct {
	name string
	make func(r *rand.Rand, i int) K
}

func u64Gens() []keyGen[uint64] {
	return []keyGen[uint64]{
		{"random", func(r *rand.Rand, _ int) uint64 { return r.Uint64() }},
		{"small-space", func(r *rand.Rand, _ int) uint64 { return r.Uint64N(3000) }},
		{"dense", func(r *rand.Rand, i int) uint64 { return uint64(i) }},
		{"strided", func(r *rand.Rand, i int) uint64 { return uint64(i) << 24 }},
		{"extremes", func(r *rand.Rand, i int) uint64 {
			if i%2 == 0 {
				return r.Uint64N(500)
			}
			return ^uint64(0) - r.Uint64N(500)
		}},
	}
}

func uuidGens() []keyGen[[16]byte] {
	return []keyGen[[16]byte]{
		{"random", func(r *rand.Rand, _ int) (k [16]byte) {
			binary.BigEndian.PutUint64(k[:8], r.Uint64())
			binary.BigEndian.PutUint64(k[8:], r.Uint64())
			return k
		}},
		{"shared-high-half", func(r *rand.Rand, _ int) (k [16]byte) {
			binary.BigEndian.PutUint64(k[:8], 0xdeadbeef)
			binary.BigEndian.PutUint64(k[8:], r.Uint64N(5000))
			return k
		}},
	}
}

const alphabet = "abcdefghijklmnopqrstuvwxyz"

func randText(r *rand.Rand, n int, chars string) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[r.IntN(len(chars))]
	}
	return string(b)
}

func strGens() []keyGen[string] {
	return []keyGen[string]{
		{"short", func(r *rand.Rand, _ int) string { return randText(r, r.IntN(12), alphabet) }},
		{"tiny-alphabet", func(r *rand.Rand, _ int) string { return randText(r, r.IntN(10), "ab") }},
		{"urls", func(r *rand.Rand, _ int) string {
			return "https://example.com/" + randText(r, 1+r.IntN(3), "xyz") + "/" + randText(r, r.IntN(20), alphabet)
		}},
		{"long-prefix", func(r *rand.Rand, _ int) string {
			return strings.Repeat("p", 300) + randText(r, r.IntN(8), alphabet)
		}},
		{"binary", func(r *rand.Rand, _ int) string {
			return randText(r, r.IntN(20), "\x00\x01\xff\x7f\x80")
		}},
		{"numbered", func(r *rand.Rand, i int) string { return fmt.Sprintf("user:%09d", i) }},
	}
}

// model is the reference the map is checked against.
type model[K Key] map[K]uint64

func (m model[K]) sorted() []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b K) int { return keyCompare(a, b, 0) })
	return keys
}

func runDifferential[K Key](t *testing.T, gen keyGen[K], ops int, seed uint64) {
	r := rand.New(rand.NewPCG(seed, 7))
	m := New[K, uint64]()
	ref := model[K]{}
	var pool []K // keys seen, to hit present ones
	pick := func(i int) K {
		if len(pool) > 0 && r.IntN(3) == 0 {
			return pool[r.IntN(len(pool))]
		}
		k := gen.make(r, i)
		pool = append(pool, k)
		return k
	}
	for i := range ops {
		k := pick(i)
		switch op := r.IntN(10); {
		case op < 6:
			v := r.Uint64()
			old, replaced := m.Insert(k, v)
			refOld, refHad := ref[k]
			if replaced != refHad || old != refOld {
				t.Fatalf("op %d: Insert(%v) = %v,%v; want %v,%v", i, k, old, replaced, refOld, refHad)
			}
			ref[k] = v
		case op < 8:
			old, found := m.Remove(k)
			refOld, refHad := ref[k]
			if found != refHad || old != refOld {
				t.Fatalf("op %d: Remove(%v) = %v,%v; want %v,%v", i, k, old, found, refOld, refHad)
			}
			delete(ref, k)
		case op < 9:
			ok := m.Update(k, func(v *uint64) { *v++ })
			if _, had := ref[k]; ok != had {
				t.Fatalf("op %d: Update(%v) = %v", i, k, ok)
			} else if had {
				ref[k]++
			}
		default:
			v, found := m.Get(k)
			refV, refHad := ref[k]
			if found != refHad || v != refV {
				t.Fatalf("op %d: Get(%v) = %v,%v; want %v,%v", i, k, v, found, refV, refHad)
			}
		}
	}
	if n := checkMap(t, m); n != len(ref) {
		t.Fatalf("tree holds %d entries, model %d", n, len(ref))
	}
	compareContents(t, m, ref)
}

func compareContents[K Key](t *testing.T, m *Map[K, uint64], ref model[K]) {
	t.Helper()
	want := ref.sorted()
	var got []K
	m.All(func(k K, v uint64) bool {
		if ref[k] != v {
			t.Fatalf("All: %v = %v, model %v", k, v, ref[k])
		}
		got = append(got, k)
		return true
	})
	if !slices.Equal(got, want) {
		t.Fatalf("All visited %d keys, model holds %d (first mismatch near %v)", len(got), len(want), firstDiff(got, want))
	}
	for _, k := range want {
		if v, ok := m.Get(k); !ok || v != ref[k] {
			t.Fatalf("Get(%v) = %v,%v; want %v", k, v, ok, ref[k])
		}
	}
	// Batched lookups agree with single ones, hits and misses alike.
	probe := append(slices.Clone(want), want...)
	vals := make([]uint64, len(probe))
	found := make([]bool, len(probe))
	m.GetMany(probe, vals, found)
	for i, k := range probe {
		if want, had := ref[k]; found[i] != had || vals[i] != want {
			t.Fatalf("GetMany[%d] (%v) = %v,%v; want %v,%v", i, k, vals[i], found[i], want, had)
		}
	}
	// Scans from every tenth key, inclusive, in order.
	for i := 0; i < len(want); i += max(1, len(want)/10) {
		j := i
		m.Ascend(want[i], func(k K, _ uint64) bool {
			if j >= len(want) || k != want[j] {
				t.Fatalf("Ascend(%v) step %d: got %v", want[i], j-i, k)
			}
			j++
			return j-i < 200
		})
	}
}

func firstDiff[K comparable](a, b []K) any {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return fmt.Sprintf("index %d: %v vs %v", i, a[i], b[i])
		}
	}
	return "the shorter one's end"
}

func TestDifferentialU64(t *testing.T) {
	for _, g := range u64Gens() {
		t.Run(g.name, func(t *testing.T) { runDifferential(t, g, 60_000, 1) })
	}
}

func TestDifferentialUUID(t *testing.T) {
	for _, g := range uuidGens() {
		t.Run(g.name, func(t *testing.T) { runDifferential(t, g, 60_000, 2) })
	}
}

func TestDifferentialString(t *testing.T) {
	for _, g := range strGens() {
		t.Run(g.name, func(t *testing.T) { runDifferential(t, g, 40_000, 3) })
	}
}

// TestGrowth builds trees of three and four levels in each insertion order,
// checking every invariant, prefixes included.
func TestGrowth(t *testing.T) {
	orders := map[string]func(i int) uint64{
		"ascending":  func(i int) uint64 { return uint64(i) },
		"descending": func(i int) uint64 { return uint64(1)<<40 - uint64(i) },
		"random":     func(i int) uint64 { return mix64(uint64(i)) },
	}
	for name, key := range orders {
		t.Run(name, func(t *testing.T) {
			m := New[uint64, uint64]()
			const n = 300_000
			for i := range n {
				m.Insert(key(i), uint64(i))
			}
			if got := checkMap(t, m); got != n {
				t.Fatalf("holds %d, want %d", got, n)
			}
			if m.Height() < 3 {
				t.Fatalf("height %d", m.Height())
			}
			for i := range n {
				if v, ok := m.Get(key(i)); !ok || v != uint64(i) {
					t.Fatalf("Get(%d) = %d,%v", key(i), v, ok)
				}
			}
		})
	}
}

func TestInsertMany(t *testing.T) {
	m := New[uint64, uint64]()
	ref := model[uint64]{}
	r := rand.New(rand.NewPCG(9, 9))
	keys := make([]uint64, 37)
	vals := make([]uint64, 37)
	for round := range 8000 {
		for i := range keys {
			if round%5 == 0 {
				keys[i] = uint64(round*37 + i) // ascending runs
			} else {
				keys[i] = r.Uint64N(400_000) // repeats within and across batches
			}
			vals[i] = r.Uint64()
		}
		added := m.InsertMany(keys, vals)
		want := 0
		for i, k := range keys {
			if _, had := ref[k]; !had {
				want++
			}
			ref[k] = vals[i]
		}
		if added != want {
			t.Fatalf("round %d: InsertMany added %d, want %d", round, added, want)
		}
	}
	if n := checkMap(t, m); n != len(ref) {
		t.Fatalf("holds %d, model %d", n, len(ref))
	}
	compareContents(t, m, ref)
}

func TestInsertManyStrings(t *testing.T) {
	m := New[string, uint64]()
	ref := model[string]{}
	r := rand.New(rand.NewPCG(4, 4))
	keys := make([]string, 20)
	vals := make([]uint64, 20)
	for range 5000 {
		for i := range keys {
			keys[i] = "k/" + randText(r, 1+r.IntN(10), alphabet[:6])
			vals[i] = r.Uint64()
		}
		m.InsertMany(keys, vals)
		for i, k := range keys {
			ref[k] = vals[i]
		}
	}
	checkMap(t, m)
	compareContents(t, m, ref)
}

func TestRangeAndLen(t *testing.T) {
	m := New[string, []byte]()
	for i := range 5000 {
		m.Insert(fmt.Sprintf("key:%05d", i), []byte{byte(i)})
	}
	var got []string
	m.Range("key:01000", "key:01010", func(k string, _ []byte) bool {
		got = append(got, k)
		return true
	})
	if len(got) != 10 || got[0] != "key:01000" || got[9] != "key:01009" {
		t.Fatalf("Range = %v", got)
	}
	if m.Len() != 5000 {
		t.Fatalf("Len = %d", m.Len())
	}
	if v, ok := m.Get(BytesKey([]byte("key:00042"))); !ok || v[0] != 42 {
		t.Fatalf("Get by bytes = %v,%v", v, ok)
	}
	m.Clear()
	if m.Len() != 0 || m.Contains("key:00042") {
		t.Fatal("Clear left entries")
	}
}

// TestCountSearch checks the SIMD counts against the scalar ones on random
// sorted arrays, every target position, and padding at every count.
func TestCountSearch(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 5))
	for range 2000 {
		n := r.IntN(slots)
		var k16 [slots]uint16
		var k32 [slots]uint32
		w := make([]uint64, n)
		for i := range w {
			w[i] = r.Uint64()
			if r.IntN(4) == 0 && i > 0 {
				w[i] = w[i-1] // ties
			}
		}
		slices.Sort(w)
		for i, x := range w {
			k16[i], k32[i] = toInline[uint16](x), toInline[uint32](x)
		}
		for i := n; i < slots; i++ {
			k16[i], k32[i] = padInline[uint16](), padInline[uint32]()
		}
		for range 20 {
			x := r.Uint64()
			if n > 0 && r.IntN(2) == 0 {
				x = w[r.IntN(n)]
			}
			t16, t32 := toInline[uint16](x), toInline[uint32](x)
			if a, b := countLess(&k16, t16), count16Scalar(&k16, t16); a != b {
				t.Fatalf("count16 = %d, scalar %d", a, b)
			}
			if a, b := count16Branchless(&k16, t16), count16Scalar(&k16, t16); a != b {
				t.Fatalf("count16Branchless = %d, scalar %d", a, b)
			}
			if a, b := count32Branchless(&k32, t32), count32Scalar(&k32, t32); a != b {
				t.Fatalf("count32Branchless = %d, scalar %d", a, b)
			}
			if a, b := countLess(&k32, t32), count32Scalar(&k32, t32); a != b {
				t.Fatalf("count32 = %d, scalar %d", a, b)
			}
			want := 0
			for _, y := range w {
				if toInline[uint16](y)^0x8000 < t16^0x8000 {
					want++
				}
			}
			if got := count16Scalar(&k16, t16); got != want {
				t.Fatalf("count16Scalar = %d, want %d", got, want)
			}
		}
	}
}

func TestKeyEncoding(t *testing.T) {
	r := rand.New(rand.NewPCG(6, 6))
	strs := []string{"", "a", "ab", "ab\x00", "abcdefgh", "abcdefghi", "abcdefgh\x00", "\xff\xff"}
	for range 300 {
		strs = append(strs, randText(r, r.IntN(24), "ab\x00\xff"))
	}
	for _, a := range strs {
		for _, b := range strs {
			if got, want := keyCompare(a, b, 0), cmp.Compare(a, b); got != want {
				t.Fatalf("keyCompare(%q, %q) = %d, want %d", a, b, got, want)
			}
			p := keyCPL(a, b)
			if p > min(len(a), len(b)) || a[:p] != b[:p] || (p < min(len(a), len(b)) && a[p] == b[p]) {
				t.Fatalf("keyCPL(%q, %q) = %d", a, b, p)
			}
			for off := 0; off <= p; off++ {
				wa, wb := keyWindow(a, off), keyWindow(b, off)
				if wa != wb && (wa < wb) != (a < b) {
					t.Fatalf("windows at %d disorder %q, %q", off, a, b)
				}
				if got, want := keyCompare(a, b, off), cmp.Compare(a, b); got != want {
					t.Fatalf("keyCompare(%q, %q, %d) = %d", a, b, off, got)
				}
			}
		}
	}
	for range 10000 {
		a, b := r.Uint64(), r.Uint64()
		if r.IntN(2) == 0 {
			b = a ^ (1 << r.IntN(64))
		}
		var ua, ub [16]byte
		binary.BigEndian.PutUint64(ua[:8], a)
		binary.BigEndian.PutUint64(ub[:8], a)
		binary.BigEndian.PutUint64(ua[8:], b)
		binary.BigEndian.PutUint64(ub[8:], a^b)
		for off := 0; off <= keyCPL(a, b); off++ {
			wa, wb := keyWindow(a, off), keyWindow(b, off)
			if wa != wb && (wa < wb) != (a < b) {
				t.Fatalf("u64 windows at %d disorder %x, %x", off, a, b)
			}
		}
		if (keyCompare(ua, ub, 0) < 0) != (string(ua[:]) < string(ub[:])) {
			t.Fatalf("uuid order")
		}
		for off := 0; off <= keyCPL(ua, ub); off++ {
			wa, wb := keyWindow(ua, off), keyWindow(ub, off)
			if wa != wb && (wa < wb) != (string(ua[:]) < string(ub[:])) {
				t.Fatalf("uuid windows at %d disorder", off)
			}
			if wa != strWindow(string(ua[:]), off) {
				t.Fatalf("uuid window at %d differs from its bytes'", off)
			}
		}
	}
}

func mix64(x uint64) uint64 {
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}
