package btree

import (
	"fmt"
	"testing"
	"unsafe"
)

// TestNodesAligned checks that, past the first slabAfter nodes of each kind,
// every node starts on a cache line, for each key type. It guards a
// performance property: if a Go release stops page-aligning large objects,
// lookups stay correct and get slower, and only this test notices.
func TestNodesAligned(t *testing.T) {
	t.Run("u64", func(t *testing.T) {
		m := New[uint64, uint64]()
		for i := range 300_000 {
			m.Insert(mix64(uint64(i)), 1)
		}
		checkAligned(t, m.fixed)
	})
	t.Run("uuid", func(t *testing.T) {
		m := New[[16]byte, uint64]()
		for i := range 300_000 {
			var k [16]byte
			*(*uint64)(unsafe.Pointer(&k)) = mix64(uint64(i))
			m.Insert(k, 1)
		}
		checkAligned(t, m.fixed)
	})
	t.Run("string", func(t *testing.T) {
		m := New[string, []byte]()
		for i := range 300_000 {
			m.Insert(fmt.Sprintf("user:%016x", mix64(uint64(i))), nil)
		}
		checkAligned(t, m.str)
	})
}

func checkAligned[K Key, V any, W word](t *testing.T, tr *tree[K, V, W]) {
	t.Helper()
	var nodes, off int
	var walk func(h *header)
	walk = func(h *header) {
		nodes++
		if uintptr(unsafe.Pointer(h))%64 != 0 {
			off++
		}
		if h.level == 0 {
			return
		}
		in := asInner[K, V, W](h)
		for i := 0; i <= int(in.n); i++ {
			walk(in.children[i])
		}
	}
	walk(tr.root.Load())
	// The first slabAfter leaves and internal nodes, and the first root leaf,
	// are allocated one by one.
	if limit := 2*slabAfter + 1; off > limit {
		t.Fatalf("%d of %d nodes don't start on a cache line; at most %d may", off, nodes, limit)
	}
	if nodes < 10*slabAfter {
		t.Fatalf("only %d nodes; the test needs a bigger tree", nodes)
	}
}

// TestChunkFillsPages checks that a chunk is a whole number of heap pages, so
// Go's rounding of large objects wastes nothing.
func TestChunkFillsPages(t *testing.T) {
	check := func(name string, ct interface{ Size() uintptr }) {
		if ct.Size()%heapPage != 0 || ct.Size() < slabChunk {
			t.Errorf("%s: chunk of %d bytes is not a whole number of %d-byte pages of at least %d", name, ct.Size(), heapPage, slabChunk)
		}
	}
	check("u64 leaf", chunkType[leaf[uint64, uint64, uint16]]())
	check("uuid leaf", chunkType[leaf[[16]byte, uint64, uint16]]())
	check("string leaf", chunkType[leaf[string, uint64, uint32]]())
	check("string inner", chunkType[inner[string, uint64, uint32]]())
	check("odd value", chunkType[leaf[uint64, [3]byte, uint16]]())
}
