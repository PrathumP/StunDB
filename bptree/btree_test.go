package bptree

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Btree{}

			t.Logf("Test case: %s", tt.name)
			t.Logf("Keys to insert: %v", tt.keys)

			for _, key := range tt.keys {
				tree.insert(key, key)
				t.Logf("After inserting %d:", key)
				printBFS(t, tree.root)

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

			t.Logf("All traversed keys: %v", traversedKeys)
			t.Logf("Expected keys: %v", tt.expected)

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
				tree.insert(key, key)
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
