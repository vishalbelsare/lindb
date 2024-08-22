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

package table

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestMapCache_GetReader(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer func() {
		newMMapStoreReaderFunc = newMMapStoreReader
		ctrl.Finish()
	}()
	cache := NewCache(t.TempDir(), time.Hour)
	// case 1: get reader err
	newMMapStoreReaderFunc = func(path, fileName string) (r Reader, err error) {
		return nil, fmt.Errorf("err")
	}
	r, err := cache.GetReader("f", "100000.sst")
	assert.Error(t, err)
	assert.Nil(t, r)
	// case 2: get reader success
	mockReader := NewMockReader(ctrl)
	newMMapStoreReaderFunc = func(path, fileName string) (reader Reader, err error) {
		return mockReader, nil
	}
	r, err = cache.GetReader("f", "100000.sst")
	assert.NoError(t, err)
	assert.Equal(t, mockReader, r)
	// case 3: get exist reader
	r, err = cache.GetReader("f", "100000.sst")
	assert.NoError(t, err)
	assert.Equal(t, mockReader, r)

	// case 4: evict not exist
	cache.Evict("200000.sst")
	// case 5: evict reader err
	mockReader.EXPECT().Close().Return(fmt.Errorf("err"))
	cache.Evict("100000.sst")
	// case6, evict ok
	mockReader.EXPECT().Close().Return(nil)
	_, _ = cache.GetReader("f", "100000.sst")
	cache.Evict("100000.sst")

	// case 6: close err
	mockReader.EXPECT().Close().Return(fmt.Errorf("err")).MaxTimes(2)
	_, _ = cache.GetReader("f", "100000.sst")
	_, _ = cache.GetReader("f", "200000.sst")
	err = cache.Close()
	assert.NoError(t, err)
	// case7, close ok
	mockReader.EXPECT().Close().Return(nil).AnyTimes()
	err = cache.Close()
	assert.NoError(t, err)
}

func TestStoreCache_Cleanup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer func() {
		newMMapStoreReaderFunc = newMMapStoreReader
		ctrl.Finish()
	}()

	cache := NewCache(t.TempDir(), time.Millisecond)
	mockReader := NewMockReader(ctrl)
	newMMapStoreReaderFunc = func(path, fileName string) (reader Reader, err error) {
		return mockReader, nil
	}
	r, err := cache.GetReader("f", "100000.sst")
	assert.NoError(t, err)
	assert.Equal(t, mockReader, r)

	time.Sleep(time.Millisecond * 100)
	cache.Cleanup()
	cache1 := cache.(*storeCache)
	assert.Len(t, cache1.cache.items, 1)

	mockReader.EXPECT().FileName().Return("100000.sst")
	cache.ReleaseReaders([]Reader{r})

	mockReader.EXPECT().Close().Return(nil)
	cache.Cleanup()
	assert.Len(t, cache1.cache.items, 0)

	err = cache.Close()
	assert.NoError(t, err)
}
