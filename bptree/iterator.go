package bptree

import (
	"bytes"
	"errors"
)

// GetRange returns all key-value pairs in the range [startKey, endKey].
// Thread-safe: acquires read lock on tree.
func (t *Btree) GetRange(startKey, endKey []byte) ([]Keytype, []Valuetype, error) {
	t.treeLock.RLock()
	defer t.treeLock.RUnlock()

	if t.root == nil {
		return nil, nil, nil
	}
	if bytes.Compare(startKey, endKey) > 0 {
		return nil, nil, errors.New("invalid range: startKey is greater than endKey")
	}

	keys := make([]Keytype, 0)
	values := make([]Valuetype, 0)
	t.root.getRange(startKey, endKey, &keys, &values)
	return keys, values, nil
}

// getRange collects key-value pairs in the range. Called under treeLock.
func (n *Node) getRange(startKey, endKey []byte, keys *[]Keytype, values *[]Valuetype) {
	pos := 0
	for pos < len(n.keys) && bytes.Compare(n.keys[pos], startKey) < 0 {
		pos++
	}

	if n.isleaf {
		for i := pos; i < len(n.keys) && bytes.Compare(n.keys[i], endKey) <= 0; i++ {
			// Make copies
			keyCopy := make([]byte, len(n.keys[i]))
			copy(keyCopy, n.keys[i])
			valueCopy := make([]byte, len(n.values[i]))
			copy(valueCopy, n.values[i])
			*keys = append(*keys, keyCopy)
			*values = append(*values, valueCopy)
		}
		return
	}

	// Internal node
	if pos < len(n.children) {
		n.children[pos].getRange(startKey, endKey, keys, values)
	}

	for i := pos; i < len(n.keys); i++ {
		if bytes.Compare(n.keys[i], endKey) > 0 {
			break
		}
		keyCopy := make([]byte, len(n.keys[i]))
		copy(keyCopy, n.keys[i])
		valueCopy := make([]byte, len(n.values[i]))
		copy(valueCopy, n.values[i])
		*keys = append(*keys, keyCopy)
		*values = append(*values, valueCopy)

		if i+1 < len(n.children) {
			n.children[i+1].getRange(startKey, endKey, keys, values)
		}
	}
}

// DeleteRange deletes all keys in the range [startKey, endKey].
// Thread-safe: uses GetRange for snapshot, then Delete for each key.
func (t *Btree) DeleteRange(startKey, endKey []byte) (int, error) {
	// First, get a snapshot of keys to delete
	keys, _, err := t.GetRange(startKey, endKey)
	if err != nil {
		return 0, err
	}

	deletedCount := 0
	for _, key := range keys {
		if t.Delete(key) {
			deletedCount++
		}
	}

	return deletedCount, nil
}
