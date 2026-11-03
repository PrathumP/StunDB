<!-- AMD Ryzen 7 4800H with Radeon Graphics, GOMAXPROCS=16, SIMD=true, go1.26.8-X:simd , thp=true -->

### Threads, u64 keys, 10000000 entries, get: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 | 8 | 12 | 16 |
|---|--:|--:|--:|--:|--:|--:|
| stundb | 3.4 | 6.9 | 12.6 | 19.3 | 23.9 | 29.8 |
| stundb (batched) | 5.3 | 10.0 | 16.7 | 25.9 | 25.5 | 26.3 |
| skipmap | 0.4 | 0.8 | 1.6 | 2.9 | 3.8 | 4.5 |
| RWMutex+google/btree | 1.5 | 2.8 | 5.5 | 9.4 | 11.8 | 14.0 |
| xsync.Map *(unordered)* | 12.3 | 25.2 | 47.2 | 75.0 | 83.7 | 89.4 |

### Threads, u64 keys, 10000000 entries, insert: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 | 8 | 12 | 16 |
|---|--:|--:|--:|--:|--:|--:|
| stundb | 2.2 | 4.1 | 6.5 | 9.7 | 11.4 | 11.9 |
| stundb (batched) | 3.4 | 5.7 | 8.9 | 10.8 | 11.3 | 11.2 |
| skipmap | 0.3 | 0.6 | 1.2 | 2.0 | 2.5 | 2.7 |
| RWMutex+google/btree | 1.4 | 1.3 | 1.2 | 1.2 | 1.2 | 1.2 |
| xsync.Map *(unordered)* | 3.2 | 5.4 | 8.3 | 10.4 | 11.3 | 11.4 |

### Threads, u64 keys, 10000000 entries, update: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 | 8 | 12 | 16 |
|---|--:|--:|--:|--:|--:|--:|
| stundb | 2.9 | 6.1 | 9.2 | 14.7 | 18.7 | 20.9 |
| skipmap | 0.4 | 0.8 | 1.6 | 2.8 | 3.6 | 4.4 |
| RWMutex+google/btree | 1.5 | 1.4 | 1.3 | 1.3 | 1.3 | 1.3 |
| xsync.Map *(unordered)* | 4.9 | 10.3 | 19.7 | 29.7 | 34.7 | 38.5 |

### Threads, u64 keys, 10000000 entries, get90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 | 8 | 12 | 16 |
|---|--:|--:|--:|--:|--:|--:|
| stundb | 3.5 | 7.0 | 12.0 | 18.5 | 22.8 | 28.4 |
| skipmap | 0.4 | 0.8 | 1.6 | 2.8 | 3.7 | 4.4 |
| RWMutex+google/btree | 1.5 | 1.4 | 1.3 | 1.3 | 1.3 | 1.3 |
| xsync.Map *(unordered)* | 11.0 | 22.3 | 42.8 | 59.0 | 70.6 | 68.4 |

### Threads, u64 keys, 10000000 entries, scan90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 | 8 | 12 | 16 |
|---|--:|--:|--:|--:|--:|--:|
| stundb | 1.0 | 2.0 | 3.5 | 5.2 | 6.3 | 6.8 |
| RWMutex+google/btree | 0.6 | 0.6 | 0.6 | 0.6 | 0.6 | 0.6 |
