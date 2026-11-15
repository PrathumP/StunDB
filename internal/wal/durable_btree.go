package wal

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/prathump/stundb/internal/kv"
)

// Keytype and Valuetype are re-exported from the kv package.
type (
	Keytype   = kv.Keytype
	Valuetype = kv.Valuetype
)

// DurableBTree wraps the in-memory store with a write-ahead log so that it can
// be rebuilt after a crash.
//
//   - Every mutation is appended to the WAL before it is applied to the tree.
//   - On startup the WAL is replayed to restore state.
//   - Checkpoint truncates the WAL once the caller is happy with tree state.
//
// Writers are serialized by mu, so that the log's order is the order the
// mutations reach the tree, and replay rebuilds the same state. Readers take
// no lock: the tree serves them concurrently with the writer.
//
// BulkInsert is atomic for readers too. While it applies a batch, bulkSeq is
// odd and bulk is held; Find, GetRange and Count read between two loads of
// bulkSeq and retry if a batch overlapped them, waiting on bulk if one is
// running. So outside a batch a read costs two atomic loads of a line that
// only BulkInsert writes, and readers still write nothing.
//
// Note that the tree itself lives only in memory; see docs/DURABILITY.md.
type DurableBTree struct {
	tree *kv.Store
	wal  *WAL
	mu   sync.Mutex

	bulk    sync.RWMutex
	bulkSeq atomic.Uint64

	config DurableConfig
}

// stable runs read so that it sees each BulkInsert entirely or not at all.
// read may run more than once.
func (db *DurableBTree) stable(read func()) {
	for {
		s := db.bulkSeq.Load()
		if s&1 != 0 {
			db.bulk.RLock() // wait for the batch to finish
			db.bulk.RUnlock()
			continue
		}
		read()
		if db.bulkSeq.Load() == s {
			return
		}
	}
}

// DurableConfig configures the durable B-Tree.
type DurableConfig struct {
	// WALPath is the path to the WAL file (required).
	WALPath string

	// SyncMode controls WAL durability (default: SyncBatch).
	SyncMode SyncMode

	// BatchSize for SyncBatch mode (default: 100).
	BatchSize int
}

// DurableStats summarises the durable B-Tree.
type DurableStats struct {
	TreeStats kv.Stats
	WALStats  WALStats
}

// NewDurableBTree creates a new durable B-Tree with WAL. If a WAL already
// exists at the path, its entries are replayed to restore state.
func NewDurableBTree(config DurableConfig) (*DurableBTree, error) {
	if config.WALPath == "" {
		return nil, fmt.Errorf("WAL path is required")
	}

	// SyncBatch is the zero value of SyncMode, so a config that leaves it unset
	// is durable by default. Callers who want maximum throughput can set
	// SyncNone explicitly.
	wal, err := NewWAL(WALConfig{
		Path:      config.WALPath,
		SyncMode:  config.SyncMode,
		BatchSize: config.BatchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create WAL: %w", err)
	}

	tree := kv.New()

	db := &DurableBTree{
		tree:   tree,
		wal:    wal,
		config: config,
	}

	// Replay the WAL to restore tree state.
	count, err := db.recover()
	if err != nil {
		wal.Close()
		return nil, fmt.Errorf("failed to recover from WAL: %w", err)
	}

	_ = count // number of entries replayed on recovery

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
		case OpBatch:
			keys, values, err := DecodeBatch(entry.Value)
			if err != nil {
				return err
			}
			return db.tree.BulkInsert(keys, values)
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
func (db *DurableBTree) Find(key Keytype) (v Valuetype, err error) {
	db.stable(func() { v, err = db.tree.Find(key) })
	return v, err
}

// Get is an alias for Find.
func (db *DurableBTree) Get(key Keytype) (Valuetype, error) {
	return db.Find(key)
}

// GetRange returns all key-value pairs in the range (read-only, no WAL).
func (db *DurableBTree) GetRange(startKey, endKey Keytype) (keys []Keytype, values []Valuetype, err error) {
	db.stable(func() { keys, values, err = db.tree.GetRange(startKey, endKey) })
	return keys, values, err
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

// BulkInsert inserts multiple key-value pairs with WAL durability, as one
// atomic batch: the batch is one WAL entry, synced before it is applied, so
// recovery restores all of it or none; and Find, GetRange and Count see all of
// it or none (see DurableBTree). ForEach is not a snapshot and may see part of
// a batch.
func (db *DurableBTree) BulkInsert(keys []Keytype, values []Valuetype) error {
	if len(keys) != len(values) {
		return fmt.Errorf("keys and values length mismatch")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	if _, err := db.wal.AppendBatch(keys, values); err != nil {
		return fmt.Errorf("WAL bulk insert failed: %w", err)
	}
	if err := db.wal.Sync(); err != nil {
		return fmt.Errorf("WAL sync failed: %w", err)
	}

	db.bulk.Lock()
	db.bulkSeq.Add(1)
	err := db.tree.BulkInsert(keys, values)
	db.bulkSeq.Add(1)
	db.bulk.Unlock()
	return err
}

// Count returns the total number of keys.
func (db *DurableBTree) Count() (n int64) {
	db.stable(func() { n = db.tree.Count() })
	return n
}

// ForEach calls fn on copies of the entries in key order until fn returns
// false. It is not a snapshot: it may see part of a concurrent write.
func (db *DurableBTree) ForEach(fn func(key Keytype, value Valuetype) bool) {
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
