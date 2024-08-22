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

package replica

import (
	"context"
	"sort"
	"sync"

	"go.uber.org/atomic"

	"github.com/lindb/common/pkg/logger"

	"github.com/lindb/lindb/metrics"
	"github.com/lindb/lindb/models"
	"github.com/lindb/lindb/pkg/timeutil"
	"github.com/lindb/lindb/rpc"
	"github.com/lindb/lindb/series/metric"
)

//go:generate mockgen -source=./channel_database.go -destination=./channel_database_mock.go -package=replica

// for testing
var (
	createChannel = newShardChannel
)

// DatabaseChannel represents the database level replication shardChannel
type DatabaseChannel interface {
	// Write writes the metric data into shardChannel's buffer
	Write(ctx context.Context, brokerBatchRows *metric.BrokerBatchRows) error
	// CreateChannel creates the shard level replication shardChannel by given shard id
	CreateChannel(numOfShard int32, shardID models.ShardID) (ShardChannel, error)
	// Stop stops current database write shardChannel.
	Stop()

	// garbageCollect recycles write families which is expired.
	garbageCollect()
}

type (
	shard2Channel map[models.ShardID]ShardChannel
	shardChannels struct {
		value atomic.Value // readonly shard2Channel
		mu    sync.Mutex   // lock for modifying shard2Channel
	}
	databaseChannel struct {
		databaseCfg   models.Database
		ahead         *atomic.Int64
		behind        *atomic.Int64
		ctx           context.Context
		cancel        context.CancelFunc
		fct           rpc.ClientStreamFactory
		numOfShard    atomic.Int32
		shardChannels shardChannels
		interval      timeutil.Interval

		statistics *metrics.BrokerDatabaseWriteStatistics
		logger     logger.Logger
	}
)

// newDatabaseChannel creates a new database replication shardChannel
func newDatabaseChannel(
	ctx context.Context,
	databaseCfg models.Database,
	numOfShard int32,
	fct rpc.ClientStreamFactory,
) DatabaseChannel {
	c, cancel := context.WithCancel(ctx)
	ch := &databaseChannel{
		databaseCfg: databaseCfg,
		ctx:         c,
		cancel:      cancel,
		fct:         fct,
		statistics:  metrics.NewBrokerDatabaseWriteStatistics(databaseCfg.Name),
		logger:      logger.GetLogger("Replica", "DatabaseChannel"),
	}
	ch.shardChannels.value.Store(make(shard2Channel))

	opt := databaseCfg.Option
	ahead, behind := opt.GetAcceptWritableRange()
	ch.ahead = atomic.NewInt64(ahead)
	ch.behind = atomic.NewInt64(behind)

	// TODO need validation
	sort.Sort(databaseCfg.Option.Intervals)
	ch.interval = databaseCfg.Option.Intervals[0].Interval

	ch.numOfShard.Store(numOfShard)

	return ch
}

// garbageCollect recycles write families which is expired.
func (dc *databaseChannel) garbageCollect() {
	dc.shardChannels.mu.Lock()
	defer func() {
		dc.shardChannels.mu.Unlock()
	}()

	channels := dc.shardChannels.value.Load().(shard2Channel)
	ahead := dc.ahead.Load()
	behind := dc.behind.Load()
	for _, channel := range channels {
		channel.garbageCollect(ahead, behind)
	}
}

// Write writes the metric data into shardChannel's buffer
func (dc *databaseChannel) Write(ctx context.Context, brokerBatchRows *metric.BrokerBatchRows) error {
	var err error

	behind := dc.behind.Load()
	ahead := dc.ahead.Load()

	evicted := brokerBatchRows.EvictOutOfTimeRange(behind, ahead)
	dc.statistics.OutOfTimeRange.Add(float64(evicted))

	// sharding metrics to shards
	shardingIterator := brokerBatchRows.NewShardGroupIterator(dc.numOfShard.Load())
	for shardingIterator.HasRowsForNextShard() {
		shardIdx, familyIterator := shardingIterator.FamilyRowsForNextShard(dc.interval)
		shardID := models.ShardID(shardIdx)
		channel, ok := dc.getChannelByShardID(shardID)
		if !ok {
			dc.statistics.ShardNotFound.Incr()
			err = errChannelNotFound
			// broker error, do not return to client
			dc.logger.Error("shardChannel not found",
				logger.String("database", dc.databaseCfg.Name),
				logger.Int("shardID", shardID.Int()))
			continue
		}
		for familyIterator.HasNextFamily() {
			familyTime, rows := familyIterator.NextFamily()
			familyChannel := channel.GetOrCreateFamilyChannel(familyTime)
			if err = familyChannel.Write(ctx, rows); err != nil {
				dc.logger.Error("failed writing rows to family shardChannel",
					logger.String("database", dc.databaseCfg.Name),
					logger.Int("shardID", shardID.Int()),
					logger.Int("rows", len(rows)),
					logger.Int64("familyTime", familyTime),
					logger.Error(err))
			}
		}
	}
	return err
}

// CreateChannel creates the shard level replication shardChannel by given shard id
func (dc *databaseChannel) CreateChannel(numOfShard int32, shardID models.ShardID) (ShardChannel, error) {
	if channel, ok := dc.getChannelByShardID(shardID); ok {
		return channel, nil
	}
	dc.shardChannels.mu.Lock()
	defer dc.shardChannels.mu.Unlock()

	// double check
	if channel, ok := dc.getChannelByShardID(shardID); ok {
		return channel, nil
	}
	if numOfShard <= 0 || int32(shardID) >= numOfShard {
		return nil, errInvalidShardID
	}
	if numOfShard < dc.numOfShard.Load() {
		return nil, errInvalidShardNum
	}
	ch := createChannel(dc.ctx, dc.databaseCfg.Name, shardID, dc.fct)

	// cache shard level shardChannel
	dc.insertShardChannel(shardID, ch)
	return ch, nil
}

// Stop stops current database write shardChannel.
func (dc *databaseChannel) Stop() {
	dc.shardChannels.mu.Lock()
	defer func() {
		dc.cancel()
		dc.shardChannels.mu.Unlock()
	}()

	channels := dc.shardChannels.value.Load().(shard2Channel)
	for _, channel := range channels {
		channel.Stop()
	}
}

// getChannelByShardID gets the replica shardChannel by shard id
func (dc *databaseChannel) getChannelByShardID(shardID models.ShardID) (ShardChannel, bool) {
	ch, ok := dc.shardChannels.value.Load().(shard2Channel)[shardID]
	return ch, ok
}

func (dc *databaseChannel) insertShardChannel(newShardID models.ShardID, newChannel ShardChannel) {
	oldMap := dc.shardChannels.value.Load().(shard2Channel)
	newMap := make(shard2Channel)
	for shardID, channel := range oldMap {
		newMap[shardID] = channel
	}
	newMap[newShardID] = newChannel
	dc.shardChannels.value.Store(newMap)
}
