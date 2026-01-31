package bptree

import (
	"fmt"
	"sync"
)

// DurableBTree wraps a ShardedBTree with WAL for durability.
//
// DESIGN:
// - All mutations are logged to WAL BEFORE being applied to the tree
// - On crash recovery, replay WAL to restore state
// - Checkpoint to truncate WAL after tree is stable
//
// USAGE:
//
//	db, err := NewDurableBTree(DurableConfig{
//	    WALPath:   "/data/stundb.wal",
//	    NumShards: 8,
//	    SyncMode:  SyncBatch,
//	})
//	defer db.Close()
//
//	db.Insert(key, value)  // Logged + applied atomically
//	value, err := db.Find(key)
type DurableBTree struct {
	tree *ShardedBTree
	wal  *WAL
	mu   sync.RWMutex

	// Configuration
	config DurableConfig
}

// DurableConfig configures the durable B-Tree.
type DurableConfig struct {
	// WALPath is the path to the WAL file (required)
	WALPath string

	// NumShards for the underlying ShardedBTree (default: NumCPU)
	NumShards int

	// SyncMode controls WAL durability (default: SyncBatch)
	SyncMode SyncMode

	// BatchSize for SyncBatch mode (default: 100)
	BatchSize int
}

// DurableStats provides statistics for the durable B-Tree.
type DurableStats struct {
	TreeStats ShardStats
	WALStats  WALStats
}

// NewDurableBTree creates a new durable B-Tree with WAL.
// If a WAL exists with entries, they will be replayed to restore state.
func NewDurableBTree(config DurableConfig) (*DurableBTree, error) {
	if config.WALPath == "" {
		return nil, fmt.Errorf("WAL path is required")
	}

	// Create WAL first
	wal, err := NewWAL(WALConfig{
		Path:      config.WALPath,
		SyncMode:  config.SyncMode,
		BatchSize: config.BatchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL: %w", err)
	}

	// Create tree
	tree := NewShardedBTree(ShardConfig{
		NumShards: config.NumShards,
	})

	db := &DurableBTree{
		tree:   tree,
		wal:    wal,
		config: config,
	}

	// Replay WAL to restore state
	count, err := db.recover()
	if err != nil {
		wal.Close()
		return nil, fmt.Errorf("failed to recover from WAL: %w", err)
	}

	if count > 0 {
		// Log recovery info (could use a logger in production)
		_ = count // Recovered entries
	}

	return db, nil
}

// recover replays the WAL to restore tree state.
func (db *DurableBTree) recover() (int, error) {
	return db.wal.Replay(func(entry *LogEntry) error {
		switch entry.Op {
		case OpInsert:
			db.tree.Insert(entry.Key, entry.Value)
		case OpDelete:
			db.tree.Delete(entry.Key)
		case OpClear:
			db.tree.Clear()
		}
		return nil
	})
}

// Insert adds a key-value pair with WAL durability.
func (db *DurableBTree) Insert(key Keytype, value Valuetype) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Log to WAL first
	if _, err := db.wal.AppendInsert(key, value); err != nil {
		return fmt.Errorf("WAL insert failed: %w", err)
	}

	// Then apply to tree
	db.tree.Insert(key, value)
	return nil
}

// Put is an alias for Insert.
func (db *DurableBTree) Put(key Keytype, value Valuetype) error {
	return db.Insert(key, value)
}

// Delete removes a key with WAL durability.
func (db *DurableBTree) Delete(key Keytype) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Log to WAL first
	if _, err := db.wal.AppendDelete(key); err != nil {
		return false, fmt.Errorf("WAL delete failed: %w", err)
	}

	// Then apply to tree
	deleted := db.tree.Delete(key)
	return deleted, nil
}

// Find searches for a key (read-only, no WAL).
func (db *DurableBTree) Find(key Keytype) (Valuetype, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.tree.Find(key)
}

// Get is an alias for Find.
func (db *DurableBTree) Get(key Keytype) (Valuetype, error) {
	return db.Find(key)
}

// GetRange returns all key-value pairs in the range (read-only, no WAL).
func (db *DurableBTree) GetRange(startKey, endKey Keytype) ([]Keytype, []Valuetype, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.tree.GetRange(startKey, endKey)
}

// Clear removes all entries with WAL durability.
func (db *DurableBTree) Clear() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Log to WAL
	if _, err := db.wal.AppendClear(); err != nil {
		return fmt.Errorf("WAL clear failed: %w", err)
	}

	// Apply to tree
	db.tree.Clear()
	return nil
}

// BulkInsert inserts multiple key-value pairs with WAL durability.
// All entries are logged before any are applied (atomic batch).
func (db *DurableBTree) BulkInsert(keys []Keytype, values []Valuetype) error {
	if len(keys) != len(values) {
		return fmt.Errorf("keys and values length mismatch")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	// Log all to WAL first
	for i := range keys {
		if _, err := db.wal.AppendInsert(keys[i], values[i]); err != nil {
			return fmt.Errorf("WAL bulk insert failed at index %d: %w", i, err)
		}
	}

	// Sync WAL before applying (ensures durability of batch)
	if err := db.wal.Sync(); err != nil {
		return fmt.Errorf("WAL sync failed: %w", err)
	}

	// Apply all to tree
	db.tree.BulkInsert(keys, values)
	return nil
}

// Count returns the total number of keys.
func (db *DurableBTree) Count() int64 {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.tree.Count()
}

// ForEach iterates over all key-value pairs.
func (db *DurableBTree) ForEach(fn func(key Keytype, value Valuetype) bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	db.tree.ForEach(fn)
}

// Checkpoint truncates the WAL after tree state is stable.
// Call this periodically to prevent unbounded WAL growth.
// In a real system, this would be called after persisting the tree to disk.
func (db *DurableBTree) Checkpoint() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.wal.Checkpoint()
}

// Sync forces a sync of the WAL to disk.
func (db *DurableBTree) Sync() error {
	return db.wal.Sync()
}

// Stats returns combined statistics for tree and WAL.
func (db *DurableBTree) Stats() DurableStats {
	return DurableStats{
		TreeStats: db.tree.Stats(),
		WALStats:  db.wal.Stats(),
	}
}

// Close closes the durable B-Tree and its WAL.
func (db *DurableBTree) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.wal.Close()
}

// WALPath returns the path to the WAL file.
func (db *DurableBTree) WALPath() string {
	return db.wal.Path()
}

// WALSequence returns the current WAL sequence number.
func (db *DurableBTree) WALSequence() uint64 {
	return db.wal.Sequence()
}
