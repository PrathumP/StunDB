package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	getBatch    = 64 // keys per GetMany, as bbtree's get_many_copied cells
	insertBatch = 16 // entries per InsertMany, as bbtree's insert_many cells
	scanLen     = 100
)

type config struct {
	n, holdout, ops int
	dur             time.Duration
	threads         []int
	thp             bool
	out             *os.File
}

// record prints one cell as CSV to the results file.
func (c *config) record(mode, keys, mapName, workload string, threads int, value float64, unit string) {
	if c.out != nil {
		fmt.Fprintf(c.out, "%s,%s,%s,%s,%d,%.2f,%s\n", mode, keys, mapName, workload, threads, value, unit)
	}
}

func settle() {
	runtime.GC()
	debug.FreeOSMemory()
}

// ---- one thread ---------------------------------------------------------------

var singleWorkloads = []string{"get", "insert", "update", "scan100"}

// runSingle measures each map on one thread: the mean time per operation, run
// back to back, at the map's full size.
func runSingle[K comparable](c *config, ks keySet[K], less func(a, b K) bool, bare any, kinds []mapKind) {
	probe := halfProbe(ks, c.ops, 1)
	hits := hitProbe(ks, c.ops, 2)
	rows := map[string]map[string]float64{}
	for _, kind := range kinds {
		settle()
		m := newMap(kind, less, bare)
		row := map[string]float64{}
		rows[kind.name] = row
		b, _ := m.(Batcher[K])

		start := time.Now()
		if kind.batched && b != nil {
			vals := make([]uint64, insertBatch)
			for i := 0; i < len(ks.present); i += insertBatch {
				end := min(i+insertBatch, len(ks.present))
				for j := i; j < end; j++ {
					vals[j-i] = uint64(j)
				}
				b.InsertMany(ks.present[i:end], vals[:end-i])
			}
		} else {
			for i, k := range ks.present {
				m.Insert(k, uint64(i))
			}
		}
		row["insert"] = nsPer(time.Since(start), len(ks.present))
		runtime.GC() // finish the build's collection before timing reads
		if c.thp {
			collapseHeap()
			fmt.Fprintf(os.Stderr, "  (huge pages: %d MB)\n", hugePagesKB()>>10)
		}

		var sink uint64
		start = time.Now()
		if kind.batched && b != nil {
			vals := make([]uint64, getBatch)
			found := make([]bool, getBatch)
			for i := 0; i+getBatch <= len(probe); i += getBatch {
				b.GetMany(probe[i:i+getBatch], vals, found)
				sink += vals[0]
			}
		} else {
			for _, k := range probe {
				v, _ := m.Get(k)
				sink += v
			}
		}
		row["get"] = nsPer(time.Since(start), len(probe))

		if !kind.batched {
			start = time.Now()
			for i, k := range hits {
				m.Update(k, uint64(i))
			}
			row["update"] = nsPer(time.Since(start), len(hits))
			if kind.ordered {
				scans := hits[:min(len(hits), c.ops/10)]
				start = time.Now()
				for _, k := range scans {
					sink += uint64(m.Scan(k, scanLen))
				}
				row["scan100"] = nsPer(time.Since(start), len(scans))
			}
		}
		for w, v := range row {
			c.record("single", ks.name, kind.name, w, 1, v, "ns/op")
		}
		fmt.Fprintf(os.Stderr, "  %-22s %v (%d)\n", kind.name, fmtRow(row), sink%2)
		m = nil
	}
	printTable(fmt.Sprintf("One thread, %s keys, %d entries: ns per operation (lower is better)", ks.name, len(ks.present)),
		kinds, singleWorkloads, rows, "%.0f")
}

func nsPer(d time.Duration, ops int) float64 { return float64(d.Nanoseconds()) / float64(ops) }

func fmtRow(row map[string]float64) string {
	var parts []string
	for _, w := range []string{"insert", "get", "update", "scan100"} {
		if v, ok := row[w]; ok {
			parts = append(parts, fmt.Sprintf("%s=%.0f", w, v))
		}
	}
	return strings.Join(parts, " ")
}

// ---- threads -------------------------------------------------------------------

var threadWorkloads = []string{"get", "insert", "update", "get90/update10", "scan90/update10"}

// runThreads measures throughput as goroutines are added. For each thread
// count each map is built anew by that many goroutines (the insert cell), then
// each workload runs for c.dur on every goroutine at once.
func runThreads[K comparable](c *config, ks keySet[K], less func(a, b K) bool, bare any, kinds []mapKind) {
	probe := halfProbe(ks, 1<<23, 3)
	hits := hitProbe(ks, 1<<23, 4)
	// results[workload][map][threads] = Mops/s
	results := map[string]map[string]map[int]float64{}
	put := func(w, m string, t int, v float64) {
		if results[w] == nil {
			results[w] = map[string]map[int]float64{}
		}
		if results[w][m] == nil {
			results[w][m] = map[int]float64{}
		}
		results[w][m][t] = v
		c.record("threads", ks.name, m, w, t, v, "Mops/s")
	}
	for _, kind := range kinds {
		for _, t := range c.threads {
			settle()
			m := newMap(kind, less, bare)
			b, _ := m.(Batcher[K])
			batched := kind.batched && b != nil

			// insert: t goroutines build the map, each its share of the keys.
			start := time.Now()
			var wg sync.WaitGroup
			per := (len(ks.present) + t - 1) / t
			for g := range t {
				wg.Go(func() {
					lo, hi := g*per, min((g+1)*per, len(ks.present))
					if batched {
						vals := make([]uint64, insertBatch)
						for i := lo; i < hi; i += insertBatch {
							end := min(i+insertBatch, hi)
							b.InsertMany(ks.present[i:end], vals[:end-i])
						}
						return
					}
					for i := lo; i < hi; i++ {
						m.Insert(ks.present[i], uint64(i))
					}
				})
			}
			wg.Wait()
			put("insert", kind.name, t, float64(len(ks.present))/time.Since(start).Seconds()/1e6)
			runtime.GC() // finish the build's collection before timing reads
			if c.thp {
				collapseHeap()
			}

			put("get", kind.name, t, timed(c, t, func(g int, stop *atomic.Bool) int {
				i, ops := g*(len(probe)/t), 0
				if batched {
					vals := make([]uint64, getBatch)
					found := make([]bool, getBatch)
					for !stop.Load() {
						if i+getBatch > len(probe) {
							i = 0
						}
						b.GetMany(probe[i:i+getBatch], vals, found)
						i += getBatch
						ops += getBatch
					}
					return ops
				}
				for !stop.Load() {
					for range 64 {
						m.Get(probe[i&(len(probe)-1)])
						i++
					}
					ops += 64
				}
				return ops
			}))
			if batched {
				continue // the batch calls are for lookups and inserts
			}
			put("update", kind.name, t, timed(c, t, func(g int, stop *atomic.Bool) int {
				i, ops := g*(len(hits)/t), 0
				for !stop.Load() {
					for range 64 {
						m.Update(hits[i&(len(hits)-1)], uint64(i))
						i++
					}
					ops += 64
				}
				return ops
			}))
			put("get90/update10", kind.name, t, timed(c, t, func(g int, stop *atomic.Bool) int {
				i, ops := g*(len(probe)/t), 0
				for !stop.Load() {
					for range 64 {
						if i%10 == 0 {
							m.Update(hits[i&(len(hits)-1)], uint64(i))
						} else {
							m.Get(probe[i&(len(probe)-1)])
						}
						i++
					}
					ops += 64
				}
				return ops
			}))
			if kind.ordered {
				put("scan90/update10", kind.name, t, timed(c, t, func(g int, stop *atomic.Bool) int {
					i, ops := g*(len(hits)/t), 0
					for !stop.Load() {
						for range 16 {
							if i%10 == 0 {
								m.Update(hits[i&(len(hits)-1)], uint64(i))
							} else {
								m.Scan(hits[(i*7)&(len(hits)-1)], scanLen)
							}
							i++
						}
						ops += 16
					}
					return ops
				}))
			}
			fmt.Fprintf(os.Stderr, "  %-22s threads=%-3d get=%.1f M/s\n", kind.name, t, results["get"][kind.name][t])
			m = nil
		}
	}
	for _, w := range threadWorkloads {
		printThreadTable(fmt.Sprintf("Threads, %s keys, %d entries, %s: millions of operations per second (higher is better)",
			ks.name, len(ks.present), w), kinds, c.threads, results[w])
	}
}

// timed runs body on t goroutines for c.dur and returns the total rate in
// millions of operations a second.
func timed(c *config, t int, body func(g int, stop *atomic.Bool) int) float64 {
	var stop atomic.Bool
	var total atomic.Int64
	var wg sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(t)
	gate := make(chan struct{})
	for g := range t {
		wg.Go(func() {
			ready.Done()
			<-gate
			total.Add(int64(body(g, &stop)))
		})
	}
	ready.Wait()
	start := time.Now()
	close(gate)
	time.Sleep(c.dur)
	stop.Store(true)
	wg.Wait()
	return float64(total.Load()) / time.Since(start).Seconds() / 1e6
}

// ---- memory ----------------------------------------------------------------------

// runMemory measures the heap each map holds per entry once built, its keys'
// own allocations included: string keys are inserted as fresh copies, so
// that the copies are the map's.
func runMemory[K comparable](c *config, ks keySet[K], less func(a, b K) bool, bare any, kinds []mapKind, clone func(K) K) {
	rows := map[string]map[string]float64{}
	for _, kind := range kinds {
		if kind.batched {
			continue
		}
		settle()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		m := newMap(kind, less, bare)
		for i, k := range ks.present {
			m.Insert(clone(k), uint64(i))
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		per := float64(after.HeapAlloc-before.HeapAlloc) / float64(len(ks.present))
		rows[kind.name] = map[string]float64{"bytes/entry": per}
		c.record("memory", ks.name, kind.name, "bytes/entry", 1, per, "B")
		fmt.Fprintf(os.Stderr, "  %-22s %.1f B/entry\n", kind.name, per)
		runtime.KeepAlive(m)
		m = nil
	}
	printTable(fmt.Sprintf("Memory, %s keys, %d entries: heap bytes per entry once built, keys included (lower is better)",
		ks.name, len(ks.present)), kinds, []string{"bytes/entry"}, rows, "%.1f")
}

// ---- tables ----------------------------------------------------------------------

func printTable(title string, kinds []mapKind, cols []string, rows map[string]map[string]float64, f string) {
	fmt.Printf("\n### %s\n\n| map | %s |\n|---|%s\n", title, strings.Join(cols, " | "), strings.Repeat("--:|", len(cols)))
	for _, k := range kinds {
		row, ok := rows[k.name]
		if !ok {
			continue
		}
		cells := make([]string, len(cols))
		for i, col := range cols {
			if v, ok := row[col]; ok {
				cells[i] = fmt.Sprintf(f, v)
			} else {
				cells[i] = "–"
			}
		}
		name := k.name
		if k.unordered {
			name += " *(unordered)*"
		}
		fmt.Printf("| %s | %s |\n", name, strings.Join(cells, " | "))
	}
}

func printThreadTable(title string, kinds []mapKind, threads []int, res map[string]map[int]float64) {
	if len(res) == 0 {
		return
	}
	hdr := make([]string, len(threads))
	for i, t := range threads {
		hdr[i] = fmt.Sprint(t)
	}
	fmt.Printf("\n### %s\n\n| map \\ threads | %s |\n|---|%s\n", title, strings.Join(hdr, " | "), strings.Repeat("--:|", len(threads)))
	for _, k := range kinds {
		row, ok := res[k.name]
		if !ok {
			continue
		}
		cells := make([]string, len(threads))
		for i, t := range threads {
			if v, ok := row[t]; ok {
				cells[i] = fmt.Sprintf("%.1f", v)
			} else {
				cells[i] = "–"
			}
		}
		name := k.name
		if k.unordered {
			name += " *(unordered)*"
		}
		fmt.Printf("| %s | %s |\n", name, strings.Join(cells, " | "))
	}
}
