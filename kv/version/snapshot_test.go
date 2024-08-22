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

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/lindb/lindb/kv/table"
)

func TestSnapshot_FindReaders(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	fv := NewMockFamilyVersion(ctrl)
	vs := NewMockStoreVersionSet(ctrl)
	fv.EXPECT().GetVersionSet().Return(vs).AnyTimes()
	vs.EXPECT().numberOfLevels().Return(2).AnyTimes()
	v := NewMockVersion(ctrl)
	v.EXPECT().Retain().AnyTimes()
	cache := table.NewMockCache(ctrl)
	cache.EXPECT().ReleaseReaders(gomock.Any()).AnyTimes()
	snapshot := newSnapshot("test", v, cache)

	// case 1: get reader err
	cache.EXPECT().GetReader("test", Table(table.FileNumber(10))).Return(nil, fmt.Errorf("err"))
	_, err := snapshot.GetReader(table.FileNumber(10))
	assert.Error(t, err)
	// case 2: get reader ok
	cache.EXPECT().GetReader("test", Table(table.FileNumber(11))).Return(table.NewMockReader(ctrl), nil)
	reader, err := snapshot.GetReader(table.FileNumber(11))
	assert.NoError(t, err)
	assert.NotNil(t, reader)
	// case 3: get version
	assert.NotNil(t, snapshot.GetCurrent())
	// case 4: get reader by key
	v.EXPECT().FindFiles(uint32(80)).Return([]*FileMeta{{fileNumber: 10}}).AnyTimes()
	cache.EXPECT().GetReader("test", Table(table.FileNumber(10))).Return(table.NewMockReader(ctrl), nil)
	readers, err := snapshot.FindReaders(uint32(80))
	assert.NoError(t, err)
	assert.Len(t, readers, 1)
	// case 5: cannot get reader by key
	cache.EXPECT().GetReader("test", Table(table.FileNumber(10))).Return(nil, nil)
	readers, err = snapshot.FindReaders(uint32(80))
	assert.NoError(t, err)
	assert.Empty(t, readers)
	// case 6: get reader by key err
	cache.EXPECT().GetReader("test", Table(table.FileNumber(10))).Return(nil, fmt.Errorf("err"))
	readers, err = snapshot.FindReaders(uint32(80))
	assert.Error(t, err)
	assert.Nil(t, readers)
	// case 7: close snapshot
	v.EXPECT().Release()
	snapshot.Close()
	snapshot.Close() // test version release only once
}

func TestSnapshot_Load(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	fv := NewMockFamilyVersion(ctrl)
	vs := NewMockStoreVersionSet(ctrl)
	fv.EXPECT().GetVersionSet().Return(vs).AnyTimes()
	vs.EXPECT().numberOfLevels().Return(2).AnyTimes()
	v := NewMockVersion(ctrl)
	v.EXPECT().Retain().AnyTimes()
	cache := table.NewMockCache(ctrl)
	cache.EXPECT().ReleaseReaders(gomock.Any()).AnyTimes()
	snapshot := newSnapshot("test", v, cache)
	reader := table.NewMockReader(ctrl)

	cases := []struct {
		name    string
		prepare func()
		loader  func(value []byte) error
		wantErr bool
	}{
		{
			name: "no reader",
			prepare: func() {
				v.EXPECT().FindFiles(gomock.Any()).Return(nil)
			},
		},
		{
			name: "get reader error",
			prepare: func() {
				v.EXPECT().FindFiles(gomock.Any()).Return([]*FileMeta{{}})
				cache.EXPECT().GetReader(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("err"))
			},
			wantErr: true,
		},
		{
			name: "get nil reader",
			prepare: func() {
				v.EXPECT().FindFiles(gomock.Any()).Return([]*FileMeta{{}})
				cache.EXPECT().GetReader(gomock.Any(), gomock.Any()).Return(nil, nil)
			},
		},
		{
			name: "key not exist",
			prepare: func() {
				v.EXPECT().FindFiles(gomock.Any()).Return([]*FileMeta{{}})
				cache.EXPECT().GetReader(gomock.Any(), gomock.Any()).Return(reader, nil)
				reader.EXPECT().Get(gomock.Any()).Return(nil, table.ErrKeyNotExist)
			},
		},
		{
			name: "reader key error",
			prepare: func() {
				v.EXPECT().FindFiles(gomock.Any()).Return([]*FileMeta{{}})
				cache.EXPECT().GetReader(gomock.Any(), gomock.Any()).Return(reader, nil)
				reader.EXPECT().Get(gomock.Any()).Return(nil, fmt.Errorf("err"))
			},
			wantErr: true,
		},
		{
			name: "do loader error",
			prepare: func() {
				v.EXPECT().FindFiles(gomock.Any()).Return([]*FileMeta{{}})
				cache.EXPECT().GetReader(gomock.Any(), gomock.Any()).Return(reader, nil)
				reader.EXPECT().Get(gomock.Any()).Return(nil, nil)
			},
			loader:  func(value []byte) error { return fmt.Errorf("err") },
			wantErr: true,
		},
		{
			name: "do loader successfully",
			prepare: func() {
				v.EXPECT().FindFiles(gomock.Any()).Return([]*FileMeta{{}})
				cache.EXPECT().GetReader(gomock.Any(), gomock.Any()).Return(reader, nil)
				reader.EXPECT().Get(gomock.Any()).Return(nil, nil)
			},
			loader: func(value []byte) error { return nil },
		},
	}
	for i := range cases {
		tt := cases[i]
		t.Run(tt.name, func(t *testing.T) {
			tt.prepare()
			err := snapshot.Load(11, tt.loader)
			if (err != nil) != tt.wantErr {
				t.Fatal(tt.name)
			}
		})
	}
}
