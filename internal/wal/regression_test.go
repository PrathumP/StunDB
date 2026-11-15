package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// TestAdvTornTailLosesLaterAckedWrites: a crash leaves a torn entry at the end
// of the WAL. Reopening recovers the entries before it, as DURABILITY.md says.
// But the torn bytes are left in the file and the WAL is opened O_APPEND, so
// every write acknowledged after that restart lands behind the garbage, and
// the next restart's replay stops at the garbage: those writes are lost, even
// with SyncAlways.
func TestAdvTornTailLosesLaterAckedWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "torn.wal")
	open := func() *DurableBTree {
		db, err := NewDurableBTree(DurableConfig{WALPath: path, SyncMode: SyncAlways})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}

	db := open()
	for _, k := range []string{"a", "b"} {
		if err := db.Insert([]byte(k), []byte("v-"+k)); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	// Crash in the middle of appending "c": only part of its entry reached
	// the disk (simulated by appending the first 11 bytes of a real entry).
	full, _ := os.ReadFile(path)
	db = open()
	db.Insert([]byte("c"), []byte("v-c"))
	db.Close()
	withC, _ := os.ReadFile(path)
	entryC := withC[len(full):]
	if err := os.WriteFile(path, append(full, entryC[:11]...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restart 1: recovers a, b (c was never acknowledged). Then acknowledge d.
	db = open()
	t.Logf("restart 1: Count = %d (want 2)", db.Count())
	if err := db.Insert([]byte("d"), []byte("v-d")); err != nil {
		t.Fatal(err)
	}
	if v, err := db.Find([]byte("d")); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("restart 1: d acknowledged with SyncAlways, Find(d) = %q", v)
	}
	db.Close()

	// Restart 2: d was acknowledged and fsynced before this restart.
	db = open()
	defer db.Close()
	v, err := db.Find([]byte("d"))
	t.Logf("restart 2: Count = %d, Find(d) = %q, %v", db.Count(), v, err)
	if err != nil {
		t.Errorf("acknowledged write d lost after restart: %v", err)
	}
}

// TestAdvBulkInsertVisiblePartially: BulkInsert says "atomic batch"; readers
// of the DurableBTree now take no lock, so they observe a half-applied batch.
func TestAdvBulkInsertVisiblePartially(t *testing.T) {
	db, err := NewDurableBTree(DurableConfig{WALPath: filepath.Join(t.TempDir(), "b.wal"), SyncMode: SyncNone})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const n = 200000
	keys := make([]Keytype, n)
	vals := make([]Valuetype, n)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("k%07d", i))
		vals[i] = []byte("v")
	}
	var stop atomic.Bool
	var partial []int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			if c := db.Count(); c > 0 && c < n && len(partial) < 5 {
				partial = append(partial, c)
			}
		}
	}()
	if err := db.BulkInsert(keys, vals); err != nil {
		t.Fatal(err)
	}
	stop.Store(true)
	wg.Wait()
	t.Logf("Count values seen by a reader during one BulkInsert of %d keys: %v", n, partial)
	if len(partial) > 0 {
		t.Errorf("reader saw a partially applied BulkInsert (%d of %d keys)", partial[0], n)
	}
}

// TestAdvForEachDivergesFromWAL: DurableBTree.ForEach hands out the stored
// value slices; writing to one changes what Find serves without a WAL record,
// so the state served before a restart differs from the state after it.
func TestAdvForEachDivergesFromWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fe.wal")
	db, err := NewDurableBTree(DurableConfig{WALPath: path, SyncMode: SyncAlways})
	if err != nil {
		t.Fatal(err)
	}
	db.Insert([]byte("k"), []byte("original"))
	db.ForEach(func(k Keytype, v Valuetype) bool { copy(v, "MUTATED!"); return true })
	before, _ := db.Find([]byte("k"))
	db.Close()
	db, err = NewDurableBTree(DurableConfig{WALPath: path, SyncMode: SyncAlways})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	after, _ := db.Find([]byte("k"))
	t.Logf("Find before restart = %q, after restart = %q", before, after)
	if string(before) != "original" {
		t.Errorf("ForEach callback changed the stored value: %q", before)
	}
}
