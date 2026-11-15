package btree

import (
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"testing"
)

// Microbenchmarks at the blog's scale: 10M random uint64 keys and values
// (STUNDB_BENCH_N overrides it). Lookups are half hits, half misses, as in the
// blog's Get cells. The full comparison against other maps is the bench
// module (bench/).

func benchN() int {
	if s := os.Getenv("STUNDB_BENCH_N"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 10_000_000
}

type u64Fixture struct {
	m     *Map[uint64, uint64]
	probe []uint64 // half present, half absent, shuffled
}

var (
	fixtureOnce sync.Once
	fixture     u64Fixture
)

func u64Bench() *u64Fixture {
	fixtureOnce.Do(func() {
		n := benchN()
		m := New[uint64, uint64]()
		for i := range n {
			k := mix64(uint64(i))
			m.Insert(k, k)
		}
		r := rand.New(rand.NewPCG(1, 2))
		probe := make([]uint64, 1<<22)
		for i := range probe {
			if i%2 == 0 {
				probe[i] = mix64(uint64(r.IntN(n)))
			} else {
				probe[i] = mix64(uint64(n + r.IntN(n)))
			}
		}
		r.Shuffle(len(probe), func(i, j int) { probe[i], probe[j] = probe[j], probe[i] })
		fixture = u64Fixture{m, probe}
	})
	return &fixture
}

func BenchmarkGetU64(b *testing.B) {
	f := u64Bench()
	mask := len(f.probe) - 1
	i := 0
	var sink uint64
	for b.Loop() {
		v, _ := f.m.Get(f.probe[i&mask])
		sink += v
		i++
	}
	_ = sink
}

func BenchmarkGetManyU64(b *testing.B) {
	f := u64Bench()
	const batch = 64
	vals := make([]uint64, batch)
	found := make([]bool, batch)
	i := 0
	for b.Loop() {
		f.m.GetMany(f.probe[i:i+batch], vals, found)
		i += batch
		if i+batch > len(f.probe) {
			i = 0
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*batch), "ns/key")
}

func BenchmarkScan100U64(b *testing.B) {
	f := u64Bench()
	mask := len(f.probe) - 1
	i := 0
	for b.Loop() {
		c := 0
		f.m.Ascend(f.probe[i&mask], func(_, _ uint64) bool { c++; return c < 100 })
		i += 2
	}
}

// BenchmarkInsertU64 inserts fresh keys into a map that starts with
// STUNDB_BENCH_N entries, so the result doesn't depend on b.N the way growing
// a map from empty does. The map grows by b.N while it runs.
func BenchmarkInsertU64(b *testing.B) {
	n := benchN()
	m := New[uint64, uint64]()
	for i := range n {
		k := mix64(uint64(i))
		m.Insert(k, k)
	}
	i := uint64(n)
	for b.Loop() {
		k := mix64(i)
		m.Insert(k, k)
		i++
	}
}

func BenchmarkGetU64Parallel(b *testing.B) {
	f := u64Bench()
	mask := len(f.probe) - 1
	var seed sync.Mutex
	s := 0
	b.RunParallel(func(pb *testing.PB) {
		seed.Lock()
		i := s * 7919
		s++
		seed.Unlock()
		var sink uint64
		for pb.Next() {
			v, _ := f.m.Get(f.probe[i&mask])
			sink += v
			i++
		}
		_ = sink
	})
}

// ---- the pieces ---------------------------------------------------------------

var sinkI int

func benchCount(b *testing.B, count func(*[slots]uint16, uint16) int) {
	var ik [slots]uint16
	for i := range ik {
		ik[i] = uint16(i*1000) ^ 0x8000
	}
	t := uint16(0)
	for b.Loop() {
		sinkI += count(&ik, t)
		t += 977
	}
}

// The node search's count, as the tree runs it (AVX2 with
// GOEXPERIMENT=simd), and its portable forms.
func BenchmarkCount16(b *testing.B)           { benchCount(b, countLess[uint16]) }
func BenchmarkCount16Branchless(b *testing.B) { benchCount(b, count16Branchless) }
func BenchmarkCount16Scalar(b *testing.B)     { benchCount(b, count16Scalar) }

// BenchmarkGetCached: lookups in a three-level tree that fits in L2, where
// instructions, not misses, set the time.
func BenchmarkGetCached(b *testing.B) {
	m := New[uint64, uint64]()
	for i := range 3000 {
		k := mix64(uint64(i))
		m.Insert(k, k)
	}
	i := 0
	for b.Loop() {
		v, _ := m.Get(mix64(uint64(i % 3000)))
		sinkI += int(v)
		i++
	}
}
