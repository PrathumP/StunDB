package bptree

import (
	"bytes"
	"sync"
)

type Keytype []byte
type Valuetype []byte

type Node struct {
	keys     []Keytype
	values   []Valuetype
	children []*Node
	isleaf   bool
	mu       sync.RWMutex
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
	node.keys = append(node.keys[:index+1], node.keys[index:]...)
	node.keys[index] = key
	node.values = append(node.values[:index+1], node.values[index:]...)
	node.values[index] = value
}

func (node *Node) insertChildAt(index int, child *Node) {
	node.children = append(node.children[:index+1], node.children[index:]...)
	node.children[index] = child
}

func (node *Node) removeAt(index int) (Keytype, Valuetype) {
	removedKey := node.keys[index]
	removedValue := node.values[index]
	node.keys = append(node.keys[:index], node.keys[index+1:]...)
	node.values = append(node.values[:index], node.values[index+1:]...)
	return removedKey, removedValue
}
