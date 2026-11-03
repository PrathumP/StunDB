package btree

import "unsafe"

// Software prefetches, for the batched lanes (batch.go). Go has no prefetch
// intrinsic, so these are three instructions of assembly each; a call costs a
// nanosecond or two, against the ~100 ns miss it hides.

//go:noescape
func prefetch(addr unsafe.Pointer)

// prefetch2 fetches the line at addr and the one after it.
//
//go:noescape
func prefetch2(addr unsafe.Pointer)

// prefetch4 fetches the line at addr and the three after it.
//
//go:noescape
func prefetch4(addr unsafe.Pointer)
