package kv

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestStoreBasics(t *testing.T) {
	s := New()
	key, val := []byte("k1"), []byte("v1")
	s.Insert(key, val)
	key[0], val[0] = 'x', 'x' // the store kept copies
	got, err := s.Find([]byte("k1"))
	if err != nil || string(got) != "v1" {
		t.Fatalf("Find = %q, %v", got, err)
	}
	got[0] = 'z' // and hands out copies
	if again, _ := s.Find([]byte("k1")); string(again) != "v1" {
		t.Fatalf("Find after caller changed its copy = %q", again)
	}
	if _, err := s.Find([]byte("nope")); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Find missing: %v", err)
	}
	s.Insert([]byte("k1"), []byte("v2"))
	if s.Count() != 1 {
		t.Fatalf("Count = %d after replace", s.Count())
	}
	if !s.Delete([]byte("k1")) || s.Delete([]byte("k1")) || s.Count() != 0 {
		t.Fatal("Delete")
	}
	s.Insert([]byte{}, []byte{}) // empty key and value are allowed
	if v, err := s.Find(nil); err != nil || v == nil || len(v) != 0 {
		t.Fatalf("empty key: %q, %v", v, err)
	}
}

func TestStoreRanges(t *testing.T) {
	s := New()
	for i := range 1000 {
		s.Insert(fmt.Appendf(nil, "key:%04d", i), fmt.Appendf(nil, "%d", i))
	}
	keys, vals, err := s.GetRange([]byte("key:0100"), []byte("key:0199"))
	if err != nil || len(keys) != 100 || string(keys[0]) != "key:0100" || string(keys[99]) != "key:0199" || string(vals[5]) != "105" {
		t.Fatalf("GetRange: %d keys, %v", len(keys), err)
	}
	if _, _, err := s.GetRange([]byte("b"), []byte("a")); err == nil {
		t.Fatal("GetRange accepted start > end")
	}
	n, err := s.DeleteRange([]byte("key:0500"), []byte("key:0599"))
	if err != nil || n != 100 || s.Count() != 900 {
		t.Fatalf("DeleteRange = %d, %v; count %d", n, err, s.Count())
	}
	prev := ""
	seen := 0
	s.ForEach(func(k, _ []byte) bool {
		if string(k) <= prev {
			t.Fatalf("ForEach out of order: %q after %q", k, prev)
		}
		prev = string(k)
		seen++
		return true
	})
	if seen != 900 {
		t.Fatalf("ForEach saw %d", seen)
	}
	s.Clear()
	if s.Count() != 0 || s.Contains([]byte("key:0001")) {
		t.Fatal("Clear")
	}
}

func TestStoreBulkAndConcurrentCount(t *testing.T) {
	s := New()
	var keys, vals [][]byte
	for i := range 5000 {
		keys = append(keys, fmt.Appendf(nil, "bulk:%06d", i))
		vals = append(vals, []byte{byte(i)})
	}
	if err := s.BulkInsert(keys, vals); err != nil || s.Count() != 5000 {
		t.Fatalf("BulkInsert: %v, count %d", err, s.Count())
	}
	if err := s.BulkInsert(keys[:1], nil); err == nil {
		t.Fatal("BulkInsert accepted mismatched lengths")
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 2000 {
				s.Insert(fmt.Appendf(nil, "g%d:%d", g, i), nil)
			}
			for i := range 1000 {
				s.Delete(fmt.Appendf(nil, "g%d:%d", g, i))
			}
		})
	}
	wg.Wait()
	if got := s.Count(); got != 5000+8*1000 {
		t.Fatalf("Count = %d", got)
	}
	if st := s.Stats(); st.TotalKeys != 13000 || st.Height < 2 {
		t.Fatalf("Stats = %+v", st)
	}
}
