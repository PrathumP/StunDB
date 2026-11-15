package index

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

// consistency checks that every index entry points at a live record whose
// field matches, and every record with the field is indexed under it.
func consistency(t *testing.T, db *IndexedBTree, name string, ext KeyExtractor) []string {
	t.Helper()
	db.mu.RLock()
	idx := db.indexes[name]
	db.mu.RUnlock()
	var problems []string
	idx.tree.ForEach(func(ik, v []byte) bool {
		var pks [][]byte
		if idx.unique {
			pks = [][]byte{bytes.Clone(v)}
		} else {
			pks = decodePrimaryKeys(v)
		}
		for _, pk := range pks {
			rec, err := db.Find(pk)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s[%q] -> %q, but %q does not exist", name, ik, pk, pk))
			} else if f := ext(rec); !bytes.Equal(f, ik) {
				problems = append(problems, fmt.Sprintf("%s[%q] -> %q, but %q has %s=%q", name, ik, pk, pk, name, f))
			}
		}
		return true
	})
	db.ForEach(func(k, v []byte) bool {
		f := ext(v)
		if f == nil {
			return true
		}
		pks, err := idx.FindAll(f)
		found := false
		for _, pk := range pks {
			found = found || bytes.Equal(pk, k)
		}
		if err != nil || !found {
			problems = append(problems, fmt.Sprintf("record %q has %s=%q but is not indexed under it", k, name, f))
		}
		return true
	})
	return problems
}

// TestAdvPutOverwriteLeavesStaleEntries: Insert/Put of an existing primary key
// overwrites the record but never removes the old record's index entries.
func TestAdvPutOverwriteLeavesStaleEntries(t *testing.T) {
	db := NewIndexedBTreeDefault()
	email, age := JSONFieldExtractor("email"), JSONFieldExtractor("age")
	db.CreateIndex("email", email, true)
	db.CreateIndex("age", age, false)

	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.Put([]byte("u1"), []byte(`{"email":"a@x","age":30}`)))
	must(db.Put([]byte("u1"), []byte(`{"email":"b@x","age":31}`))) // overwrite u1

	pk, err := db.FindByIndex("email", []byte("a@x"))
	t.Logf("FindByIndex(email, a@x) = %q, %v   (u1 now has email b@x)", pk, err)
	pks, err := db.FindAllByIndex("age", []byte("30"))
	t.Logf("FindAllByIndex(age, 30) = %q, %v   (u1 now has age 31)", pks, err)
	t.Logf("index Count: email=%d age=%d, records=%d", db.indexes["email"].Count(), db.indexes["age"].Count(), db.Count())

	err = db.Put([]byte("u2"), []byte(`{"email":"a@x","age":40}`))
	t.Logf("Put(u2, email=a@x) when no record has a@x: %v", err)

	_, err = db.Delete([]byte("u1"))
	must(err)
	pk, err = db.FindByIndex("email", []byte("a@x"))
	t.Logf("after Delete(u1): FindByIndex(email, a@x) = %q, %v", pk, err)

	for _, p := range append(consistency(t, db, "email", email), consistency(t, db, "age", age)...) {
		t.Error(p)
	}
}

// TestAdvPutSameRecordRejected: re-putting a record with its own unchanged
// unique field fails, because Insert's unique check finds the record itself.
func TestAdvPutSameRecordRejected(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)
	if err := db.Put([]byte("u1"), []byte(`{"email":"a@x","age":30}`)); err != nil {
		t.Fatal(err)
	}
	err := db.Put([]byte("u1"), []byte(`{"email":"a@x","age":31}`))
	v, _ := db.Find([]byte("u1"))
	t.Logf("Put(u1, same email, new age) = %v; u1 = %s", err, v)
	if err != nil {
		t.Errorf("upsert of the same record with the same unique value rejected: %v", err)
	}
}

// TestAdvConcurrentInsertSameEmail: goroutines insert distinct records that
// share a unique email, with a second (non-unique) index present. The losers
// roll back their primary record but not the entries they already added to
// the other index.
func TestAdvConcurrentInsertSameEmail(t *testing.T) {
	email, age := JSONFieldExtractor("email"), JSONFieldExtractor("age")
	for round := 0; round < 3000; round++ {
		db := NewIndexedBTreeDefault()
		db.CreateIndex("email", email, true)
		db.CreateIndex("age", age, false)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				db.Insert([]byte(fmt.Sprintf("u%d", g)), []byte(fmt.Sprintf(`{"email":"same@x","age":%d}`, 20+g)))
			}()
		}
		close(start)
		wg.Wait()
		if p := append(consistency(t, db, "email", email), consistency(t, db, "age", age)...); len(p) > 0 {
			t.Fatalf("round %d: records=%d\n  %s", round, db.Count(), joinLines(p))
		}
	}
}

// TestAdvConcurrentUpdateSameKey: two goroutines Update the same record to
// different emails. Both remove the old entry and add their own, so the
// index ends with an entry for a value the record no longer has.
func TestAdvConcurrentUpdateSameKey(t *testing.T) {
	email := JSONFieldExtractor("email")
	for round := 0; round < 3000; round++ {
		db := NewIndexedBTreeDefault()
		db.CreateIndex("email", email, true)
		db.Insert([]byte("u1"), []byte(`{"email":"old@x"}`))
		var wg sync.WaitGroup
		start := make(chan struct{})
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				db.Update([]byte("u1"), []byte(fmt.Sprintf(`{"email":"new%d@x"}`, g)))
			}()
		}
		close(start)
		wg.Wait()
		if p := consistency(t, db, "email", email); len(p) > 0 {
			t.Fatalf("round %d:\n  %s", round, joinLines(p))
		}
	}
}

// TestAdvRebuildRacesInsert: CreateIndexWithRebuild registers the index and
// then scans; a record inserted concurrently is indexed by Insert and then
// again by the scan, and the unique index rejects it as a duplicate of itself,
// failing the whole rebuild.
func TestAdvRebuildRacesInsert(t *testing.T) {
	email := JSONFieldExtractor("email")
	fails := 0
	var firstErr error
	for round := 0; round < 200; round++ {
		db := NewIndexedBTreeDefault()
		for i := 0; i < 3000; i++ {
			db.Insert([]byte(fmt.Sprintf("u%05d", i)), []byte(fmt.Sprintf(`{"email":"e%d@x"}`, i)))
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 200; i++ {
				db.Insert([]byte(fmt.Sprintf("u9%04d", i)), []byte(fmt.Sprintf(`{"email":"late%d@x"}`, i)))
			}
		}()
		close(start)
		err := db.CreateIndexWithRebuild("email", email, true)
		wg.Wait()
		if err != nil {
			fails++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	t.Logf("CreateIndexWithRebuild failed in %d of 200 rounds; first error: %v (all emails are distinct)", fails, firstErr)
	if fails > 0 {
		t.Errorf("unique rebuild over distinct values failed %d times", fails)
	}
}

// TestAdvRebuildControl: the same rebuild with no concurrent writer succeeds.
func TestAdvRebuildControl(t *testing.T) {
	db := NewIndexedBTreeDefault()
	for i := 0; i < 3000; i++ {
		db.Insert([]byte(fmt.Sprintf("u%05d", i)), []byte(fmt.Sprintf(`{"email":"e%d@x"}`, i)))
	}
	if err := db.CreateIndexWithRebuild("email", JSONFieldExtractor("email"), true); err != nil {
		t.Fatal(err)
	}
}

// TestAdvFailedPutDeletesExistingRecord: u1 exists. Put(u1, email=same) races
// Put(u2, email=same). When u2 wins the unique index, u1's Put "rolls back" by
// deleting u1 from the primary store, so a rejected write destroys the record
// that was there before it.
func TestAdvFailedPutDeletesExistingRecord(t *testing.T) {
	email := JSONFieldExtractor("email")
	lost := 0
	var first []string
	for round := 0; round < 500; round++ {
		db := NewIndexedBTreeDefault()
		db.CreateIndex("email", email, true)
		db.Insert([]byte("u1"), []byte(`{"email":"orig@x"}`))
		var wg sync.WaitGroup
		start := make(chan struct{})
		var err1 error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; err1 = db.Put([]byte("u1"), []byte(`{"email":"same@x"}`)) }()
		go func() { defer wg.Done(); <-start; db.Put([]byte("u2"), []byte(`{"email":"same@x"}`)) }()
		close(start)
		wg.Wait()
		if _, err := db.Find([]byte("u1")); err != nil {
			lost++
			if first == nil {
				first = append([]string{fmt.Sprintf("round %d: Put(u1) returned %v; u1 now: %v", round, err1, err)}, consistency(t, db, "email", email)...)
			}
		}
	}
	if lost > 0 {
		t.Errorf("pre-existing record u1 deleted by a rejected Put in %d of 500 rounds\n  %s", lost, joinLines(first))
	}
}

// TestAdvFailedUpdateLeavesRecordUnindexed: Update(u1 -> email z) races
// Insert(u2, email z). When u2 wins the index, Update returns an error, but
// u1's primary value is already z and its old entry is already removed: two
// records hold z, and u1 is not indexed at all.
func TestAdvFailedUpdateLeavesRecordUnindexed(t *testing.T) {
	email := JSONFieldExtractor("email")
	for round := 0; round < 2000; round++ {
		db := NewIndexedBTreeDefault()
		db.CreateIndex("email", email, true)
		db.Insert([]byte("u1"), []byte(`{"email":"a@x"}`))
		var wg sync.WaitGroup
		start := make(chan struct{})
		var errU error
		wg.Add(2)
		go func() { defer wg.Done(); <-start; errU = db.Update([]byte("u1"), []byte(`{"email":"z@x"}`)) }()
		go func() { defer wg.Done(); <-start; db.Insert([]byte("u2"), []byte(`{"email":"z@x"}`)) }()
		close(start)
		wg.Wait()
		if p := consistency(t, db, "email", email); len(p) > 0 {
			v1, _ := db.Find([]byte("u1"))
			v2, e2 := db.Find([]byte("u2"))
			t.Fatalf("round %d: Update err=%v; u1=%s u2=%s(%v)\n  %s", round, errU, v1, v2, e2, joinLines(p))
		}
	}
}

func joinLines(p []string) string {
	var b bytes.Buffer
	for i, s := range p {
		if i > 0 {
			b.WriteString("\n  ")
		}
		b.WriteString(s)
	}
	return b.String()
}
