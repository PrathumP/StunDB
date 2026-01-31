package bptree

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ==================== WAL Basic Tests ====================

func TestWALCreation(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	// Check file exists
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Error("WAL file was not created")
	}

	// Check initial sequence
	if wal.Sequence() != 0 {
		t.Errorf("Expected initial sequence 0, got %d", wal.Sequence())
	}
}

func TestWALCreationWithMissingDir(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "subdir", "nested", "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL with nested dir: %v", err)
	}
	defer wal.Close()

	// Check file exists
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Error("WAL file was not created in nested directory")
	}
}

func TestWALRequiresPath(t *testing.T) {
	_, err := NewWAL(WALConfig{})
	if err == nil {
		t.Error("Expected error when path is empty")
	}
}

// ==================== WAL Append Tests ====================

func TestWALAppendInsert(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	// Append insert
	seq, err := wal.AppendInsert([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to append insert: %v", err)
	}

	if seq != 1 {
		t.Errorf("Expected sequence 1, got %d", seq)
	}

	// Append another
	seq, err = wal.AppendInsert([]byte("key2"), []byte("value2"))
	if err != nil {
		t.Fatalf("Failed to append insert: %v", err)
	}

	if seq != 2 {
		t.Errorf("Expected sequence 2, got %d", seq)
	}

	// Check stats
	stats := wal.Stats()
	if stats.TotalWrites != 2 {
		t.Errorf("Expected 2 total writes, got %d", stats.TotalWrites)
	}
}

func TestWALAppendDelete(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	seq, err := wal.AppendDelete([]byte("key1"))
	if err != nil {
		t.Fatalf("Failed to append delete: %v", err)
	}

	if seq != 1 {
		t.Errorf("Expected sequence 1, got %d", seq)
	}
}

func TestWALAppendClear(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	seq, err := wal.AppendClear()
	if err != nil {
		t.Fatalf("Failed to append clear: %v", err)
	}

	if seq != 1 {
		t.Errorf("Expected sequence 1, got %d", seq)
	}
}

// ==================== WAL Replay Tests ====================

func TestWALReplay(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL and write entries
	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	testData := []struct {
		op    OpType
		key   []byte
		value []byte
	}{
		{OpInsert, []byte("key1"), []byte("value1")},
		{OpInsert, []byte("key2"), []byte("value2")},
		{OpDelete, []byte("key1"), nil},
		{OpInsert, []byte("key3"), []byte("value3")},
	}

	for _, td := range testData {
		_, err := wal.Append(td.op, td.key, td.value)
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}
	wal.Close()

	// Reopen and replay
	wal2, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer wal2.Close()

	// Sequence should be recovered
	if wal2.Sequence() != 4 {
		t.Errorf("Expected recovered sequence 4, got %d", wal2.Sequence())
	}

	// Replay and verify entries
	var entries []*LogEntry
	count, err := wal2.Replay(func(entry *LogEntry) error {
		entries = append(entries, entry)
		return nil
	})

	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	if count != 4 {
		t.Errorf("Expected 4 entries, got %d", count)
	}

	// Verify each entry
	for i, td := range testData {
		if entries[i].Op != td.op {
			t.Errorf("Entry %d: expected op %d, got %d", i, td.op, entries[i].Op)
		}
		if !bytes.Equal(entries[i].Key, td.key) {
			t.Errorf("Entry %d: key mismatch", i)
		}
		if !bytes.Equal(entries[i].Value, td.value) {
			t.Errorf("Entry %d: value mismatch", i)
		}
	}
}

func TestWALReplayWithTreeRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL and simulate operations
	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Insert some keys
	wal.AppendInsert([]byte("key1"), []byte("value1"))
	wal.AppendInsert([]byte("key2"), []byte("value2"))
	wal.AppendInsert([]byte("key3"), []byte("value3"))
	wal.AppendDelete([]byte("key2"))
	wal.Close()

	// Reopen and recover to a tree
	wal2, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer wal2.Close()

	tree := NewShardedBTree(ShardConfig{NumShards: 4})

	_, err = wal2.Replay(func(entry *LogEntry) error {
		switch entry.Op {
		case OpInsert:
			tree.Insert(entry.Key, entry.Value)
		case OpDelete:
			tree.Delete(entry.Key)
		case OpClear:
			tree.Clear()
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	// Verify tree state
	v1, err := tree.Find([]byte("key1"))
	if err != nil || !bytes.Equal(v1, []byte("value1")) {
		t.Error("key1 should exist with value1")
	}

	_, err = tree.Find([]byte("key2"))
	if err == nil {
		t.Error("key2 should have been deleted")
	}

	v3, err := tree.Find([]byte("key3"))
	if err != nil || !bytes.Equal(v3, []byte("value3")) {
		t.Error("key3 should exist with value3")
	}
}

// ==================== WAL Checkpointing Tests ====================

func TestWALCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write some entries
	for i := 0; i < 100; i++ {
		_, err := wal.AppendInsert([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	// Get file size before checkpoint
	stats := wal.Stats()
	sizeBefore := stats.FileSize

	// Checkpoint
	if err := wal.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}

	// Get file size after checkpoint
	stats = wal.Stats()
	sizeAfter := stats.FileSize

	if sizeAfter >= sizeBefore {
		t.Errorf("File should be smaller after checkpoint: before=%d, after=%d", sizeBefore, sizeAfter)
	}

	// Sequence should continue
	seq, err := wal.AppendInsert([]byte("new_key"), []byte("new_value"))
	if err != nil {
		t.Fatalf("Failed to append after checkpoint: %v", err)
	}

	if seq != 101 {
		t.Errorf("Expected sequence 101 after checkpoint, got %d", seq)
	}

	wal.Close()
}

// ==================== WAL Sync Mode Tests ====================

func TestWALSyncModes(t *testing.T) {
	modes := []struct {
		name string
		mode SyncMode
	}{
		{"SyncNone", SyncNone},
		{"SyncBatch", SyncBatch},
		{"SyncAlways", SyncAlways},
	}

	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			walPath := filepath.Join(tmpDir, "test.wal")

			wal, err := NewWAL(WALConfig{
				Path:      walPath,
				SyncMode:  m.mode,
				BatchSize: 10,
			})
			if err != nil {
				t.Fatalf("Failed to create WAL with %s: %v", m.name, err)
			}

			// Write entries
			for i := 0; i < 25; i++ {
				_, err := wal.AppendInsert([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
				if err != nil {
					t.Fatalf("Failed to append: %v", err)
				}
			}

			stats := wal.Stats()

			// Verify writes were recorded
			if stats.TotalWrites != 25 {
				t.Errorf("Expected 25 writes, got %d", stats.TotalWrites)
			}

			// SyncAlways should have 25 syncs, SyncBatch should have 2 (at 10, 20)
			if m.mode == SyncAlways && stats.TotalSyncs != 25 {
				t.Errorf("SyncAlways: expected 25 syncs, got %d", stats.TotalSyncs)
			}
			if m.mode == SyncBatch && stats.TotalSyncs < 2 {
				t.Errorf("SyncBatch: expected at least 2 syncs, got %d", stats.TotalSyncs)
			}

			wal.Close()

			// Verify replay still works
			wal2, err := NewWAL(WALConfig{Path: walPath})
			if err != nil {
				t.Fatalf("Failed to reopen WAL: %v", err)
			}
			defer wal2.Close()

			count, err := wal2.Replay(func(entry *LogEntry) error { return nil })
			if err != nil {
				t.Fatalf("Replay failed: %v", err)
			}
			if count != 25 {
				t.Errorf("Expected 25 entries on replay, got %d", count)
			}
		})
	}
}

// ==================== WAL Rotation Tests ====================

func TestWALRotation(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Write some entries
	for i := 0; i < 50; i++ {
		wal.AppendInsert([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
	}

	// Rotate log
	archivePath, err := wal.RotateLog()
	if err != nil {
		t.Fatalf("Rotation failed: %v", err)
	}

	// Check archive exists
	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		t.Error("Archive file was not created")
	}

	// Check new WAL exists
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		t.Error("New WAL file was not created")
	}

	// Sequence should continue
	seq, err := wal.AppendInsert([]byte("after_rotate"), []byte("value"))
	if err != nil {
		t.Fatalf("Failed to append after rotation: %v", err)
	}
	if seq != 51 {
		t.Errorf("Expected sequence 51, got %d", seq)
	}

	wal.Close()

	// Verify archive can be replayed
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("Failed to open archive: %v", err)
	}
	defer archiveFile.Close()

	// Skip header
	archiveFile.Seek(8, io.SeekStart)
	reader := bufio.NewReader(archiveFile)

	archiveCount := 0
	for {
		_, err := readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read archive entry: %v", err)
		}
		archiveCount++
	}

	if archiveCount != 50 {
		t.Errorf("Archive should have 50 entries, got %d", archiveCount)
	}
}

// ==================== WAL Concurrent Tests ====================

func TestWALConcurrentAppends(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath, SyncMode: SyncNone})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	const numGoroutines = 10
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := []byte(fmt.Sprintf("g%d_key%d", id, i))
				value := []byte(fmt.Sprintf("g%d_value%d", id, i))
				_, err := wal.AppendInsert(key, value)
				if err != nil {
					t.Errorf("Concurrent append failed: %v", err)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify all entries were written
	stats := wal.Stats()
	expectedWrites := uint64(numGoroutines * opsPerGoroutine)
	if stats.TotalWrites != expectedWrites {
		t.Errorf("Expected %d writes, got %d", expectedWrites, stats.TotalWrites)
	}

	// Verify sequence
	if wal.Sequence() != expectedWrites {
		t.Errorf("Expected sequence %d, got %d", expectedWrites, wal.Sequence())
	}
}

// ==================== WAL Edge Case Tests ====================

func TestWALEmptyKeyValue(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Empty key
	_, err = wal.AppendInsert([]byte{}, []byte("value"))
	if err != nil {
		t.Errorf("Empty key should be allowed: %v", err)
	}

	// Empty value
	_, err = wal.AppendInsert([]byte("key"), []byte{})
	if err != nil {
		t.Errorf("Empty value should be allowed: %v", err)
	}

	// Both empty
	_, err = wal.AppendInsert([]byte{}, []byte{})
	if err != nil {
		t.Errorf("Empty key and value should be allowed: %v", err)
	}

	wal.Close()

	// Verify replay
	wal2, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer wal2.Close()

	count, err := wal2.Replay(func(entry *LogEntry) error { return nil })
	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}
	if count != 3 {
		t.Errorf("Expected 3 entries, got %d", count)
	}
}

func TestWALLargeValues(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Large key and value (1MB each)
	largeKey := make([]byte, 1024*1024)
	largeValue := make([]byte, 1024*1024)
	for i := range largeKey {
		largeKey[i] = byte(i % 256)
		largeValue[i] = byte((i + 1) % 256)
	}

	_, err = wal.AppendInsert(largeKey, largeValue)
	if err != nil {
		t.Fatalf("Large value insert failed: %v", err)
	}

	wal.Close()

	// Verify replay
	wal2, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer wal2.Close()

	var recovered *LogEntry
	_, err = wal2.Replay(func(entry *LogEntry) error {
		recovered = entry
		return nil
	})

	if err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	if !bytes.Equal(recovered.Key, largeKey) {
		t.Error("Large key mismatch after replay")
	}
	if !bytes.Equal(recovered.Value, largeValue) {
		t.Error("Large value mismatch after replay")
	}
}

func TestWALCorruptedEntry(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create WAL with valid entries
	wal, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	for i := 0; i < 5; i++ {
		wal.AppendInsert([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
	}
	wal.Close()

	// Corrupt the file by appending garbage
	f, err := os.OpenFile(walPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open file for corruption: %v", err)
	}
	f.Write([]byte("garbage data that will corrupt parsing"))
	f.Close()

	// Reopen - should recover valid entries
	wal2, err := NewWAL(WALConfig{Path: walPath})
	if err != nil {
		t.Fatalf("Failed to reopen corrupted WAL: %v", err)
	}
	defer wal2.Close()

	// Replay should stop at corruption but return valid entries
	count, err := wal2.Replay(func(entry *LogEntry) error { return nil })
	if err != nil {
		t.Fatalf("Replay should succeed for valid entries: %v", err)
	}

	// Should recover at least some entries (exact number depends on corruption)
	if count < 1 {
		t.Error("Should recover at least some entries from corrupted WAL")
	}
}

// ==================== WAL Header Tests ====================

func TestWALInvalidHeader(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create file with invalid header
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	binary.Write(f, binary.LittleEndian, uint32(0x12345678)) // Wrong magic
	binary.Write(f, binary.LittleEndian, uint32(1))          // Version
	f.Close()

	// Should fail to open
	_, err = NewWAL(WALConfig{Path: walPath})
	if err == nil {
		t.Error("Expected error for invalid magic number")
	}
}

func TestWALUnsupportedVersion(t *testing.T) {
	tmpDir := t.TempDir()
	walPath := filepath.Join(tmpDir, "test.wal")

	// Create file with unsupported version
	f, err := os.Create(walPath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	binary.Write(f, binary.LittleEndian, uint32(walMagic)) // Correct magic
	binary.Write(f, binary.LittleEndian, uint32(99))       // Future version
	f.Close()

	// Should fail to open
	_, err = NewWAL(WALConfig{Path: walPath})
	if err == nil {
		t.Error("Expected error for unsupported version")
	}
}

// ==================== Checksum Tests ====================

func TestWALChecksumVerification(t *testing.T) {
	entry := &LogEntry{
		Sequence: 42,
		Op:       OpInsert,
		Key:      []byte("test_key"),
		Value:    []byte("test_value"),
	}

	checksum1 := calculateEntryChecksum(entry)
	checksum2 := calculateEntryChecksum(entry)

	if checksum1 != checksum2 {
		t.Error("Checksum should be deterministic")
	}

	// Modify entry and checksum should change
	entry.Key = []byte("different_key")
	checksum3 := calculateEntryChecksum(entry)

	if checksum1 == checksum3 {
		t.Error("Checksum should change when data changes")
	}
}
