//go:build goexperiment.simd && amd64

package btree

import (
	"math/bits"
	"simd/archsimd"
	"unsafe"
)

// The SIMD counts (GOEXPERIMENT=simd on amd64): the blog's child_index, on
// AVX2. One VPCMPGTW compares the target with 16 inline keys and yields a mask,
// a lane of ones per smaller key; VPMOVMSKB packs the mask's bytes into an
// integer, and POPCNT counts them.
//
// AVX2 has no 16- or 32-bit movemask (VPMOVW2M is AVX-512), so masks are
// merged byte by byte before the byte movemask: each lane's bytes are taken
// from different masks, so that every set bit of the result is one smaller key.
//
// Each count ends with VZEROUPPER. Go's own code uses legacy SSE encodings
// (the ABI zeroes X15 with XORPS), and on this generation of cores mixing
// those with dirty upper YMM halves costs a state transition: leaving it out
// made batched lookups three times slower.

var hasAVX2 = archsimd.X86.AVX2()

// SIMDEnabled reports whether node searches run in AVX2.
func SIMDEnabled() bool { return hasAVX2 }

var (
	lowBytes16  = archsimd.BroadcastInt16x16(0x00ff)
	byte0of32   = archsimd.BroadcastInt32x8(0x000000ff)
	byte1of32   = archsimd.BroadcastInt32x8(0x0000ff00)
	byte2of32   = archsimd.BroadcastInt32x8(0x00ff0000)
	byte3of32   = archsimd.BroadcastInt32x8(-0x01000000)
	zeroBytes32 = archsimd.BroadcastInt8x32(0)
	zeroBytes16 = archsimd.BroadcastInt8x16(0)
)

// countSlots counts the inline keys below t among a node's slots: 16-bit
// slots, or 32-bit ones if wide.
func countSlots(p unsafe.Pointer, t uint32, wide bool) int {
	if !hasAVX2 {
		if wide {
			return count32Branchless((*[slots]uint32)(p), t)
		}
		return count16Branchless((*[slots]uint16)(p), uint16(t))
	}
	if wide {
		q := (*[slots]int32)(p)
		tv := archsimd.BroadcastInt32x8(int32(t))
		m := func(i int) archsimd.Int32x8 {
			return tv.Greater(archsimd.LoadInt32x8((*[8]int32)(q[i : i+8]))).ToInt32x8()
		}
		a := m(0).And(byte0of32).Or(m(8).And(byte1of32)).Or(m(16).And(byte2of32)).Or(m(24).And(byte3of32))
		b := m(32).And(byte0of32).Or(m(40).And(byte1of32)).Or(m(48).And(byte2of32))
		c := bits.OnesCount32(a.AsInt8x32().Less(zeroBytes32).ToBits()) +
			bits.OnesCount32(b.AsInt8x32().Less(zeroBytes32).ToBits())
		archsimd.ClearAVXUpperBits()
		return c
	}
	q := (*[slots]int16)(p)
	tv := archsimd.BroadcastInt16x16(int16(t))
	m0 := tv.Greater(archsimd.LoadInt16x16((*[16]int16)(q[0:16]))).ToInt16x16()
	m1 := tv.Greater(archsimd.LoadInt16x16((*[16]int16)(q[16:32]))).ToInt16x16()
	m2 := tv.Greater(archsimd.LoadInt16x16((*[16]int16)(q[32:48]))).ToInt16x16()
	m3 := archsimd.BroadcastInt16x8(int16(t)).Greater(archsimd.LoadInt16x8((*[8]int16)(q[48:56]))).ToInt16x8()
	pair := m0.And(lowBytes16).Or(m1.AndNot(lowBytes16)) // a bit per lane of m0 and of m1
	c01 := bits.OnesCount32(pair.AsInt8x32().Less(zeroBytes32).ToBits())
	c2 := bits.OnesCount32(m2.AsInt8x32().Less(zeroBytes32).ToBits()) // two bits per lane
	c3 := bits.OnesCount16(m3.AsInt8x16().Less(zeroBytes16).ToBits()) // two bits per lane
	archsimd.ClearAVXUpperBits()
	return c01 + (c2+c3)>>1
}
