#!/bin/sh
# Runs every cell of the blog-style comparison and writes the tables to
# results/<host>-<date>.md and every cell to results/<host>-<date>.csv.
# Takes about an hour at the default 10M entries; pass extra flags through,
# e.g. ./run-all.sh -n 1000000 for a quick pass. STUNDB_RESULTS overrides the
# results file name.
set -eu
cd "$(dirname "$0")"
./fetch-data.sh >/dev/null
mkdir -p results
name="results/${STUNDB_RESULTS:-$(hostname)-$(date +%Y%m%d)}"
export GOAMD64=v3 GOEXPERIMENT=simd
go build -o stundb-bench .
for mode in single memory threads; do
	./stundb-bench -mode "$mode" -out "$name.csv" "$@" | tee -a "$name.md"
done
