package bptree

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
)

// SecondaryIndex provides secondary indexing for a B-Tree.
//
// DESIGN:
// - Maps field values (extracted from records) to primary keys
// - Supports both unique and non-unique indexes
// - Automatically maintained on insert/update/delete
//
// USAGE:
//
//	// Create index on "email" field
//	emailIdx := NewSecondaryIndex("email", extractEmail, true)
//
//	// Query by secondary key
//	primaryKey, err := emailIdx.FindOne([]byte("alice@example.com"))
//	record, err := db.Find(primaryKey)
//
// IMPLEMENTATION:
// - Unique index: field_value → primary_key (direct mapping)
// - Non-unique index: field_value → [primary_key1, primary_key2, ...]
type SecondaryIndex struct {
	name      string
	tree      *ShardedBTree
	extractor KeyExtractor
	unique    bool
	mu        sync.RWMutex

	// Statistics
	entries uint64
}

// KeyExtractor extracts the indexed field value from a record value.
// Returns nil if the field doesn't exist (record won't be indexed).
type KeyExtractor func(value Valuetype) []byte

// IndexConfig configures a secondary index.
type IndexConfig struct {
	// Name of the index (for identification)
	Name string
	// Extractor function to extract indexed field from record
	Extractor KeyExtractor
	// Unique indicates if the index enforces uniqueness
	Unique bool
	// NumShards for the underlying tree (default: 4)
	NumShards int
}

// IndexStats provides statistics about an index.
type IndexStats struct {
	Name      string
	Entries   uint64
	Unique    bool
	TreeStats ShardStats
}

// NewSecondaryIndex creates a new secondary index.
func NewSecondaryIndex(config IndexConfig) *SecondaryIndex {
	numShards := config.NumShards
	if numShards <= 0 {
		numShards = 4 // Smaller default for indexes
	}

	return &SecondaryIndex{
		name:      config.Name,
		tree:      NewShardedBTree(ShardConfig{NumShards: numShards}),
		extractor: config.Extractor,
		unique:    config.Unique,
	}
}

// Name returns the index name.
func (idx *SecondaryIndex) Name() string {
	return idx.name
}

// IsUnique returns whether the index enforces uniqueness.
func (idx *SecondaryIndex) IsUnique() bool {
	return idx.unique
}

// Index adds a primary key to the index based on record value.
// For unique indexes, returns error if the indexed value already exists.
func (idx *SecondaryIndex) Index(primaryKey Keytype, value Valuetype) error {
	indexKey := idx.extractor(value)
	if indexKey == nil {
		// Field doesn't exist in record, skip indexing
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.unique {
		// Check if already exists
		_, err := idx.tree.Find(indexKey)
		if err == nil {
			return errors.New("duplicate key in unique index")
		}
		// Store: indexKey → primaryKey
		idx.tree.Insert(indexKey, Valuetype(primaryKey))
	} else {
		// Non-unique: indexKey → list of primary keys
		existing, err := idx.tree.Find(indexKey)
		if err != nil {
			// First entry for this index key
			idx.tree.Insert(indexKey, encodePrimaryKeys([][]byte{primaryKey}))
		} else {
			// Append to existing list
			keys := decodePrimaryKeys(existing)
			// Check if already in list (idempotent)
			for _, k := range keys {
				if bytes.Equal(k, primaryKey) {
					return nil // Already indexed
				}
			}
			keys = append(keys, primaryKey)
			idx.tree.Insert(indexKey, encodePrimaryKeys(keys))
		}
	}

	idx.entries++
	return nil
}

// Remove removes a primary key from the index.
func (idx *SecondaryIndex) Remove(primaryKey Keytype, value Valuetype) error {
	indexKey := idx.extractor(value)
	if indexKey == nil {
		return nil
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	if idx.unique {
		// Simply delete the index entry
		idx.tree.Delete(indexKey)
		if idx.entries > 0 {
			idx.entries--
		}
	} else {
		// Remove from list
		existing, err := idx.tree.Find(indexKey)
		if err != nil {
			return nil // Not in index
		}

		keys := decodePrimaryKeys(existing)
		newKeys := make([][]byte, 0, len(keys))
		for _, k := range keys {
			if !bytes.Equal(k, primaryKey) {
				newKeys = append(newKeys, k)
			}
		}

		if len(newKeys) == 0 {
			idx.tree.Delete(indexKey)
		} else {
			idx.tree.Insert(indexKey, encodePrimaryKeys(newKeys))
		}

		if idx.entries > 0 {
			idx.entries--
		}
	}

	return nil
}

// Update updates an index entry when a record changes.
// Removes old index entry and adds new one.
func (idx *SecondaryIndex) Update(primaryKey Keytype, oldValue, newValue Valuetype) error {
	oldIndexKey := idx.extractor(oldValue)
	newIndexKey := idx.extractor(newValue)

	// If index key didn't change, nothing to do
	if bytes.Equal(oldIndexKey, newIndexKey) {
		return nil
	}

	// Remove old, add new
	if err := idx.Remove(primaryKey, oldValue); err != nil {
		return err
	}
	return idx.Index(primaryKey, newValue)
}

// FindOne finds the primary key for a unique index lookup.
// Returns error if index is not unique or key not found.
func (idx *SecondaryIndex) FindOne(indexKey []byte) (Keytype, error) {
	if !idx.unique {
		return nil, errors.New("FindOne only works on unique indexes")
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	val, err := idx.tree.Find(indexKey)
	if err != nil {
		return nil, err
	}
	return Keytype(val), nil
}

// FindAll finds all primary keys matching an index key.
// Works for both unique and non-unique indexes.
func (idx *SecondaryIndex) FindAll(indexKey []byte) ([]Keytype, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	value, err := idx.tree.Find(indexKey)
	if err != nil {
		return nil, err
	}

	if idx.unique {
		return []Keytype{Keytype(value)}, nil
	}

	pks := decodePrimaryKeys(value)
	result := make([]Keytype, len(pks))
	for i, pk := range pks {
		result[i] = Keytype(pk)
	}
	return result, nil
}

// FindRange finds all primary keys for index keys in a range.
// Returns primary keys for all index keys where startKey <= indexKey <= endKey.
func (idx *SecondaryIndex) FindRange(startKey, endKey []byte) ([]Keytype, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	keys, values, err := idx.tree.GetRange(startKey, endKey)
	if err != nil {
		return nil, err
	}

	var result []Keytype
	for i := range keys {
		if idx.unique {
			result = append(result, Keytype(values[i]))
		} else {
			pks := decodePrimaryKeys(values[i])
			for _, pk := range pks {
				result = append(result, Keytype(pk))
			}
		}
	}

	return result, nil
}

// Count returns the number of entries in the index.
func (idx *SecondaryIndex) Count() uint64 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.entries
}

// Stats returns index statistics.
func (idx *SecondaryIndex) Stats() IndexStats {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return IndexStats{
		Name:      idx.name,
		Entries:   idx.entries,
		Unique:    idx.unique,
		TreeStats: idx.tree.Stats(),
	}
}

// Clear removes all entries from the index.
func (idx *SecondaryIndex) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.tree.Clear()
	idx.entries = 0
}

// encodePrimaryKeys encodes a list of primary keys into a single value.
// Format: [count:4][len1:4][key1][len2:4][key2]...
func encodePrimaryKeys(keys [][]byte) []byte {
	// Calculate total size
	size := 4 // count
	for _, k := range keys {
		size += 4 + len(k) // length + key
	}

	buf := make([]byte, size)
	offset := 0

	// Write count
	binary.LittleEndian.PutUint32(buf[offset:], uint32(len(keys)))
	offset += 4

	// Write each key
	for _, k := range keys {
		binary.LittleEndian.PutUint32(buf[offset:], uint32(len(k)))
		offset += 4
		copy(buf[offset:], k)
		offset += len(k)
	}

	return buf
}

// decodePrimaryKeys decodes a list of primary keys from a value.
func decodePrimaryKeys(data []byte) [][]byte {
	if len(data) < 4 {
		return nil
	}

	count := binary.LittleEndian.Uint32(data[0:4])
	offset := 4

	keys := make([][]byte, 0, count)
	for i := uint32(0); i < count && offset < len(data); i++ {
		if offset+4 > len(data) {
			break
		}
		keyLen := binary.LittleEndian.Uint32(data[offset:])
		offset += 4

		if offset+int(keyLen) > len(data) {
			break
		}
		key := make([]byte, keyLen)
		copy(key, data[offset:offset+int(keyLen)])
		offset += int(keyLen)
		keys = append(keys, key)
	}

	return keys
}

// ============================================================================
// Common Key Extractors
// ============================================================================

// JSONFieldExtractor creates an extractor for a JSON field.
// This is a simple implementation that looks for "field": "value" patterns.
// For production, use a proper JSON parser.
func JSONFieldExtractor(fieldName string) KeyExtractor {
	searchPattern := []byte(`"` + fieldName + `":`)

	return func(value Valuetype) []byte {
		// Find field
		idx := bytes.Index(value, searchPattern)
		if idx == -1 {
			return nil
		}

		// Skip to value
		start := idx + len(searchPattern)
		for start < len(value) && (value[start] == ' ' || value[start] == '\t') {
			start++
		}

		if start >= len(value) {
			return nil
		}

		// Handle string value
		if value[start] == '"' {
			start++
			end := start
			for end < len(value) && value[end] != '"' {
				end++
			}
			// For string values, return nil if empty, otherwise return the content
			if end == start {
				return nil // empty string
			}
			return value[start:end]
		}

		// Handle number/boolean value
		end := start
		for end < len(value) && value[end] != ',' && value[end] != '}' && value[end] != ' ' {
			end++
		}
		if end > start {
			return value[start:end]
		}

		return nil
	}
}

// PrefixExtractor creates an extractor that takes the first N bytes.
func PrefixExtractor(length int) KeyExtractor {
	return func(value Valuetype) []byte {
		if len(value) < length {
			return value
		}
		return value[:length]
	}
}

// OffsetExtractor creates an extractor that takes bytes from offset to offset+length.
func OffsetExtractor(offset, length int) KeyExtractor {
	return func(value Valuetype) []byte {
		if len(value) < offset+length {
			return nil
		}
		return value[offset : offset+length]
	}
}

// CompositeExtractor combines multiple extractors into a composite key.
func CompositeExtractor(extractors ...KeyExtractor) KeyExtractor {
	return func(value Valuetype) []byte {
		var result []byte
		for _, ext := range extractors {
			part := ext(value)
			if part == nil {
				return nil
			}
			// Add length prefix for each part
			lenBuf := make([]byte, 4)
			binary.LittleEndian.PutUint32(lenBuf, uint32(len(part)))
			result = append(result, lenBuf...)
			result = append(result, part...)
		}
		return result
	}
}
