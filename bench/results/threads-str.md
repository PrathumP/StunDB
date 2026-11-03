<!-- Measured before the harness collected garbage between building a map and timing it: every cell is depressed by a concurrent GC mark, most at 16 threads (JSON batched gets read 5.5 M/s here; 23.1 once collected). Ratios between maps hold roughly, absolute values do not. See threads-u64.md for a run with the fix. -->

<!-- AMD Ryzen 7 4800H with Radeon Graphics, GOMAXPROCS=16, SIMD=true, go1.26.8-X:simd , thp=true -->

### Threads, uuid keys, 10000000 entries, get: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 3.0 | 10.9 | 16.8 | 25.6 |
| stundb (batched) | 4.6 | 14.4 | 18.6 | 23.5 |
| skipmap | 0.4 | 1.5 | 2.6 | 4.4 |
| RWMutex+google/btree | 0.9 | 3.0 | 5.6 | 9.0 |
| xsync.Map *(unordered)* | 11.1 | 43.0 | 65.8 | 74.6 |

### Threads, uuid keys, 10000000 entries, insert: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 1.8 | 5.3 | 6.6 | 6.8 |
| stundb (batched) | 2.5 | 6.6 | 6.8 | 6.6 |
| skipmap | 0.3 | 1.1 | 1.8 | 2.6 |
| RWMutex+google/btree | 0.9 | 0.8 | 0.8 | 0.8 |
| xsync.Map *(unordered)* | 3.1 | 8.5 | 11.7 | 12.6 |

### Threads, uuid keys, 10000000 entries, update: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 2.7 | 8.1 | 12.1 | 17.3 |
| skipmap | 0.4 | 1.5 | 2.5 | 4.4 |
| RWMutex+google/btree | 0.9 | 0.8 | 0.8 | 0.8 |
| xsync.Map *(unordered)* | 4.6 | 15.0 | 28.1 | 35.1 |

### Threads, uuid keys, 10000000 entries, get90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 3.0 | 10.5 | 14.8 | 24.7 |
| skipmap | 0.4 | 1.5 | 2.5 | 4.4 |
| RWMutex+google/btree | 0.9 | 0.8 | 0.9 | 0.8 |
| xsync.Map *(unordered)* | 9.8 | 37.0 | 46.7 | 62.8 |

### Threads, uuid keys, 10000000 entries, scan90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 0.9 | 2.9 | 4.0 | 5.4 |
| RWMutex+google/btree | 0.4 | 0.4 | 0.4 | 0.5 |

### Threads, json keys, 10000000 entries, get: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 2.2 | 8.6 | 14.3 | 21.1 |
| stundb (batched) | 3.1 | 10.3 | 17.3 | 5.5 |
| skipmap | 0.3 | 1.2 | 1.9 | 3.0 |
| RWMutex+google/btree | 0.6 | 2.3 | 3.7 | 5.4 |
| xsync.Map *(unordered)* | 7.7 | 25.8 | 43.7 | 40.6 |

### Threads, json keys, 10000000 entries, insert: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 0.9 | 4.6 | 6.2 | 8.1 |
| stundb (batched) | 1.5 | 5.8 | 7.5 | 6.1 |
| skipmap | 0.2 | 0.9 | 1.1 | 1.3 |
| RWMutex+google/btree | 0.6 | 0.4 | 0.5 | 0.4 |
| xsync.Map *(unordered)* | 1.7 | 5.5 | 3.4 | 4.6 |

### Threads, json keys, 10000000 entries, update: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 2.0 | 7.4 | 11.3 | 16.1 |
| skipmap | 0.3 | 1.2 | 2.0 | 3.1 |
| RWMutex+google/btree | 0.6 | 0.6 | 0.6 | 0.6 |
| xsync.Map *(unordered)* | 3.4 | 12.3 | 20.5 | 30.2 |

### Threads, json keys, 10000000 entries, get90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 2.2 | 8.2 | 13.4 | 19.6 |
| skipmap | 0.3 | 1.2 | 1.9 | 3.0 |
| RWMutex+google/btree | 0.6 | 0.6 | 0.6 | 0.6 |
| xsync.Map *(unordered)* | 7.0 | 24.2 | 37.8 | 39.2 |

### Threads, json keys, 10000000 entries, scan90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 4 | 8 | 16 |
|---|--:|--:|--:|--:|
| stundb | 0.8 | 2.7 | 4.1 | 5.4 |
| RWMutex+google/btree | 0.3 | 0.4 | 0.4 | 0.4 |
