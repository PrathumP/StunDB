//go:build race

package btree

import "sync"

// raceLock serializes writers against readers in race-detector builds.
//
// Optimistic readers read nodes that writers are changing, and throw away
// what they read when the node's version says it changed. That is the design
// (the blog's version lock), and it is a data race by the Go memory model's
// definition, which the race detector reports. So under -race the map runs
// readers under a read lock and writers under a write lock: the layers above
// get a race-clean map, and the tree's own concurrency is tested by the
// stress tests in ordinary builds.
type raceLock struct{ sync.RWMutex }

// RaceSerialized reports whether Map operations are serialized by a lock
// (race-detector builds) instead of running on the optimistic paths.
const RaceSerialized = true
