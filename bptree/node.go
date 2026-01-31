package bptree

import (
	"bytes"
	"sync"
)

type Keytype []byte
type Valuetype []byte

type Node struct {
	keys         []Keytype
	values       []Valuetype
	children     []*Node
	isleaf       bool
	mu           sync.RWMutex
	rightSibling *Node
}

const (
	MaxKeys = 4
	MinKeys = MaxKeys / 2
)

func (node *Node) findindex(key []byte) int {
	length := len(node.keys)
	for i := 0; i < length; i++ {
		if bytes.Compare(node.keys[i], key) >= 0 {
			return i
		}
	}
	return length
}

func (node *Node) alreadyExists(key []byte) bool {
	length := len(node.keys)
	for i := 0; i < length; i++ {
		if bytes.Equal(node.keys[i], key) {
			return true
		}
	}
	return false
}

func NewNode(isleaf bool) *Node {
	return &Node{
		keys:     make([]Keytype, 0, MaxKeys),
		values:   make([]Valuetype, 0, MaxKeys),
		children: make([]*Node, 0, MaxKeys+1),
		isleaf:   isleaf,
	}
}

func (node *Node) insertAt(index int, key Keytype, value Valuetype) {
	// Grow slices by appending a zero value first
	node.keys = append(node.keys, nil)
	node.values = append(node.values, nil)
	// Shift elements right
	copy(node.keys[index+1:], node.keys[index:len(node.keys)-1])
	copy(node.values[index+1:], node.values[index:len(node.values)-1])
	// Insert at index
	node.keys[index] = key
	node.values[index] = value
}

func (node *Node) insertChildAt(index int, child *Node) {
	// Grow slice by appending nil first
	node.children = append(node.children, nil)
	// Shift elements right
	copy(node.children[index+1:], node.children[index:len(node.children)-1])
	// Insert at index
	node.children[index] = child
}

func (node *Node) removeAt(index int) (Keytype, Valuetype) {
	removedKey := node.keys[index]
	removedValue := node.values[index]
	node.keys = append(node.keys[:index], node.keys[index+1:]...)
	node.values = append(node.values[:index], node.values[index+1:]...)
	return removedKey, removedValue
}
