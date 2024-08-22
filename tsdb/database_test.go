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

package tsdb

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lindb/common/pkg/fileutil"
	"github.com/lindb/common/pkg/ltoml"
	commontimeutil "github.com/lindb/common/pkg/timeutil"
	"github.com/stretchr/testify/assert"
	"go.uber.org/atomic"
	"go.uber.org/mock/gomock"

	"github.com/lindb/lindb/index"
	"github.com/lindb/lindb/kv"
	"github.com/lindb/lindb/metrics"
	"github.com/lindb/lindb/models"
	"github.com/lindb/lindb/pkg/option"
	"github.com/lindb/lindb/tsdb/memdb"
)

func TestDatabase_New(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer func() {
		encodeToml = ltoml.EncodeToml
		mkDirIfNotExist = fileutil.MkDirIfNotExist
		newMetaDBFunc = index.NewMetricMetaDatabase
		ctrl.Finish()
	}()

	opt := &option.DatabaseOption{}
	shard := NewMockShard(ctrl)

	cases := []struct {
		cfg     *models.DatabaseConfig
		prepare func()
		name    string
		wantErr bool
	}{
		{
			name: "create database path err",
			prepare: func() {
				mkDirIfNotExist = func(path string) error {
					return fmt.Errorf("mkdir err")
				}
			},
			wantErr: true,
		},
		{
			name: "dump config err",
			prepare: func() {
				encodeToml = func(fileName string, v interface{}) error {
					return fmt.Errorf("err")
				}
			},
			wantErr: true,
		},
		{
			name: "create metadata err",
			prepare: func() {
				newMetaDBFunc = func(_, _ string) (metadata index.MetricMetaDatabase, err error) {
					return nil, fmt.Errorf("err")
				}
			},
			wantErr: true,
		},
		{
			name:    "option validation fail",
			cfg:     &models.DatabaseConfig{Option: opt},
			wantErr: true,
		},
		{
			name: "create shard err",
			prepare: func() {
				newShardFunc = func(db Database, shardID models.ShardID) (s Shard, err error) {
					return nil, fmt.Errorf("err")
				}
			},
			wantErr: true,
		},
		{
			name: "create database successfully",
			prepare: func() {
				metaDB := index.NewMockMetricMetaDatabase(ctrl)
				newMetaDBFunc = func(_, _ string) (index.MetricMetaDatabase, error) {
					return metaDB, nil
				}
				newShardFunc = func(db Database, shardID models.ShardID) (s Shard, err error) {
					return shard, nil
				}
			},
			wantErr: false,
		},
		{
			name: "close metadata err when create database failure",
			prepare: func() {
				metaDB := index.NewMockMetricMetaDatabase(ctrl)
				newMetaDBFunc = func(_, _ string) (index.MetricMetaDatabase, error) {
					return metaDB, nil
				}
				newShardFunc = func(db Database, shardID models.ShardID) (s Shard, err error) {
					return nil, fmt.Errorf("err")
				}
				metaDB.EXPECT().Close().Return(fmt.Errorf("err"))
			},
			wantErr: true,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				encodeToml = func(fileName string, v interface{}) error {
					return nil
				}
				mkDirIfNotExist = func(path string) error {
					return nil
				}
				newMetaDBFunc = func(_, _ string) (index.MetricMetaDatabase, error) {
					return nil, nil
				}
				newShardFunc = newShard
			}()
			if tt.prepare != nil {
				tt.prepare()
			}
			opt := &option.DatabaseOption{Intervals: option.Intervals{{Interval: 10}}}
			cfg := &models.DatabaseConfig{
				ShardIDs: []models.ShardID{1, 2, 3},
				Option:   opt,
			}
			if tt.cfg != nil {
				cfg = tt.cfg
			}
			db, err := newDatabase("db", cfg, models.NewDefaultLimits(), nil)
			if ((err != nil) != tt.wantErr && db == nil) || (!tt.wantErr && db == nil) {
				t.Errorf("newDatabase() error = %v, wantErr %v", err, tt.wantErr)
			}

			if db != nil {
				// assert database information after create successfully
				assert.NotNil(t, db.MetaDB())
				assert.NotNil(t, db.ExecutorPool())
				assert.Equal(t, "db", db.Name())
				assert.True(t, db.NumOfShards() >= 0)
				assert.Equal(t, &option.DatabaseOption{Intervals: option.Intervals{{Interval: 10}}}, db.GetOption())
				assert.NotNil(t, db.GetConfig())
				assert.NotNil(t, db.GetLimits())
				assert.NotNil(t, db.MemMetaDB())
				db.SetLimits(models.NewDefaultLimits())
			}
		})
	}
}

func TestDatabase_CreateShards(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer func() {
		encodeToml = ltoml.EncodeToml
		ctrl.Finish()
	}()
	db := &database{
		config:   &models.DatabaseConfig{},
		shardSet: *newShardSet(),
	}
	type args struct {
		shardIDs []models.ShardID
		option   option.DatabaseOption
	}
	cases := []struct {
		prepare func()
		name    string
		args    args
		wantErr bool
	}{
		{
			name:    "shard ids cannot be empty",
			args:    args{},
			wantErr: true,
		},
		{
			name: "create shard err",
			args: args{option: option.DatabaseOption{}, shardIDs: []models.ShardID{4, 5, 6}},
			prepare: func() {
				newShardFunc = func(db Database, shardID models.ShardID) (s Shard, err error) {
					return nil, fmt.Errorf("err")
				}
			},
			wantErr: true,
		},
		{
			name: "create exist shard",
			args: args{option: option.DatabaseOption{}, shardIDs: []models.ShardID{4}},
			prepare: func() {
				db.shardSet.InsertShard(models.ShardID(4), nil)
			},
			wantErr: false,
		},
		{
			name: "create shard successfully",
			args: args{option: option.DatabaseOption{}, shardIDs: []models.ShardID{5}},
			prepare: func() {
				newShardFunc = func(db Database, shardID models.ShardID) (s Shard, err error) {
					return nil, nil
				}
			},
			wantErr: false,
		},
		{
			name: "dump option err",
			args: args{option: option.DatabaseOption{}, shardIDs: []models.ShardID{6}},
			prepare: func() {
				newShardFunc = func(db Database, shardID models.ShardID) (s Shard, err error) {
					return nil, nil
				}
				encodeToml = func(fileName string, v interface{}) error {
					return fmt.Errorf("err")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				newShardFunc = newShard
				encodeToml = func(fileName string, v interface{}) error {
					return nil
				}
			}()
			if tt.prepare != nil {
				tt.prepare()
			}
			if err := db.CreateShards(tt.args.shardIDs); (err != nil) != tt.wantErr {
				t.Errorf("CreateShards() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	t.Run("create exist shard", func(t *testing.T) {
		db.shardSet.InsertShard(1, nil)
		err := db.createShard(1)
		assert.NoError(t, err)
	})
}

func TestDatabase_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer func() {
		ctrl.Finish()
	}()

	memMetaDB := memdb.NewMockMetadataDatabase(ctrl)
	metaDB := index.NewMockMetricMetaDatabase(ctrl)
	memMetaDB.EXPECT().Notify(gomock.Any()).DoAndReturn(func(event any) {
		mn := event.(*memdb.FlushEvent)
		mn.Callback(nil)
	}).AnyTimes()
	store := kv.NewMockStore(ctrl)
	store.EXPECT().Name().Return("metaStore").AnyTimes()
	db := &database{
		metaDB:         metaDB,
		memMetaDB:      memMetaDB,
		shardSet:       *newShardSet(),
		flushCondition: sync.NewCond(&sync.Mutex{}),
	}
	cases := []struct {
		prepare func()
		name    string
		wantErr bool
	}{
		{
			name: "close metadata database err",
			prepare: func() {
				memMetaDB.EXPECT().Close()
				metaDB.EXPECT().Close().Return(fmt.Errorf("err"))
			},
			wantErr: true,
		},
		{
			name: "close meta store err",
			prepare: func() {
				mockShard := NewMockShard(ctrl)
				mockShard.EXPECT().FlushIndex().Return(fmt.Errorf("err"))
				db.shardSet.InsertShard(models.ShardID(1), mockShard)
				gomock.InOrder(
					memMetaDB.EXPECT().Close(),
					metaDB.EXPECT().Close().Return(nil),
					mockShard.EXPECT().Close().Return(fmt.Errorf("err")),
				)
			},
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.prepare != nil {
				tt.prepare()
			}
			if err := db.Close(); (err != nil) != tt.wantErr {
				t.Errorf("Close() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDatabase_FlushMeta(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	metaDB := memdb.NewMockMetadataDatabase(ctrl)
	db := &database{
		memMetaDB:      metaDB,
		flushCondition: sync.NewCond(&sync.Mutex{}),
		isFlushing:     *atomic.NewBool(false),
		statistics:     metrics.NewDatabaseStatistics("test"),
	}
	cases := []struct {
		prepare func()
		name    string
		wantErr bool
	}{
		{
			name: "meta flushing",
			prepare: func() {
				db.isFlushing.Store(true)
			},
			wantErr: false,
		},
		{
			name: "flush meta failure",
			prepare: func() {
				db.isFlushing.Store(false)
				metaDB.EXPECT().Notify(gomock.Any()).DoAndReturn(func(event any) {
					mn := event.(*memdb.FlushEvent)
					mn.Callback(fmt.Errorf("err"))
				})
			},
			wantErr: true,
		},
		{
			name: "flush meta successfully",
			prepare: func() {
				db.isFlushing.Store(false)
				metaDB.EXPECT().Notify(gomock.Any()).DoAndReturn(func(event any) {
					mn := event.(*memdb.FlushEvent)
					mn.Callback(nil)
				})
			},
			wantErr: false,
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				db.isFlushing.Store(false)
			}()
			if tt.prepare != nil {
				tt.prepare()
			}
			if err := db.FlushMeta(); (err != nil) != tt.wantErr {
				t.Errorf("FlushMeta() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDatabase_Flush(t *testing.T) {
	ctrl := gomock.NewController(t)

	defer ctrl.Finish()

	checker := NewMockDataFlushChecker(ctrl)

	db := &database{
		shardSet:     *newShardSet(),
		isFlushing:   *atomic.NewBool(false),
		flushChecker: checker,
	}
	shard1 := NewMockShard(ctrl)
	shard2 := NewMockShard(ctrl)
	shard1.EXPECT().ShardID().Return(models.ShardID(1)).AnyTimes()
	shard2.EXPECT().ShardID().Return(models.ShardID(2)).AnyTimes()
	db.shardSet.InsertShard(1, shard1)
	db.shardSet.InsertShard(2, shard2)
	checker.EXPECT().requestFlushJob(gomock.Any())
	checker.EXPECT().requestFlushJob(gomock.Any())
	err := db.Flush()
	assert.NoError(t, err)
}

func Test_ShardSet_multi(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	set := newShardSet()
	shard1 := NewMockShard(ctrl)
	for i := 0; i < 100; i += 2 {
		set.InsertShard(models.ShardID(i), shard1)
	}
	assert.Equal(t, set.GetShardNum(), 50)
	_, ok := set.GetShard(0)
	assert.True(t, ok)
	_, ok = set.GetShard(11)
	assert.False(t, ok)
	_, ok = set.GetShard(101)
	assert.False(t, ok)
}

func TestDatabase_WaitFlushMetaCompleted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	now := commontimeutil.Now()
	metaDB := memdb.NewMockMetadataDatabase(ctrl)
	db := &database{
		memMetaDB:      metaDB,
		isFlushing:     *atomic.NewBool(false),
		flushCondition: sync.NewCond(&sync.Mutex{}),
		statistics:     metrics.NewDatabaseStatistics("test"),
	}

	metaDB.EXPECT().Notify(gomock.Any()).DoAndReturn(func(event any) {
		time.Sleep(100 * time.Millisecond)
		mn := event.(*memdb.FlushEvent)
		mn.Callback(nil)
	})
	var wait sync.WaitGroup
	wait.Add(2)
	ch := make(chan struct{})
	go func() {
		ch <- struct{}{}
		err := db.FlushMeta()
		assert.NoError(t, err)
	}()
	<-ch
	time.Sleep(10 * time.Millisecond)
	go func() {
		db.WaitFlushMetaCompleted()
		wait.Done()
	}()
	go func() {
		db.WaitFlushMetaCompleted()
		wait.Done()
	}()
	wait.Wait()
	assert.True(t, commontimeutil.Now()-now >= 100*time.Millisecond.Milliseconds())
}

func TestDatabase_Drop(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer func() {
		removeDir = fileutil.RemoveDir
		ctrl.Finish()
	}()
	metaDB := memdb.NewMockMetadataDatabase(ctrl)
	mDB := index.NewMockMetricMetaDatabase(ctrl)
	db := &database{
		memMetaDB:      metaDB,
		metaDB:         mDB,
		shardSet:       *newShardSet(),
		isFlushing:     *atomic.NewBool(false),
		flushCondition: sync.NewCond(&sync.Mutex{}),
		statistics:     metrics.NewDatabaseStatistics("test-drop"),
	}
	// flush error
	metaDB.EXPECT().Notify(gomock.Any()).DoAndReturn(func(event any) {
		mn := event.(*memdb.FlushEvent)
		mn.Callback(fmt.Errorf("err"))
	})
	assert.Error(t, db.Drop())
	removeDir = func(path string) error {
		return fmt.Errorf("err")
	}
	metaDB.EXPECT().Notify(gomock.Any()).DoAndReturn(func(event any) {
		mn := event.(*memdb.FlushEvent)
		mn.Callback(nil)
	}).MaxTimes(2)
	mDB.EXPECT().Close().Return(nil).MaxTimes(2)
	metaDB.EXPECT().Close().MaxTimes(2)
	assert.Error(t, db.Drop())
	removeDir = func(path string) error {
		return nil
	}
	assert.NoError(t, db.Drop())
}

func TestDatabase_TTL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	set := newShardSet()
	shard1 := NewMockShard(ctrl)
	set.InsertShard(models.ShardID(0), shard1)
	db := &database{
		shardSet: *set,
	}
	shard1.EXPECT().TTL()
	db.TTL()
}

func TestDatabase_EvictSegment(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	set := newShardSet()
	shard1 := NewMockShard(ctrl)
	set.InsertShard(models.ShardID(0), shard1)
	db := &database{
		shardSet: *set,
	}
	shard1.EXPECT().EvictSegment()
	db.EvictSegment()
}

func Benchmark_LoadSyncMap(b *testing.B) {
	var m sync.Map
	for i := 0; i < boundaryShardSetLen; i++ {
		m.Store(i, &shard{})
	}
	// 8.435 ns
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			item, ok := m.Load(boundaryShardSetLen - 1)
			if ok {
				_, _ = item.(*shard)
			}
		}
	})
}

func Benchmark_LoadAtomicValue(b *testing.B) {
	var v atomic.Value
	l := make([]*shard, boundaryShardSetLen)
	v.Store(l)

	// 2.631ns
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			list := v.Load().([]*shard)
			for i := 0; i < boundaryShardSetLen; i++ {
				if i == boundaryShardSetLen-1 {
					_ = list[boundaryShardSetLen-1]
				}
			}
		}
	})
}

func Benchmark_SyncRWMutex(b *testing.B) {
	var lock sync.RWMutex
	m := make(map[int]*shard)
	for i := 0; i < boundaryShardSetLen; i++ {
		m[i] = &shard{}
	}

	// 34.75 ns
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lock.RLock()
			_ = m[boundaryShardSetLen-1]
			lock.RUnlock()
		}
	})
}

func Benchmark_MapWithoutLock(b *testing.B) {
	m := make(map[int]*shard)
	for i := 0; i < boundaryShardSetLen; i++ {
		m[i] = &shard{}
	}
	var v atomic.Value
	v.Store(m)
	// 3.066 ns
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			item := v.Load().(map[int]*shard)
			_ = item[boundaryShardSetLen-1]
		}
	})
}

var boundaryShardSetLen = 20

func Benchmark_ShardSet_iterating(b *testing.B) {
	set := newShardSet()
	for i := 0; i < boundaryShardSetLen; i++ {
		set.InsertShard(models.ShardID(i), nil)
	}
	// 2.8ns
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			set.GetShard(models.ShardID(boundaryShardSetLen - 1))
		}
	})
}

func Benchmark_ShardSet_binarySearch(b *testing.B) {
	set := newShardSet()
	for i := 0; i < boundaryShardSetLen+1; i++ {
		set.InsertShard(models.ShardID(i), nil)
	}
	// 4.68ns
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			set.GetShard(models.ShardID(boundaryShardSetLen))
		}
	})
}
