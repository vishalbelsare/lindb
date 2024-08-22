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

package root

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lindb/common/pkg/encoding"
	"github.com/lindb/common/pkg/logger"
	"github.com/stretchr/testify/assert"
	gomock "go.uber.org/mock/gomock"

	"github.com/lindb/lindb/config"
	"github.com/lindb/lindb/coordinator/discovery"
	"github.com/lindb/lindb/models"
	"github.com/lindb/lindb/pkg/state"
	"github.com/lindb/lindb/rpc"
)

func TestStateManager_Close(t *testing.T) {
	mgr := NewStateManager(context.TODO(), nil, nil)
	fct := &stateMachineFactory{}
	mgr.SetStateMachineFactory(fct)
	assert.Equal(t, fct, mgr.GetStateMachineFactory())

	mgr.Close()
}

func TestStateManager_Handle_Event_Panic(t *testing.T) {
	mgr := NewStateManager(context.TODO(), nil, nil)
	// case 1: panic
	mgr.EmitEvent(&discovery.Event{
		Type: discovery.NodeFailure,
		Key:  "/1.1.1.1:9000",
	})
	time.Sleep(100 * time.Millisecond)
	mgr.Close()
}

func TestStateManager_NotRunning(t *testing.T) {
	mgr := NewStateManager(context.TODO(), nil, nil)
	mgr1 := mgr.(*stateManager)
	mgr1.running.Store(false)
	// case 1: not running
	mgr.EmitEvent(&discovery.Event{
		Type: discovery.BrokerConfigDeletion,
		Key:  "/shard/assign/test",
	})
	time.Sleep(100 * time.Millisecond)
	mgr.Close()
}

func TestStateManager_BrokerCfg(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer func() {
		ctrl.Finish()
	}()
	mgr := NewStateManager(context.TODO(), nil, nil)
	mgr1 := mgr.(*stateManager)
	// case 1: unmarshal cfg err
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.BrokerConfigChanged,
		Key:   "/broker/test",
		Value: []byte("value"),
	})
	// case 2: broker name is empty
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.BrokerConfigChanged,
		Key:   "/broker/test",
		Value: encoding.JSONMarshal(&config.BrokerCluster{}),
	})
	// case 3: new broker cluster err
	mgr1.mutex.Lock()
	mgr1.newBrokerClusterFn = func(cfg *config.BrokerCluster,
		stateMgr StateManager, repoFactory state.RepositoryFactory,
	) (cluster BrokerCluster, err error) {
		return nil, fmt.Errorf("err")
	}
	mgr1.mutex.Unlock()
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.BrokerConfigChanged,
		Key:   "/broker/test",
		Value: encoding.JSONMarshal(&config.BrokerCluster{Config: &config.RepoState{Namespace: "/broker/test"}}),
	})
	time.Sleep(100 * time.Millisecond)
	// case 4: start broker err
	broker1 := NewMockBrokerCluster(ctrl)
	mgr1.mutex.Lock()
	mgr1.newBrokerClusterFn = func(cfg *config.BrokerCluster,
		stateMgr StateManager, repoFactory state.RepositoryFactory,
	) (cluster BrokerCluster, err error) {
		return broker1, nil
	}
	mgr1.mutex.Unlock()
	broker1.EXPECT().Start().Return(fmt.Errorf("err"))
	broker1.EXPECT().Close().AnyTimes()

	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.BrokerConfigChanged,
		Key:   "/broker/test",
		Value: encoding.JSONMarshal(&config.BrokerCluster{Config: &config.RepoState{Namespace: "/broker/test"}}),
	})
	time.Sleep(100 * time.Millisecond)

	// case 5: start broker ok
	broker1.EXPECT().Start().Return(nil)
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.BrokerConfigChanged,
		Key:   "/broker/test",
		Value: encoding.JSONMarshal(&config.BrokerCluster{Config: &config.RepoState{Namespace: "/broker/test"}}),
	})
	// case 6: remove not exist broker
	mgr.EmitEvent(&discovery.Event{
		Type: discovery.BrokerConfigDeletion,
		Key:  "/broker/test2",
	})
	time.Sleep(100 * time.Millisecond)
	broker1.EXPECT().GetState().Return(models.NewBrokerState("/broker/test")).MaxTimes(2)
	brokers := mgr.GetBrokerStates()
	assert.Len(t, brokers, 1)
	_, ok := mgr.GetBrokerState("test3")
	assert.False(t, ok)
	_, ok = mgr.GetBrokerState("/broker/test")
	assert.True(t, ok)
	// case 7: modify broker config
	broker1.EXPECT().Start().Return(nil)
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.BrokerConfigChanged,
		Key:   "/broker/test",
		Value: encoding.JSONMarshal(&config.BrokerCluster{Config: &config.RepoState{Namespace: "/broker/test"}}),
	})
	// case 8: remove broker
	mgr.EmitEvent(&discovery.Event{
		Type: discovery.BrokerConfigDeletion,
		Key:  "/broker/test",
	})
	// case 9: namespace is empty
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.BrokerConfigChanged,
		Key:   "/broker/test2",
		Value: encoding.JSONMarshal(&config.BrokerCluster{Config: &config.RepoState{}}),
	})
	time.Sleep(100 * time.Millisecond)
	brokers = mgr.GetBrokerStates()
	assert.Len(t, brokers, 0)

	mgr.Close()
}

func TestStateManager_BrokerNodeStartup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	connectionMgr := rpc.NewMockConnectionManager(ctrl)
	connectionMgr.EXPECT().CreateConnection(gomock.Any()).AnyTimes()
	broker := NewMockBrokerCluster(ctrl)
	broker.EXPECT().Close().AnyTimes()
	mgr := NewStateManager(context.TODO(), nil, connectionMgr)
	mgr1 := mgr.(*stateManager)
	mgr1.mutex.Lock()
	mgr1.brokers["test"] = broker
	mgr1.mutex.Unlock()
	// case 1: unmarshal err
	mgr.EmitEvent(&discovery.Event{
		Type:       discovery.NodeStartup,
		Key:        "/test/1",
		Value:      []byte("dd"),
		Attributes: map[string]string{brokerNameKey: "test"},
	})
	// case 2: broker node startup
	broker.EXPECT().GetState().Return(models.NewBrokerState("test"))
	mgr.EmitEvent(&discovery.Event{
		Type:       discovery.NodeStartup,
		Key:        "/test/1",
		Value:      []byte(`{"hostIp":"1.1.1.1"}`),
		Attributes: map[string]string{brokerNameKey: "test"},
	})
	time.Sleep(100 * time.Millisecond)

	broker.EXPECT().GetState().Return(&models.BrokerState{})
	assert.Len(t, mgr.GetBrokerStates(), 1)
	mgr.Close()
}

func TestStateManager_BrokerNodeFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	connectionMgr := rpc.NewMockConnectionManager(ctrl)
	broker := NewMockBrokerCluster(ctrl)
	mgr := NewStateManager(context.TODO(), nil, connectionMgr)
	mgr1 := mgr.(*stateManager)
	mgr1.mutex.Lock()
	mgr1.brokers["test"] = broker
	mgr1.mutex.Unlock()
	liveNodes := map[string]models.StatelessNode{"test_1": {HostIP: "1.1.1.1"}, "test_2": {HostIP: "2.2.2.2"}}
	broker.EXPECT().GetState().Return(&models.BrokerState{
		Name:      "test",
		LiveNodes: liveNodes,
	}).AnyTimes()
	connectionMgr.EXPECT().CloseConnection(gomock.Any())
	broker.EXPECT().Close()
	mgr.EmitEvent(&discovery.Event{
		Type:       discovery.NodeFailure,
		Key:        "/test/test_1",
		Attributes: map[string]string{brokerNameKey: "test"},
	})
	connectionMgr.EXPECT().CloseConnection(gomock.Any()).
		Do(func(node models.Node) {
			panic("err")
		})
	mgr.EmitEvent(&discovery.Event{
		Type:       discovery.NodeFailure,
		Key:        "/test/test_2",
		Attributes: map[string]string{brokerNameKey: "test"},
	})
	time.Sleep(300 * time.Millisecond)
	mgr1.mutex.Lock()
	assert.Len(t, liveNodes, 1)
	mgr1.mutex.Unlock()
	mgr.Close()
}

func TestStateManager_DatabaseCfg(t *testing.T) {
	mgr := NewStateManager(context.TODO(), nil, nil)

	// case 1: unmarshal cfg err
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.DatabaseConfigChanged,
		Key:   "/database/config/test",
		Value: []byte("value"),
	})
	// case 2: database cfg changed
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.DatabaseConfigChanged,
		Key:   "/database/config/test",
		Value: encoding.JSONMarshal(&models.LogicDatabase{Name: "test"}),
	})

	time.Sleep(300 * time.Millisecond)
	db, ok := mgr.GetDatabase("test")
	assert.True(t, ok)
	assert.Equal(t, "test", db.Name)

	// case 3: database delete
	mgr.EmitEvent(&discovery.Event{
		Type: discovery.DatabaseConfigDeletion,
		Key:  "/database/config/test",
	})
	time.Sleep(100 * time.Millisecond)

	db, ok = mgr.GetDatabase("test")
	assert.False(t, ok)
	assert.Empty(t, db.Name)

	mgr.Close()
}

func TestStateManager_Choose(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mgr := &stateManager{
		logger:    logger.GetLogger("Test", "StateManager"),
		databases: make(map[string]*models.LogicDatabase),
		brokers:   make(map[string]BrokerCluster),
	}

	plan, err := mgr.Choose("test", 1)
	assert.NoError(t, err)
	assert.Nil(t, plan)

	mgr.mutex.Lock()
	mgr.databases["test"] = &models.LogicDatabase{
		Routers: []models.Router{{Broker: "broker"}, {Broker: "broker", Database: "test"}},
	}
	mgr.mutex.Unlock()
	plan, err = mgr.Choose("test", 1)
	assert.NoError(t, err)
	assert.Len(t, plan, 0)

	mgr.mutex.Lock()
	broker := NewMockBrokerCluster(ctrl)
	mgr.brokers["broker"] = broker
	mgr.mutex.Unlock()
	broker.EXPECT().GetState().Return(&models.BrokerState{}).Times(2)
	plan, err = mgr.Choose("test", 1)
	assert.NoError(t, err)
	assert.Len(t, plan, 2)
}

func TestStateManager_Node(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mgr := NewStateManager(context.TODO(), nil, nil)
	// case 1: unmarshal node info err
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.NodeStartup,
		Key:   "/lives/1.1.1.1:9000",
		Value: []byte("221"),
	})
	// case 2: cache node
	mgr.EmitEvent(&discovery.Event{
		Type:  discovery.NodeStartup,
		Key:   "/lives/1.1.1.1:9000",
		Value: []byte(`{"HostIp":"1.1.1.1","GRPCPort":9000}`),
	})
	time.Sleep(time.Second) // wait
	nodes := mgr.GetLiveNodes()
	assert.Equal(t, []models.StatelessNode{{HostIP: "1.1.1.1", GRPCPort: 9000}}, nodes)

	// case 4: remove not exist node
	mgr.EmitEvent(&discovery.Event{
		Type: discovery.NodeFailure,
		Key:  "/lives/2.2.2.2:9000",
	})
	// case 5: remove node
	mgr.EmitEvent(&discovery.Event{
		Type: discovery.NodeFailure,
		Key:  "/lives/1.1.1.1:9000",
	})
	time.Sleep(time.Second) // wait
	nodes = mgr.GetLiveNodes()
	assert.Empty(t, nodes)

	mgr.Close()
}

func TestStateManager_GetLiveNodes(t *testing.T) {
	s := &stateManager{
		nodes: make(map[string]models.StatelessNode),
	}
	nodes := s.GetLiveNodes()
	assert.Empty(t, nodes)

	s.nodes["test1"] = models.StatelessNode{HostIP: "1.1.1.1"}
	s.nodes["test2"] = models.StatelessNode{HostIP: "1.1.2.1"}
	nodes = s.GetLiveNodes()
	assert.Equal(t, []models.StatelessNode{{HostIP: "1.1.1.1"}, {HostIP: "1.1.2.1"}}, nodes)
}

func TestStateManager_GetBrokerStates(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s := &stateManager{
		brokers: make(map[string]BrokerCluster),
	}
	nodes := s.GetBrokerStates()
	assert.Empty(t, nodes)
	broker := NewMockBrokerCluster(ctrl)
	broker.EXPECT().GetState().Return(&models.BrokerState{Name: "test2"})
	broker.EXPECT().GetState().Return(&models.BrokerState{Name: "test1"})
	s.brokers["test1"] = broker
	s.brokers["test2"] = broker
	nodes = s.GetBrokerStates()
	assert.Equal(t, []models.BrokerState{{Name: "test1"}, {Name: "test2"}}, nodes)
}

func TestStateManager_GetDatabases(t *testing.T) {
	s := &stateManager{
		databases: make(map[string]*models.LogicDatabase),
	}
	dbs := s.GetDatabases()
	assert.Empty(t, dbs)

	s.databases["test1"] = &models.LogicDatabase{Name: "test1"}
	s.databases["test2"] = &models.LogicDatabase{Name: "test2"}
	dbs = s.GetDatabases()
	assert.Equal(t, []models.LogicDatabase{{Name: "test1"}, {Name: "test2"}}, dbs)
}
