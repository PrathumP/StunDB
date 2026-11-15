package btree

import (
	"encoding/binary"
	"math/bits"
	"unsafe"
)

// Key is the set of key types the tree specializes for. Each orders the way
// its big-endian byte encoding does, which is what inline keys and prefixes
// rely on:
//
//   - ~uint64: 8 bytes, big-endian.
//   - ~[16]byte: UUIDs, hashes; the bytes as they are.
//   - ~string: the bytes as they are, ordered as Go orders strings.
//
// Byte-slice keys go through string (see Map.GetBytes and friends): a key in
// an ordered map must not change after it is inserted, which a []byte can't
// promise.
type Key interface {
	~uint64 | ~[16]byte | ~string
}

type keyKind uint8

const (
	kindU64 keyKind = iota
	kindA16
	kindStr
)

// kindOf tells the key types apart. Go compiles a generic function once per
// "GC shape", and uint64, [16]byte and string are three different shapes, so
// within each instantiation unsafe.Sizeof and unsafe.Alignof are constants and
// every `switch kindOf[K]()` folds away: a uint64 tree contains no string code
// and pays nothing to ask. This is how the tree gets per-type node code without
// Rust's monomorphization (the blog's "we specialize the common types by hand").
//
// isU64 is the one test that depends on the platform: see key_kind64.go and
// key_kind32.go.
func kindOf[K Key]() keyKind {
	var k K
	if isU64[K]() {
		return kindU64
	}
	if unsafe.Alignof(k) == 1 {
		return kindA16
	}
	return kindStr
}

func asU64[K Key](k *K) uint64    { return *(*uint64)(unsafe.Pointer(k)) }
func asA16[K Key](k *K) *[16]byte { return (*[16]byte)(unsafe.Pointer(k)) }
func asStr[K Key](k *K) string    { return *(*string)(unsafe.Pointer(k)) }

func a16Words(p *[16]byte) (hi, lo uint64) {
	return binary.BigEndian.Uint64(p[0:8]), binary.BigEndian.Uint64(p[8:16])
}

// keyWindow returns bytes off..off+8 of the key's encoding as a big-endian
// word, zero past the key's end. If two keys share their first off bytes, their
// windows at off order as the keys do, or tie. This is the blog's InlineKey
// contract: `inline_key_at(offset)`.
func keyWindow[K Key](k K, off int) uint64 {
	switch kindOf[K]() {
	case kindU64:
		if off >= 8 {
			return 0
		}
		return asU64(&k) << (8 * uint(off))
	case kindA16:
		hi, lo := a16Words(asA16(&k))
		switch {
		case off == 0:
			return hi
		case off < 8:
			return hi<<(8*uint(off)) | lo>>(64-8*uint(off))
		case off < 16:
			return lo << (8 * uint(off-8))
		}
		return 0
	default:
		return strWindow(asStr(&k), off)
	}
}

// keyCompare orders a and b, which share their first off bytes: string keys
// start comparing there. Fixed-size keys compare whole, in a register or two.
func keyCompare[K Key](a, b K, off int) int {
	switch kindOf[K]() {
	case kindU64:
		x, y := asU64(&a), asU64(&b)
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	case kindA16:
		ah, al := a16Words(asA16(&a))
		bh, bl := a16Words(asA16(&b))
		if ah != bh {
			if ah < bh {
				return -1
			}
			return 1
		}
		if al < bl {
			return -1
		}
		if al > bl {
			return 1
		}
		return 0
	default:
		return strCompareFrom(asStr(&a), asStr(&b), off)
	}
}

func keyLess[K Key](a, b K) bool { return keyCompare(a, b, 0) < 0 }

// keyCPL is the length of the longest common prefix of the two keys'
// encodings, in bytes.
func keyCPL[K Key](a, b K) int {
	switch kindOf[K]() {
	case kindU64:
		return bits.LeadingZeros64(asU64(&a)^asU64(&b)) / 8
	case kindA16:
		ah, al := a16Words(asA16(&a))
		bh, bl := a16Words(asA16(&b))
		if ah != bh {
			return bits.LeadingZeros64(ah^bh) / 8
		}
		return 8 + bits.LeadingZeros64(al^bl)/8
	default:
		return strCPL(asStr(&a), asStr(&b))
	}
}

// ---- strings, a word at a time --------------------------------------------
//
// The blog's "the compiler is not your friend": comparing two short keys with
// memcmp is a function call, so we load 8 bytes at a time as big-endian
// integers and compare those, as Arrow does for StringView.

func strPtr(s string, i int) unsafe.Pointer {
	return unsafe.Add(unsafe.Pointer(unsafe.StringData(s)), i)
}

func load8BE(p unsafe.Pointer) uint64 { return bits.ReverseBytes64(*(*uint64)(p)) }
func load8LE(p unsafe.Pointer) uint64 { return *(*uint64)(p) }

func strWindow(s string, off int) uint64 {
	n := len(s)
	if off+8 <= n {
		return load8BE(strPtr(s, off))
	}
	if off >= n {
		return 0
	}
	if n >= 8 {
		// The last 8 bytes, shifted so that s[off] is on top.
		return load8BE(strPtr(s, n-8)) << (8 * uint(off+8-n))
	}
	var w uint64
	for i := off; i < n; i++ {
		w |= uint64(s[i]) << (56 - 8*uint(i-off))
	}
	return w
}

func strCompareFrom(a, b string, off int) int {
	n := min(len(a), len(b))
	i := off
	for ; i+8 <= n; i += 8 {
		x, y := load8BE(strPtr(a, i)), load8BE(strPtr(b, i))
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	if i < n {
		// Windows are zero past the end, so a tie here is settled by length.
		x, y := strWindow(a[:n], i), strWindow(b[:n], i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func strCPL(a, b string) int {
	n := min(len(a), len(b))
	i := 0
	for ; i+8 <= n; i += 8 {
		if x := load8LE(strPtr(a, i)) ^ load8LE(strPtr(b, i)); x != 0 {
			return i + bits.TrailingZeros64(x)/8
		}
	}
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}
