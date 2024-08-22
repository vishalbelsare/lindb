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

package models

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/lindb/common/models"
	"github.com/lindb/common/pkg/encoding"
	"github.com/lindb/common/pkg/timeutil"

	"github.com/lindb/lindb/config"
)

type ShardStateType int

const (
	UnknownShard ShardStateType = iota
	NewShard
	OnlineShard
	OfflineShard
	NonExistentShard
)

const NoLeader NodeID = -1

// NodeStateType represents node state type
type NodeStateType int

const (
	NodeOnline NodeStateType = iota + 1
	NodeOffline
)

// ClusterStatus represents current cluster config status.
type ClusterStatus int

const (
	ClusterStatusUnknown ClusterStatus = iota
	ClusterStatusInitialize
	ClusterStatusReady
)

// String returns the string value of StorageStatus.
func (s ClusterStatus) String() string {
	val := "Unknown"
	switch s {
	case ClusterStatusInitialize:
		val = "Initialize"
	case ClusterStatusReady:
		val = "Ready"
	}
	return val
}

// MarshalJSON encodes storage status.
func (s ClusterStatus) MarshalJSON() ([]byte, error) {
	val := s.String()
	return json.Marshal(&val)
}

// UnmarshalJSON decodes storage status.
func (s *ClusterStatus) UnmarshalJSON(value []byte) error {
	switch string(value) {
	case `"Initialize"`:
		*s = ClusterStatusInitialize
		return nil
	case `"Ready"`:
		*s = ClusterStatusReady
		return nil
	default:
		*s = ClusterStatusUnknown
		return nil
	}
}

// Storage represents storage config and state.
type Broker struct {
	config.BrokerCluster
	Status ClusterStatus `json:"status"`
}

// Brokers represents the broker list.
type Brokers []Broker

// ToTable returns broker list as table if it has value, else return empty string.
func (s Brokers) ToTable() (rows int, tableStr string) {
	if len(s) == 0 {
		return 0, ""
	}
	writer := models.NewTableFormatter()
	writer.AppendHeader(table.Row{"Namespace", "Status", "Configuration"})
	for i := range s {
		r := s[i]
		writer.AppendRow(table.Row{
			r.Config.Namespace,
			r.Status.String(),
			r.Config.String(),
		})
	}
	return len(s), writer.Render()
}

// Storages represents the storage list.
type Storages []Storage

// ToTable returns storage list as table if it has value, else return empty string.
func (s Storages) ToTable() (rows int, tableStr string) {
	if len(s) == 0 {
		return 0, ""
	}
	writer := models.NewTableFormatter()
	writer.AppendHeader(table.Row{"Namespace", "Status", "Configuration"})
	for i := range s {
		r := s[i]
		writer.AppendRow(table.Row{
			r.Config.Namespace,
			r.Status.String(),
			r.Config.String(),
		})
	}
	return len(s), writer.Render()
}

// Storage represents storage config and state.
type Storage struct {
	config.StorageCluster
	Status ClusterStatus `json:"status"`
}

// ReplicaState represents the relationship for a replica.
type ReplicaState struct {
	Database   string  `json:"database"`
	ShardID    ShardID `json:"shardId"`
	Leader     NodeID  `json:"leader"`
	Follower   NodeID  `json:"follower"`
	FamilyTime int64   `json:"familyTime"`
}

// String returns the string value of ReplicaState.
func (r ReplicaState) String() string {
	return "[" +
		"database:" + r.Database +
		",shard:" + strconv.Itoa(int(r.ShardID)) +
		",family:" + timeutil.FormatTimestamp(r.FamilyTime, timeutil.DataTimeFormat4) +
		",from(leader):" + strconv.Itoa(int(r.Leader)) +
		",to(follower):" + strconv.Itoa(int(r.Follower)) +
		"]"
}

// ShardState represents current state of shard.
type ShardState struct {
	Replica Replica        `json:"replica"`
	ID      ShardID        `json:"id"`
	State   ShardStateType `json:"state"`
	Leader  NodeID         `json:"leader"`
}

// FamilyState represents current state of shard's family.
type FamilyState struct {
	Database   string     `json:"database"`
	Shard      ShardState `json:"shard"`
	FamilyTime int64      `json:"familyTime"`
}

// BrokerState represents broker cluster state.
// NOTICE: it is not safe for concurrent use.
type BrokerState struct {
	LiveNodes map[string]StatelessNode `json:"liveNodes"`
	Name      string                   `json:"name"`
}

func NewBrokerState(name string) *BrokerState {
	return &BrokerState{
		Name:      name,
		LiveNodes: make(map[string]StatelessNode),
	}
}

// GetLiveNodes returns all live node list.
func (b *BrokerState) GetLiveNodes() (rs []StatelessNode) {
	for _, node := range b.LiveNodes {
		rs = append(rs, node)
	}
	return
}

// NodeOnline adds a live node into node list.
func (b *BrokerState) NodeOnline(nodeID string, node StatelessNode) {
	b.LiveNodes[nodeID] = node
}

// NodeOffline removes a offline node from live node list.
func (b *BrokerState) NodeOffline(nodeID string) {
	delete(b.LiveNodes, nodeID)
}

// StorageState represents storage cluster state.
// NOTICE: it is not safe for concurrent use.
// TODO: need concurrent safe????
type StorageState struct {
	LiveNodes map[NodeID]StatefulNode `json:"liveNodes"`

	// TODO: remove??
	ShardAssignments map[string]*ShardAssignment       `json:"shardAssignments"` // database's name => shard assignment
	ShardStates      map[string]map[ShardID]ShardState `json:"shardStates"`      // database's name => shard state
}

// NewStorageState creates storage cluster state
func NewStorageState() *StorageState {
	return &StorageState{
		LiveNodes:        make(map[NodeID]StatefulNode),
		ShardAssignments: make(map[string]*ShardAssignment),
		ShardStates:      make(map[string]map[ShardID]ShardState),
	}
}

// LeadersOnNode returns leaders on this node.
func (s *StorageState) LeadersOnNode(nodeID NodeID) map[string][]ShardID {
	result := make(map[string][]ShardID)
	for name, shards := range s.ShardStates {
		for shardID, shard := range shards {
			if shard.Leader == nodeID {
				result[name] = append(result[name], shardID)
			}
		}
	}
	return result
}

// ReplicasOnNode returns replicas on this node.
func (s *StorageState) ReplicasOnNode(nodeID NodeID) map[string][]ShardID {
	result := make(map[string][]ShardID)
	for name, shardAssignment := range s.ShardAssignments {
		shards := shardAssignment.Shards
		for shardID, replicas := range shards {
			if replicas.Contain(nodeID) {
				result[name] = append(result[name], shardID)
			}
		}
	}
	return result
}

// DropDatabase drops shard state/assignment by database's name.
func (s *StorageState) DropDatabase(name string) {
	delete(s.ShardStates, name)
	delete(s.ShardAssignments, name)
}

// NodeOnline adds a live node into node list.
func (s *StorageState) NodeOnline(node StatefulNode) {
	s.LiveNodes[node.ID] = node
}

// NodeOffline removes a offline node from live node list.
func (s *StorageState) NodeOffline(nodeID NodeID) {
	delete(s.LiveNodes, nodeID)
}

// Stringer returns a human readable string
func (s *StorageState) String() string {
	return string(encoding.JSONMarshal(s))
}

// StateMachineInfo represents state machine register info.
type StateMachineInfo struct {
	CreateState func() interface{} `json:"-"`
	Path        string             `json:"path"`
}

// StateMetric represents internal state metric.
type StateMetric struct {
	Tags   map[string]string `json:"tags"`
	Fields []StateField      `json:"fields"`
}

// StateField represents internal state value.
type StateField struct {
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Value float64 `json:"value"`
}

// DataFamilyState represents the state of data family.
type DataFamilyState struct {
	AckSequences     map[int32]int64       `json:"ackSequences"`
	ReplicaSequences map[int32]int64       `json:"replicaSequences"`
	FamilyTime       string                `json:"familyTime"`
	MemoryDatabases  []MemoryDatabaseState `json:"memoryDatabases"`
	ShardID          ShardID               `json:"shardId"`
}

// MemoryDatabaseState represents the state of memory database.
type MemoryDatabaseState struct {
	State       string        `json:"state"`
	Uptime      time.Duration `json:"uptime"`
	MemSize     int64         `json:"memSize"`
	NumOfSeries int           `json:"numOfSeries"`
}
