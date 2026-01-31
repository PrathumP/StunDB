package bptree

import (
	"bytes"
	"errors"
	"sync"
)

// Btree is a concurrent B+Tree implementation.
//
// CONCURRENCY MODEL:
// - All write operations (Insert, Put, Delete) acquire treeLock.Lock()
// - All read operations (Find, Get, GetRange) acquire treeLock.RLock()
// - This ensures writes are serialized and readers see consistent state
//
// This approach provides:
// - Correctness: Single lock prevents all race conditions
// - Read concurrency: Multiple readers can proceed simultaneously
// - Simplicity: Easy to reason about
//
// Tradeoff: Writes block all reads. For higher throughput, consider:
// - Partitioning data across multiple trees
// - Implementing full B-Link tree protocol with fine-grained locking
type Btree struct {
	root     *Node
	treeLock sync.RWMutex // Single lock for all operations
}

// isSafe checks if a node has space for insertion (not full)
func (n *Node) isSafe() bool {
	return len(n.keys) < MaxKeys
}

// getHighKey returns the highest key in the node, or nil if empty
func (n *Node) getHighKey() Keytype {
	if len(n.keys) == 0 {
		return nil
	}
	return n.keys[len(n.keys)-1]
}

// Put performs a thread-safe insert. Alias for Insert.
func (t *Btree) Put(key Keytype, value Valuetype) {
	t.Insert(key, value)
}

// splitNodeSimple splits a full node and inserts a new key.
// Called under treeLock - no per-node locking needed.
func (t *Btree) splitNodeSimple(node *Node, insertKey Keytype, insertValue Valuetype, insertChildPos int, insertChild *Node) (Keytype, Valuetype, *Node) {
	tempKeys := make([]Keytype, 0, len(node.keys)+1)
	tempValues := make([]Valuetype, 0, len(node.values)+1)

	insertPos := node.findindex(insertKey)

	// Build temporary list with new key
	tempKeys = append(tempKeys, node.keys[:insertPos]...)
	tempKeys = append(tempKeys, insertKey)
	tempKeys = append(tempKeys, node.keys[insertPos:]...)

	tempValues = append(tempValues, node.values[:insertPos]...)
	tempValues = append(tempValues, insertValue)
	tempValues = append(tempValues, node.values[insertPos:]...)

	mid := len(tempKeys) / 2

	// For Pure B-Tree: median key+value is promoted
	midKey := tempKeys[mid]
	midValue := tempValues[mid]

	newNode := NewNode(node.isleaf)

	// Right node gets keys after median - copy to avoid shared backing array
	rightKeys := tempKeys[mid+1:]
	rightValues := tempValues[mid+1:]
	newNode.keys = make([]Keytype, len(rightKeys), MaxKeys)
	copy(newNode.keys, rightKeys)
	newNode.values = make([]Valuetype, len(rightValues), MaxKeys)
	copy(newNode.values, rightValues)

	// Handle children for internal nodes
	if !node.isleaf {
		tempChildren := make([]*Node, 0, len(node.children)+1)

		// Build temporary children list with new child
		if insertChildPos >= 0 {
			tempChildren = append(tempChildren, node.children[:insertChildPos]...)
			tempChildren = append(tempChildren, insertChild)
			tempChildren = append(tempChildren, node.children[insertChildPos:]...)
		} else {
			tempChildren = node.children
		}

		rightChildren := tempChildren[mid+1:]
		newNode.children = make([]*Node, len(rightChildren), MaxKeys+1)
		copy(newNode.children, rightChildren)

		leftChildren := tempChildren[:mid+1]
		node.children = make([]*Node, len(leftChildren), MaxKeys+1)
		copy(node.children, leftChildren)
	}

	// Left node gets keys up to (but not including) median - copy to avoid shared backing array
	leftKeys := tempKeys[:mid]
	leftValues := tempValues[:mid]
	node.keys = make([]Keytype, len(leftKeys), MaxKeys)
	copy(node.keys, leftKeys)
	node.values = make([]Valuetype, len(leftValues), MaxKeys)
	copy(node.values, leftValues)

	return midKey, midValue, newNode
}

// Insert inserts a key-value pair into the tree. Thread-safe.
func (tree *Btree) Insert(key Keytype, value Valuetype) {
	tree.treeLock.Lock()
	defer tree.treeLock.Unlock()

	if tree.root == nil {
		tree.root = NewNode(true)
		tree.root.insertAt(0, key, value)
		return
	}

	node := tree.root
	var path []*Node
	for !node.isleaf {
		path = append(path, node)
		idx := node.findindex(key)
		node = node.children[idx]
	}

	idx := node.findindex(key)
	if idx < len(node.keys) && bytes.Equal(node.keys[idx], key) {
		node.values[idx] = value
		return
	}

	if len(node.keys) < MaxKeys {
		node.insertAt(idx, key, value)
		return
	}

	// Split logic
	midKey, midValue, newNode := tree.splitNodeSimple(node, key, value, -1, nil)

	for i := len(path) - 1; i >= 0; i-- {
		parent := path[i]
		childIdx := parent.findindex(midKey)

		if len(parent.keys) < MaxKeys {
			parent.insertAt(childIdx, midKey, midValue)
			parent.insertChildAt(childIdx+1, newNode)
			return
		}

		midKey, midValue, newNode = tree.splitNodeSimple(parent, midKey, midValue, childIdx+1, newNode)
	}

	// Need new root
	newRoot := NewNode(false)
	newRoot.keys = append(newRoot.keys, midKey)
	newRoot.values = append(newRoot.values, midValue)
	newRoot.children = append(newRoot.children, tree.root, newNode)
	tree.root = newRoot
}

func (tree *Btree) splitNodeWithInsert(node *Node, insertKey Keytype, insertValue Valuetype, insertChildPos int, insertChild *Node) (Keytype, Valuetype, *Node) {
	tempKeys := make([]Keytype, 0, len(node.keys)+1)
	tempValues := make([]Valuetype, 0, len(node.values)+1)

	insertPos := node.findindex(insertKey)

	tempKeys = append(tempKeys, node.keys[:insertPos]...)
	tempKeys = append(tempKeys, insertKey)
	tempKeys = append(tempKeys, node.keys[insertPos:]...)

	tempValues = append(tempValues, node.values[:insertPos]...)
	tempValues = append(tempValues, insertValue)
	tempValues = append(tempValues, node.values[insertPos:]...)

	mid := len(tempKeys) / 2

	midKey := tempKeys[mid]
	midValue := tempValues[mid]

	newNode := NewNode(node.isleaf)
	newNode.mu.Lock()

	newNode.keys = append(newNode.keys, tempKeys[mid+1:]...)
	newNode.values = append(newNode.values, tempValues[mid+1:]...)

	if !node.isleaf {
		tempChildren := make([]*Node, 0, len(node.children)+1)

		tempChildren = append(tempChildren, node.children[:insertChildPos]...)
		tempChildren = append(tempChildren, insertChild)
		tempChildren = append(tempChildren, node.children[insertChildPos:]...)

		newNode.children = append(newNode.children, tempChildren[mid+1:]...)
		node.children = tempChildren[:mid+1]
	}

	node.keys = tempKeys[:mid]
	node.values = tempValues[:mid]
	if newNode != nil {
		newNode.mu.Unlock()
	}
	return midKey, midValue, newNode
}

// Delete removes a key from the tree. Thread-safe.
func (t *Btree) Delete(key []byte) bool {
	t.treeLock.Lock()
	defer t.treeLock.Unlock()

	if t.root == nil {
		return false
	}

	deletedkey, _ := t.root.delete(key, false)

	if len(t.root.keys) == 0 {
		if t.root.isleaf {
			t.root = nil
		} else if len(t.root.children) > 0 {
			t.root = t.root.children[0]
		}
	}

	return deletedkey != nil
}

// Find searches for a key in the tree. Thread-safe.
func (t *Btree) Find(key []byte) ([]byte, error) {
	t.treeLock.RLock()
	defer t.treeLock.RUnlock()

	if t.root == nil {
		return nil, errors.New("key not found")
	}

	current := t.root
	for {
		pos := current.findindex(key)

		if pos < len(current.keys) && bytes.Equal(current.keys[pos], key) {
			// Make a copy of the value to return
			valueCopy := make([]byte, len(current.values[pos]))
			copy(valueCopy, current.values[pos])
			return valueCopy, nil
		}

		if current.isleaf {
			return nil, errors.New("key not found")
		}

		if pos >= len(current.children) {
			return nil, errors.New("invalid tree structure")
		}

		next := current.children[pos]
		if next == nil {
			return nil, errors.New("invalid tree structure")
		}

		current = next
	}
}

// Get is an alias for Find.
func (t *Btree) Get(key []byte) ([]byte, error) {
	return t.Find(key)
}

func (n *Node) delete(key []byte, isSeekingSuccessor bool) (Keytype, Valuetype) {
	pos := n.findindex(key)

	if n.isleaf && isSeekingSuccessor {
		return n.removeAt(0)
	}

	found := false
	if pos < len(n.keys) && bytes.Equal(n.keys[pos], key) {
		found = true
	}

	if !found && n.isleaf {
		return nil, nil // Key doesn't exist in the tree
	}

	var next *Node
	if found {
		if n.isleaf {
			return n.removeAt(pos)
		}
		next, isSeekingSuccessor = n.children[pos+1], true
	} else {
		if pos >= len(n.children) {
			return nil, nil // Key would be past last child, doesn't exist
		}
		next = n.children[pos]
	}

	deletedkey, deletedvalue := next.delete(key, isSeekingSuccessor)

	if deletedkey == nil {
		return nil, nil
	}

	if found && isSeekingSuccessor {
		n.keys[pos] = deletedkey
		n.values[pos] = deletedvalue
	}

	if len(next.keys) < MinKeys {
		if found && isSeekingSuccessor {
			n.fillChildAt(pos + 1)
		} else {
			n.fillChildAt(pos)
		}
	}

	return deletedkey, deletedvalue
}

func (n *Node) fillChildAt(pos int) {
	switch {

	case pos > 0 && len(n.children[pos-1].keys) > MinKeys:
		left, right := n.children[pos-1], n.children[pos]

		right.keys = append([]Keytype{n.keys[pos-1]}, right.keys...)
		right.values = append([]Valuetype{n.values[pos-1]}, right.values...)

		if !right.isleaf {
			right.children = append([]*Node{left.children[len(left.children)-1]}, right.children...)
			left.children = left.children[:len(left.children)-1]
		}

		n.keys[pos-1] = left.keys[len(left.keys)-1]
		n.values[pos-1] = left.values[len(left.values)-1]
		left.keys = left.keys[:len(left.keys)-1]
		left.values = left.values[:len(left.values)-1]

	case pos < len(n.children)-1 && len(n.children[pos+1].keys) > MinKeys:
		left, right := n.children[pos], n.children[pos+1]

		left.keys = append(left.keys, n.keys[pos])
		left.values = append(left.values, n.values[pos])

		if !left.isleaf {
			left.children = append(left.children, right.children[0])
			right.children = right.children[1:]
		}

		n.keys[pos] = right.keys[0]
		n.values[pos] = right.values[0]
		right.keys = right.keys[1:]
		right.values = right.values[1:]

	// Merge casee
	default:
		if pos >= len(n.keys) {
			pos = len(n.keys) - 1
		}

		left, right := n.children[pos], n.children[pos+1]

		// Append parent key to left node
		left.keys = append(left.keys, n.keys[pos])
		left.values = append(left.values, n.values[pos])

		// Append all right node keys to left node
		left.keys = append(left.keys, right.keys...)
		left.values = append(left.values, right.values...)

		if !left.isleaf {
			left.children = append(left.children, right.children...)
		}

		// Remove the parent key and right child pointer
		n.keys = append(n.keys[:pos], n.keys[pos+1:]...)
		n.values = append(n.values[:pos], n.values[pos+1:]...)
		n.children = append(n.children[:pos+1], n.children[pos+2:]...)
	}
}
