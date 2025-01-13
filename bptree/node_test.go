package bptree

// func TestConcurrentInserts(t *testing.T) {
// 	tree := &Btree{}
// 	var wg sync.WaitGroup
// 	numGoroutines := 100
// 	insertsPerRoutine := 100

// 	// Concurrent inserts
// 	for i := 0; i < numGoroutines; i++ {
// 		wg.Add(1)
// 		go func(base int) {
// 			defer wg.Done()
// 			for j := 0; j < insertsPerRoutine; j++ {
// 				key := []byte(fmt.Sprintf("key%d", base+j))
// 				value := []byte(fmt.Sprintf("value%d", base+j))
// 				tree.insert(key, value)
// 			}
// 		}(i * insertsPerRoutine)
// 	}
// 	wg.Wait()

// 	// Verify all insertions
// 	for i := 0; i < numGoroutines; i++ {
// 		for j := 0; j < insertsPerRoutine; j++ {
// 			key := []byte(fmt.Sprintf("key%d", i*insertsPerRoutine+j))
// 			value, err := tree.Find(key)
// 			if err != nil {
// 				t.Errorf("Key %s not found", key)
// 			}
// 			expectedValue := []byte(fmt.Sprintf("value%d", i*insertsPerRoutine+j))
// 			if !bytes.Equal(value, expectedValue) {
// 				t.Errorf("Wrong value for key %s: got %s, want %s", key, value, expectedValue)
// 			}
// 		}
// 	}
// }

// func TestConcurrentReadsAndWrites(t *testing.T) {
// 	tree := &Btree{}
// 	var wg sync.WaitGroup
// 	numWriters := 10
// 	numReaders := 20
// 	operationsPerRoutine := 100

// 	// Pre-populate tree
// 	for i := 0; i < 1000; i++ {
// 		key := []byte(fmt.Sprintf("init_key%d", i))
// 		value := []byte(fmt.Sprintf("init_value%d", i))
// 		tree.insert(key, value)
// 	}

// 	// Start writers
// 	for i := 0; i < numWriters; i++ {
// 		wg.Add(1)
// 		go func(id int) {
// 			defer wg.Done()
// 			for j := 0; j < operationsPerRoutine; j++ {
// 				key := []byte(fmt.Sprintf("key%d_%d", id, j))
// 				value := []byte(fmt.Sprintf("value%d_%d", id, j))
// 				tree.insert(key, value)
// 				time.Sleep(time.Millisecond) // Simulate some work
// 			}
// 		}(i)
// 	}

// 	// Start readers
// 	for i := 0; i < numReaders; i++ {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			for j := 0; j < operationsPerRoutine; j++ {
// 				// Read some pre-populated keys
// 				key := []byte(fmt.Sprintf("init_key%d", j))
// 				_, err := tree.Find(key)
// 				if err != nil {
// 					// Key might have been deleted, that's OK
// 					continue
// 				}
// 				time.Sleep(time.Millisecond) // Simulate some work
// 			}
// 		}()
// 	}

// 	wg.Wait()
// }

// func TestConcurrentRangeOperations(t *testing.T) {
// 	tree := &Btree{}
// 	var wg sync.WaitGroup
// 	numRoutines := 10
// 	keysPerRange := 100

// 	// Pre-populate tree
// 	for i := 0; i < 1000; i++ {
// 		key := []byte(fmt.Sprintf("key%03d", i))
// 		value := []byte(fmt.Sprintf("value%03d", i))
// 		tree.insert(key, value)
// 	}

// 	// Concurrent range operations
// 	for i := 0; i < numRoutines; i++ {
// 		wg.Add(1)
// 		go func(base int) {
// 			defer wg.Done()
// 			startKey := []byte(fmt.Sprintf("key%03d", base))
// 			endKey := []byte(fmt.Sprintf("key%03d", base+keysPerRange))
// 			keys, _, err := tree.GetRange(startKey, endKey)
// 			if err != nil {
// 				t.Errorf("Range query failed: %v", err)
// 				return
// 			}
// 			if len(keys) == 0 {
// 				t.Errorf("Expected non-empty range result")
// 				return
// 			}
// 		}(i * keysPerRange)
// 	}

// 	wg.Wait()
// }

// func TestConcurrentDeleteRange(t *testing.T) {
// 	tree := &Btree{}
// 	var wg sync.WaitGroup
// 	numRoutines := 5
// 	keysPerRange := 20

// 	// Pre-populate tree
// 	for i := 0; i < 1000; i++ {
// 		key := []byte(fmt.Sprintf("key%03d", i))
// 		value := []byte(fmt.Sprintf("value%03d", i))
// 		tree.insert(key, value)
// 	}

// 	// Concurrent delete range operations
// 	for i := 0; i < numRoutines; i++ {
// 		wg.Add(1)
// 		go func(base int) {
// 			defer wg.Done()
// 			startKey := []byte(fmt.Sprintf("key%03d", base))
// 			endKey := []byte(fmt.Sprintf("key%03d", base+keysPerRange))
// 			count, err := tree.DeleteRange(startKey, endKey)
// 			if err != nil {
// 				t.Errorf("DeleteRange failed: %v", err)
// 				return
// 			}
// 			if count == 0 {
// 				t.Errorf("Expected non-zero delete count")
// 				return
// 			}
// 		}(i * keysPerRange * 2) // *2 to avoid overlapping ranges
// 	}

// 	wg.Wait()
// }

// func TestConcurrentOperations(t *testing.T) {
// 	tree := &Btree{}
// 	const numOperations = 10
// 	var wg sync.WaitGroup

// 	// Concurrent inserts
// 	for i := 0; i < numOperations; i++ {
// 		wg.Add(1)
// 		go func(i int) {
// 			defer wg.Done()
// 			key := []byte(fmt.Sprintf("key%d", i))
// 			value := []byte(fmt.Sprintf("value%d", i))
// 			tree.insert(key, value)
// 		}(i)
// 	}

// 	// Concurrent reads
// 	for i := 0; i < numOperations; i++ {
// 		wg.Add(1)
// 		go func(i int) {
// 			defer wg.Done()
// 			key := []byte(fmt.Sprintf("key%d", i))
// 			_, _ = tree.Find(key)
// 		}(i)
// 	}

// 	// Wait for all operations to complete
// 	wg.Wait()

// 	// Verify all insertions
// 	for i := 0; i < numOperations; i++ {
// 		key := []byte(fmt.Sprintf("key%d", i))
// 		value, err := tree.Find(key)
// 		if err != nil {
// 			t.Errorf("Key %s not found", key)
// 		}
// 		expectedValue := []byte(fmt.Sprintf("value%d", i))
// 		if !bytes.Equal(value, expectedValue) {
// 			t.Errorf("Wrong value for key %s: got %s, want %s", key, value, expectedValue)
// 		}
// 	}
// }
