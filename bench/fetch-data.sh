#!/bin/sh
# Downloads the Wikipedia titles the "wiki" key set reads: every article title
# of English Wikipedia (namespace 0), about 18M, 110 MB gzipped.
set -eu
dir="${HOME}/.cache/stundb-bench"
mkdir -p "$dir"
url="https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-all-titles-in-ns0.gz"
if [ ! -s "$dir/enwiki-latest-all-titles-in-ns0.gz" ]; then
	curl -fL --retry 3 -o "$dir/enwiki-latest-all-titles-in-ns0.gz.part" "$url"
	mv "$dir/enwiki-latest-all-titles-in-ns0.gz.part" "$dir/enwiki-latest-all-titles-in-ns0.gz"
fi
ls -l "$dir"
