//go:build !(goexperiment.simd && amd64)

package btree

import "unsafe"

// SIMDEnabled reports whether node searches run in AVX2. Build with
// GOEXPERIMENT=simd on amd64 to enable them.
func SIMDEnabled() bool { return false }

// countSlots, without vector registers, is a branchless binary search over
// the sorted inline keys (count16Branchless), as bbtree does on targets
// without SSE2.
func countSlots(p unsafe.Pointer, t uint32, wide bool) int {
	if wide {
		return count32Branchless((*[slots]uint32)(p), t)
	}
	return count16Branchless((*[slots]uint16)(p), uint16(t))
}
