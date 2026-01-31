# StunDB Benchmarking - Quick Reference Card

## 📋 All Available Benchmarks

### Read Pattern Benchmarks
```bash
# Hot key distribution (WHERE STUNDB EXCELS)
go test -bench=BenchmarkStunDB_Read_Zipfian -benchmem -benchtime=5s ./bptree

# Sequential access (control)
go test -bench=BenchmarkStunDB_Read_Sequential -benchmem -benchtime=5s ./bptree

# Uniform random (control)
go test -bench=BenchmarkStunDB_Read_Uniform -benchmem -benchtime=5s ./bptree
```

### Concurrency Benchmarks
```bash
# 100% reads - lock-free advantage
go test -bench=BenchmarkStunDB_Concurrency_ReadOnly -benchmem -benchtime=5s ./bptree

# 90% reads / 10% writes - realistic cache pattern
go test -bench=BenchmarkStunDB_Concurrency_90Read10Write -benchmem -benchtime=5s ./bptree

# 50% reads / 50% writes - stress test
go test -bench=BenchmarkStunDB_Concurrency_WriteHeavy -benchmem -benchtime=5s ./bptree

# Scaling analysis (1-8 cores)
go test -bench=BenchmarkStunDB_Concurrency_Scaling -benchmem -benchtime=5s ./bptree
```

### Memory Benchmarks
```bash
# Heap usage analysis vs alternatives
go test -bench=BenchmarkMemory_StunDB_Large -benchmem ./bptree
```

### Analysis
```bash
# Tree depth and theoretical metrics
go test -bench=BenchmarkStunDB_DepthAnalysis -benchmem ./bptree
```

### Comparison Benchmarks
```bash
# Read-only: StunDB vs sync.Map vs GoMap
go test -bench=BenchmarkComparison_ReadOnly -benchmem -benchtime=5s ./bptree

# Mixed 90/10: StunDB vs sync.Map vs GoMap
go test -bench=BenchmarkComparison_Mixed -benchmem -benchtime=5s ./bptree
```

---

## 🎯 Benchmark Cheat Sheet

### Most Important (Run These First)
```bash
# Hot key performance
go test -bench=Zipfian -benchmem -benchtime=5s ./bptree

# Concurrent performance
go test -bench=Concurrency_90Read -benchmem -benchtime=5s ./bptree

# Memory efficiency
go test -bench=Memory_StunDB_Large -benchmem ./bptree

# Competitive comparison
go test -bench=Comparison -benchmem -benchtime=5s ./bptree
```

### All at Once
```bash
go test -bench=. -benchmem -benchtime=5s ./bptree
```

### Specific Patterns
```bash
# All read-related
go test -bench=Read -benchmem -benchtime=5s ./bptree

# All concurrency-related
go test -bench=Concurrency -benchmem -benchtime=5s ./bptree

# All comparisons
go test -bench=Comparison -benchmem -benchtime=5s ./bptree
```

---

## 📊 Reading the Output

### Sample Output Line
```
BenchmarkStunDB_Read_Zipfian-16  5487542  140.6 ns/op  16 B/op  1 allocs/op
                            │        │       │         │       │
                    CPU cores    Iterations  Latency  Memory  Allocations
```

### Quick Interpretation

| Metric | Good | Bad | Note |
|--------|------|-----|------|
| ns/op | Lower | Higher | Latency (lower=faster) |
| B/op | Lower | Higher | Memory per op |
| allocs/op | Lower | Higher | GC pressure |
| Iterations | Higher | Lower | More stable result |

---

## 🚀 Execution Modes

### Fastest (3 min)
```bash
./run_benchmarks.sh quick
```

### Full Suite (15-20 min)
```bash
./run_benchmarks.sh thorough
```

### With Profiling (10 min)
```bash
./run_benchmarks.sh profile
```
Then analyze:
```bash
go tool pprof benchmark_results_*/cpu.prof
go tool pprof benchmark_results_*/mem.prof
```

### Comparison Mode (5 min)
```bash
./run_benchmarks.sh comparison
```

---

## 💾 Saving & Comparing Results

### Save Baseline
```bash
go test -bench=. -benchmem -benchtime=5s ./bptree > baseline.txt
```

### Compare After Changes
```bash
go test -bench=. -benchmem -benchtime=5s ./bptree > new.txt
benchstat baseline.txt new.txt
```

### Interpreting benchstat Output
```
-5% = 5% faster (good!)
+5% = 5% slower (investigate)
±2% = Noise (ignore)
```

---

## 📈 Expected Results

| Benchmark | Metric | Value | Note |
|-----------|--------|-------|------|
| Zipfian | ns/op | ~140 | Early exit on hot keys |
| ReadOnly | ns/op | ~96 | Lock-free readers |
| 90/10 Mix | ns/op | ~1500 | Typical cache workload |
| Memory | B/key | ~100 | For 1M items |
| Scaling | cores | 1-8 | Near-linear scaling |

---

## 🔧 Common Commands

### Profile CPU Usage
```bash
go test -bench=BenchmarkStunDB_Read_Zipfian -cpuprofile=cpu.prof ./bptree
go tool pprof cpu.prof
> top10      # See top functions
> list main  # See per-line CPU usage
```

### Profile Memory
```bash
go test -bench=BenchmarkMemory_StunDB_Large -memprofile=mem.prof ./bptree
go tool pprof mem.prof
> alloc_space  # Total allocations
> inuse_space  # Current heap usage
```

### Trace Execution
```bash
go test -bench=BenchmarkStunDB_Concurrency_ReadOnly -trace=trace.out ./bptree
go tool trace trace.out  # Opens browser
```

### Run Multiple Times
```bash
go test -bench=. -benchmem -benchtime=5s -count=5 ./bptree
```

---

## 📚 Documentation Reference

| Document | When to Read |
|----------|------|
| BENCHMARKING_EXECUTIVE_SUMMARY | Overview for decision makers |
| BENCHMARKING_QUICKSTART | "How do I run this?" |
| BENCHMARK_RESULTS_GUIDE | "What do these numbers mean?" |
| BENCHMARKING_GUIDE | Technical reference |
| BENCHMARKING_STRATEGY | "How do I position this?" |
| BENCHMARKING_README | Complete overview |
| BENCHMARKING_INDEX | Navigation help |

---

## 🎯 Benchmark Strategy

### StunDB Wins ✅
- Hot key distribution (Zipfian)
- Read-heavy concurrency (90%+)
- Memory efficiency
- Lock-free reading

### StunDB Competes ≈
- Single-threaded point lookups
- General concurrent access

### Others Win ❌
- Write-heavy workloads (LSM)
- Range scans (B+Trees)
- Unordered data (hash maps)

---

## 💡 Pro Tips

1. **Stable Results**: Use `-benchtime=10s` for variance < 5%

2. **Fair Comparison**: Same benchtime, same system load, multiple runs

3. **Real Workloads**: Zipfian benchmark simulates realistic traffic

4. **Profile First**: Use CPU profiling to find bottlenecks

5. **Track Progress**: Save baseline, compare after changes

6. **Statistical Test**: Use `benchstat` for significance testing

---

## ⚠️ Common Pitfalls

❌ **Short benchmark time** → High variance
✅ **Use -benchtime=10s** → Stable results

❌ **Running on busy system** → Noisy results
✅ **Minimize background processes** → Clean results

❌ **Comparing different conditions** → Unfair comparison
✅ **Run same command** → Fair comparison

❌ **Trusting single run** → Luck-dependent
✅ **Run multiple times** → Statistical validity

---

## 🎉 Quick Start (Copy & Paste)

```bash
# Navigate to project
cd /home/pookie/StunDB

# Run quick benchmarks (3 minutes)
./run_benchmarks.sh quick

# View results
cat benchmark_results_*/results.txt | head -50

# Extract key metrics
grep "Benchmark\|ns/op" benchmark_results_*/results.txt
```

---

## 📞 Need Help?

- **Running benchmarks?** → BENCHMARKING_QUICKSTART.md
- **Understanding results?** → BENCHMARK_RESULTS_GUIDE.md
- **Strategic guidance?** → BENCHMARKING_STRATEGY.md
- **Technical details?** → BENCHMARKING_GUIDE.md
- **Overview?** → BENCHMARKING_README.md

---

**Happy benchmarking!** 🚀
