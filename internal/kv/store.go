// Package kv is StunDB's byte-keyed store: the concurrent B+-tree of package
// btree with []byte keys and values, and the conveniences the layers above it
// (the write-ahead log, secondary indexes) need: copies in and out, inclusive
// ranges, a count.
//
// It replaces the hash-sharded wrapper StunDB used to put around its old
// single-lock tree. Sharding was there for write concurrency; the tree now
// latches one node at a time and reads take no latch at all, so one tree
// scales across cores by itself, and range scans walk it in order instead of
// gathering from every shard and sorting.
package kv

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"sync/atomic"

	"github.com/prathump/stundb/internal/btree"
)

// Keytype and Valuetype are byte slices, so the store holds arbitrary binary
// data, independent of any serialization format.
type (
	Keytype   = []byte
	Valuetype = []byte
)

// ErrKeyNotFound is returned by Find for a key the store doesn't hold.
var ErrKeyNotFound = errors.New("key not found")

// Store is a concurrent ordered map from byte keys to byte values. Every
// method is safe for concurrent use.
type Store struct {
	m     *btree.Map[string, []byte]
	count stripedCounter
}

// Stats summarizes a store.
type Stats struct {
	TotalKeys int64
	Height    int
}

// New returns an empty store.
func New() *Store {
	return &Store{m: btree.New[string, []byte]()}
}

// Insert sets key to value. Both are copied: the caller may reuse them.
func (s *Store) Insert(key Keytype, value Valuetype) {
	if _, replaced := s.m.Insert(string(key), bytes.Clone(value)); !replaced {
		s.count.add(1)
	}
}

// Put is an alias for Insert.
func (s *Store) Put(key Keytype, value Valuetype) { s.Insert(key, value) }

// Find returns a copy of key's value, or ErrKeyNotFound.
func (s *Store) Find(key Keytype) (Valuetype, error) {
	v, ok := s.m.Get(btree.BytesKey(key))
	if !ok {
		return nil, ErrKeyNotFound
	}
	return bytes.Clone(v), nil
}

// Get is an alias for Find.
func (s *Store) Get(key Keytype) (Valuetype, error) { return s.Find(key) }

// Contains reports whether key is in the store.
func (s *Store) Contains(key Keytype) bool { return s.m.Contains(btree.BytesKey(key)) }

// Delete removes key and reports whether it was there.
func (s *Store) Delete(key Keytype) bool {
	if _, ok := s.m.Remove(btree.BytesKey(key)); ok {
		s.count.add(-1)
		return true
	}
	return false
}

// GetRange returns copies of the entries with startKey <= key <= endKey, in
// key order.
func (s *Store) GetRange(startKey, endKey Keytype) ([]Keytype, []Valuetype, error) {
	if bytes.Compare(startKey, endKey) > 0 {
		return nil, nil, errors.New("invalid range: startKey is greater than endKey")
	}
	var keys []Keytype
	var vals []Valuetype
	end := btree.BytesKey(endKey)
	s.m.Ascend(string(startKey), func(k string, v []byte) bool {
		if k > end {
			return false
		}
		keys = append(keys, []byte(k))
		vals = append(vals, bytes.Clone(v))
		return true
	})
	return keys, vals, nil
}

// Scan calls fn on each entry with key >= startKey, in key order, until fn
// returns false. fn must not keep or change the slices it is given.
func (s *Store) Scan(startKey Keytype, fn func(key Keytype, value Valuetype) bool) {
	s.m.Ascend(string(startKey), func(k string, v []byte) bool {
		return fn([]byte(k), v)
	})
}

// DeleteRange deletes the entries with startKey <= key <= endKey and returns
// how many it deleted. It is not atomic with respect to concurrent writers.
func (s *Store) DeleteRange(startKey, endKey Keytype) (int, error) {
	keys, _, err := s.GetRange(startKey, endKey)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range keys {
		if s.Delete(k) {
			n++
		}
	}
	return n, nil
}

// BulkInsert inserts keys[i] with values[i], with the tree's batched insert:
// the descents of a batch overlap their waits for memory.
func (s *Store) BulkInsert(keys []Keytype, values []Valuetype) error {
	if len(keys) != len(values) {
		return errors.New("keys and values must have the same length")
	}
	const batch = 16
	ks := make([]string, 0, batch)
	vs := make([][]byte, 0, batch)
	for i := 0; i < len(keys); i += batch {
		ks, vs = ks[:0], vs[:0]
		for j := i; j < min(i+batch, len(keys)); j++ {
			ks = append(ks, string(keys[j]))
			vs = append(vs, bytes.Clone(values[j]))
		}
		s.count.add(int64(s.m.InsertMany(ks, vs)))
	}
	return nil
}

// Count returns the number of entries.
func (s *Store) Count() int64 { return s.count.sum() }

// ForEach calls fn on every entry in key order until fn returns false. fn must
// not keep or change the slices it is given.
func (s *Store) ForEach(fn func(key Keytype, value Valuetype) bool) {
	s.m.All(func(k string, v []byte) bool { return fn([]byte(k), v) })
}

// Clear removes every entry. Writers running concurrently with it may land
// in the old contents.
func (s *Store) Clear() {
	s.m.Clear()
	s.count.reset()
}

// Stats returns the store's size and the tree's height.
func (s *Store) Stats() Stats {
	return Stats{TotalKeys: s.Count(), Height: s.m.Height()}
}

// stripedCounter is a count that many goroutines add to without sharing a
// cache line: each add lands on a random stripe, and a read sums them. One
// shared counter would be one line every insert on every core writes, the
// coherence traffic the tree itself is built to avoid.
type stripedCounter struct {
	stripes [16]struct {
		n atomic.Int64
		_ [56]byte
	}
}

func (c *stripedCounter) add(d int64) {
	if d != 0 {
		c.stripes[rand.Uint32()%uint32(len(c.stripes))].n.Add(d)
	}
}

func (c *stripedCounter) sum() int64 {
	var t int64
	for i := range c.stripes {
		t += c.stripes[i].n.Load()
	}
	return t
}

func (c *stripedCounter) reset() {
	for i := range c.stripes {
		c.stripes[i].n.Store(0)
	}
}
