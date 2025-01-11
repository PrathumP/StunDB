package bptree

import (
	"bytes"
	"errors"
)

func (t *Btree) GetRange(startKey, endKey []byte) ([]Keytype, []Valuetype, error) {
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

func (n *Node) getRange(startKey, endKey []byte, keys *[]Keytype, values *[]Valuetype) {
	pos := 0

	for pos < len(n.keys) && bytes.Compare(n.keys[pos], startKey) < 0 {
		pos++
	}

	if n.isleaf {

		for i := pos; i < len(n.keys) && bytes.Compare(n.keys[i], endKey) <= 0; i++ {
			*keys = append(*keys, n.keys[i])
			*values = append(*values, n.values[i])
		}
		return
	}

	if pos < len(n.children) {
		n.children[pos].getRange(startKey, endKey, keys, values)
	}

	for i := pos; i < len(n.keys); i++ {
		if bytes.Compare(n.keys[i], endKey) > 0 {
			break
		}
		*keys = append(*keys, n.keys[i])
		*values = append(*values, n.values[i])

		if i+1 < len(n.children) {
			n.children[i+1].getRange(startKey, endKey, keys, values)
		}
	}
}

func (t *Btree) DeleteRange(startKey, endKey []byte) (int, error) {
	if t.root == nil {
		return 0, nil
	}
	if bytes.Compare(startKey, endKey) > 0 {
		return 0, errors.New("invalid range: startKey is greater than endKey")
	}

	keys, _, _ := t.GetRange(startKey, endKey)
	deletedCount := 0

	for _, key := range keys {
		if t.Delete(key) {
			deletedCount++
		}
	}

	return deletedCount, nil
}
