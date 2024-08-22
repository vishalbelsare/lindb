// Licensed to LinDB under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. LinDB licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package model

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/lindb/roaring"

	"github.com/lindb/lindb/pkg/trie"
)

type mockWriter struct{}

func (w *mockWriter) Write(_ []byte) (int, error) {
	return 0, fmt.Errorf("err")
}

func TestTrieBucket(t *testing.T) {
	keys, values, data := createTriesData(t, 4)
	tries := NewTrieBucketWithBlockSize(4)
	defer tries.Release()

	assert.NoError(t, tries.Unmarshal(data))

	cases := func() {
		assert.Len(t, tries.GetValues(), len(keys))

		for i, key := range keys {
			id, ok := tries.GetValue(key)
			assert.True(t, ok)
			assert.Equal(t, values[i], id)
		}
		// not value
		_, ok := tries.GetValue([]byte("123"))
		assert.False(t, ok)
	}
	assert.Len(t, tries.kvs, 3)
	cases()
	w := bytes.NewBuffer([]byte{})
	assert.NoError(t, tries.Write(w))
	data2 := w.Bytes()
	assert.Equal(t, len(data), len(data2))

	tries = NewTrieBucket()
	assert.NoError(t, tries.Unmarshal(data2))
	assert.Len(t, tries.kvs, 3)

	assert.Len(t, tries.Suggest("a", 3), 3)
	assert.Len(t, tries.Suggest("a", 10), 6)
	cases()
}

func TestTrieBucket_Write(t *testing.T) {
	keys, values, data := createTriesData(t, 3)
	cases := []struct {
		name      string
		blockSize int
	}{
		{
			name:      "one bucket",
			blockSize: math.MaxInt16,
		},
		{
			name:      "no pending kv",
			blockSize: 5,
		},
		{
			name:      "no pending kv",
			blockSize: 3,
		},
	}
	for i := range cases {
		tt := cases[i]
		t.Run(tt.name, func(t *testing.T) {
			tries := NewTrieBucketWithBlockSize(tt.blockSize)
			defer tries.Release()

			assert.NoError(t, tries.Unmarshal(data))
			w := bytes.NewBuffer([]byte{})
			assert.NoError(t, tries.Write(w))
			data2 := w.Bytes()

			tries = NewTrieBucket()
			assert.NoError(t, tries.Unmarshal(data2))
			assert.Len(t, tries.GetValues(), len(keys))
			for i, key := range keys {
				id, ok := tries.GetValue(key)
				assert.True(t, ok)
				assert.Equal(t, values[i], id)
			}
			// not value
			_, ok := tries.GetValue([]byte("123"))
			assert.False(t, ok)
		})
	}
}

func TestTrieBucket_Write_Error(t *testing.T) {
	mockW := &mockWriter{}
	t.Run("write raw buf error", func(t *testing.T) {
		_, _, data := createTriesData(t, 3)
		tries := NewTrieBucketWithBlockSize(3)
		assert.NoError(t, tries.Unmarshal(data))
		assert.Error(t, tries.Write(mockW))
	})
	t.Run("write pending error", func(t *testing.T) {
		_, _, data := createTriesData(t, 10)
		tries := NewTrieBucket()
		assert.NoError(t, tries.Unmarshal(data))
		assert.Error(t, tries.Write(mockW))
	})
}

func TestTrieBucket_CollectKVs(t *testing.T) {
	keys, values, data := createTriesData(t, math.MaxUint16)
	tries := NewTrieBucket()
	defer tries.Release()

	assert.NoError(t, tries.Unmarshal(data))

	cases := []struct {
		name   string
		values []uint32
		size   int
	}{
		{
			name:   "collect sub",
			values: values[0:4],
			size:   4,
		},
		{
			name: "empty",
			size: 0,
		},
		{
			name:   "collect all",
			values: values,
			size:   len(keys),
		},
	}

	for i := range cases {
		tt := cases[i]
		t.Run(tt.name, func(t *testing.T) {
			result := make(map[uint32]string)
			tries.CollectKVs(roaring.BitmapOf(tt.values...), result)
			assert.Len(t, result, tt.size)
		})
	}
}

func TestTrieBucket_FindValuesByRegexp(t *testing.T) {
	keys, _, data := createTriesData(t, math.MaxUint16)
	tries := NewTrieBucket()
	defer tries.Release()

	assert.NoError(t, tries.Unmarshal(data))

	cases := []struct {
		regexp string
		size   int
	}{
		{
			regexp: "^abc",
			size:   4,
		},
		{
			regexp: "hh",
			size:   0,
		},
		{
			size: len(keys),
		},
	}

	for i := range cases {
		tt := cases[i]
		t.Run(tt.regexp, func(t *testing.T) {
			var ids []uint32
			rp, err := regexp.Compile(tt.regexp)
			assert.NoError(t, err)
			ids = tries.FindValuesByRegexp(rp, ids)
			assert.Len(t, ids, tt.size)
		})
	}
}

func TestTrieBucket_FindValuesByLike(t *testing.T) {
	keys, _, data := createTriesData(t, math.MaxUint16)
	tries := NewTrieBucket()
	defer tries.Release()

	assert.NoError(t, tries.Unmarshal(data))

	cases := []struct {
		prefix string
		size   int
	}{
		{
			prefix: "abc",
			size:   4,
		},
		{
			prefix: "hh",
			size:   0,
		},
		{
			size: len(keys),
		},
	}

	for i := range cases {
		tt := cases[i]
		t.Run(tt.prefix, func(t *testing.T) {
			var ids []uint32
			ids = tries.FindValuesByLike([]byte(tt.prefix), []byte(tt.prefix), bytes.HasPrefix, ids)
			assert.Len(t, ids, tt.size)
		})
	}
}

func TestTrieBucket_Unmarlshal_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer func() {
		ctrl.Finish()
		getTrieFn = trie.GetTrie
	}()

	tries := NewTrieBucket()
	mockTrie := trie.NewMockSuccinctTrie(ctrl)
	getTrieFn = func() trie.SuccinctTrie {
		return mockTrie
	}
	mockTrie.EXPECT().UnmarshalBinary(gomock.Any()).Return(fmt.Errorf("err"))
	_, _, data := createTriesData(t, 4)
	err := tries.Unmarshal(data)
	assert.Error(t, err)
}

func createTriesData(t *testing.T, blockSize int) (keys [][]byte, values []uint32, data []byte) {
	keysString := []string{
		"a", "ab", "b", "abc", "abcdefgh", "abcdefghijklmnopqrstuvwxyz", "abcdefghijkl", "zzzzzz", "ice",
	}
	for idx, key := range keysString {
		keys = append(keys, []byte(key))
		values = append(values, uint32(idx))
	}
	w := bytes.NewBuffer([]byte{})
	b := NewTrieBucketBuilder(blockSize, w)
	assert.NoError(t, b.Write(keys, values))
	data = w.Bytes()
	return
}

func createTriesDataBulk(t *testing.T, blockSize int) (keys [][]byte, values []uint32, data []byte, keysString []string) {
	keysString = []string{
		"go_memstats_alloc_bytes_total",
		"process_virtual_memory_bytes",
		"request_count",
		"err_request_count",
		"go_info",
		"go_memstats_heap_idle_bytes",
		"go_memstats_mcache_inuse_bytes",
		"go_memstats_mallocs_total",
		"go_memstats_mspan_inuse_bytes",
		"go_memstats_mspan_sys_bytes",
		"go_gc_duration_seconds_sum",
		"go_gc_duration_seconds_count",
		"go_memstats_frees_total",
		"go_memstats_heap_alloc_bytes",
		"go_memstats_heap_objects",
		"request_duration_seconds_count",
		"go_memstats_heap_inuse_bytes",
		"process_max_fds",
		"process_start_time_seconds",
		"promhttp_metric_handler_requests_total",
		"request_duration_seconds_sum",
		"go_memstats_heap_released_bytes",
		"go_memstats_next_gc_bytes",
		"go_memstats_stack_sys_bytes",
		"go_threads",
		"process_open_fds",
		"go_memstats_gc_sys_bytes",
		"process_resident_memory_bytes",
		"go_memstats_buck_hash_sys_bytes",
		"go_memstats_last_gc_time_seconds",
		"go_memstats_lookups_total",
		"go_memstats_sys_bytes",
		"promhttp_metric_handler_requests_in_flight",
		"go_memstats_other_sys_bytes",
		"go_memstats_stack_inuse_bytes",
		"process_cpu_seconds_total",
		"go_gc_duration_seconds",
		"go_goroutines",
		"go_memstats_alloc_bytes",
		"go_memstats_heap_sys_bytes",
		"go_memstats_mcache_sys_bytes",
		"process_virtual_memory_max_bytes",
		"request_duration_seconds_bucket",
	}
	for idx, key := range keysString {
		keys = append(keys, []byte(key))
		values = append(values, uint32(idx))
	}
	w := bytes.NewBuffer([]byte{})
	b := NewTrieBucketBuilder(blockSize, w)
	assert.NoError(t, b.Write(keys, values))
	return keys, values, w.Bytes(), keysString
}

func TestTrieBucket_Unmarlshal(t *testing.T) {
	tries := NewTrieBucket()
	_, _, data, keys := createTriesDataBulk(t, 4)
	err := tries.Unmarshal(data)
	assert.Nil(t, err)
	for _, key := range keys {
		id, ok := tries.GetValue([]byte(key))
		assert.Equal(t, ok, true)
		assert.GreaterOrEqual(t, id, uint32(0))
	}
	rs := tries.Suggest("", len(keys))
	sort.Strings(keys)
	sort.Strings(rs)
	assert.Equal(t, keys, rs)
}
