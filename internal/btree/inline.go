package btree

import "unsafe"

// word is the type of one inline key as a node stores it: 2 bytes for keys
// that compare in the node (uint64, [16]byte), 4 bytes for strings, whose
// ties cost a read of the heap (the blog: "2 bytes a key for integers and
// UUIDs, 4 to 8 for strings").
type word interface{ ~uint16 | ~uint32 }

func wordBits[W word]() uint {
	var w W
	return uint(unsafe.Sizeof(w)) * 8
}

// toInline turns a key's 64-bit window into the inline key a node stores: its
// top bits, with the sign bit flipped. The flip makes a signed comparison of
// two stored inline keys order them as unsigned windows, and AVX2 only
// compares signed integers.
func toInline[W word](window uint64) W {
	var w W
	b := unsafe.Sizeof(w) * 8
	return W(window>>(64-b)) ^ W(1)<<(b-1)
}

// padInline is the stored form of the greatest window. Slots past a node's
// count hold it, so that a count over every slot never counts them as smaller
// than the target, and the search needs no length-dependent loop.
func padInline[W word]() W {
	b := wordBits[W]()
	return W(1)<<(b-1) - 1
}

// countLess counts the inline keys smaller than t over all of a node's slots.
// That count is where t belongs: the child to follow in an internal node, the
// first candidate in a leaf. It has no branches and no dependent loads; the
// blog's "Binary search vs linear search, again".
//
// It is one call, so that it inlines into every node search, leaving
// countSlots as the search's only call.
func countLess[W word](ik *[slots]W, t W) int {
	return countSlots(unsafe.Pointer(ik), uint32(t), wordBits[W]() == 32)
}

// The scalar counts: the reference the other counts are tested against. Each
// compare is a subtraction whose sign bit is the answer.

func count16Scalar(ik *[slots]uint16, t uint16) int {
	tt := uint32(t ^ 0x8000)
	c := 0
	for i := range ik {
		c += int((uint32(ik[i]^0x8000) - tt) >> 31)
	}
	return c
}

func count32Scalar(ik *[slots]uint32, t uint32) int {
	tt := uint64(t ^ 0x8000_0000)
	c := 0
	for i := range ik {
		c += int((uint64(ik[i]^0x8000_0000) - tt) >> 63)
	}
	return c
}

// count16Branchless is the count as a branchless binary search (Khuong and
// Morin, "Array Layouts for Comparison-Based Searching"): the inline keys are
// sorted, padding included, so the number below t is a lower bound, found in
// six halvings whose comparison picks the next base with a conditional move.
// This is bbtree's search for targets without vector registers.
func count16Branchless(ik *[slots]uint16, t uint16) int {
	tt := t ^ 0x8000
	base, n := 0, slots
	for n > 1 {
		half := n >> 1
		if ik[base+half-1]^0x8000 < tt {
			base += half
		}
		n -= half
	}
	if ik[base]^0x8000 < tt {
		base++
	}
	return base
}

func count32Branchless(ik *[slots]uint32, t uint32) int {
	tt := t ^ 0x8000_0000
	base, n := 0, slots
	for n > 1 {
		half := n >> 1
		if ik[base+half-1]^0x8000_0000 < tt {
			base += half
		}
		n -= half
	}
	if ik[base]^0x8000_0000 < tt {
		base++
	}
	return base
}
