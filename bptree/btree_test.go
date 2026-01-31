package bptree

import (
	"bytes"
	"errors"
	"fmt"
	_ "net/http/pprof"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBTreeInsert(t *testing.T) {

	tests := []struct {
		name     string
		keys     [][]byte
		expected [][]byte
	}{
		// Basic Operations
		{
			name:     "Insert into empty tree",
			keys:     [][]byte{[]byte("a")},
			expected: [][]byte{[]byte("a")},
		},
		{
			name:     "Sequential insert ascending",
			keys:     [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
		},
		{
			name:     "Sequential insert descending",
			keys:     [][]byte{[]byte("d"), []byte("c"), []byte("b"), []byte("a")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
		},

		// Node Splitting Scenarios
		{
			name:     "Insert causing single split",
			keys:     [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")},
		},
		{
			name:     "Insert causing multiple splits",
			keys:     [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f"), []byte("g"), []byte("h"), []byte("i")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f"), []byte("g"), []byte("h"), []byte("i")},
		},
		{
			name:     "Insert causing root split",
			keys:     [][]byte{[]byte("1"), []byte("2"), []byte("3"), []byte("4"), []byte("5")},
			expected: [][]byte{[]byte("1"), []byte("2"), []byte("3"), []byte("4"), []byte("5")},
		},

		// Duplicate Handling
		{
			name:     "Insert duplicates sequentially",
			keys:     [][]byte{[]byte("a"), []byte("a"), []byte("b"), []byte("b"), []byte("c")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c")},
		},
		{
			name:     "Insert duplicates with splits",
			keys:     [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("a"), []byte("b"), []byte("d"), []byte("e")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")},
		},
		{
			name:     "Insert multiple duplicates of same key",
			keys:     [][]byte{[]byte("a"), []byte("a"), []byte("a"), []byte("a")},
			expected: [][]byte{[]byte("a")},
		},

		// Edge Cases
		{
			name:     "Insert empty key",
			keys:     [][]byte{[]byte("")},
			expected: [][]byte{[]byte("")},
		},
		{
			name:     "Insert mix of empty and non-empty keys",
			keys:     [][]byte{[]byte(""), []byte("a"), []byte(""), []byte("b")},
			expected: [][]byte{[]byte(""), []byte("a"), []byte("b")},
		},
		{
			name:     "Insert single character keys",
			keys:     [][]byte{[]byte("a"), []byte("b"), []byte("c")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c")},
		},

		// Complex Patterns
		{
			name: "Insert alternating pattern",
			keys: [][]byte{
				[]byte("a"), []byte("z"), []byte("b"), []byte("y"),
				[]byte("c"), []byte("x"), []byte("d"), []byte("w"),
			},
			expected: [][]byte{
				[]byte("a"), []byte("b"), []byte("c"), []byte("d"),
				[]byte("w"), []byte("x"), []byte("y"), []byte("z"),
			},
		},
		{
			name: "Insert with gaps in values",
			keys: [][]byte{
				[]byte("a"), []byte("e"), []byte("i"), []byte("o"),
				[]byte("u"), []byte("c"), []byte("g"), []byte("k"),
			},
			expected: [][]byte{
				[]byte("a"), []byte("c"), []byte("e"), []byte("g"),
				[]byte("i"), []byte("k"), []byte("o"), []byte("u"),
			},
		},

		//Large Key Sets
		{
			name: "Insert large sequential set",
			keys: func() [][]byte {
				var keys [][]byte
				for i := 0; i < 20; i++ {
					keys = append(keys, []byte(fmt.Sprintf("key%02d", i)))
				}
				return keys
			}(),
			expected: func() [][]byte {
				var expected [][]byte
				for i := 0; i < 20; i++ {
					expected = append(expected, []byte(fmt.Sprintf("key%02d", i)))
				}
				return expected
			}(),
		},
		{
			name: "Insert all keys from a to z",
			keys: func() [][]byte {
				var keys [][]byte
				for c := 'a'; c <= 'z'; c++ {
					keys = append(keys, []byte{byte(c)})
				}
				return keys
			}(),
			expected: func() [][]byte {
				var expected [][]byte
				for c := 'a'; c <= 'z'; c++ {
					expected = append(expected, []byte{byte(c)})
				}
				return expected
			}(),
		},
		// Special Characters
		{
			name: "Insert keys with special characters",
			keys: [][]byte{
				[]byte("!key"), []byte("@key"), []byte("#key"),
				[]byte("$key"), []byte("%key"), []byte("^key"),
			},
			expected: [][]byte{
				[]byte("!key"), []byte("#key"), []byte("$key"),
				[]byte("%key"), []byte("@key"), []byte("^key"),
			},
		},

		// Mixed Length Keys
		{
			name: "Insert keys of varying lengths",
			keys: [][]byte{
				[]byte("a"), []byte("aa"), []byte("aaa"),
				[]byte("aaaa"), []byte("aaaaa"), []byte("aaaaaa"),
			},
			expected: [][]byte{
				[]byte("a"), []byte("aa"), []byte("aaa"),
				[]byte("aaaa"), []byte("aaaaa"), []byte("aaaaaa"),
			},
		},
		{
			name: "Insert 100 sequential keys",
			keys: func() [][]byte {
				var keys [][]byte
				for i := 0; i < 100; i++ {
					keys = append(keys, []byte(fmt.Sprintf("key%03d", i))) // Use %03d for 3-digit padding
				}
				return keys
			}(),
			expected: func() [][]byte {
				var expected [][]byte
				for i := 0; i < 100; i++ {
					expected = append(expected, []byte(fmt.Sprintf("key%03d", i))) // Use %03d for 3-digit padding
				}
				return expected
			}(),
		},
	}
	start := time.Now()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Btree{}

			for _, key := range tt.keys {
				tree.Insert(key, key)

				// Validate tree properties after each insertion
				if err := validateBTreeProperties(tree); err != nil {
					t.Errorf("Invalid B-tree state after inserting %s: %v", key, err)
				}
			}

			var traversedKeys [][]byte
			traverseBFS(tree.root, func(key Keytype, value Valuetype) {
				traversedKeys = append(traversedKeys, key)
			})

			// Sort both expected and traversed keys for comparison
			sort.Slice(traversedKeys, func(i, j int) bool {
				return bytes.Compare(traversedKeys[i], traversedKeys[j]) < 0
			})
			sort.Slice(tt.expected, func(i, j int) bool {
				return bytes.Compare(tt.expected[i], tt.expected[j]) < 0
			})

			if len(traversedKeys) != len(tt.expected) {
				t.Errorf("Length mismatch: got %d keys, want %d keys", len(traversedKeys), len(tt.expected))
			}

			for i, expected := range tt.expected {
				if i < len(traversedKeys) && !bytes.Equal(traversedKeys[i], expected) {
					t.Errorf("Key mismatch at position %d: got %s, want %s", i, traversedKeys[i], expected)
				}
			}

			if err := validateBTreeProperties(tree); err != nil {
				t.Error("Final tree validation failed:", err)
			}
		})
	}
	fmt.Println("Time taken : ", time.Since(start))
}

func TestBTreeDelete(t *testing.T) {
	tests := []struct {
		name          string
		insertKeys    [][]byte
		deleteKeys    [][]byte
		expectedKeys  [][]byte
		shouldSucceed bool
	}{
		{
			name:          "Delete from leaf node",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c")},
			deleteKeys:    [][]byte{[]byte("b")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("c")},
			shouldSucceed: true,
		},
		{
			name:          "Delete non-existent key",
			insertKeys:    [][]byte{[]byte("a"), []byte("c")},
			deleteKeys:    [][]byte{[]byte("b")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("c")},
			shouldSucceed: false,
		},
		{
			name:          "Delete root when it's the only node",
			insertKeys:    [][]byte{[]byte("a")},
			deleteKeys:    [][]byte{[]byte("a")},
			expectedKeys:  [][]byte{},
			shouldSucceed: true,
		},
		{
			name:          "Delete causing redistribution from left sibling",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")},
			deleteKeys:    [][]byte{[]byte("d")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("e")},
			shouldSucceed: true,
		},
		{
			name:          "Delete multiple keys",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
			deleteKeys:    [][]byte{[]byte("a"), []byte("c")},
			expectedKeys:  [][]byte{[]byte("b"), []byte("d")},
			shouldSucceed: true,
		},
		{
			name:          "Delete all keys",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c")},
			deleteKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c")},
			expectedKeys:  [][]byte{},
			shouldSucceed: true,
		},
		{
			name:          "Delete from leaf node",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c")},
			deleteKeys:    [][]byte{[]byte("b")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("c")},
			shouldSucceed: true,
		},
		{
			name:          "Delete non-existent key",
			insertKeys:    [][]byte{[]byte("a"), []byte("c")},
			deleteKeys:    [][]byte{[]byte("b")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("c")},
			shouldSucceed: false,
		},

		// New test cases for internal node operations
		{
			name:          "Delete key from internal node with successor",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f"), []byte("g")},
			deleteKeys:    [][]byte{[]byte("d")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("e"), []byte("f"), []byte("g")},
			shouldSucceed: true,
		},
		{
			name:          "Delete key from internal node with predecessor",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f"), []byte("g")},
			deleteKeys:    [][]byte{[]byte("b")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("c"), []byte("d"), []byte("e"), []byte("f"), []byte("g")},
			shouldSucceed: true,
		},

		// Merge scenarios
		{
			name:          "Delete causing merge with left sibling",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f")},
			deleteKeys:    [][]byte{[]byte("c"), []byte("d")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("b"), []byte("e"), []byte("f")},
			shouldSucceed: true,
		},
		{
			name:          "Delete causing merge with right sibling",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f")},
			deleteKeys:    [][]byte{[]byte("b"), []byte("c")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("d"), []byte("e"), []byte("f")},
			shouldSucceed: true,
		},

		// Complex redistribution scenarios
		{
			name:          "Delete causing cascading redistribution",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f"), []byte("g"), []byte("h")},
			deleteKeys:    [][]byte{[]byte("b"), []byte("f")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("c"), []byte("d"), []byte("e"), []byte("g"), []byte("h")},
			shouldSucceed: true,
		},
		{
			name:          "Delete causing multiple merges",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f"), []byte("g"), []byte("h")},
			deleteKeys:    [][]byte{[]byte("b"), []byte("d"), []byte("f"), []byte("h")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("c"), []byte("e"), []byte("g")},
			shouldSucceed: true,
		},

		// Edge cases
		{
			name:          "Delete from minimum sized root",
			insertKeys:    [][]byte{[]byte("a"), []byte("b")},
			deleteKeys:    [][]byte{[]byte("a")},
			expectedKeys:  [][]byte{[]byte("b")},
			shouldSucceed: true,
		},
		{
			name:          "Delete causing empty tree",
			insertKeys:    [][]byte{[]byte("a")},
			deleteKeys:    [][]byte{[]byte("a")},
			expectedKeys:  [][]byte{},
			shouldSucceed: true,
		},

		// Boundary cases
		{
			name:          "Delete last key in internal node",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")},
			deleteKeys:    [][]byte{[]byte("e")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
			shouldSucceed: true,
		},
		{
			name:          "Delete first key in internal node",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")},
			deleteKeys:    [][]byte{[]byte("a")},
			expectedKeys:  [][]byte{[]byte("b"), []byte("c"), []byte("d"), []byte("e")},
			shouldSucceed: true,
		},

		// Sequential operations
		{
			name:          "Sequential delete from start",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
			deleteKeys:    [][]byte{[]byte("a"), []byte("b")},
			expectedKeys:  [][]byte{[]byte("c"), []byte("d")},
			shouldSucceed: true,
		},
		{
			name:          "Sequential delete from end",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
			deleteKeys:    [][]byte{[]byte("d"), []byte("c")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("b")},
			shouldSucceed: true,
		},

		// Special cases
		{
			name:          "Delete causing root replacement",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")},
			deleteKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c")},
			expectedKeys:  [][]byte{[]byte("d"), []byte("e")},
			shouldSucceed: true,
		},
		{
			name:          "Delete alternating keys",
			insertKeys:    [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e")},
			deleteKeys:    [][]byte{[]byte("b"), []byte("d")},
			expectedKeys:  [][]byte{[]byte("a"), []byte("c"), []byte("e")},
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Btree{}

			t.Logf("Test case: %s", tt.name)
			t.Logf("Inserting keys: %v", tt.insertKeys)
			for _, key := range tt.insertKeys {
				tree.Insert(key, key)
			}

			t.Log("Initial tree structure:")
			printBFS(t, tree.root)

			t.Logf("Deleting keys: %v", tt.deleteKeys)
			for _, key := range tt.deleteKeys {
				success := tree.Delete(key)
				if success != tt.shouldSucceed {
					t.Errorf("Delete(%s) = %v, want %v", key, success, tt.shouldSucceed)
				}

				t.Logf("After deleting %s:", key)
				printBFS(t, tree.root)
				if err := validateBTreeProperties(tree); err != nil {
					t.Errorf("Invalid B-tree state after deleting %s: %v", key, err)
				}
			}

			// Collect remaining keys
			var remainingKeys [][]byte
			traverseBFS(tree.root, func(key Keytype, value Valuetype) {
				remainingKeys = append(remainingKeys, key)
			})

			// Verify remaining keys match expected
			if len(remainingKeys) != len(tt.expectedKeys) {
				t.Errorf("Got %d keys, want %d keys", len(remainingKeys), len(tt.expectedKeys))
			}

			for _, expected := range tt.expectedKeys {
				found := false
				for _, remaining := range remainingKeys {
					if bytes.Equal(remaining, expected) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected key %s not found. Remaining keys: %v", expected, remainingKeys)
				}
			}
		})
	}
}

func TestBTreeGet(t *testing.T) {
	// Test the new Get() function with lock coupling and right-link traversal
	testCases := []struct {
		name        string
		keys        [][]byte
		searchKey   []byte
		expectedKey []byte
		expectedErr error
	}{
		{
			name:        "Get key from root node",
			keys:        [][]byte{[]byte("key1"), []byte("key2"), []byte("key3")},
			searchKey:   []byte("key2"),
			expectedKey: []byte("key2"),
			expectedErr: nil,
		},
		{
			name:        "Get key from leaf node",
			keys:        [][]byte{[]byte("key1"), []byte("key2"), []byte("key3"), []byte("key4"), []byte("key5")},
			searchKey:   []byte("key4"),
			expectedKey: []byte("key4"),
			expectedErr: nil,
		},
		{
			name:        "Get key with right sibling traversal",
			keys:        [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f")},
			searchKey:   []byte("f"),
			expectedKey: []byte("f"),
			expectedErr: nil,
		},
		{
			name:        "Get non-existent key",
			keys:        [][]byte{[]byte("key1"), []byte("key2"), []byte("key3")},
			searchKey:   []byte("key4"),
			expectedKey: nil,
			expectedErr: errors.New("key not found"),
		},
		{
			name:        "Get from empty tree",
			keys:        [][]byte{},
			searchKey:   []byte("key1"),
			expectedKey: nil,
			expectedErr: errors.New("key not found"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tree := &Btree{}

			for _, key := range tc.keys {
				tree.Insert(key, key)
			}

			foundKey, err := tree.Get(tc.searchKey)

			if (err == nil && tc.expectedErr != nil) || (err != nil && tc.expectedErr == nil) {
				t.Errorf("Unexpected error: got %v, expected %v", err, tc.expectedErr)
			}

			if !bytes.Equal(foundKey, tc.expectedKey) {
				t.Errorf("Unexpected key: got %v, expected %v", foundKey, tc.expectedKey)
			}
		})
	}
}

func TestBTreePut(t *testing.T) {
	// Test the new Put() function with optimistic/pessimistic locking
	tests := []struct {
		name     string
		keys     [][]byte
		expected [][]byte
	}{
		{
			name:     "Put single key",
			keys:     [][]byte{[]byte("a")},
			expected: [][]byte{[]byte("a")},
		},
		{
			name:     "Put multiple keys sequentially",
			keys:     [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")},
		},
		{
			name:     "Put keys with splits",
			keys:     [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d"), []byte("e"), []byte("f")},
		},
		{
			name:     "Put duplicate keys",
			keys:     [][]byte{[]byte("a"), []byte("b"), []byte("a"), []byte("c")},
			expected: [][]byte{[]byte("a"), []byte("b"), []byte("c")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Btree{}

			for _, key := range tt.keys {
				tree.Put(key, key)
			}

			var traversedKeys [][]byte
			traverseBFS(tree.root, func(key Keytype, value Valuetype) {
				traversedKeys = append(traversedKeys, key)
			})

			sort.Slice(traversedKeys, func(i, j int) bool {
				return bytes.Compare(traversedKeys[i], traversedKeys[j]) < 0
			})
			sort.Slice(tt.expected, func(i, j int) bool {
				return bytes.Compare(tt.expected[i], tt.expected[j]) < 0
			})

			if len(traversedKeys) != len(tt.expected) {
				t.Errorf("Length mismatch: got %d keys, want %d keys", len(traversedKeys), len(tt.expected))
			}

			for i, expected := range tt.expected {
				if i < len(traversedKeys) && !bytes.Equal(traversedKeys[i], expected) {
					t.Errorf("Key mismatch at position %d: got %s, want %s", i, traversedKeys[i], expected)
				}
			}
		})
	}
}

func validateBTreeProperties(tree *Btree) error {
	if tree.root == nil {
		return nil
	}

	return validateNode(tree.root, nil, nil, true)
}

func validateNode(node *Node, min, max []byte, isRoot bool) error {
	if !isRoot && len(node.keys) < MinKeys {
		return fmt.Errorf("node has fewer than minimum keys required")
	}

	if len(node.keys) > MaxKeys {
		return fmt.Errorf("node has more than maximum keys allowed")
	}

	for i := 1; i < len(node.keys); i++ {
		if bytes.Compare(node.keys[i-1], node.keys[i]) >= 0 {
			return fmt.Errorf("keys are not in strictly ascending order")
		}
	}

	if min != nil && bytes.Compare(node.keys[0], min) < 0 {
		return fmt.Errorf("key less than minimum allowed")
	}
	if max != nil && bytes.Compare(node.keys[len(node.keys)-1], max) > 0 {
		return fmt.Errorf("key greater than maximum allowed")
	}

	if !node.isleaf {
		if len(node.children) != len(node.keys)+1 {
			return fmt.Errorf("invalid number of children")
		}

		for i, child := range node.children {
			var childMin, childMax []byte
			if i > 0 {
				childMin = node.keys[i-1]
			} else {
				childMin = min
			}
			if i < len(node.keys) {
				childMax = node.keys[i]
			} else {
				childMax = max
			}

			if err := validateNode(child, childMin, childMax, false); err != nil {
				return err
			}
		}
	}

	return nil
}

func traverseBFS(root *Node, visit func(key Keytype, value Valuetype)) {
	if root == nil {
		return
	}

	queue := []*Node{root}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for i := 0; i < len(current.keys); i++ {
			visit(current.keys[i], current.values[i])
		}

		if !current.isleaf {
			queue = append(queue, current.children...)
		}
	}
}

func printBFS(t *testing.T, root *Node) {
	if root == nil {
		t.Log("Tree is empty")
		return
	}

	// Queue to hold nodes along with their parent information
	type NodeWithParent struct {
		node   *Node
		parent *Node // Pointer to the parent node
	}

	queue := []NodeWithParent{{node: root, parent: nil}}
	level := 0

	for len(queue) > 0 {
		levelSize := len(queue)
		indent := strings.Repeat(" ", level)
		t.Logf("%sLevel %d:", indent, level)

		for i := 0; i < levelSize; i++ {
			currentWithParent := queue[0]
			currentNode := currentWithParent.node
			parentNode := currentWithParent.parent
			queue = queue[1:]

			// Include parent information in the log
			if parentNode != nil {
				t.Logf("%s Node %d keys: %v (Parent keys: %v)", indent, i, currentNode.keys, parentNode.keys)
			} else {
				t.Logf("%s Node %d keys: %v (Parent: nil)", indent, i, currentNode.keys)
			}

			// Add children to the queue along with their parent information
			if !currentNode.isleaf {
				for _, child := range currentNode.children {
					queue = append(queue, NodeWithParent{node: child, parent: currentNode})
				}
			}
		}
		level++
	}
}

func TestConcurrentOperations(t *testing.T) {
	// Re-enabled after fixing locking bugs
	tree := &Btree{}
	const numWorkers = 1000
	const numOperations = 5000 // Increased to stress test concurrency fixes

	// Create buffered channels to prevent goroutine leaks
	insertCh := make(chan int, numOperations)
	readCh := make(chan int, numOperations)

	// Fill channels before starting workers
	for i := 0; i < numOperations; i++ {
		insertCh <- i
		readCh <- i
	}
	close(insertCh)
	close(readCh)

	// Use errgroup for better error handling
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)

	// Start insert workers
	for i := 0; i < numWorkers/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for num := range insertCh {
				key := []byte(fmt.Sprintf("%d", num))
				value := []byte(fmt.Sprintf("%d", num*2))
				tree.Insert(key, value)
			}
		}()
	}

	// Start read workers
	for i := 0; i < numWorkers/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for num := range readCh {
				key := []byte(fmt.Sprintf("%d", num))
				_, err := tree.Find(key)
				if err != nil && err.Error() != "key not found" {
					errCh <- fmt.Errorf("unexpected error reading key %d: %v", num, err)
				}
			}
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success case
	case err := <-errCh:
		t.Fatalf("Test failed with error: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Test timed out")
	}

	// Verify final state
	for i := 0; i < numOperations; i++ {
		key := []byte(fmt.Sprintf("%d", i))
		expectedValue := []byte(fmt.Sprintf("%d", i*2))
		value, err := tree.Find(key)
		if err != nil {
			t.Errorf("Key %d not found after concurrent operations", i)
			continue
		}
		if !bytes.Equal(value, expectedValue) {
			t.Errorf("Wrong value for key %d: got %v, want %v", i, value, expectedValue)
		}
	}
}

func TestConcurrentStress(t *testing.T) {
	// Re-enabled after fixing locking bugs
	tree := &Btree{}
	const numWorkers = 50
	const numOperations = 200

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Start mixed operation workers
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			// Each worker performs a mix of operations
			for j := 0; j < numOperations; j++ {
				key := []byte(string(rune(workerID*numOperations + j)))
				value := []byte(string(rune(j)))

				switch j % 3 {
				case 0: // Insert
					tree.Insert(key, value)
				case 1: // Read
					tree.Find(key)
				case 2: // Delete
					tree.Delete(key)
				}

				// Small random delay to increase contention chances
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentDeleteAndInsert(t *testing.T) {
	// Re-enabled after fixing locking bugs
	tree := &Btree{}
	const numPairs = 500

	// First insert some data
	for i := 0; i < numPairs; i++ {
		key := []byte(string(rune(i)))
		value := []byte(string(rune(i)))
		tree.Insert(key, value)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Concurrent delete
	go func() {
		defer wg.Done()
		for i := 0; i < numPairs; i++ {
			if i%2 == 0 { // Delete even numbers
				key := []byte(string(rune(i)))
				tree.Delete(key)
			}
		}
	}()

	// Concurrent insert
	go func() {
		defer wg.Done()
		for i := numPairs; i < numPairs*2; i++ {
			key := []byte(string(rune(i)))
			value := []byte(string(rune(i)))
			tree.Insert(key, value)
		}
	}()

	wg.Wait()

	// Verify final state
	for i := 0; i < numPairs*2; i++ {
		key := []byte(string(rune(i)))
		value, err := tree.Find(key)

		if i < numPairs && i%2 == 0 {
			// Should be deleted
			if err == nil {
				t.Errorf("Key %d should have been deleted", i)
			}
		} else if i >= numPairs {
			// Should exist with correct value
			if err != nil {
				t.Errorf("Key %d should exist", i)
			} else if !bytes.Equal(value, key) {
				t.Errorf("Wrong value for key %d", i)
			}
		}
	}
}

func TestGranularConcurrency(t *testing.T) {
	// This test verifies concurrent operations work correctly
	// Each worker operates on its own key range
	tree := &Btree{}
	const numWorkers = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	// Track errors in a thread-safe way
	var errCount int32

	// Start workers performing insert operations on different key ranges
	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			// First, insert all keys for this worker
			for j := 0; j < numOperations; j++ {
				key := []byte(fmt.Sprintf("w%d-k%d", workerID, j))
				value := []byte(fmt.Sprintf("v%d-%d", workerID, j))
				tree.Insert(key, value)
			}

			// Then verify all keys exist
			for j := 0; j < numOperations; j++ {
				key := []byte(fmt.Sprintf("w%d-k%d", workerID, j))
				expectedValue := []byte(fmt.Sprintf("v%d-%d", workerID, j))
				if val, err := tree.Find(key); err != nil {
					t.Errorf("Worker %d: key %d not found after insert: %v", workerID, j, err)
					atomic.AddInt32(&errCount, 1)
				} else if !bytes.Equal(val, expectedValue) {
					t.Errorf("Worker %d: value mismatch for key %d", workerID, j)
					atomic.AddInt32(&errCount, 1)
				}
			}

			// Then delete half the keys
			for j := 0; j < numOperations; j += 2 {
				key := []byte(fmt.Sprintf("w%d-k%d", workerID, j))
				tree.Delete(key)
			}

			// Verify deletes worked
			for j := 0; j < numOperations; j += 2 {
				key := []byte(fmt.Sprintf("w%d-k%d", workerID, j))
				if _, err := tree.Find(key); err == nil {
					t.Errorf("Worker %d: key %d should have been deleted", workerID, j)
					atomic.AddInt32(&errCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	if errCount > 0 {
		t.Errorf("Total errors: %d", errCount)
	}
}

func TestBasicConcurrency(t *testing.T) {
	tree := &Btree{}

	// Insert some initial data
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("%d", i))
		value := []byte(fmt.Sprintf("%d", i))
		tree.Insert(key, value)
	}

	// Run concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Mix of operations
			key := []byte(fmt.Sprintf("%d", id))
			tree.Insert(key, []byte("new"))
			tree.Find(key)
			tree.Delete(key)
		}(i)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out")
	}
}

func TestConcurrentInsertBasic(t *testing.T) {
	// Test concurrent inserts using the original Insert function
	// which has serialization through insertLock
	tree := &Btree{}
	const (
		numGoroutines  = 1000
		keysPerRoutine = 5000
	)
	var wg sync.WaitGroup

	startTime := time.Now()

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for i := 0; i < keysPerRoutine; i++ {
				key := fmt.Sprintf("key-%d-%d", routineID, i)
				value := fmt.Sprintf("val-%d-%d", routineID, i)
				tree.Insert([]byte(key), []byte(value))
			}
		}(g)
	}

	wg.Wait()

	// Verify final tree state
	totalExpectedKeys := numGoroutines * keysPerRoutine
	count := countKeys(tree.root)
	if count != totalExpectedKeys {
		t.Errorf("Expected %d keys in tree, but got %d", totalExpectedKeys, count)
	}

	// Verify all keys are present and retrievable
	for g := 0; g < numGoroutines; g++ {
		for i := 0; i < keysPerRoutine; i++ {
			key := fmt.Sprintf("key-%d-%d", g, i)
			expectedValue := fmt.Sprintf("val-%d-%d", g, i)
			found, err := tree.Find([]byte(key))
			if err != nil {
				t.Errorf("Final verification: key %s not found: %v", key, err)
			}
			if !bytes.Equal(found, []byte(expectedValue)) {
				t.Errorf("Final verification: value mismatch for key %s, got %s, want %s",
					key, found, expectedValue)
			}
		}
	}

	t.Logf("Test completed in %v", time.Since(startTime))
}

// Helper function to count total keys in the tree
func countKeys(node *Node) int {
	if node == nil {
		return 0
	}

	node.mu.RLock()
	defer node.mu.RUnlock()

	count := len(node.keys)
	if !node.isleaf {
		for _, child := range node.children {
			count += countKeys(child)
		}
	}
	return count
}

// Helper function to verify tree structure integrity
func verifyTreeStructure(node *Node) error {
	if node == nil {
		return nil
	}

	node.mu.RLock()
	defer node.mu.RUnlock()

	// Verify node constraints
	if len(node.keys) > MaxKeys {
		return fmt.Errorf("node has %d keys, exceeding MaxKeys %d", len(node.keys), MaxKeys)
	}

	if !node.isleaf && len(node.keys) < MinKeys-1 {
		return fmt.Errorf("internal node has %d keys, below MinKeys-1 %d", len(node.keys), MinKeys-1)
	}

	// Verify key ordering within node
	for i := 1; i < len(node.keys); i++ {
		if bytes.Compare(node.keys[i-1], node.keys[i]) >= 0 {
			return fmt.Errorf("keys not in order: %s >= %s",
				string(node.keys[i-1]), string(node.keys[i]))
		}
	}

	// Verify children if not leaf
	if !node.isleaf {
		if len(node.children) != len(node.keys)+1 {
			return fmt.Errorf("internal node has %d children for %d keys",
				len(node.children), len(node.keys))
		}

		// Recursively verify children
		for _, child := range node.children {
			if err := verifyTreeStructure(child); err != nil {
				return err
			}
		}
	}

	return nil
}
