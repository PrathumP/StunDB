package bptree

import (
	"errors"
	"sync"
)

// IndexedBTree wraps a ShardedBTree with automatic secondary index maintenance.
//
// DESIGN:
// - Primary data stored in main tree (primary key → record)
// - Secondary indexes maintained automatically on insert/update/delete
// - Supports multiple indexes on different fields
//
// USAGE:
//
//	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
//
//	// Create indexes
//	db.CreateIndex("email", JSONFieldExtractor("email"), true)   // unique
//	db.CreateIndex("age", JSONFieldExtractor("age"), false)      // non-unique
//
//	// Insert (indexes updated automatically)
//	db.Insert([]byte("user:1"), []byte(`{"email":"a@b.com","age":"25"}`))
//
//	// Query by index
//	pk, _ := db.FindByIndex("email", []byte("a@b.com"))
//	record, _ := db.Find(pk)
type IndexedBTree struct {
	tree    *ShardedBTree
	indexes map[string]*SecondaryIndex
	mu      sync.RWMutex
}

// IndexedConfig configures the indexed B-Tree.
type IndexedConfig struct {
	// NumShards for the primary tree (default: runtime.NumCPU())
	NumShards int
}

// IndexedStats provides statistics about the indexed tree.
type IndexedStats struct {
	PrimaryStats ShardStats
	IndexStats   map[string]IndexStats
}

// NewIndexedBTree creates a new indexed B-Tree.
func NewIndexedBTree(config IndexedConfig) *IndexedBTree {
	return &IndexedBTree{
		tree:    NewShardedBTree(ShardConfig{NumShards: config.NumShards}),
		indexes: make(map[string]*SecondaryIndex),
	}
}

// NewIndexedBTreeDefault creates an indexed B-Tree with default settings.
func NewIndexedBTreeDefault() *IndexedBTree {
	return NewIndexedBTree(IndexedConfig{})
}

// CreateIndex creates a new secondary index on the tree.
// If rebuild is true, indexes all existing records.
func (db *IndexedBTree) CreateIndex(name string, extractor KeyExtractor, unique bool) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.indexes[name]; exists {
		return errors.New("index already exists")
	}

	idx := NewSecondaryIndex(IndexConfig{
		Name:      name,
		Extractor: extractor,
		Unique:    unique,
		NumShards: 4,
	})

	db.indexes[name] = idx
	return nil
}

// CreateIndexWithRebuild creates an index and populates it with existing data.
func (db *IndexedBTree) CreateIndexWithRebuild(name string, extractor KeyExtractor, unique bool) error {
	if err := db.CreateIndex(name, extractor, unique); err != nil {
		return err
	}

	// Rebuild index from existing data
	db.mu.RLock()
	idx := db.indexes[name]
	db.mu.RUnlock()

	var indexErr error
	db.tree.ForEach(func(key Keytype, value Valuetype) bool {
		if err := idx.Index(key, value); err != nil {
			indexErr = err
			return false // Stop on error (unique constraint violation)
		}
		return true
	})

	if indexErr != nil {
		// Rollback: remove the index
		db.mu.Lock()
		delete(db.indexes, name)
		db.mu.Unlock()
		return indexErr
	}

	return nil
}

// DropIndex removes a secondary index.
func (db *IndexedBTree) DropIndex(name string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.indexes[name]; !exists {
		return errors.New("index not found")
	}

	delete(db.indexes, name)
	return nil
}

// HasIndex checks if an index exists.
func (db *IndexedBTree) HasIndex(name string) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	_, exists := db.indexes[name]
	return exists
}

// ListIndexes returns the names of all indexes.
func (db *IndexedBTree) ListIndexes() []string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	names := make([]string, 0, len(db.indexes))
	for name := range db.indexes {
		names = append(names, name)
	}
	return names
}

// Insert adds a record and updates all secondary indexes.
func (db *IndexedBTree) Insert(key Keytype, value Valuetype) error {
	db.mu.RLock()
	indexes := make([]*SecondaryIndex, 0, len(db.indexes))
	for _, idx := range db.indexes {
		indexes = append(indexes, idx)
	}
	db.mu.RUnlock()

	// Check unique constraints first
	for _, idx := range indexes {
		if idx.unique {
			indexKey := idx.extractor(value)
			if indexKey != nil {
				if _, err := idx.FindOne(indexKey); err == nil {
					return errors.New("unique constraint violation on index: " + idx.name)
				}
			}
		}
	}

	// Insert into primary tree
	db.tree.Insert(key, value)

	// Update all indexes
	for _, idx := range indexes {
		if err := idx.Index(key, value); err != nil {
			// Rollback: remove from primary tree
			db.tree.Delete(key)
			return err
		}
	}

	return nil
}

// Put is an alias for Insert.
func (db *IndexedBTree) Put(key Keytype, value Valuetype) error {
	return db.Insert(key, value)
}

// Update updates a record and maintains all secondary indexes.
func (db *IndexedBTree) Update(key Keytype, newValue Valuetype) error {
	// Get old value for index update
	oldValue, err := db.tree.Find(key)
	if err != nil {
		return errors.New("key not found")
	}

	db.mu.RLock()
	indexes := make([]*SecondaryIndex, 0, len(db.indexes))
	for _, idx := range db.indexes {
		indexes = append(indexes, idx)
	}
	db.mu.RUnlock()

	// Check unique constraints for new value
	for _, idx := range indexes {
		if idx.unique {
			oldIndexKey := idx.extractor(oldValue)
			newIndexKey := idx.extractor(newValue)

			// Only check if index key changed
			if newIndexKey != nil && !bytesEqual(oldIndexKey, newIndexKey) {
				if _, err := idx.FindOne(newIndexKey); err == nil {
					return errors.New("unique constraint violation on index: " + idx.name)
				}
			}
		}
	}

	// Update primary tree
	db.tree.Insert(key, newValue)

	// Update all indexes
	for _, idx := range indexes {
		if err := idx.Update(key, oldValue, newValue); err != nil {
			// This shouldn't fail after constraint check, but handle it
			return err
		}
	}

	return nil
}

// Delete removes a record and updates all secondary indexes.
func (db *IndexedBTree) Delete(key Keytype) (bool, error) {
	// Get value for index removal
	value, err := db.tree.Find(key)
	if err != nil {
		return false, nil // Key doesn't exist
	}

	db.mu.RLock()
	indexes := make([]*SecondaryIndex, 0, len(db.indexes))
	for _, idx := range db.indexes {
		indexes = append(indexes, idx)
	}
	db.mu.RUnlock()

	// Delete from primary tree
	deleted := db.tree.Delete(key)
	if !deleted {
		return false, nil
	}

	// Remove from all indexes
	for _, idx := range indexes {
		idx.Remove(key, value)
	}

	return true, nil
}

// Find searches for a key in the primary tree.
func (db *IndexedBTree) Find(key Keytype) (Valuetype, error) {
	return db.tree.Find(key)
}

// Get is an alias for Find.
func (db *IndexedBTree) Get(key Keytype) (Valuetype, error) {
	return db.Find(key)
}

// FindByIndex finds records by secondary index (unique index).
// Returns the primary key for the given index key.
func (db *IndexedBTree) FindByIndex(indexName string, indexKey []byte) (Keytype, error) {
	db.mu.RLock()
	idx, exists := db.indexes[indexName]
	db.mu.RUnlock()

	if !exists {
		return nil, errors.New("index not found")
	}

	return idx.FindOne(indexKey)
}

// FindAllByIndex finds all records matching an index key.
// Returns all primary keys for the given index key.
func (db *IndexedBTree) FindAllByIndex(indexName string, indexKey []byte) ([]Keytype, error) {
	db.mu.RLock()
	idx, exists := db.indexes[indexName]
	db.mu.RUnlock()

	if !exists {
		return nil, errors.New("index not found")
	}

	return idx.FindAll(indexKey)
}

// FindRangeByIndex finds all records with index keys in a range.
// Returns all primary keys where startKey <= indexKey <= endKey.
func (db *IndexedBTree) FindRangeByIndex(indexName string, startKey, endKey []byte) ([]Keytype, error) {
	db.mu.RLock()
	idx, exists := db.indexes[indexName]
	db.mu.RUnlock()

	if !exists {
		return nil, errors.New("index not found")
	}

	return idx.FindRange(startKey, endKey)
}

// GetRange returns all key-value pairs in the primary key range.
func (db *IndexedBTree) GetRange(startKey, endKey Keytype) ([]Keytype, []Valuetype, error) {
	return db.tree.GetRange(startKey, endKey)
}

// Count returns the number of records in the primary tree.
func (db *IndexedBTree) Count() int64 {
	return db.tree.Count()
}

// ForEach iterates over all key-value pairs.
func (db *IndexedBTree) ForEach(fn func(key Keytype, value Valuetype) bool) {
	db.tree.ForEach(fn)
}

// Clear removes all records and clears all indexes.
func (db *IndexedBTree) Clear() {
	db.mu.RLock()
	indexes := make([]*SecondaryIndex, 0, len(db.indexes))
	for _, idx := range db.indexes {
		indexes = append(indexes, idx)
	}
	db.mu.RUnlock()

	// Clear indexes
	for _, idx := range indexes {
		idx.Clear()
	}

	// Clear primary tree
	db.tree.Clear()
}

// Stats returns combined statistics.
func (db *IndexedBTree) Stats() IndexedStats {
	db.mu.RLock()
	defer db.mu.RUnlock()

	indexStats := make(map[string]IndexStats)
	for name, idx := range db.indexes {
		indexStats[name] = idx.Stats()
	}

	return IndexedStats{
		PrimaryStats: db.tree.Stats(),
		IndexStats:   indexStats,
	}
}

// bytesEqual compares two byte slices, handling nil.
func bytesEqual(a, b []byte) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
