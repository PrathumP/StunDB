//go:build !amd64

package btree

import "unsafe"

func prefetch(addr unsafe.Pointer)  {}
func prefetch2(addr unsafe.Pointer) {}
func prefetch4(addr unsafe.Pointer) {}
