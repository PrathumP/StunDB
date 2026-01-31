package bptree

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

// ==================== SecondaryIndex Basic Tests ====================

func TestSecondaryIndexCreation(t *testing.T) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "email",
		Extractor: JSONFieldExtractor("email"),
		Unique:    true,
	})

	if idx.Name() != "email" {
		t.Errorf("Expected name 'email', got '%s'", idx.Name())
	}

	if !idx.IsUnique() {
		t.Error("Expected unique index")
	}

	if idx.Count() != 0 {
		t.Errorf("Expected 0 entries, got %d", idx.Count())
	}
}

func TestSecondaryIndexUniqueInsert(t *testing.T) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "email",
		Extractor: JSONFieldExtractor("email"),
		Unique:    true,
	})

	// Insert first entry
	err := idx.Index([]byte("user:1"), []byte(`{"email":"alice@example.com"}`))
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Find it
	pk, err := idx.FindOne([]byte("alice@example.com"))
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	if !bytes.Equal(pk, []byte("user:1")) {
		t.Errorf("Expected 'user:1', got '%s'", pk)
	}

	// Duplicate should fail
	err = idx.Index([]byte("user:2"), []byte(`{"email":"alice@example.com"}`))
	if err == nil {
		t.Error("Expected error for duplicate in unique index")
	}
}

func TestSecondaryIndexNonUniqueInsert(t *testing.T) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "city",
		Extractor: JSONFieldExtractor("city"),
		Unique:    false,
	})

	// Insert multiple with same city
	idx.Index([]byte("user:1"), []byte(`{"city":"NYC"}`))
	idx.Index([]byte("user:2"), []byte(`{"city":"NYC"}`))
	idx.Index([]byte("user:3"), []byte(`{"city":"LA"}`))

	// FindAll should return both NYC users
	pks, err := idx.FindAll([]byte("NYC"))
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}

	if len(pks) != 2 {
		t.Errorf("Expected 2 primary keys, got %d", len(pks))
	}

	// Verify both users are in results
	hasUser1, hasUser2 := false, false
	for _, pk := range pks {
		if bytes.Equal(pk, []byte("user:1")) {
			hasUser1 = true
		}
		if bytes.Equal(pk, []byte("user:2")) {
			hasUser2 = true
		}
	}
	if !hasUser1 || !hasUser2 {
		t.Error("Missing expected primary keys")
	}
}

func TestSecondaryIndexRemove(t *testing.T) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "email",
		Extractor: JSONFieldExtractor("email"),
		Unique:    true,
	})

	// Insert and remove
	idx.Index([]byte("user:1"), []byte(`{"email":"alice@example.com"}`))
	idx.Remove([]byte("user:1"), []byte(`{"email":"alice@example.com"}`))

	// Should not find
	_, err := idx.FindOne([]byte("alice@example.com"))
	if err == nil {
		t.Error("Expected error after removal")
	}
}

func TestSecondaryIndexRemoveNonUnique(t *testing.T) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "city",
		Extractor: JSONFieldExtractor("city"),
		Unique:    false,
	})

	// Insert multiple
	idx.Index([]byte("user:1"), []byte(`{"city":"NYC"}`))
	idx.Index([]byte("user:2"), []byte(`{"city":"NYC"}`))

	// Remove one
	idx.Remove([]byte("user:1"), []byte(`{"city":"NYC"}`))

	// Should still find user:2
	pks, err := idx.FindAll([]byte("NYC"))
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}

	if len(pks) != 1 {
		t.Errorf("Expected 1 primary key, got %d", len(pks))
	}

	if !bytes.Equal(pks[0], []byte("user:2")) {
		t.Errorf("Expected 'user:2', got '%s'", pks[0])
	}
}

func TestSecondaryIndexUpdate(t *testing.T) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "email",
		Extractor: JSONFieldExtractor("email"),
		Unique:    true,
	})

	// Insert
	idx.Index([]byte("user:1"), []byte(`{"email":"alice@example.com"}`))

	// Update
	idx.Update([]byte("user:1"),
		[]byte(`{"email":"alice@example.com"}`),
		[]byte(`{"email":"alice@newdomain.com"}`))

	// Old key should not exist
	_, err := idx.FindOne([]byte("alice@example.com"))
	if err == nil {
		t.Error("Old email should not exist")
	}

	// New key should exist
	pk, err := idx.FindOne([]byte("alice@newdomain.com"))
	if err != nil {
		t.Fatalf("Find new email failed: %v", err)
	}
	if !bytes.Equal(pk, []byte("user:1")) {
		t.Errorf("Expected 'user:1', got '%s'", pk)
	}
}

func TestSecondaryIndexFindRange(t *testing.T) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "name",
		Extractor: JSONFieldExtractor("name"),
		Unique:    true,
	})

	// Insert sorted names
	names := []string{"alice", "bob", "carol", "dave", "eve"}
	for i, name := range names {
		idx.Index(
			[]byte(fmt.Sprintf("user:%d", i)),
			[]byte(fmt.Sprintf(`{"name":"%s"}`, name)),
		)
	}

	// Range query: bob to dave
	pks, err := idx.FindRange([]byte("bob"), []byte("dave"))
	if err != nil {
		t.Fatalf("FindRange failed: %v", err)
	}

	if len(pks) != 3 { // bob, carol, dave
		t.Errorf("Expected 3 results, got %d", len(pks))
	}
}

func TestSecondaryIndexNilExtractor(t *testing.T) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "missing",
		Extractor: JSONFieldExtractor("nonexistent"),
		Unique:    true,
	})

	// Insert record without the indexed field
	err := idx.Index([]byte("user:1"), []byte(`{"name":"alice"}`))
	if err != nil {
		t.Errorf("Should not error for missing field: %v", err)
	}

	// Should not be indexed
	_, err = idx.FindOne([]byte("alice"))
	if err == nil {
		t.Error("Should not find record indexed on missing field")
	}
}

// ==================== Key Extractor Tests ====================

func TestJSONFieldExtractor(t *testing.T) {
	extractor := JSONFieldExtractor("email")

	tests := []struct {
		input    string
		expected string
	}{
		{`{"email":"test@example.com"}`, "test@example.com"},
		{`{"name":"alice","email":"alice@example.com"}`, "alice@example.com"},
		{`{"email": "spaced@example.com"}`, "spaced@example.com"},
		{`{"name":"bob"}`, ""}, // missing field
		{`{"email":""}`, ""},   // empty value
	}

	for _, tc := range tests {
		result := extractor([]byte(tc.input))
		if tc.expected == "" && result != nil {
			t.Errorf("Expected nil for '%s', got '%s'", tc.input, result)
		} else if tc.expected != "" && !bytes.Equal(result, []byte(tc.expected)) {
			t.Errorf("For '%s': expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestJSONFieldExtractorNumeric(t *testing.T) {
	extractor := JSONFieldExtractor("age")

	result := extractor([]byte(`{"name":"alice","age":25}`))
	if !bytes.Equal(result, []byte("25")) {
		t.Errorf("Expected '25', got '%s'", result)
	}
}

func TestPrefixExtractor(t *testing.T) {
	extractor := PrefixExtractor(4)

	result := extractor([]byte("hello world"))
	if !bytes.Equal(result, []byte("hell")) {
		t.Errorf("Expected 'hell', got '%s'", result)
	}

	// Shorter than prefix
	result = extractor([]byte("hi"))
	if !bytes.Equal(result, []byte("hi")) {
		t.Errorf("Expected 'hi', got '%s'", result)
	}
}

func TestOffsetExtractor(t *testing.T) {
	extractor := OffsetExtractor(2, 3)

	result := extractor([]byte("hello"))
	if !bytes.Equal(result, []byte("llo")) {
		t.Errorf("Expected 'llo', got '%s'", result)
	}

	// Too short
	result = extractor([]byte("hi"))
	if result != nil {
		t.Errorf("Expected nil for short input, got '%s'", result)
	}
}

func TestCompositeExtractor(t *testing.T) {
	extractor := CompositeExtractor(
		JSONFieldExtractor("city"),
		JSONFieldExtractor("name"),
	)

	result := extractor([]byte(`{"city":"NYC","name":"alice"}`))
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// Verify it contains both parts (with length prefixes)
	if len(result) < 8 { // At least two 4-byte length prefixes
		t.Errorf("Result too short: %d bytes", len(result))
	}
}

// ==================== IndexedBTree Basic Tests ====================

func TestIndexedBTreeCreation(t *testing.T) {
	db := NewIndexedBTreeDefault()

	if db.Count() != 0 {
		t.Errorf("Expected 0 count, got %d", db.Count())
	}

	if len(db.ListIndexes()) != 0 {
		t.Error("Expected no indexes initially")
	}
}

func TestIndexedBTreeCreateIndex(t *testing.T) {
	db := NewIndexedBTreeDefault()

	err := db.CreateIndex("email", JSONFieldExtractor("email"), true)
	if err != nil {
		t.Fatalf("CreateIndex failed: %v", err)
	}

	if !db.HasIndex("email") {
		t.Error("Index should exist")
	}

	// Duplicate should fail
	err = db.CreateIndex("email", JSONFieldExtractor("email"), true)
	if err == nil {
		t.Error("Expected error for duplicate index")
	}
}

func TestIndexedBTreeDropIndex(t *testing.T) {
	db := NewIndexedBTreeDefault()

	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	err := db.DropIndex("email")
	if err != nil {
		t.Fatalf("DropIndex failed: %v", err)
	}

	if db.HasIndex("email") {
		t.Error("Index should not exist after drop")
	}

	// Drop non-existent should fail
	err = db.DropIndex("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent index")
	}
}

func TestIndexedBTreeInsertAndFind(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Insert
	err := db.Insert([]byte("user:1"), []byte(`{"email":"alice@example.com","name":"Alice"}`))
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Find by primary key
	value, err := db.Find([]byte("user:1"))
	if err != nil {
		t.Fatalf("Find by primary key failed: %v", err)
	}

	if !bytes.Contains(value, []byte("alice@example.com")) {
		t.Error("Value should contain email")
	}

	// Find by index
	pk, err := db.FindByIndex("email", []byte("alice@example.com"))
	if err != nil {
		t.Fatalf("FindByIndex failed: %v", err)
	}

	if !bytes.Equal(pk, []byte("user:1")) {
		t.Errorf("Expected 'user:1', got '%s'", pk)
	}
}

func TestIndexedBTreeUniqueConstraint(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// First insert
	db.Insert([]byte("user:1"), []byte(`{"email":"alice@example.com"}`))

	// Duplicate email should fail
	err := db.Insert([]byte("user:2"), []byte(`{"email":"alice@example.com"}`))
	if err == nil {
		t.Error("Expected unique constraint violation")
	}

	// Different email should succeed
	err = db.Insert([]byte("user:2"), []byte(`{"email":"bob@example.com"}`))
	if err != nil {
		t.Fatalf("Insert with different email should succeed: %v", err)
	}
}

func TestIndexedBTreeUpdate(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Insert
	db.Insert([]byte("user:1"), []byte(`{"email":"old@example.com"}`))

	// Update
	err := db.Update([]byte("user:1"), []byte(`{"email":"new@example.com"}`))
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Old email should not work
	_, err = db.FindByIndex("email", []byte("old@example.com"))
	if err == nil {
		t.Error("Old email should not be indexed")
	}

	// New email should work
	pk, err := db.FindByIndex("email", []byte("new@example.com"))
	if err != nil {
		t.Fatalf("Find new email failed: %v", err)
	}
	if !bytes.Equal(pk, []byte("user:1")) {
		t.Errorf("Expected 'user:1', got '%s'", pk)
	}
}

func TestIndexedBTreeUpdateUniqueConstraint(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	db.Insert([]byte("user:1"), []byte(`{"email":"alice@example.com"}`))
	db.Insert([]byte("user:2"), []byte(`{"email":"bob@example.com"}`))

	// Try to update user:1 to have user:2's email
	err := db.Update([]byte("user:1"), []byte(`{"email":"bob@example.com"}`))
	if err == nil {
		t.Error("Expected unique constraint violation on update")
	}

	// Original should still work
	pk, _ := db.FindByIndex("email", []byte("alice@example.com"))
	if !bytes.Equal(pk, []byte("user:1")) {
		t.Error("Original email should still be indexed")
	}
}

func TestIndexedBTreeDelete(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	db.Insert([]byte("user:1"), []byte(`{"email":"alice@example.com"}`))

	// Delete
	deleted, err := db.Delete([]byte("user:1"))
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if !deleted {
		t.Error("Expected deleted=true")
	}

	// Primary key should not exist
	_, err = db.Find([]byte("user:1"))
	if err == nil {
		t.Error("Primary key should not exist after delete")
	}

	// Index should not exist
	_, err = db.FindByIndex("email", []byte("alice@example.com"))
	if err == nil {
		t.Error("Index entry should not exist after delete")
	}
}

func TestIndexedBTreeFindAllByIndex(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("city", JSONFieldExtractor("city"), false) // non-unique

	db.Insert([]byte("user:1"), []byte(`{"city":"NYC","name":"Alice"}`))
	db.Insert([]byte("user:2"), []byte(`{"city":"NYC","name":"Bob"}`))
	db.Insert([]byte("user:3"), []byte(`{"city":"LA","name":"Carol"}`))

	// Find all NYC users
	pks, err := db.FindAllByIndex("city", []byte("NYC"))
	if err != nil {
		t.Fatalf("FindAllByIndex failed: %v", err)
	}

	if len(pks) != 2 {
		t.Errorf("Expected 2 results, got %d", len(pks))
	}
}

func TestIndexedBTreeFindRangeByIndex(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("name", JSONFieldExtractor("name"), true)

	names := []string{"alice", "bob", "carol", "dave", "eve"}
	for i, name := range names {
		db.Insert(
			[]byte(fmt.Sprintf("user:%d", i)),
			[]byte(fmt.Sprintf(`{"name":"%s"}`, name)),
		)
	}

	// Range: bob to dave
	pks, err := db.FindRangeByIndex("name", []byte("bob"), []byte("dave"))
	if err != nil {
		t.Fatalf("FindRangeByIndex failed: %v", err)
	}

	if len(pks) != 3 { // bob, carol, dave
		t.Errorf("Expected 3 results, got %d", len(pks))
	}
}

func TestIndexedBTreeCreateIndexWithRebuild(t *testing.T) {
	db := NewIndexedBTreeDefault()

	// Insert data first
	for i := 0; i < 10; i++ {
		db.tree.Insert(
			[]byte(fmt.Sprintf("user:%d", i)),
			[]byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i)),
		)
	}

	// Create index with rebuild
	err := db.CreateIndexWithRebuild("email", JSONFieldExtractor("email"), true)
	if err != nil {
		t.Fatalf("CreateIndexWithRebuild failed: %v", err)
	}

	// Should be able to find by email
	pk, err := db.FindByIndex("email", []byte("user5@example.com"))
	if err != nil {
		t.Fatalf("Find after rebuild failed: %v", err)
	}
	if !bytes.Equal(pk, []byte("user:5")) {
		t.Errorf("Expected 'user:5', got '%s'", pk)
	}
}

func TestIndexedBTreeClear(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Insert data
	for i := 0; i < 10; i++ {
		db.Insert(
			[]byte(fmt.Sprintf("user:%d", i)),
			[]byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i)),
		)
	}

	// Clear
	db.Clear()

	if db.Count() != 0 {
		t.Errorf("Expected 0 count after clear, got %d", db.Count())
	}

	// Index should also be cleared
	_, err := db.FindByIndex("email", []byte("user5@example.com"))
	if err == nil {
		t.Error("Index should be cleared")
	}
}

func TestIndexedBTreeMultipleIndexes(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)
	db.CreateIndex("city", JSONFieldExtractor("city"), false)
	db.CreateIndex("name", JSONFieldExtractor("name"), true)

	// Insert
	db.Insert([]byte("user:1"), []byte(`{"email":"alice@example.com","city":"NYC","name":"Alice"}`))
	db.Insert([]byte("user:2"), []byte(`{"email":"bob@example.com","city":"NYC","name":"Bob"}`))

	// Find by each index
	pk, _ := db.FindByIndex("email", []byte("alice@example.com"))
	if !bytes.Equal(pk, []byte("user:1")) {
		t.Error("Email index failed")
	}

	pk, _ = db.FindByIndex("name", []byte("Bob"))
	if !bytes.Equal(pk, []byte("user:2")) {
		t.Error("Name index failed")
	}

	pks, _ := db.FindAllByIndex("city", []byte("NYC"))
	if len(pks) != 2 {
		t.Error("City index failed")
	}

	// Delete should update all indexes
	db.Delete([]byte("user:1"))

	_, err := db.FindByIndex("email", []byte("alice@example.com"))
	if err == nil {
		t.Error("Email index should be updated after delete")
	}

	pks, _ = db.FindAllByIndex("city", []byte("NYC"))
	if len(pks) != 1 {
		t.Error("City index should be updated after delete")
	}
}

func TestIndexedBTreeStats(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	for i := 0; i < 100; i++ {
		db.Insert(
			[]byte(fmt.Sprintf("user:%d", i)),
			[]byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i)),
		)
	}

	stats := db.Stats()

	if stats.PrimaryStats.TotalKeys != 100 {
		t.Errorf("Expected 100 primary keys, got %d", stats.PrimaryStats.TotalKeys)
	}

	emailStats, exists := stats.IndexStats["email"]
	if !exists {
		t.Fatal("Email index stats should exist")
	}

	if emailStats.Entries != 100 {
		t.Errorf("Expected 100 index entries, got %d", emailStats.Entries)
	}
}

// ==================== Concurrent Tests ====================

func TestIndexedBTreeConcurrentInserts(t *testing.T) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
	db.CreateIndex("id", JSONFieldExtractor("id"), true)

	const numGoroutines = 10
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := []byte(fmt.Sprintf("user:g%d_%d", id, i))
				value := []byte(fmt.Sprintf(`{"id":"g%d_%d","name":"User"}`, id, i))
				db.Insert(key, value)
			}
		}(g)
	}

	wg.Wait()

	expectedCount := int64(numGoroutines * opsPerGoroutine)
	if db.Count() != expectedCount {
		t.Errorf("Expected %d records, got %d", expectedCount, db.Count())
	}
}

func TestIndexedBTreeConcurrentMixed(t *testing.T) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
	db.CreateIndex("id", JSONFieldExtractor("id"), true)

	// Pre-populate
	for i := 0; i < 100; i++ {
		db.Insert(
			[]byte(fmt.Sprintf("user:%d", i)),
			[]byte(fmt.Sprintf(`{"id":"user_%d"}`, i)),
		)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3)

	// Writers
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := []byte(fmt.Sprintf("new:g%d_%d", id, i))
				value := []byte(fmt.Sprintf(`{"id":"new_g%d_%d"}`, id, i))
				db.Insert(key, value)
			}
		}(g)
	}

	// Readers (by index)
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				db.FindByIndex("id", []byte(fmt.Sprintf("user_%d", i%100)))
			}
		}(g)
	}

	// Deleters
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				key := []byte(fmt.Sprintf("user:%d", (id*10+i)%100))
				db.Delete(key)
			}
		}(g)
	}

	wg.Wait()

	// Just verify no crashes
	t.Logf("Final count: %d", db.Count())
}

// ==================== Edge Case Tests ====================

func TestIndexedBTreeFindNonExistentIndex(t *testing.T) {
	db := NewIndexedBTreeDefault()

	_, err := db.FindByIndex("nonexistent", []byte("value"))
	if err == nil {
		t.Error("Expected error for non-existent index")
	}
}

func TestIndexedBTreeUpdateNonExistent(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	err := db.Update([]byte("nonexistent"), []byte(`{"email":"test@example.com"}`))
	if err == nil {
		t.Error("Expected error for non-existent key")
	}
}

func TestIndexedBTreeDeleteNonExistent(t *testing.T) {
	db := NewIndexedBTreeDefault()

	deleted, err := db.Delete([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("Delete should not error: %v", err)
	}
	if deleted {
		t.Error("Should not report deleted for non-existent")
	}
}

func TestIndexedBTreeEmptyValue(t *testing.T) {
	db := NewIndexedBTreeDefault()
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Insert with empty/nil value
	err := db.Insert([]byte("user:1"), []byte{})
	if err != nil {
		t.Fatalf("Insert empty value should succeed: %v", err)
	}

	// Should not be indexed (no email field)
	_, err = db.FindByIndex("email", []byte(""))
	if err == nil {
		t.Error("Should not find empty indexed value")
	}
}

// ==================== Encoding Tests ====================

func TestPrimaryKeyEncoding(t *testing.T) {
	keys := [][]byte{
		[]byte("user:1"),
		[]byte("user:2"),
		[]byte("user:3"),
	}

	encoded := encodePrimaryKeys(keys)
	decoded := decodePrimaryKeys(encoded)

	if len(decoded) != len(keys) {
		t.Fatalf("Expected %d keys, got %d", len(keys), len(decoded))
	}

	for i, k := range keys {
		if !bytes.Equal(decoded[i], k) {
			t.Errorf("Key %d mismatch: expected '%s', got '%s'", i, k, decoded[i])
		}
	}
}

func TestPrimaryKeyEncodingEmpty(t *testing.T) {
	encoded := encodePrimaryKeys([][]byte{})
	decoded := decodePrimaryKeys(encoded)

	if len(decoded) != 0 {
		t.Errorf("Expected 0 keys, got %d", len(decoded))
	}
}

func TestPrimaryKeyEncodingLargeKeys(t *testing.T) {
	// Large keys
	key1 := make([]byte, 1000)
	key2 := make([]byte, 2000)
	for i := range key1 {
		key1[i] = byte(i % 256)
	}
	for i := range key2 {
		key2[i] = byte((i + 1) % 256)
	}

	keys := [][]byte{key1, key2}
	encoded := encodePrimaryKeys(keys)
	decoded := decodePrimaryKeys(encoded)

	if len(decoded) != 2 {
		t.Fatalf("Expected 2 keys, got %d", len(decoded))
	}

	if !bytes.Equal(decoded[0], key1) {
		t.Error("Key 1 mismatch")
	}
	if !bytes.Equal(decoded[1], key2) {
		t.Error("Key 2 mismatch")
	}
}
