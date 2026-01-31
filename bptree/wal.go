package bptree

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// WAL (Write-Ahead Log) provides durability for the B-Tree.
//
// DESIGN:
// - All mutations are logged BEFORE being applied to the tree
// - On crash, replay the log to recover the tree state
// - Checkpointing truncates the log after tree is persisted
//
// LOG FORMAT:
// Each entry: [length:4][sequence:8][op:1][keyLen:4][key][valueLen:4][value][checksum:4]
//
// DURABILITY LEVELS:
// - SyncNone: No fsync (fastest, least durable)
// - SyncBatch: Fsync every N entries
// - SyncAlways: Fsync every entry (slowest, most durable)
type WAL struct {
	file     *os.File
	mu       sync.Mutex
	sequence uint64
	path     string

	// Configuration
	syncMode   SyncMode
	batchSize  int
	batchCount int

	// Statistics
	totalWrites    uint64
	totalBytes     uint64
	totalSyncs     uint64
	lastCheckpoint uint64

	// Buffered writer for performance
	writer *bufio.Writer
}

// SyncMode controls when the WAL flushes to disk.
type SyncMode int

const (
	// SyncNone never calls fsync (rely on OS)
	SyncNone SyncMode = iota
	// SyncBatch calls fsync every N entries
	SyncBatch
	// SyncAlways calls fsync after every entry
	SyncAlways
)

// OpType represents the type of operation in the log.
type OpType byte

const (
	OpInsert OpType = iota + 1
	OpDelete
	OpClear
)

// LogEntry represents a single entry in the WAL.
type LogEntry struct {
	Sequence uint64
	Op       OpType
	Key      []byte
	Value    []byte
	Checksum uint32
}

// WALConfig configures the WAL behavior.
type WALConfig struct {
	// Path to the WAL file
	Path string
	// SyncMode controls durability vs performance tradeoff
	SyncMode SyncMode
	// BatchSize for SyncBatch mode (default: 100)
	BatchSize int
	// BufferSize for buffered writes (default: 64KB)
	BufferSize int
}

// WALStats provides statistics about WAL operations.
type WALStats struct {
	Sequence       uint64
	TotalWrites    uint64
	TotalBytes     uint64
	TotalSyncs     uint64
	LastCheckpoint uint64
	FileSize       int64
}

const (
	defaultBatchSize  = 100
	defaultBufferSize = 64 * 1024  // 64KB
	walMagic          = 0x57414C31 // "WAL1"
	walVersion        = 1
)

// Header written at the start of each WAL file
type walHeader struct {
	Magic   uint32
	Version uint32
}

// NewWAL creates a new WAL with the given configuration.
func NewWAL(config WALConfig) (*WAL, error) {
	if config.Path == "" {
		return nil, errors.New("WAL path is required")
	}

	if config.BatchSize <= 0 {
		config.BatchSize = defaultBatchSize
	}

	if config.BufferSize <= 0 {
		config.BufferSize = defaultBufferSize
	}

	// Ensure directory exists
	dir := filepath.Dir(config.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	// Open file with append mode
	file, err := os.OpenFile(config.Path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	w := &WAL{
		file:      file,
		path:      config.Path,
		syncMode:  config.SyncMode,
		batchSize: config.BatchSize,
		writer:    bufio.NewWriterSize(file, config.BufferSize),
	}

	// Check if file is empty (new WAL)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat WAL file: %w", err)
	}

	if info.Size() == 0 {
		// Write header for new file
		if err := w.writeHeader(); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to write WAL header: %w", err)
		}
	} else {
		// Validate existing header and find last sequence
		if err := w.validateAndRecover(); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to validate WAL: %w", err)
		}
	}

	return w, nil
}

// writeHeader writes the WAL file header.
func (w *WAL) writeHeader() error {
	header := walHeader{
		Magic:   walMagic,
		Version: walVersion,
	}

	if err := binary.Write(w.writer, binary.LittleEndian, header); err != nil {
		return err
	}

	return w.writer.Flush()
}

// validateAndRecover validates the WAL header and recovers the sequence number.
func (w *WAL) validateAndRecover() error {
	// Seek to beginning
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// Read header
	var header walHeader
	if err := binary.Read(w.file, binary.LittleEndian, &header); err != nil {
		return fmt.Errorf("failed to read WAL header: %w", err)
	}

	if header.Magic != walMagic {
		return errors.New("invalid WAL magic number")
	}

	if header.Version != walVersion {
		return fmt.Errorf("unsupported WAL version: %d", header.Version)
	}

	// Scan through entries to find last sequence
	reader := bufio.NewReader(w.file)
	var lastSeq uint64

	for {
		entry, err := readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Truncate corrupted tail
			break
		}
		lastSeq = entry.Sequence
	}

	w.sequence = lastSeq

	// Seek to end for appending
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	return nil
}

// Append logs an operation to the WAL.
func (w *WAL) Append(op OpType, key, value []byte) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Increment sequence
	seq := atomic.AddUint64(&w.sequence, 1)

	entry := LogEntry{
		Sequence: seq,
		Op:       op,
		Key:      key,
		Value:    value,
	}

	// Calculate checksum
	entry.Checksum = w.calculateChecksum(&entry)

	// Write entry
	if err := w.writeEntry(&entry); err != nil {
		return 0, fmt.Errorf("failed to write WAL entry: %w", err)
	}

	atomic.AddUint64(&w.totalWrites, 1)
	w.batchCount++

	// Handle sync based on mode
	if err := w.maybeSync(); err != nil {
		return 0, fmt.Errorf("failed to sync WAL: %w", err)
	}

	return seq, nil
}

// AppendInsert logs an insert operation.
func (w *WAL) AppendInsert(key, value []byte) (uint64, error) {
	return w.Append(OpInsert, key, value)
}

// AppendDelete logs a delete operation.
func (w *WAL) AppendDelete(key []byte) (uint64, error) {
	return w.Append(OpDelete, key, nil)
}

// AppendClear logs a clear operation.
func (w *WAL) AppendClear() (uint64, error) {
	return w.Append(OpClear, nil, nil)
}

// writeEntry serializes and writes a log entry.
func (w *WAL) writeEntry(entry *LogEntry) error {
	// Calculate total length
	// sequence(8) + op(1) + keyLen(4) + key + valueLen(4) + value + checksum(4)
	entryLen := 8 + 1 + 4 + len(entry.Key) + 4 + len(entry.Value) + 4

	// Write length prefix
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(entryLen)); err != nil {
		return err
	}

	// Write sequence
	if err := binary.Write(w.writer, binary.LittleEndian, entry.Sequence); err != nil {
		return err
	}

	// Write operation type
	if err := w.writer.WriteByte(byte(entry.Op)); err != nil {
		return err
	}

	// Write key
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(entry.Key))); err != nil {
		return err
	}
	if _, err := w.writer.Write(entry.Key); err != nil {
		return err
	}

	// Write value
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(entry.Value))); err != nil {
		return err
	}
	if _, err := w.writer.Write(entry.Value); err != nil {
		return err
	}

	// Write checksum
	if err := binary.Write(w.writer, binary.LittleEndian, entry.Checksum); err != nil {
		return err
	}

	atomic.AddUint64(&w.totalBytes, uint64(4+entryLen)) // length prefix + entry

	return nil
}

// readEntry reads a single log entry from the reader.
func readEntry(reader *bufio.Reader) (*LogEntry, error) {
	// Read length
	var length uint32
	if err := binary.Read(reader, binary.LittleEndian, &length); err != nil {
		return nil, err
	}

	// Read sequence
	var seq uint64
	if err := binary.Read(reader, binary.LittleEndian, &seq); err != nil {
		return nil, err
	}

	// Read operation type
	opByte, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}

	// Read key
	var keyLen uint32
	if err := binary.Read(reader, binary.LittleEndian, &keyLen); err != nil {
		return nil, err
	}
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}

	// Read value
	var valueLen uint32
	if err := binary.Read(reader, binary.LittleEndian, &valueLen); err != nil {
		return nil, err
	}
	value := make([]byte, valueLen)
	if _, err := io.ReadFull(reader, value); err != nil {
		return nil, err
	}

	// Read checksum
	var checksum uint32
	if err := binary.Read(reader, binary.LittleEndian, &checksum); err != nil {
		return nil, err
	}

	entry := &LogEntry{
		Sequence: seq,
		Op:       OpType(opByte),
		Key:      key,
		Value:    value,
		Checksum: checksum,
	}

	// Verify checksum
	expectedChecksum := calculateEntryChecksum(entry)
	if checksum != expectedChecksum {
		return nil, errors.New("checksum mismatch")
	}

	return entry, nil
}

// calculateChecksum computes CRC32 checksum for an entry.
func (w *WAL) calculateChecksum(entry *LogEntry) uint32 {
	return calculateEntryChecksum(entry)
}

// calculateEntryChecksum computes CRC32 checksum for an entry (standalone function).
func calculateEntryChecksum(entry *LogEntry) uint32 {
	h := crc32.NewIEEE()

	// Write sequence
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, entry.Sequence)
	h.Write(buf)

	// Write operation
	h.Write([]byte{byte(entry.Op)})

	// Write key
	h.Write(entry.Key)

	// Write value
	h.Write(entry.Value)

	return h.Sum32()
}

// maybeSync handles sync based on the configured mode.
func (w *WAL) maybeSync() error {
	switch w.syncMode {
	case SyncAlways:
		return w.sync()
	case SyncBatch:
		if w.batchCount >= w.batchSize {
			w.batchCount = 0
			return w.sync()
		}
		return w.writer.Flush() // At least flush to OS buffer
	default: // SyncNone
		return w.writer.Flush()
	}
}

// sync flushes the buffer and calls fsync.
func (w *WAL) sync() error {
	if err := w.writer.Flush(); err != nil {
		return err
	}
	atomic.AddUint64(&w.totalSyncs, 1)
	return w.file.Sync()
}

// Sync forces a sync to disk.
func (w *WAL) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sync()
}

// Replay reads all entries from the WAL and applies them using the callback.
// Returns the number of entries replayed.
func (w *WAL) Replay(callback func(*LogEntry) error) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Flush any pending writes
	if err := w.writer.Flush(); err != nil {
		return 0, err
	}

	// Seek to beginning (after header)
	if _, err := w.file.Seek(8, io.SeekStart); err != nil { // 8 bytes for header
		return 0, err
	}

	reader := bufio.NewReader(w.file)
	count := 0

	for {
		entry, err := readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Log corruption - stop replay at last good entry
			break
		}

		if err := callback(entry); err != nil {
			return count, fmt.Errorf("replay callback failed at seq %d: %w", entry.Sequence, err)
		}
		count++
	}

	// Seek back to end for appending
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return count, err
	}

	return count, nil
}

// Checkpoint truncates the WAL after confirming tree is persisted.
// This should be called after the tree has been fully persisted to disk.
func (w *WAL) Checkpoint() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Flush pending writes
	if err := w.writer.Flush(); err != nil {
		return err
	}

	// Sync to ensure all data is on disk
	if err := w.file.Sync(); err != nil {
		return err
	}

	// Record checkpoint sequence
	atomic.StoreUint64(&w.lastCheckpoint, w.sequence)

	// Close current file
	if err := w.file.Close(); err != nil {
		return err
	}

	// Truncate file (keep header)
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	w.file = file
	w.writer = bufio.NewWriterSize(file, defaultBufferSize)

	// Write fresh header
	if err := w.writeHeader(); err != nil {
		return err
	}

	// Note: sequence number is NOT reset - it continues incrementing
	// This ensures entries are always uniquely ordered

	return nil
}

// RotateLog rotates the WAL to a new file (for archiving).
// Returns the path to the archived file.
func (w *WAL) RotateLog() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Flush and sync
	if err := w.writer.Flush(); err != nil {
		return "", err
	}
	if err := w.file.Sync(); err != nil {
		return "", err
	}

	// Close current file
	if err := w.file.Close(); err != nil {
		return "", err
	}

	// Rename current file with sequence number
	archivePath := fmt.Sprintf("%s.%d", w.path, w.sequence)
	if err := os.Rename(w.path, archivePath); err != nil {
		return "", err
	}

	// Create new file
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return "", err
	}

	w.file = file
	w.writer = bufio.NewWriterSize(file, defaultBufferSize)

	// Write header
	if err := w.writeHeader(); err != nil {
		return "", err
	}

	return archivePath, nil
}

// Stats returns statistics about the WAL.
func (w *WAL) Stats() WALStats {
	w.mu.Lock()
	defer w.mu.Unlock()

	var fileSize int64
	if info, err := w.file.Stat(); err == nil {
		fileSize = info.Size()
	}

	return WALStats{
		Sequence:       atomic.LoadUint64(&w.sequence),
		TotalWrites:    atomic.LoadUint64(&w.totalWrites),
		TotalBytes:     atomic.LoadUint64(&w.totalBytes),
		TotalSyncs:     atomic.LoadUint64(&w.totalSyncs),
		LastCheckpoint: atomic.LoadUint64(&w.lastCheckpoint),
		FileSize:       fileSize,
	}
}

// Close closes the WAL file.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return err
	}
	if err := w.file.Sync(); err != nil {
		return err
	}
	return w.file.Close()
}

// Path returns the path to the WAL file.
func (w *WAL) Path() string {
	return w.path
}

// Sequence returns the current sequence number.
func (w *WAL) Sequence() uint64 {
	return atomic.LoadUint64(&w.sequence)
}
