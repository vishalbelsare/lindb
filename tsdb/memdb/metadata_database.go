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

package memdb

import (
	"context"
	"sync"

	"github.com/lindb/common/pkg/fasttime"
	"github.com/lindb/common/pkg/logger"
	"github.com/lindb/common/pkg/timeutil"
	"github.com/lindb/roaring"

	"github.com/lindb/lindb/index"
	"github.com/lindb/lindb/models"
	"github.com/lindb/lindb/pkg/imap"
	"github.com/lindb/lindb/series/metric"
)

//go:generate mockgen -source ./metadata_database.go -destination=./metadata_database_mock.go -package=memdb

var empty = struct{}{}

// MetadataDatabase represents memory metadata database for storing metric meta(name,field etc./database level)
type MetadataDatabase interface {
	// Config returns database's config.
	Config() *models.DatabaseConfig
	// GetOrCreateMetricMeta returns metric meta store, if not exist create new store.
	GetOrCreateMetricMeta(row *metric.StorageRow) (ms mStoreINTF, isNew bool)
	// GetMetricMeta returns metric meta store by memory metric id.
	GetMetricMeta(memMetricID uint64) (mStoreINTF, bool)
	// GetMetaDB returnes metric meta database.
	GetMetaDB() index.MetricMetaDatabase
	// GetMetricIDs returns all metric ids under database.
	GetMetricIDs() *roaring.Bitmap
	// GetMemMetricID returns memory metric id by metric id.
	GetMemMetricID(metricID uint32) (uint64, bool)
	// Notify notifies update or flush metric metadata.
	Notify(event any)
	// Close closed metadata database.
	Close()
}

// metadataDatabase implements MetadataDatabase interface.
type metadataDatabase struct {
	cfg    *models.DatabaseConfig
	metaDB index.MetricMetaDatabase

	ctx    context.Context
	cancel context.CancelFunc

	ch               chan any
	metricIndexStore *imap.IntMap[uint64] // metric id => hash(ns + metric name)
	metricMetadatas  sync.Map             // hash(ns + metirc name) => metric store index(map[uint64]mStoreINTF)

	lock sync.RWMutex
}

// NewMetadataDatabase creates MetadataDatabase instance.
func NewMetadataDatabase(cfg *models.DatabaseConfig, metaDB index.MetricMetaDatabase) MetadataDatabase {
	ctx, cancel := context.WithCancel(context.TODO())
	db := &metadataDatabase{
		cfg:              cfg,
		ctx:              ctx,
		cancel:           cancel,
		metaDB:           metaDB,
		metricIndexStore: imap.NewIntMap[uint64](),
		ch:               make(chan any, 128), // TODO: add config
	}
	go db.handle()
	return db
}

// Config returns database's config.
func (mdb *metadataDatabase) Config() *models.DatabaseConfig {
	return mdb.cfg
}

// Notify notifies update or flush metric metadata.
func (mdb *metadataDatabase) Notify(event any) {
	mdb.ch <- event
}

// GetMetaDB returnes metric meta database.
func (mdb *metadataDatabase) GetMetaDB() index.MetricMetaDatabase {
	return mdb.metaDB
}

// GetOrCreateMetricMeta returns metric meta store, if not exist create new store.
func (mdb *metadataDatabase) GetOrCreateMetricMeta(row *metric.StorageRow) (mStoreINTF, bool) {
	hash := row.NameHash()
	mStore, ok := mdb.metricMetadatas.Load(hash)
	if ok {
		return mStore.(mStoreINTF), false
	}
	mdb.lock.Lock()
	defer mdb.lock.Unlock()

	return mdb.getOrCreateMetricMeta(hash)
}

func (mdb *metadataDatabase) getOrCreateMetricMeta(hash uint64) (mStoreINTF, bool) {
	mStore, ok := mdb.metricMetadatas.Load(hash)
	if ok {
		return mStore.(mStoreINTF), false
	}
	// if not found need create new metric store
	newStore := newMetricStore()
	mdb.metricMetadatas.Store(hash, newStore)
	return newStore, true
}

func (mdb *metadataDatabase) GetMetricIDs() *roaring.Bitmap {
	mdb.lock.RLock()
	defer mdb.lock.RUnlock()
	// return all metric ids(copy map keys)
	return mdb.metricIndexStore.Keys().Clone()
}

// GetMemMetricID returns memory metric id by metric id.
func (mdb *metadataDatabase) GetMemMetricID(metricID uint32) (uint64, bool) {
	mdb.lock.RLock()
	defer mdb.lock.RUnlock()

	return mdb.metricIndexStore.Get(metricID)
}

// GetMetricMeta returns metric meta store by memory metric id.
func (mdb *metadataDatabase) GetMetricMeta(memMetricID uint64) (mStoreINTF, bool) {
	store, ok := mdb.metricMetadatas.Load(memMetricID)
	if !ok {
		return nil, false
	}
	return store.(mStoreINTF), true
}

// Close closed metadata database.
func (mdb *metadataDatabase) Close() {
	close(mdb.ch)
}

// indexMetaStore indexes metric id and memory metric id.
func (mdb *metadataDatabase) indexMetaStore(metricID metric.ID, hash uint64) {
	mdb.lock.Lock()
	defer mdb.lock.Unlock()

	mdb.metricIndexStore.PutIfNotExist(uint32(metricID), hash)
}

// handle handles metadata event.
func (mdb *metadataDatabase) handle() {
	for e := range mdb.ch {
		switch event := e.(type) {
		case *metric.StorageRow:
			mdb.handleRow(event)
		case *FlushEvent:
			mdb.metaDB.PrepareFlush()
			// flush data background
			go mdb.handleFlush(event)
		}
	}
}

// handleFlush flushes metadata into metric meta database.
func (mdb *metadataDatabase) handleFlush(event *FlushEvent) {
	err := mdb.metaDB.Flush()
	event.Callback(err)

	mdb.gc(fasttime.UnixMilliseconds() - timeutil.OneDay)
}

// handleRow lookups metric metedata and indexes.
func (mdb *metadataDatabase) handleRow(row *metric.StorageRow) {
	defer row.Done()

	metricID, err := mdb.metaDB.GenMetricID(row.NameSpace(), row.Name())
	if err != nil {
		memDBLogger.Warn("generate metric id error", logger.String("namespace", string(row.NameSpace())),
			logger.String("metric", string(row.Name())), logger.Error(err))
		return
	}
	memMetricID := row.NameHash()
	mdb.indexMetaStore(metricID, memMetricID)

	if len(row.Fields) == 0 {
		return
	}

	mStore, ok := mdb.GetMetricMeta(memMetricID)
	if !ok {
		return
	}

	for _, fm := range row.Fields {
		fieldID, err := mdb.metaDB.GenFieldID(metricID, fm)
		if err != nil {
			memDBLogger.Warn("generate field error", logger.String("namespace", string(row.NameSpace())),
				logger.String("metric", string(row.Name())), logger.String("field", fm.Name.String()), logger.Error(err))
			continue
		}
		mStore.UpdateFieldMeta(fieldID, fm)
	}
}

// gc clears expired metric meta store.
func (mdb *metadataDatabase) gc(gcTimestamp int64) {
	activeMetricIDs := make(map[uint64]struct{})

	// gc metric store
	mdb.metricMetadatas.Range(func(key, value any) bool {
		mStore := value.(mStoreINTF)
		if mStore.IsActive(gcTimestamp) {
			activeMetricIDs[key.(uint64)] = empty
		} else {
			mdb.metricMetadatas.Delete(key) // delete inactive metric store
		}
		return true
	})

	active := len(activeMetricIDs)

	mdb.lock.Lock()
	defer mdb.lock.Unlock()
	// gc metric store index
	if active == 0 && !mdb.metricIndexStore.IsEmpty() {
		mdb.metricIndexStore = imap.NewIntMap[uint64]()
	} else if float64(active) <= 0.5*float64(mdb.metricIndexStore.Size()) {
		// TODO: add config?
		newIds := imap.NewIntMap[uint64]()
		_ = mdb.metricIndexStore.WalkEntry(func(key uint32, value uint64) error {
			_, ok := activeMetricIDs[value]
			if ok {
				newIds.Put(key, value)
			}
			return nil
		})
		mdb.metricIndexStore = newIds
	}
}
