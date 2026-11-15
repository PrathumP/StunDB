//go:build 386 || arm || mips || mipsle

package btree

import "reflect"

// isU64 reports whether K is a uint64 key. On 32-bit platforms a string is 8
// bytes, as a uint64 is, with the same alignment, so size can't tell them
// apart; the type's kind can. This costs a call on every key operation, a price
// paid only on 32-bit platforms, which the tree is not tuned for.
func isU64[K Key]() bool {
	return reflect.TypeFor[K]().Kind() == reflect.Uint64
}
