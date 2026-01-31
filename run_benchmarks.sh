#!/bin/bash

# StunDB Benchmark Runner
# Quick script to run benchmarks and compare results

set -e

BENCHMARK_DIR="./bptree"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
RESULTS_DIR="benchmark_results_$TIMESTAMP"
mkdir -p "$RESULTS_DIR"

echo "========================================"
echo "StunDB Benchmarking Suite"
echo "========================================"
echo "Results will be saved to: $RESULTS_DIR/"
echo ""

# Parse command line arguments
MODE=${1:-"quick"}
BENCHTIME=${2:-"3s"}

case $MODE in
  quick)
    echo "Mode: Quick (${BENCHTIME} per benchmark)"
    BENCHMARKS="Zipfian|ReadOnly|90Read10Write"
    ;;
  thorough)
    echo "Mode: Thorough (${BENCHTIME} per benchmark)"
    BENCHMARKS="."
    BENCHTIME="10s"
    ;;
  profile)
    echo "Mode: Profile with CPU and Memory analysis"
    BENCHMARKS="Zipfian"
    ;;
  comparison)
    echo "Mode: Comparison against alternatives"
    BENCHMARKS="Comparison"
    ;;
  *)
    echo "Usage: $0 [quick|thorough|profile|comparison] [benchtime]"
    echo ""
    echo "Modes:"
    echo "  quick       - Fast run with key benchmarks (default, ~3 min)"
    echo "  thorough    - All benchmarks, longer runtime (15-20 min)"
    echo "  profile     - CPU & memory profiling (10 min)"
    echo "  comparison  - vs sync.Map and Go map (5 min)"
    echo ""
    echo "Example: $0 thorough 10s"
    exit 1
    ;;
esac

echo "Benchtime: $BENCHTIME"
echo ""

# Run benchmarks
if [ "$MODE" = "profile" ]; then
  echo "[1/2] Running CPU profile..."
  go test -bench=$BENCHMARKS -cpuprofile="$RESULTS_DIR/cpu.prof" \
    -benchmem -benchtime=$BENCHTIME -timeout 30m ./bptree 2>&1 | \
    tee "$RESULTS_DIR/benchmark_cpu.txt"
  
  echo ""
  echo "[2/2] Running memory profile..."
  go test -bench=$BENCHMARKS -memprofile="$RESULTS_DIR/mem.prof" \
    -benchmem -benchtime=$BENCHTIME -timeout 30m ./bptree 2>&1 | \
    tee "$RESULTS_DIR/benchmark_mem.txt"
  
  echo ""
  echo "Profile files generated:"
  echo "  - CPU: $RESULTS_DIR/cpu.prof"
  echo "  - Memory: $RESULTS_DIR/mem.prof"
  echo ""
  echo "Analyze with:"
  echo "  go tool pprof $RESULTS_DIR/cpu.prof"
  echo "  go tool pprof $RESULTS_DIR/mem.prof"
  
else
  echo "Running benchmarks..."
  go test -bench=$BENCHMARKS -benchmem -benchtime=$BENCHTIME \
    -timeout 30m -v ./bptree 2>&1 | tee "$RESULTS_DIR/results.txt"
  
  echo ""
  echo "========================================"
  echo "Benchmark Summary"
  echo "========================================"
  
  # Extract and display key metrics
  if grep -q "BenchmarkStunDB_Read_Zipfian" "$RESULTS_DIR/results.txt"; then
    echo ""
    echo "Hot Key Performance (Zipfian):"
    grep "BenchmarkStunDB_Read_Zipfian" "$RESULTS_DIR/results.txt" | head -5 || true
  fi
  
  if grep -q "BenchmarkStunDB_Concurrency_ReadOnly" "$RESULTS_DIR/results.txt"; then
    echo ""
    echo "Read-Only Concurrency:"
    grep "BenchmarkStunDB_Concurrency_ReadOnly" "$RESULTS_DIR/results.txt" | head -5 || true
  fi
  
  if grep -q "BenchmarkComparison" "$RESULTS_DIR/results.txt"; then
    echo ""
    echo "Comparison Results:"
    grep "BenchmarkComparison" "$RESULTS_DIR/results.txt" | head -10 || true
  fi
fi

echo ""
echo "========================================"
echo "Complete! Results saved to:"
echo "  $RESULTS_DIR/"
echo "========================================"
echo ""
echo "Next steps:"
echo "1. Review results.txt for detailed output"
echo "2. Save baseline: cp $RESULTS_DIR/results.txt baseline.txt"
echo "3. Compare future runs: benchstat baseline.txt results.txt"
echo ""
if [ "$MODE" = "profile" ]; then
  echo "To explore profiles:"
  echo "  go tool pprof $RESULTS_DIR/cpu.prof"
  echo "  go tool pprof $RESULTS_DIR/mem.prof"
  echo ""
fi
