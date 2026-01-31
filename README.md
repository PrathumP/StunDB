# StunDB 

A thread-safe B+Tree implementation in Go with fine-grained locking for high-concurrency workloads.

## Overview

This project implements a B+Tree data structure with node-level locking using latch coupling (also known as lock crabbing) to enable safe concurrent access. Unlike traditional implementations that use a single root lock, this approach allows multiple goroutines to traverse and modify the tree simultaneously, maximizing parallelism while maintaining data consistency.

## Features

- **Concurrent Operations**: Insert, Find, Delete, and Range operations with fine-grained locking
- **Latch Coupling**: Minimizes lock contention by releasing parent locks as soon as it's safe
- **Optimized Split/Merge Logic**: Only holds multiple locks when tree structure changes are necessary
- **Range Queries**: Efficient range scanning and batch deletion
- **Comprehensive Testing**: Extensive test suite covering sequential and concurrent scenarios

## Key Concepts

### Latch Coupling (Lock Crabbing)

The implementation uses a sophisticated locking strategy:

1. **Insert**: Locks parent and child nodes during traversal. Releases the parent lock only when the child has space (no split needed). If a split is possible, keeps locks up the tree until the split completes.

2. **Delete**: Similar to insert, maintains locks on parent-child pairs. Releases parent lock only when the child has enough keys to avoid merging (keys > MinKeys).

3. **Find**: Simple read locks during traversal with immediate release of parent lock once child is locked.

### Tree Structure

- **MaxKeys**: 4 (configurable constant)
- **MinKeys**: 2 (MaxKeys / 2)
- **Node Types**: Internal nodes and leaf nodes
- **Key/Value Storage**: Byte slices for maximum flexibility

## Installation

```bash
go get github.com/yourusername/bptree
```

## Usage

### Basic Operations

```go
package main

import (
    "github.com/yourusername/bptree"
)

func main() {
    tree := &bptree.Btree{}
    
    // Insert key-value pairs
    tree.Insert([]byte("apple"), []byte("fruit"))
    tree.Insert([]byte("banana"), []byte("fruit"))
    tree.Insert([]byte("carrot"), []byte("vegetable"))
    
    // Find a value
    value, err := tree.Find([]byte("apple"))
    if err != nil {
        // Key not found
    }
    
    // Delete a key
    success := tree.Delete([]byte("banana"))
    
    // Range query
    keys, values, err := tree.GetRange([]byte("a"), []byte("c"))
    
    // Delete range
    count, err := tree.DeleteRange([]byte("a"), []byte("c"))
}
```

### Concurrent Access

```go
var wg sync.WaitGroup

// Multiple goroutines can safely access the tree
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        key := []byte(fmt.Sprintf("key%d", id))
        tree.Insert(key, key)
    }(i)
}

wg.Wait()
```

## API Reference

### Core Operations

- `Insert(key Keytype, value Valuetype)` - Insert or update a key-value pair
- `Find(key []byte) ([]byte, error)` - Retrieve value for a key
- `Delete(key []byte) bool` - Remove a key from the tree
- `GetRange(startKey, endKey []byte) ([]Keytype, []Valuetype, error)` - Retrieve all keys in range
- `DeleteRange(startKey, endKey []byte) (int, error)` - Delete all keys in range

### Types

```go
type Keytype []byte
type Valuetype []byte

type Node struct {
    keys     []Keytype
    values   []Valuetype
    children []*Node
    isleaf   bool
    mu       sync.RWMutex
}

type Btree struct {
    root     *Node
    rootLock sync.RWMutex
}
```

## Implementation Details

### Locking Strategy

The implementation balances between:
- **Safety**: Preventing race conditions during concurrent modifications
- **Performance**: Minimizing the time locks are held
- **Deadlock Prevention**: Consistent lock ordering (always parent before child)

### Split Handling

When a node overflows (keys exceed MaxKeys):
1. Lock chain is maintained from the leaf up to the first ancestor with space
2. Split propagates upward, creating new nodes as needed
3. If split reaches root, a new root is created with proper synchronization
4. Locks are released in reverse order after completion

### Merge Handling

When a node underflows (keys < MinKeys):
1. Attempts to borrow from siblings first
2. If borrowing fails, merges with a sibling
3. Recursive merge may propagate up to root
4. Root lock is used when tree height changes

## Testing

Run the test suite:

```bash
go test -v ./...
```

Run specific test:

```bash
go test -v -run TestConcurrentInsert
```

Run with race detector:

```bash
go test -race ./...
```

## Performance Characteristics

- **Insert**: O(log n) average, with locking overhead
- **Find**: O(log n) with read locks
- **Delete**: O(log n) average
- **Range Query**: O(log n + k) where k is the number of results
- **Space**: O(n)

## Known Issues

- The initial version in `a.txt` had a locking bug causing "Unlock of unlocked mutex" errors
- Fixed in the main implementation through proper lock state management
- See test suite for comprehensive validation of concurrent scenarios

## Contributing

Contributions are welcome! Areas for improvement:
- Adaptive node sizes based on workload
- Lock-free read operations
- Bulk loading optimization
- Persistence layer

## License

MIT License - see LICENSE file for details

## References

- [B+Tree Wikipedia](https://en.wikipedia.org/wiki/B%2B_tree)
- Latch Coupling: "Database System Concepts" by Silberschatz, Korth, and Sudarshan
- Concurrent Data Structures: "The Art of Multiprocessor Programming" by Herlihy and Shavit
