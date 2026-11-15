package btree

import (
	"reflect"
	"sync"
	"unsafe"
)

// Nodes start on a cache line.
//
// The search reads a node's header and inline keys, which for 2-byte inline
// keys are exactly 128 bytes: two cache lines, if the node starts on a line.
// A node allocated with new does not. Go puts an 8-byte malloc header in front
// of every object over 512 bytes that holds pointers, so every such node
// starts 8 bytes past a line, and its first 128 bytes span three lines.
//
// A slab carves nodes out of large chunks instead. An object over 32 KiB gets
// its own span, starts on a page, and has no malloc header (the collector keeps
// its type in the span), and the slab pads each node to a multiple of 64 bytes,
// so every node in a chunk starts on a line. Measured at 10M uint64 keys this
// made single lookups 5% faster and batched ones 13%.
//
// Padding also replaces Go's size-class rounding, which a slab node doesn't
// pay: a string leaf of 1,568 bytes takes 1,600 in a slab against 1,792 on its
// own. A chunk stays alive while any node in it does; nodes are freed only
// with their whole tree, by Clear, so that costs nothing.
//
// A tree's first slabAfter nodes are allocated one by one, so that small trees
// don't each hold a chunk. Their top levels are the only misaligned nodes, and
// a busy tree keeps those in cache.
type slab[T any] struct {
	mu    sync.Mutex
	count int            // nodes allocated one by one so far
	chunk reflect.Type   // an array of padded nodes
	base  unsafe.Pointer // the current chunk
	next  int            // the next free node in it
}

const (
	slabAfter = 64
	slabChunk = 64 << 10 // bytes in a chunk, at least
)

func (s *slab[T]) alloc() *T {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.count < slabAfter {
		s.count++
		return new(T)
	}
	if s.chunk == nil {
		s.chunk = chunkType[T]()
	}
	if s.base == nil || s.next == s.chunk.Len() {
		s.base, s.next = reflect.New(s.chunk).UnsafePointer(), 0
	}
	p := (*T)(unsafe.Add(s.base, uintptr(s.next)*s.chunk.Elem().Size()))
	s.next++
	return p
}

// chunkType is an array of T, each padded to a multiple of 64 bytes, at least
// slabChunk bytes long. reflect builds the padded type, so the collector knows
// where every node's pointers are.
func chunkType[T any]() reflect.Type {
	t := reflect.TypeFor[T]()
	elem := t
	if pad := -t.Size() & 63; pad != 0 {
		elem = reflect.StructOf([]reflect.StructField{
			{Name: "Node", Type: t},
			{Name: "Pad", Type: reflect.ArrayOf(int(pad), reflect.TypeFor[byte]())},
		})
	}
	// Go rounds a large object up to whole 8 KiB pages, so size the chunk to a
	// whole number of pages: n nodes, n a multiple of the nodes that fill
	// pages exactly. A 64 KiB chunk of 1,472-byte UUID leaves would otherwise
	// waste 11% of every chunk.
	stride := elem.Size()
	unit := heapPage / gcd(stride, heapPage)
	n := (slabChunk + stride - 1) / stride
	n = (n + unit - 1) / unit * unit
	return reflect.ArrayOf(int(n), elem)
}

// heapPage is the Go heap's page size, the unit a large object is rounded to.
const heapPage = 8 << 10

func gcd(a, b uintptr) uintptr {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
