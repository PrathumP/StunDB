package bptree

import (
	"bytes"
	"testing"
)

func TestGetRange(t *testing.T) {
	tree := &Btree{}

	// Test data
	testData := []struct {
		key   []byte
		value []byte
	}{
		{[]byte("a"), []byte("1")},
		{[]byte("b"), []byte("2")},
		{[]byte("c"), []byte("3")},
		{[]byte("d"), []byte("4")},
		{[]byte("e"), []byte("5")},
		{[]byte("f"), []byte("6")},
		{[]byte("g"), []byte("7")},
		{[]byte("h"), []byte("8")},
		{[]byte("i"), []byte("9")},
		{[]byte("j"), []byte("1")},
	}

	// insert test data
	for _, data := range testData {
		tree.insert(data.key, data.value)
	}

	tests := []struct {
		name     string
		start    []byte
		end      []byte
		expected []struct {
			key   []byte
			value []byte
		}
		expectedError bool
	}{
		{
			name:  "Full Range",
			start: []byte("a"),
			end:   []byte("j"),
			expected: []struct {
				key   []byte
				value []byte
			}{
				{[]byte("a"), []byte("1")},
				{[]byte("b"), []byte("2")},
				{[]byte("c"), []byte("3")},
				{[]byte("d"), []byte("4")},
				{[]byte("e"), []byte("5")},
				{[]byte("f"), []byte("6")},
				{[]byte("g"), []byte("7")},
				{[]byte("h"), []byte("8")},
				{[]byte("i"), []byte("9")},
				{[]byte("j"), []byte("1")},
			},
		},
		{
			name:  "Partial Range",
			start: []byte("c"),
			end:   []byte("f"),
			expected: []struct {
				key   []byte
				value []byte
			}{
				{[]byte("c"), []byte("3")},
				{[]byte("d"), []byte("4")},
				{[]byte("e"), []byte("5")},
				{[]byte("f"), []byte("6")},
			},
		},
		{
			name:  "Single Key Range",
			start: []byte("e"),
			end:   []byte("e"),
			expected: []struct {
				key   []byte
				value []byte
			}{
				{[]byte("e"), []byte("5")},
			},
		},
		{
			name:          "Invalid Range",
			start:         []byte("z"),
			end:           []byte("a"),
			expectedError: true,
		},
		{
			name:  "Empty Range",
			start: []byte("l"),
			end:   []byte("m"),
			expected: []struct {
				key   []byte
				value []byte
			}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, values, err := tree.GetRange(tt.start, tt.end)

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(keys) != len(tt.expected) {
				t.Errorf("Expected %d results, got %d", len(tt.expected), len(keys))
				t.Logf("Expected keys: %v", tt.expected)
				t.Logf("Got keys: %v", keys)
				return
			}

			for i := 0; i < len(keys); i++ {
				if !bytes.Equal(keys[i], tt.expected[i].key) {
					t.Errorf("Result %d: expected key %s, got %s", i, tt.expected[i].key, keys[i])
				}
				if !bytes.Equal(values[i], tt.expected[i].value) {
					t.Errorf("Result %d: expected value %s, got %s", i, tt.expected[i].value, values[i])
				}
			}
		})
	}
}

func TestDeleteRange(t *testing.T) {
	tests := []struct {
		name  string
		setup []struct {
			key   []byte
			value []byte
		}
		deleteStart []byte
		deleteEnd   []byte
		remaining   []struct {
			key   []byte
			value []byte
		}
		expectedError bool
	}{
		{
			name: "Delete Middle Range",
			setup: []struct {
				key   []byte
				value []byte
			}{
				{[]byte("a"), []byte("1")},
				{[]byte("b"), []byte("2")},
				{[]byte("c"), []byte("3")},
				{[]byte("d"), []byte("4")},
				{[]byte("e"), []byte("5")},
			},
			deleteStart: []byte("b"),
			deleteEnd:   []byte("d"),
			remaining: []struct {
				key   []byte
				value []byte
			}{
				{[]byte("a"), []byte("1")},
				{[]byte("e"), []byte("5")},
			},
		},
		{
			name: "Delete All",
			setup: []struct {
				key   []byte
				value []byte
			}{
				{[]byte("a"), []byte("1")},
				{[]byte("b"), []byte("2")},
			},
			deleteStart: []byte("a"),
			deleteEnd:   []byte("b"),
			remaining: []struct {
				key   []byte
				value []byte
			}{},
		},
		{
			name: "Delete Empty Range",
			setup: []struct {
				key   []byte
				value []byte
			}{
				{[]byte("a"), []byte("1")},
				{[]byte("b"), []byte("2")},
			},
			deleteStart: []byte("c"),
			deleteEnd:   []byte("d"),
			remaining: []struct {
				key   []byte
				value []byte
			}{
				{[]byte("a"), []byte("1")},
				{[]byte("b"), []byte("2")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Btree{}

			// Setup test data
			for _, data := range tt.setup {
				tree.insert(data.key, data.value)
			}

			// Perform range deletion
			_, err := tree.DeleteRange(tt.deleteStart, tt.deleteEnd)

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify remaining keys
			for _, expected := range tt.remaining {
				value, err := tree.Find(expected.key)
				if err != nil {
					t.Errorf("Expected to find key %s but got error: %v", expected.key, err)
					continue
				}
				if !bytes.Equal(value, expected.value) {
					t.Errorf("For key %s: expected value %s, got %s", expected.key, expected.value, value)
				}
			}

			// Verify deleted keys are gone
			keys, _, _ := tree.GetRange(tt.deleteStart, tt.deleteEnd)
			if len(keys) > 0 {
				t.Errorf("Found unexpected keys in deleted range: %v", keys)
			}
		})
	}
}

func TestRangeEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		actions func(t *testing.T, tree *Btree)
	}{
		{
			name: "Empty Tree Range Query",
			actions: func(t *testing.T, tree *Btree) {
				keys, values, err := tree.GetRange([]byte("a"), []byte("z"))
				if err != nil {
					t.Errorf("Expected nil error for empty tree, got: %v", err)
				}
				if len(keys) != 0 || len(values) != 0 {
					t.Error("Expected empty results for empty tree")
				}
			},
		},
		{
			name: "Single Node Multiple Iterations",
			actions: func(t *testing.T, tree *Btree) {
				tree.insert([]byte("a"), []byte("1"))
				keys, values, _ := tree.GetRange([]byte("a"), []byte("a"))

				if len(keys) != 1 || !bytes.Equal(keys[0], []byte("a")) || !bytes.Equal(values[0], []byte("1")) {
					t.Error("First iteration failed")
				}
			},
		},
		{
			name: "Range Boundaries",
			actions: func(t *testing.T, tree *Btree) {
				tree.insert([]byte("b"), []byte("2"))
				tree.insert([]byte("c"), []byte("3"))

				// Test exact boundaries
				keys, _, _ := tree.GetRange([]byte("b"), []byte("c"))
				if len(keys) != 2 {
					t.Errorf("Expected 2 items, got %d", len(keys))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := &Btree{}
			tt.actions(t, tree)
		})
	}
}

func TestRangeScanLargeTree(t *testing.T) {
	tree := &Btree{}

	// Test data - large tree with multiple levels
	testData := []struct {
		key   []byte
		value []byte
	}{
		{[]byte("a"), []byte("1")},
		{[]byte("b"), []byte("2")},
		{[]byte("c"), []byte("3")},
		{[]byte("d"), []byte("4")},
		{[]byte("e"), []byte("5")},
		{[]byte("f"), []byte("6")},
		{[]byte("g"), []byte("7")},
		{[]byte("h"), []byte("8")},
		{[]byte("i"), []byte("9")},
		{[]byte("j"), []byte("10")},
		{[]byte("k"), []byte("11")},
		{[]byte("l"), []byte("12")},
		{[]byte("m"), []byte("13")},
		{[]byte("n"), []byte("14")},
		{[]byte("o"), []byte("15")},
		{[]byte("p"), []byte("16")},
		{[]byte("q"), []byte("17")},
		{[]byte("r"), []byte("18")},
		{[]byte("s"), []byte("19")},
		{[]byte("t"), []byte("20")},
		{[]byte("u"), []byte("21")},
		{[]byte("v"), []byte("22")},
		{[]byte("w"), []byte("23")},
		{[]byte("x"), []byte("24")},
		{[]byte("y"), []byte("25")},
		{[]byte("z"), []byte("26")},
	}

	// Insert test data
	for _, data := range testData {
		tree.insert(data.key, data.value)
	}

	tests := []struct {
		name     string
		start    []byte
		end      []byte
		expected []struct {
			key   []byte
			value []byte
		}
		expectedError bool
	}{
		{
			name:     "Full Range Scan",
			start:    []byte("a"),
			end:      []byte("z"),
			expected: testData,
		},
		{
			name:     "Partial Range Middle",
			start:    []byte("d"),
			end:      []byte("g"),
			expected: testData[3:7],
		},
		{
			name:     "Partial Range Start",
			start:    []byte("a"),
			end:      []byte("c"),
			expected: testData[0:3],
		},
		{
			name:     "Partial Range End",
			start:    []byte("p"),
			end:      []byte("z"),
			expected: testData[15:],
		},
		{
			name:     "Single Key Range",
			start:    []byte("h"),
			end:      []byte("h"),
			expected: testData[7:8],
		},
		{
			name:  "Empty Range Beyond End",
			start: []byte("124"),
			end:   []byte("125"),
			expected: []struct {
				key   []byte
				value []byte
			}{},
		},
		{
			name:  "Empty Range Before Start",
			start: []byte("0"),
			end:   []byte("1"),
			expected: []struct {
				key   []byte
				value []byte
			}{},
		},
		{
			name:     "Range With Gaps",
			start:    []byte("b"),
			end:      []byte("p"),
			expected: testData[1:16],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, values, err := tree.GetRange(tt.start, tt.end)
			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(keys) != len(tt.expected) {
				t.Errorf("Expected %d results, got %d", len(tt.expected), len(keys))
				t.Logf("Expected keys: %v", tt.expected)
				t.Logf("Got keys: %v", keys)
				return
			}

			for i := 0; i < len(keys); i++ {
				if !bytes.Equal(keys[i], tt.expected[i].key) {
					t.Errorf("Result %d: expected key %s, got %s", i, tt.expected[i].key, keys[i])
				}
				if !bytes.Equal(values[i], tt.expected[i].value) {
					t.Errorf("Result %d: expected value %s, got %s", i, tt.expected[i].value, values[i])
				}
			}
		})
	}
}
