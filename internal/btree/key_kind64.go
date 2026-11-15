//go:build !(386 || arm || mips || mipsle)

package btree

import "unsafe"

// isU64 reports whether K is a uint64 key. On 64-bit platforms a string is 16
// bytes, so the only 8-byte key is a uint64, and the test is a constant.
func isU64[K Key]() bool {
	var k K
	return unsafe.Sizeof(k) == 8
}
