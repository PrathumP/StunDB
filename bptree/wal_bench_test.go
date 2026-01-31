package bptree

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ==================== WAL Benchmarks ====================

func BenchmarkWALAppendInsert(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	wal, err := NewWAL(WALConfig{Path: walPath, SyncMode: SyncNone})
	if err != nil {
		b.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	key := []byte("benchmark_key")
	value := []byte("benchmark_value_with_some_data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := wal.AppendInsert(key, value)
		if err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}
}

func BenchmarkWALSyncModes(b *testing.B) {
	modes := []struct {
		name string
		mode SyncMode
	}{
		{"SyncNone", SyncNone},
		{"SyncBatch_100", SyncBatch},
		{"SyncAlways", SyncAlways},
	}

	for _, m := range modes {
		b.Run(m.name, func(b *testing.B) {
			tmpDir := b.TempDir()
			walPath := filepath.Join(tmpDir, "bench.wal")

			wal, err := NewWAL(WALConfig{
				Path:      walPath,
				SyncMode:  m.mode,
				BatchSize: 100,
			})
			if err != nil {
				b.Fatalf("Failed to create WAL: %v", err)
			}
			defer wal.Close()

			key := []byte("benchmark_key")
			value := []byte("benchmark_value")

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				wal.AppendInsert(key, value)
			}
		})
	}
}

func BenchmarkWALConcurrentAppends(b *testing.B) {
	goroutines := []int{1, 2, 4, 8, 16}

	for _, g := range goroutines {
		b.Run(fmt.Sprintf("Goroutines_%d", g), func(b *testing.B) {
			tmpDir := b.TempDir()
			walPath := filepath.Join(tmpDir, "bench.wal")

			wal, err := NewWAL(WALConfig{Path: walPath, SyncMode: SyncNone})
			if err != nil {
				b.Fatalf("Failed to create WAL: %v", err)
			}
			defer wal.Close()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				key := []byte("concurrent_key")
				value := []byte("concurrent_value")
				for pb.Next() {
					wal.AppendInsert(key, value)
				}
			})
		})
	}
}

func BenchmarkWALReplay(b *testing.B) {
	sizes := []int{1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Entries_%d", size), func(b *testing.B) {
			tmpDir := b.TempDir()
			walPath := filepath.Join(tmpDir, "bench.wal")

			// Pre-populate WAL
			wal, _ := NewWAL(WALConfig{Path: walPath, SyncMode: SyncNone})
			for i := 0; i < size; i++ {
				wal.AppendInsert(
					[]byte(fmt.Sprintf("key%d", i)),
					[]byte(fmt.Sprintf("value%d", i)),
				)
			}
			wal.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				wal2, _ := NewWAL(WALConfig{Path: walPath})
				wal2.Replay(func(entry *LogEntry) error { return nil })
				wal2.Close()
			}
		})
	}
}

// ==================== DurableBTree Benchmarks ====================

func BenchmarkDurableBTreeInsert(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	db, err := NewDurableBTree(DurableConfig{
		WALPath:  walPath,
		SyncMode: SyncNone,
	})
	if err != nil {
		b.Fatalf("Failed to create DurableBTree: %v", err)
	}
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		db.Insert(key, value)
	}
}

func BenchmarkDurableBTreeFind(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	db, err := NewDurableBTree(DurableConfig{
		WALPath:  walPath,
		SyncMode: SyncNone,
	})
	if err != nil {
		b.Fatalf("Failed to create DurableBTree: %v", err)
	}
	defer db.Close()

	// Pre-populate
	for i := 0; i < 10000; i++ {
		db.Insert([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i%10000))
		db.Find(key)
	}
}

func BenchmarkDurableBTreeMixed(b *testing.B) {
	ratios := []struct {
		name   string
		reads  int
		writes int
	}{
		{"Read_Heavy_10R_1W", 10, 1},
		{"Balanced_5R_5W", 5, 5},
		{"Write_Heavy_1R_10W", 1, 10},
	}

	for _, r := range ratios {
		b.Run(r.name, func(b *testing.B) {
			tmpDir := b.TempDir()
			walPath := filepath.Join(tmpDir, "bench.wal")

			db, _ := NewDurableBTree(DurableConfig{
				WALPath:  walPath,
				SyncMode: SyncNone,
			})
			defer db.Close()

			// Pre-populate
			for i := 0; i < 1000; i++ {
				db.Insert([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
			}

			total := r.reads + r.writes

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				op := i % total
				if op < r.reads {
					db.Find([]byte(fmt.Sprintf("key%d", i%1000)))
				} else {
					db.Insert([]byte(fmt.Sprintf("new%d", i)), []byte("value"))
				}
			}
		})
	}
}

func BenchmarkDurableBTreeConcurrent(b *testing.B) {
	tmpDir := b.TempDir()
	walPath := filepath.Join(tmpDir, "bench.wal")

	db, err := NewDurableBTree(DurableConfig{
		WALPath:   walPath,
		NumShards: 8,
		SyncMode:  SyncNone,
	})
	if err != nil {
		b.Fatalf("Failed to create DurableBTree: %v", err)
	}
	defer db.Close()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		db.Insert([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 < 8 { // 80% reads
				db.Find([]byte(fmt.Sprintf("key%d", i%1000)))
			} else { // 20% writes
				db.Insert([]byte(fmt.Sprintf("new%d", i)), []byte("value"))
			}
			i++
		}
	})
}

// ==================== Comparison: Durable vs Non-Durable ====================

func BenchmarkDurableVsNonDurable(b *testing.B) {
	b.Run("NonDurable_ShardedBTree", func(b *testing.B) {
		tree := NewShardedBTree(ShardConfig{NumShards: 8})

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("key%d", i))
			value := []byte(fmt.Sprintf("value%d", i))
			tree.Insert(key, value)
		}
	})

	b.Run("Durable_SyncNone", func(b *testing.B) {
		tmpDir := b.TempDir()
		walPath := filepath.Join(tmpDir, "bench.wal")

		db, _ := NewDurableBTree(DurableConfig{
			WALPath:   walPath,
			NumShards: 8,
			SyncMode:  SyncNone,
		})
		defer db.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("key%d", i))
			value := []byte(fmt.Sprintf("value%d", i))
			db.Insert(key, value)
		}
	})

	b.Run("Durable_SyncBatch", func(b *testing.B) {
		tmpDir := b.TempDir()
		walPath := filepath.Join(tmpDir, "bench.wal")

		db, _ := NewDurableBTree(DurableConfig{
			WALPath:   walPath,
			NumShards: 8,
			SyncMode:  SyncBatch,
			BatchSize: 100,
		})
		defer db.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("key%d", i))
			value := []byte(fmt.Sprintf("value%d", i))
			db.Insert(key, value)
		}
	})

	b.Run("Durable_SyncAlways", func(b *testing.B) {
		tmpDir := b.TempDir()
		walPath := filepath.Join(tmpDir, "bench.wal")

		db, _ := NewDurableBTree(DurableConfig{
			WALPath:   walPath,
			NumShards: 8,
			SyncMode:  SyncAlways,
		})
		defer db.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("key%d", i))
			value := []byte(fmt.Sprintf("value%d", i))
			db.Insert(key, value)
		}
	})
}

// ==================== Recovery Benchmarks ====================

func BenchmarkRecoveryTime(b *testing.B) {
	sizes := []int{1000, 10000, 50000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Entries_%d", size), func(b *testing.B) {
			tmpDir := b.TempDir()
			walPath := filepath.Join(tmpDir, "bench.wal")

			// Pre-populate
			db, _ := NewDurableBTree(DurableConfig{
				WALPath:  walPath,
				SyncMode: SyncNone,
			})
			for i := 0; i < size; i++ {
				db.Insert([]byte(fmt.Sprintf("key%d", i)), []byte(fmt.Sprintf("value%d", i)))
			}
			db.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				db2, _ := NewDurableBTree(DurableConfig{WALPath: walPath})
				db2.Close()
			}
		})
	}
}

// ==================== Checkpoint Benchmarks ====================

func BenchmarkCheckpoint(b *testing.B) {
	sizes := []int{1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("AfterEntries_%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()

				tmpDir, _ := os.MkdirTemp("", "bench_checkpoint")
				walPath := filepath.Join(tmpDir, "bench.wal")

				db, _ := NewDurableBTree(DurableConfig{
					WALPath:  walPath,
					SyncMode: SyncNone,
				})

				for j := 0; j < size; j++ {
					db.Insert([]byte(fmt.Sprintf("key%d", j)), []byte(fmt.Sprintf("value%d", j)))
				}

				b.StartTimer()
				db.Checkpoint()
				b.StopTimer()

				db.Close()
				os.RemoveAll(tmpDir)
			}
		})
	}
}

// ==================== Concurrent Read/Write Scaling ====================

func BenchmarkDurableConcurrentScaling(b *testing.B) {
	goroutines := []int{1, 2, 4, 8, 16}

	for _, g := range goroutines {
		b.Run(fmt.Sprintf("Writers_%d", g), func(b *testing.B) {
			tmpDir := b.TempDir()
			walPath := filepath.Join(tmpDir, "bench.wal")

			db, _ := NewDurableBTree(DurableConfig{
				WALPath:   walPath,
				NumShards: 16,
				SyncMode:  SyncNone,
			})
			defer db.Close()

			b.ResetTimer()

			var wg sync.WaitGroup
			opsPerGoroutine := b.N / g
			if opsPerGoroutine == 0 {
				opsPerGoroutine = 1
			}

			wg.Add(g)
			for i := 0; i < g; i++ {
				go func(id int) {
					defer wg.Done()
					for j := 0; j < opsPerGoroutine; j++ {
						key := []byte(fmt.Sprintf("g%d_key%d", id, j))
						value := []byte(fmt.Sprintf("value%d", j))
						db.Insert(key, value)
					}
				}(i)
			}
			wg.Wait()
		})
	}
}
