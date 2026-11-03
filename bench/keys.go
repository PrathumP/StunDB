package main

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"
)

// keySet is a workload's keys: n to put in the map, and others, never in it,
// that lookups which miss probe with.
type keySet[K comparable] struct {
	name    string
	present []K
	absent  []K
}

func mix64(x uint64) uint64 {
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// u64Keys: random uint64s (hashed IDs, cache keys), bbtree's U64Random.
func u64Keys(n, holdout int) keySet[uint64] {
	all := make([]uint64, n+holdout)
	for i := range all {
		all[i] = mix64(uint64(i))
	}
	return keySet[uint64]{"u64", all[:n], all[n:]}
}

// uuidKeys: random version-4 UUIDs, as [16]byte.
func uuidKeys(n, holdout int) keySet[[16]byte] {
	r := rand.New(rand.NewPCG(16, 16))
	all := make([][16]byte, n+holdout)
	for i := range all {
		binary.BigEndian.PutUint64(all[i][:8], r.Uint64())
		binary.BigEndian.PutUint64(all[i][8:], r.Uint64())
		all[i][6] = all[i][6]&0x0f | 0x40 // version 4
		all[i][8] = all[i][8]&0x3f | 0x80 // variant 10
	}
	return keySet[[16]byte]{"uuid", all[:n], all[n:]}
}

// jsonKeys: distinct strings as long as JSON object keys (median 7 bytes, p90
// 14), from bbtree's bench (keys.rs, JsonKeyShape): lengths drawn from its
// measured histogram over [A-Za-z0-9_]; a repeat grows by a character until it
// is new.
func jsonKeys(n, holdout int) keySet[string] {
	hist := [][2]int{
		{1, 80}, {2, 1440}, {3, 2628}, {4, 8867}, {5, 6693}, {6, 13055}, {7, 3573},
		{8, 12255}, {9, 2747}, {10, 2597}, {11, 4126}, {12, 2288}, {13, 1785},
		{14, 2797}, {15, 1039}, {16, 638}, {17, 607}, {18, 584}, {19, 449},
		{20, 242}, {21, 1254}, {22, 175}, {23, 739}, {24, 207}, {25, 173},
		{26, 173}, {27, 1}, {28, 520}, {34, 173},
	}
	total := 0
	for _, h := range hist {
		total += h[1]
	}
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"
	r := rand.New(rand.NewPCG(7, 7))
	text := func(sb *strings.Builder, n int) {
		for range n {
			sb.WriteByte(chars[r.IntN(len(chars))])
		}
	}
	seen := make(map[string]struct{}, n+holdout)
	all := make([]string, 0, n+holdout)
	var sb strings.Builder
	for len(all) < n+holdout {
		w := r.IntN(total)
		length := 0
		for _, h := range hist {
			if w < h[1] {
				length = h[0]
				break
			}
			w -= h[1]
		}
		sb.Reset()
		text(&sb, length)
		for {
			if _, dup := seen[sb.String()]; !dup {
				break
			}
			text(&sb, 1)
		}
		k := strings.Clone(sb.String())
		seen[k] = struct{}{}
		all = append(all, k)
	}
	return keySet[string]{"json", all[:n], all[n:]}
}

// wikiKeys reads English Wikipedia's article titles (enwiki's
// all-titles-in-ns0 dump, gzipped or not), shuffles them, and takes n+holdout.
func wikiKeys(path string, n, holdout int) (keySet[string], error) {
	f, err := os.Open(path)
	if err != nil {
		return keySet[string]{}, fmt.Errorf("wiki titles: %w (run bench/fetch-data.sh)", err)
	}
	defer f.Close()
	var rd io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return keySet[string]{}, err
		}
		rd = gz
	}
	sc := bufio.NewScanner(bufio.NewReaderSize(rd, 1<<20))
	var all []string
	seen := map[string]struct{}{}
	for sc.Scan() {
		t := sc.Text()
		if t == "page_title" || t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		t = strings.Clone(t)
		seen[t] = struct{}{}
		all = append(all, t)
	}
	if err := sc.Err(); err != nil {
		return keySet[string]{}, err
	}
	clear(seen)
	r := rand.New(rand.NewPCG(19, 19))
	r.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	if len(all) < n+holdout {
		return keySet[string]{}, fmt.Errorf("wiki titles: %d titles, need %d", len(all), n+holdout)
	}
	all = all[: n+holdout : n+holdout]
	return keySet[string]{"wiki-titles", all[:n], all[n:]}, nil
}

// halfProbe is a lookup sequence that hits half the time: present and absent
// keys alternating at random, as bbtree's get-half cells.
func halfProbe[K comparable](ks keySet[K], ops int, seed uint64) []K {
	r := rand.New(rand.NewPCG(seed, 3))
	p := make([]K, ops)
	for i := range p {
		if r.IntN(2) == 0 {
			p[i] = ks.present[r.IntN(len(ks.present))]
		} else {
			p[i] = ks.absent[r.IntN(len(ks.absent))]
		}
	}
	return p
}

func hitProbe[K comparable](ks keySet[K], ops int, seed uint64) []K {
	r := rand.New(rand.NewPCG(seed, 5))
	p := make([]K, ops)
	for i := range p {
		p[i] = ks.present[r.IntN(len(ks.present))]
	}
	return p
}
