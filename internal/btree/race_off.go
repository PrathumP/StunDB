//go:build !race

package btree

// raceLock is a no-op outside race builds: the tree synchronizes through its
// node versions.
type raceLock struct{}

func (raceLock) Lock()    {}
func (raceLock) Unlock()  {}
func (raceLock) RLock()   {}
func (raceLock) RUnlock() {}

// RaceSerialized reports whether Map operations are serialized by a lock
// (race-detector builds) instead of running on the optimistic paths.
const RaceSerialized = false
