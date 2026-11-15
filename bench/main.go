// Command bench measures StunDB's tree against other Go maps on the
// workloads of the bbtree post (https://bbtree.xiangpeng.systems/post/):
// 10M entries, uint64 values; uint64, UUID, Wikipedia-title and JSON-key keys;
// lookups half of which miss, inserts, updates, 100-entry scans, on one
// thread and as threads are added; and heap bytes per entry.
//
//	GOAMD64=v3 GOEXPERIMENT=simd go run . -mode single -keys u64,uuid
//
// Tables go to stdout as markdown, progress to stderr, and every cell to the
// CSV file named by -out.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/prathump/stundb/internal/btree"
)

func main() {
	home, _ := os.UserHomeDir()
	var (
		mode    = flag.String("mode", "single", "single, threads, memory, or all")
		keys    = flag.String("keys", "u64,uuid,wiki,json", "key sets: u64, uuid, wiki, json")
		maps    = flag.String("maps", "", "maps to run (default: all that apply to the mode)")
		n       = flag.Int("n", 10_000_000, "entries in each map")
		holdout = flag.Int("holdout", 1_000_000, "keys kept out of the map, for lookups that miss")
		ops     = flag.Int("ops", 4_000_000, "operations per one-thread cell")
		dur     = flag.Duration("dur", time.Second, "duration of each threaded cell")
		threads = flag.String("threads", "1,2,4,8,12,16", "thread counts")
		thp     = flag.Bool("thp", true, "back every map with huge pages after building it, before timing its reads and updates; inserts are timed on whatever pages the build gets (see thp_linux.go)")
		wiki    = flag.String("wiki", filepath.Join(home, ".cache/stundb-bench/enwiki-latest-all-titles-in-ns0.gz"), "Wikipedia titles dump")
		out     = flag.String("out", "results.csv", "CSV file the cells are appended to")
	)
	flag.Parse()

	c := &config{n: *n, holdout: *holdout, ops: *ops, dur: *dur, thp: *thp}
	for _, s := range strings.Split(*threads, ",") {
		t, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			fail(err)
		}
		c.threads = append(c.threads, t)
	}
	if *out != "" {
		f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		c.out = f
	}
	runtime.GOMAXPROCS(max(runtime.NumCPU(), slicesMax(c.threads)))
	power := powerSource()
	if power == "battery" {
		fmt.Fprintln(os.Stderr, "warning: running on battery; a laptop may cap its CPU, and every number with it")
	}
	fmt.Printf("<!-- %s, GOMAXPROCS=%d, SIMD=%v, %s, %s, thp=%v, power=%s -->\n", cpuName(), runtime.GOMAXPROCS(0),
		btree.SIMDEnabled(), runtime.Version(), buildSettings(), *thp, power)

	modes := []string{*mode}
	if *mode == "all" {
		modes = []string{"single", "threads", "memory"}
	}
	for _, ksName := range strings.Split(*keys, ",") {
		ksName = strings.TrimSpace(ksName)
		fmt.Fprintf(os.Stderr, "keys %s: generating\n", ksName)
		switch ksName {
		case "u64":
			ks := u64Keys(c.n, c.holdout)
			runAll(c, modes, *maps, ks, lessOrdered[uint64], uint64(0), func(k uint64) uint64 { return k })
		case "uuid":
			ks := uuidKeys(c.n, c.holdout)
			runAll(c, modes, *maps, ks, lessUUID, [16]byte{}, func(k [16]byte) [16]byte { return k })
		case "json":
			ks := jsonKeys(c.n, c.holdout)
			runAll(c, modes, *maps, ks, lessOrdered[string], "", strings.Clone)
		case "wiki":
			ks, err := wikiKeys(*wiki, c.n, c.holdout)
			if err != nil {
				fmt.Fprintln(os.Stderr, "skipping:", err)
				continue
			}
			runAll(c, modes, *maps, ks, lessOrdered[string], "", strings.Clone)
		default:
			fail(fmt.Errorf("unknown key set %q", ksName))
		}
		settle()
	}
}

func runAll[K comparable](c *config, modes []string, mapList string, ks keySet[K], less func(a, b K) bool, bare any, clone func(K) K) {
	for _, mode := range modes {
		fmt.Fprintf(os.Stderr, "keys %s: %s\n", ks.name, mode)
		switch mode {
		case "single":
			runSingle(c, ks, less, bare, pick(mapList, []mapKind{
				kindStunDB, kindStunDBBatched, kindGoogle, kindTidwall, kindSkipmap, kindGoMap}))
		case "threads":
			runThreads(c, ks, less, bare, pick(mapList, []mapKind{
				kindStunDB, kindStunDBBatched, kindSkipmap, kindRWGoogle, kindXsync}))
		case "memory":
			runMemory(c, ks, less, bare, pick(mapList, []mapKind{
				kindStunDB, kindGoogle, kindTidwall, kindSkipmap, kindGoMap}), clone)
		default:
			fail(fmt.Errorf("unknown mode %q", mode))
		}
	}
}

func pick(list string, all []mapKind) []mapKind {
	if list == "" {
		return all
	}
	var out []mapKind
	for _, k := range all {
		for _, want := range strings.Split(list, ",") {
			if strings.TrimSpace(want) == k.name {
				out = append(out, k)
			}
		}
	}
	return out
}

func slicesMax(xs []int) int {
	m := 0
	for _, x := range xs {
		m = max(m, x)
	}
	return m
}

func cpuName() string {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return runtime.GOARCH
	}
	for _, line := range strings.Split(string(b), "\n") {
		if name, ok := strings.CutPrefix(line, "model name"); ok {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), ":"))
		}
	}
	return runtime.GOARCH
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// buildSettings reports how the binary was built: GOAMD64 and GOEXPERIMENT as
// the compiler saw them, not as the environment has them now.
func buildSettings() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "build settings unknown"
	}
	var parts []string
	for _, s := range info.Settings {
		switch s.Key {
		case "GOAMD64", "GOEXPERIMENT":
			parts = append(parts, s.Key+"="+s.Value)
		}
	}
	if len(parts) == 0 {
		return "GOAMD64 unset"
	}
	return strings.Join(parts, " ")
}
