<!-- AMD EPYC 9V74 80-Core Processor, GOMAXPROCS=4, SIMD=true, go1.26.11-X:simd, GOEXPERIMENT=simd GOAMD64=v3, thp=true, power=unknown -->

### One thread, u64 keys, 10000000 entries: ns per operation (lower is better)

| map | get | insert | update | scan100 |
|---|--:|--:|--:|--:|
| stundb | 363 | 410 | 437 | 1209 |
| stundb (batched) | 119 | 258 | – | – |
| google/btree | 690 | 648 | 748 | 2022 |
| tidwall/btree | 687 | 738 | 689 | 1599 |
| skipmap | 2634 | 3061 | 2745 | – |
| Go map *(unordered)* | 71 | 153 | 80 | – |

### One thread, uuid keys, 10000000 entries: ns per operation (lower is better)

| map | get | insert | update | scan100 |
|---|--:|--:|--:|--:|
| stundb | 345 | 500 | 410 | 1376 |
| stundb (batched) | 152 | 330 | – | – |
| google/btree | 1265 | 1276 | 1249 | 3178 |
| tidwall/btree | 1176 | 1319 | 1256 | 2645 |
| skipmap | 2730 | 3006 | 2693 | – |
| Go map *(unordered)* | 100 | 247 | 116 | – |

### One thread, wiki-titles keys, 10000000 entries: ns per operation (lower is better)

| map | get | insert | update | scan100 |
|---|--:|--:|--:|--:|
| stundb | 797 | 1037 | 845 | 1725 |
| stundb (batched) | 593 | 782 | – | – |
| google/btree | 1749 | 1844 | 1786 | 4218 |
| tidwall/btree | 1768 | 2094 | 1878 | 3042 |
| skipmap | 4027 | 4865 | 3949 | – |
| Go map *(unordered)* | 139 | 491 | 192 | – |

### One thread, json keys, 10000000 entries: ns per operation (lower is better)

| map | get | insert | update | scan100 |
|---|--:|--:|--:|--:|
| stundb | 478 | 718 | 546 | 1454 |
| stundb (batched) | 274 | 478 | – | – |
| google/btree | 1807 | 1742 | 1800 | 4313 |
| tidwall/btree | 1909 | 1897 | 1882 | 3049 |
| skipmap | 3679 | 4017 | 3312 | – |
| Go map *(unordered)* | 131 | 326 | 177 | – |
<!-- AMD EPYC 9V74 80-Core Processor, GOMAXPROCS=4, SIMD=true, go1.26.11-X:simd, GOEXPERIMENT=simd GOAMD64=v3, thp=true, power=unknown -->

### Memory, u64 keys, 10000000 entries: heap bytes per entry once built, keys included (lower is better)

| map | bytes/entry |
|---|--:|
| stundb | 27.5 |
| google/btree | 24.7 |
| tidwall/btree | 24.7 |
| skipmap | 88.4 |
| Go map *(unordered)* | 30.3 |

### Memory, uuid keys, 10000000 entries: heap bytes per entry once built, keys included (lower is better)

| map | bytes/entry |
|---|--:|
| stundb | 39.6 |
| google/btree | 36.1 |
| tidwall/btree | 36.2 |
| skipmap | 104.4 |
| Go map *(unordered)* | 44.7 |

### Memory, wiki-titles keys, 10000000 entries: heap bytes per entry once built, keys included (lower is better)

| map | bytes/entry |
|---|--:|
| stundb | 68.0 |
| google/btree | 61.6 |
| tidwall/btree | 61.5 |
| skipmap | 129.6 |
| Go map *(unordered)* | 69.7 |

### Memory, json keys, 10000000 entries: heap bytes per entry once built, keys included (lower is better)

| map | bytes/entry |
|---|--:|
| stundb | 53.4 |
| google/btree | 47.0 |
| tidwall/btree | 46.9 |
| skipmap | 115.7 |
| Go map *(unordered)* | 55.1 |
<!-- AMD EPYC 9V74 80-Core Processor, GOMAXPROCS=4, SIMD=true, go1.26.11-X:simd, GOEXPERIMENT=simd GOAMD64=v3, thp=true, power=unknown -->

### Threads, u64 keys, 10000000 entries, get: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 2.1 | 4.9 | 10.8 |
| stundb (batched) | 8.1 | 16.3 | 21.9 |
| skipmap | 0.3 | 0.7 | 1.4 |
| RWMutex+google/btree | 1.3 | 2.6 | 5.4 |
| xsync.Map *(unordered)* | 13.9 | 25.9 | 31.0 |

### Threads, u64 keys, 10000000 entries, insert: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.9 | 3.9 | 6.7 |
| stundb (batched) | 3.5 | 6.6 | 9.1 |
| skipmap | 0.2 | 0.6 | 1.1 |
| RWMutex+google/btree | 1.3 | 1.1 | 1.1 |
| xsync.Map *(unordered)* | 3.0 | 5.7 | 6.9 |

### Threads, u64 keys, 10000000 entries, update: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.7 | 4.0 | 9.1 |
| skipmap | 0.3 | 0.7 | 1.4 |
| RWMutex+google/btree | 1.2 | 1.2 | 1.1 |
| xsync.Map *(unordered)* | 5.8 | 11.3 | 13.6 |

### Threads, u64 keys, 10000000 entries, get90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 2.0 | 4.9 | 10.7 |
| skipmap | 0.3 | 0.7 | 1.4 |
| RWMutex+google/btree | 1.2 | 1.1 | 1.1 |
| xsync.Map *(unordered)* | 10.6 | 20.2 | 27.8 |

### Threads, u64 keys, 10000000 entries, scan90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 0.8 | 1.6 | 2.9 |
| RWMutex+google/btree | 0.5 | 0.4 | 0.5 |

### Threads, uuid keys, 10000000 entries, get: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 2.8 | 5.4 | 10.9 |
| stundb (batched) | 6.4 | 12.6 | 18.5 |
| skipmap | 0.4 | 0.7 | 1.4 |
| RWMutex+google/btree | 0.7 | 1.5 | 3.3 |
| xsync.Map *(unordered)* | 10.6 | 20.9 | 28.8 |

### Threads, uuid keys, 10000000 entries, insert: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.9 | 3.8 | 6.2 |
| stundb (batched) | 2.9 | 5.5 | 7.8 |
| skipmap | 0.3 | 0.7 | 1.2 |
| RWMutex+google/btree | 0.7 | 0.7 | 0.7 |
| xsync.Map *(unordered)* | 2.7 | 4.9 | 7.2 |

### Threads, uuid keys, 10000000 entries, update: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 2.4 | 4.2 | 8.9 |
| skipmap | 0.4 | 0.7 | 1.4 |
| RWMutex+google/btree | 0.8 | 0.7 | 0.8 |
| xsync.Map *(unordered)* | 3.4 | 6.8 | 13.3 |

### Threads, uuid keys, 10000000 entries, get90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 2.7 | 5.2 | 10.7 |
| skipmap | 0.3 | 0.7 | 1.4 |
| RWMutex+google/btree | 0.8 | 0.7 | 0.8 |
| xsync.Map *(unordered)* | 8.5 | 17.2 | 22.9 |

### Threads, uuid keys, 10000000 entries, scan90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 0.8 | 1.5 | 2.4 |
| RWMutex+google/btree | 0.3 | 0.3 | 0.3 |

### Threads, wiki-titles keys, 10000000 entries, get: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.4 | 2.8 | 4.7 |
| stundb (batched) | 1.8 | 3.4 | 5.2 |
| skipmap | 0.3 | 0.6 | 1.1 |
| RWMutex+google/btree | 0.6 | 1.2 | 2.1 |
| xsync.Map *(unordered)* | 7.1 | 13.7 | 18.8 |

### Threads, wiki-titles keys, 10000000 entries, insert: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.0 | 2.1 | 3.6 |
| stundb (batched) | 1.3 | 2.6 | 3.9 |
| skipmap | 0.3 | 0.5 | 0.8 |
| RWMutex+google/btree | 0.5 | 0.5 | 0.5 |
| xsync.Map *(unordered)* | 1.6 | 3.1 | 4.7 |

### Threads, wiki-titles keys, 10000000 entries, update: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.3 | 2.6 | 4.5 |
| skipmap | 0.3 | 0.6 | 1.1 |
| RWMutex+google/btree | 0.6 | 0.5 | 0.5 |
| xsync.Map *(unordered)* | 2.3 | 4.6 | 8.7 |

### Threads, wiki-titles keys, 10000000 entries, get90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.4 | 2.7 | 4.8 |
| skipmap | 0.3 | 0.6 | 1.1 |
| RWMutex+google/btree | 0.6 | 0.5 | 0.5 |
| xsync.Map *(unordered)* | 5.9 | 11.5 | 16.5 |

### Threads, wiki-titles keys, 10000000 entries, scan90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 0.6 | 1.2 | 2.0 |
| RWMutex+google/btree | 0.3 | 0.2 | 0.3 |

### Threads, json keys, 10000000 entries, get: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 2.2 | 4.5 | 7.7 |
| stundb (batched) | 3.7 | 7.3 | 9.9 |
| skipmap | 0.2 | 0.6 | 1.1 |
| RWMutex+google/btree | 0.5 | 1.0 | 1.9 |
| xsync.Map *(unordered)* | 7.1 | 14.2 | 19.3 |

### Threads, json keys, 10000000 entries, insert: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.6 | 3.2 | 5.2 |
| stundb (batched) | 2.2 | 4.3 | 6.0 |
| skipmap | 0.2 | 0.5 | 0.9 |
| RWMutex+google/btree | 0.4 | 0.5 | 0.5 |
| xsync.Map *(unordered)* | 2.0 | 3.8 | 5.2 |

### Threads, json keys, 10000000 entries, update: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 1.9 | 3.9 | 6.5 |
| skipmap | 0.2 | 0.6 | 1.0 |
| RWMutex+google/btree | 0.5 | 0.4 | 0.4 |
| xsync.Map *(unordered)* | 2.2 | 4.3 | 8.6 |

### Threads, json keys, 10000000 entries, get90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 2.2 | 4.3 | 7.5 |
| skipmap | 0.2 | 0.6 | 0.9 |
| RWMutex+google/btree | 0.5 | 0.4 | 0.5 |
| xsync.Map *(unordered)* | 5.9 | 12.0 | 17.0 |

### Threads, json keys, 10000000 entries, scan90/update10: millions of operations per second (higher is better)

| map \ threads | 1 | 2 | 4 |
|---|--:|--:|--:|
| stundb | 0.8 | 1.5 | 2.4 |
| RWMutex+google/btree | 0.2 | 0.2 | 0.2 |
