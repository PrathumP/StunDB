package bptree

import (
	"fmt"
	"testing"
)

// ==================== SecondaryIndex Benchmarks ====================

func BenchmarkSecondaryIndexInsertUnique(b *testing.B) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "email",
		Extractor: JSONFieldExtractor("email"),
		Unique:    true,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i))
		idx.Index(key, value)
	}
}

func BenchmarkSecondaryIndexInsertNonUnique(b *testing.B) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "city",
		Extractor: JSONFieldExtractor("city"),
		Unique:    false,
	})

	cities := []string{"NYC", "LA", "Chicago", "Houston", "Phoenix"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"city":"%s"}`, cities[i%len(cities)]))
		idx.Index(key, value)
	}
}

func BenchmarkSecondaryIndexFindUnique(b *testing.B) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "email",
		Extractor: JSONFieldExtractor("email"),
		Unique:    true,
	})

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i))
		idx.Index(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.FindOne([]byte(fmt.Sprintf("user%d@example.com", i%10000)))
	}
}

func BenchmarkSecondaryIndexFindNonUnique(b *testing.B) {
	idx := NewSecondaryIndex(IndexConfig{
		Name:      "city",
		Extractor: JSONFieldExtractor("city"),
		Unique:    false,
	})

	cities := []string{"NYC", "LA", "Chicago", "Houston", "Phoenix"}

	// Pre-populate - each city gets 2000 users
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"city":"%s"}`, cities[i%len(cities)]))
		idx.Index(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.FindAll([]byte(cities[i%len(cities)]))
	}
}

// ==================== IndexedBTree Benchmarks ====================

func BenchmarkIndexedBTreeInsert(b *testing.B) {
	configs := []struct {
		name       string
		numIndexes int
	}{
		{"NoIndexes", 0},
		{"1Index", 1},
		{"3Indexes", 3},
	}

	for _, cfg := range configs {
		b.Run(cfg.name, func(b *testing.B) {
			db := NewIndexedBTree(IndexedConfig{NumShards: 8})

			if cfg.numIndexes >= 1 {
				db.CreateIndex("email", JSONFieldExtractor("email"), true)
			}
			if cfg.numIndexes >= 2 {
				db.CreateIndex("city", JSONFieldExtractor("city"), false)
			}
			if cfg.numIndexes >= 3 {
				db.CreateIndex("name", JSONFieldExtractor("name"), true)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				key := []byte(fmt.Sprintf("user:%d", i))
				value := []byte(fmt.Sprintf(`{"email":"user%d@example.com","city":"NYC","name":"User%d"}`, i, i))
				db.Insert(key, value)
			}
		})
	}
}

func BenchmarkIndexedBTreeFindByPrimaryKey(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i))
		db.Insert(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Find([]byte(fmt.Sprintf("user:%d", i%10000)))
	}
}

func BenchmarkIndexedBTreeFindByIndex(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i))
		db.Insert(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.FindByIndex("email", []byte(fmt.Sprintf("user%d@example.com", i%10000)))
	}
}

func BenchmarkIndexedBTreeFindByIndexThenPrimary(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"email":"user%d@example.com","name":"User %d"}`, i, i))
		db.Insert(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Find by index, then fetch full record
		pk, _ := db.FindByIndex("email", []byte(fmt.Sprintf("user%d@example.com", i%10000)))
		db.Find(pk)
	}
}

func BenchmarkIndexedBTreeUpdate(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
	db.CreateIndex("email", JSONFieldExtractor("email"), true)
	db.CreateIndex("city", JSONFieldExtractor("city"), false)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"email":"user%d@example.com","city":"NYC"}`, i))
		db.Insert(key, value)
	}

	cities := []string{"LA", "Chicago", "Houston", "Phoenix", "Seattle"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("user:%d", i%10000))
		newValue := []byte(fmt.Sprintf(`{"email":"user%d@example.com","city":"%s"}`, i%10000, cities[i%len(cities)]))
		db.Update(key, newValue)
	}
}

func BenchmarkIndexedBTreeDelete(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()

		db := NewIndexedBTree(IndexedConfig{NumShards: 8})
		db.CreateIndex("email", JSONFieldExtractor("email"), true)

		// Pre-populate
		for j := 0; j < 1000; j++ {
			key := []byte(fmt.Sprintf("user:%d", j))
			value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, j))
			db.Insert(key, value)
		}

		b.StartTimer()

		// Delete all
		for j := 0; j < 1000; j++ {
			db.Delete([]byte(fmt.Sprintf("user:%d", j)))
		}
	}
}

// ==================== Concurrent Benchmarks ====================

func BenchmarkIndexedBTreeConcurrentInsert(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 16})
	db.CreateIndex("id", JSONFieldExtractor("id"), true)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := []byte(fmt.Sprintf("user:%d_%d", i, b.N))
			value := []byte(fmt.Sprintf(`{"id":"id_%d_%d"}`, i, b.N))
			db.Insert(key, value)
			i++
		}
	})
}

func BenchmarkIndexedBTreeConcurrentFind(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 16})
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i))
		db.Insert(key, value)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			db.FindByIndex("email", []byte(fmt.Sprintf("user%d@example.com", i%10000)))
			i++
		}
	})
}

func BenchmarkIndexedBTreeConcurrentMixed(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 16})
	db.CreateIndex("id", JSONFieldExtractor("id"), false) // non-unique for this test

	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"id":"id_%d"}`, i%100)) // 10 users per id
		db.Insert(key, value)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 < 8 { // 80% reads
				db.FindAllByIndex("id", []byte(fmt.Sprintf("id_%d", i%100)))
			} else { // 20% writes
				key := []byte(fmt.Sprintf("new:%d", i))
				value := []byte(fmt.Sprintf(`{"id":"id_%d"}`, i%100))
				db.Insert(key, value)
			}
			i++
		}
	})
}

// ==================== Extractor Benchmarks ====================

func BenchmarkJSONFieldExtractor(b *testing.B) {
	extractor := JSONFieldExtractor("email")
	value := []byte(`{"name":"Alice","email":"alice@example.com","age":30,"city":"NYC"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractor(value)
	}
}

func BenchmarkCompositeExtractor(b *testing.B) {
	extractor := CompositeExtractor(
		JSONFieldExtractor("city"),
		JSONFieldExtractor("name"),
	)
	value := []byte(`{"name":"Alice","email":"alice@example.com","age":30,"city":"NYC"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractor(value)
	}
}

// ==================== Comparison Benchmarks ====================

func BenchmarkPrimaryKeyVsIndexLookup(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
	db.CreateIndex("email", JSONFieldExtractor("email"), true)

	// Pre-populate
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i))
		db.Insert(key, value)
	}

	b.Run("PrimaryKey", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			db.Find([]byte(fmt.Sprintf("user:%d", i%10000)))
		}
	})

	b.Run("IndexLookup", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			db.FindByIndex("email", []byte(fmt.Sprintf("user%d@example.com", i%10000)))
		}
	})

	b.Run("IndexThenPrimary", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			pk, _ := db.FindByIndex("email", []byte(fmt.Sprintf("user%d@example.com", i%10000)))
			db.Find(pk)
		}
	})
}

func BenchmarkIndexOverhead(b *testing.B) {
	b.Run("WithoutIndex", func(b *testing.B) {
		db := NewIndexedBTree(IndexedConfig{NumShards: 8})

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("user:%d", i))
			value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i))
			db.tree.Insert(key, value) // Direct to tree, bypass index
		}
	})

	b.Run("With1Index", func(b *testing.B) {
		db := NewIndexedBTree(IndexedConfig{NumShards: 8})
		db.CreateIndex("email", JSONFieldExtractor("email"), true)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("user:%d", i))
			value := []byte(fmt.Sprintf(`{"email":"user%d@example.com"}`, i))
			db.Insert(key, value)
		}
	})

	b.Run("With3Indexes", func(b *testing.B) {
		db := NewIndexedBTree(IndexedConfig{NumShards: 8})
		db.CreateIndex("email", JSONFieldExtractor("email"), true)
		db.CreateIndex("city", JSONFieldExtractor("city"), false)
		db.CreateIndex("name", JSONFieldExtractor("name"), true)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			key := []byte(fmt.Sprintf("user:%d", i))
			value := []byte(fmt.Sprintf(`{"email":"u%d@e.com","city":"NYC","name":"U%d"}`, i, i))
			db.Insert(key, value)
		}
	})
}

// ==================== Range Query Benchmarks ====================

func BenchmarkIndexRangeQuery(b *testing.B) {
	db := NewIndexedBTree(IndexedConfig{NumShards: 8})
	db.CreateIndex("name", JSONFieldExtractor("name"), true)

	// Pre-populate with sorted names
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("user:%d", i))
		value := []byte(fmt.Sprintf(`{"name":"user_%05d"}`, i)) // Zero-padded for sorting
		db.Insert(key, value)
	}

	ranges := []struct {
		name  string
		start int
		end   int
	}{
		{"Small_10", 0, 10},
		{"Medium_100", 0, 100},
		{"Large_1000", 0, 1000},
	}

	for _, r := range ranges {
		b.Run(r.name, func(b *testing.B) {
			startKey := []byte(fmt.Sprintf("user_%05d", r.start))
			endKey := []byte(fmt.Sprintf("user_%05d", r.end))

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				db.FindRangeByIndex("name", startKey, endKey)
			}
		})
	}
}
