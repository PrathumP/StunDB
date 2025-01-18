package bptree

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
)

type Btree struct {
	root     *Node
	rootLock sync.RWMutex
}

func (tree *Btree) Insert(key Keytype, value Valuetype) {
	// Handle empty tree case
	tree.rootLock.Lock()
	fmt.Printf("Starting insert for key: %s\n", key)
	defer fmt.Printf("Completed insert for key: %s\n", key)

	if tree.root == nil {
		tree.root = NewNode(true)
		tree.root.mu.Lock()
		tree.root.insertAt(0, key, value)
		tree.root.mu.Unlock()
		tree.rootLock.Unlock()
		return
	}

	// Get root and build path
	node := tree.root
	node.mu.Lock()
	tree.rootLock.Unlock()

	var path []*Node
	for !node.isleaf {
		path = append(path, node)
		idx := node.findindex(key)
		nextNode := node.children[idx]
		nextNode.mu.Lock()
		node = nextNode
	}

	// Handle key already exists case
	idx := node.findindex(key)
	if idx < len(node.keys) && bytes.Equal(node.keys[idx], key) {
		node.values[idx] = value
		for i := 0; i < len(path); i++ {
			path[i].mu.Unlock()
		}
		node.mu.Unlock()
		return
	}

	// Simple insert if node has space
	if len(node.keys) < MaxKeys {
		node.insertAt(idx, key, value)
		for i := 0; i < len(path); i++ {
			path[i].mu.Unlock()
		}
		node.mu.Unlock()
		return
	}

	// Handle splitting cases
	midKey, midValue, newNode := tree.splitNodeWithInsert(node, key, value)
	node.mu.Unlock()

	// Propagate splits up the tree
	for i := len(path) - 1; i >= 0; i-- {
		parent := path[i]
		childIdx := parent.findindex(midKey)

		if len(parent.keys) < MaxKeys {
			parent.insertAt(childIdx, midKey, midValue)
			parent.insertChildAt(childIdx+1, newNode)
			for j := 0; j < i; j++ {
				path[j].mu.Unlock()
			}
			parent.mu.Unlock()
			return
		}

		parent.children = append(parent.children, newNode)
		midKey, midValue, newNode = tree.splitNodeWithInsert(parent, midKey, midValue)
		if i != 0 {
			parent.mu.Unlock()
		}
	}

	tree.rootLock.Lock()

	newRoot := NewNode(false)
	newRoot.keys = append(newRoot.keys, midKey)
	newRoot.values = append(newRoot.values, midValue)
	newRoot.children = append(newRoot.children, tree.root, newNode)

	oldRoot := tree.root
	tree.root = newRoot
	tree.rootLock.Unlock()
	if len(path) > 0 {
		oldRoot.mu.Unlock()
	}
}

func (tree *Btree) splitNodeWithInsert(node *Node, insertKey Keytype, insertValue Valuetype) (Keytype, Valuetype, *Node) {
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
		tempChildren = append(tempChildren, node.children[:insertPos+1]...)
		tempChildren = append(tempChildren, node.children[insertPos+1:]...)

		newNode.children = append(newNode.children, tempChildren[mid+1:]...)
		node.children = tempChildren[:mid+1]
	}

	node.keys = tempKeys[:mid]
	node.values = tempValues[:mid]
	newNode.mu.Unlock()
	return midKey, midValue, newNode
}

func (t *Btree) Delete(key []byte) bool {
	if t.root == nil {
		return false
	}

	t.rootLock.Lock()
	defer t.rootLock.Unlock()

	deletedkey, _ := t.root.delete(key, false)

	if len(t.root.keys) == 0 {
		if t.root.isleaf {
			t.root = nil
		} else {
			t.root = t.root.children[0]
		}
	}

	return deletedkey != nil
}

func (t *Btree) Find(key []byte) ([]byte, error) {
	t.rootLock.RLock()
	if t.root == nil {
		t.rootLock.RUnlock()
		return nil, errors.New("key not found")
	}

	current := t.root
	current.mu.RLock()
	t.rootLock.RUnlock()

	for {
		pos := current.findindex(key)

		if pos < len(current.keys) && bytes.Equal(current.keys[pos], key) {
			value := current.values[pos]
			current.mu.RUnlock()
			return value, nil
		}

		if current.isleaf {
			current.mu.RUnlock()
			return nil, errors.New("key not found")
		}

		next := current.children[pos]
		if next == nil {
			current.mu.RUnlock()
			return nil, errors.New("invalid tree structure")
		}

		next.mu.RLock()
		current.mu.RUnlock()
		current = next
	}
}

func (n *Node) delete(key []byte, isSeekingSuccessor bool) (Keytype, Valuetype) {
	pos := n.findindex(key)

	if n.isleaf && isSeekingSuccessor {
		return n.removeAt(0)
	}

	found := false
	if pos < len(n.keys) && bytes.Compare(n.keys[pos], key) == 0 {
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
