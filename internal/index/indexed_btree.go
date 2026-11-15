package index

import (
	"bytes"
	"errors"
	"sync"

	"github.com/prathump/stundb/internal/kv"
)

// IndexedBTree wraps the primary store and keeps one or more SecondaryIndexes in
// sync as records are inserted, updated, and deleted.
//
// A write changes the primary store and every index, and those changes must
// agree, so writers are serialized by wmu: each one reads the record it
// replaces, checks unique constraints, and applies its changes with no other
// writer in between. Readers take no write lock. A reader may see a write
// applied to the primary store and not yet to an index, or the other way
// round; FindByIndex and its kin can therefore return a primary key whose
// record has just changed.
type IndexedBTree struct {
	tree    *kv.Store
	indexes map[string]*SecondaryIndex
	mu      sync.RWMutex // guards indexes, the map itself
	wmu     sync.Mutex   // serializes writers
}

// IndexedConfig configures the indexed B-Tree. It has no options yet; the
// primary tree needs no sharding to scale across cores.
type IndexedConfig struct{}

// IndexedStats summarises the indexed tree.
type IndexedStats struct {
	PrimaryStats kv.Stats
	IndexStats   map[string]IndexStats
}

// NewIndexedBTree creates a new indexed B-Tree.
func NewIndexedBTree(config IndexedConfig) *IndexedBTree {
	return &IndexedBTree{
		tree:    kv.New(),
		indexes: make(map[string]*SecondaryIndex),
	}
}

// NewIndexedBTreeDefault creates an indexed B-Tree with default settings.
func NewIndexedBTreeDefault() *IndexedBTree {
	return NewIndexedBTree(IndexedConfig{})
}

// PrimaryTree returns the underlying primary store. It is exposed for
// advanced use and testing; mutating it directly will NOT update the indexes.
func (db *IndexedBTree) PrimaryTree() *kv.Store {
	return db.tree
}

// CreateIndex creates a new, empty secondary index. Records already in the
// store are not indexed; use CreateIndexWithRebuild for that.
func (db *IndexedBTree) CreateIndex(name string, extractor KeyExtractor, unique bool) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	return db.addIndex(name, NewSecondaryIndex(IndexConfig{Name: name, Extractor: extractor, Unique: unique}))
}

func (db *IndexedBTree) addIndex(name string, idx *SecondaryIndex) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, exists := db.indexes[name]; exists {
		return errors.New("index already exists")
	}
	db.indexes[name] = idx
	return nil
}

// CreateIndexWithRebuild creates an index and populates it with existing
// data. It holds off writers while it builds the index, and adds the index
// only once it is complete, so a unique index that the data violates is never
// added.
func (db *IndexedBTree) CreateIndexWithRebuild(name string, extractor KeyExtractor, unique bool) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()

	if db.HasIndex(name) {
		return errors.New("index already exists")
	}
	idx := NewSecondaryIndex(IndexConfig{Name: name, Extractor: extractor, Unique: unique})
	var indexErr error
	db.tree.ForEach(func(key Keytype, value Valuetype) bool {
		indexErr = idx.Index(key, value)
		return indexErr == nil // stop on a unique constraint violation
	})
	if indexErr != nil {
		return indexErr
	}
	return db.addIndex(name, idx)
}

// DropIndex removes a secondary index.
func (db *IndexedBTree) DropIndex(name string) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
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

// indexList returns the current indexes. Writers call it under wmu, which
// also guards against the set changing.
func (db *IndexedBTree) indexList() []*SecondaryIndex {
	db.mu.RLock()
	defer db.mu.RUnlock()
	indexes := make([]*SecondaryIndex, 0, len(db.indexes))
	for _, idx := range db.indexes {
		indexes = append(indexes, idx)
	}
	return indexes
}

// Insert sets the record at key to value and updates every index. If key
// already holds a record, its index entries are replaced. If value would give
// a unique index a second record, Insert changes nothing and returns an error.
func (db *IndexedBTree) Insert(key Keytype, value Valuetype) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	old, err := db.tree.Find(key)
	if err != nil {
		old = nil
	}
	return db.write(key, old, err == nil, value)
}

// Put is an alias for Insert.
func (db *IndexedBTree) Put(key Keytype, value Valuetype) error {
	return db.Insert(key, value)
}

// Update replaces the record at key, which must exist, and updates every
// index. If newValue would give a unique index a second record, Update
// changes nothing and returns an error.
func (db *IndexedBTree) Update(key Keytype, newValue Valuetype) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	old, err := db.tree.Find(key)
	if err != nil {
		return errors.New("key not found")
	}
	return db.write(key, old, true, newValue)
}

// write stores value at key, whose current record is old if exists, and
// updates every index. The caller holds wmu.
func (db *IndexedBTree) write(key Keytype, old Valuetype, exists bool, value Valuetype) error {
	indexes := db.indexList()

	// Check every unique constraint before changing anything. A value already
	// indexed under this same key is not a violation: it is the record being
	// replaced.
	for _, idx := range indexes {
		if !idx.unique {
			continue
		}
		indexKey := idx.extractor(value)
		if indexKey == nil {
			continue
		}
		if owner, err := idx.FindOne(indexKey); err == nil && !bytes.Equal(owner, key) {
			return errors.New("unique constraint violation on index: " + idx.name)
		}
	}

	db.tree.Insert(key, value)
	for _, idx := range indexes {
		var err error
		if exists {
			err = idx.Update(key, old, value)
		} else {
			err = idx.Index(key, value)
		}
		if err != nil {
			// The check above rules this out while writers are serialized.
			return err
		}
	}
	return nil
}

// Delete removes a record and updates all secondary indexes.
func (db *IndexedBTree) Delete(key Keytype) (bool, error) {
	db.wmu.Lock()
	defer db.wmu.Unlock()

	value, err := db.tree.Find(key)
	if err != nil {
		return false, nil // Key doesn't exist
	}
	db.tree.Delete(key)
	for _, idx := range db.indexList() {
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

// ForEach calls fn on copies of the records in key order until fn returns
// false. It is not a snapshot.
func (db *IndexedBTree) ForEach(fn func(key Keytype, value Valuetype) bool) {
	db.tree.ForEach(fn)
}

// Clear removes all records and clears all indexes.
func (db *IndexedBTree) Clear() {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	for _, idx := range db.indexList() {
		idx.Clear()
	}
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
