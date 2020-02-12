// Generated by tmpl
// https://github.com/benbjohnson/tmpl
//
// DO NOT EDIT!
// Source: int_map_test.tmpl

package indexdb

import (
	"fmt"
	"testing"

	"github.com/lindb/roaring"
	"github.com/stretchr/testify/assert"
)

// hack test
func _assertTagStoreData(t *testing.T, keys []uint32, m *TagStore) {
	for _, key := range keys {
		found, highIdx := m.keys.ContainsAndRankForHigh(key)
		assert.True(t, found)
		lowIdx := m.keys.RankForLow(key, highIdx-1)
		assert.True(t, found)
		assert.NotNil(t, m.values[highIdx-1][lowIdx-1])
	}
}

func TestTagStore_Put(t *testing.T) {
	m := NewTagStore()
	m.Put(1, roaring.New())
	m.Put(8, roaring.New())
	m.Put(3, roaring.New())
	m.Put(5, roaring.New())
	m.Put(6, roaring.New())
	m.Put(7, roaring.New())
	m.Put(4, roaring.New())
	m.Put(2, roaring.New())
	// test insert new high
	m.Put(2000000, roaring.New())
	m.Put(2000001, roaring.New())
	// test insert new high
	m.Put(200000, roaring.New())

	_assertTagStoreData(t, []uint32{1, 2, 3, 4, 5, 6, 7, 8, 200000, 2000000, 2000001}, m)
	assert.Equal(t, 11, m.Size())
	assert.Len(t, m.Values(), 3)

	err := m.WalkEntry(func(key uint32, value *roaring.Bitmap) error {
		return fmt.Errorf("err")
	})
	assert.Error(t, err)

	keys := []uint32{1, 2, 3, 4, 5, 6, 7, 8, 200000, 2000000, 2000001}
	idx := 0
	err = m.WalkEntry(func(key uint32, value *roaring.Bitmap) error {
		assert.Equal(t, keys[idx], key)
		idx++
		return nil
	})
	assert.NoError(t, err)
}

func TestTagStore_Get(t *testing.T) {
	m := NewTagStore()
	store, ok := m.Get(uint32(10))
	assert.Nil(t, store)
	assert.False(t, ok)
	m.Put(1, roaring.New())
	m.Put(8, roaring.New())
	_, ok = m.Get(1)
	assert.True(t, ok)
	_, ok = m.Get(2)
	assert.False(t, ok)
	_, ok = m.Get(0)
	assert.False(t, ok)
	_, ok = m.Get(9)
	assert.False(t, ok)
	_, ok = m.Get(999999)
	assert.False(t, ok)
}

func TestTagStore_Keys(t *testing.T) {
	m := NewTagStore()
	m.Put(1, roaring.New())
	m.Put(8, roaring.New())
	assert.Equal(t, roaring.BitmapOf(1, 8), m.Keys())
}
