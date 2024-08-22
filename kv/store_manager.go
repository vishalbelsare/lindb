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

package kv

import (
	"sync"

	"github.com/lindb/common/pkg/logger"
)

//go:generate mockgen -source ./store_manager.go -destination=./store_manager_mock.go -package kv

var (
	sManager          StoreManager
	once4StoreManager sync.Once

	lock sync.Mutex // just for test
)

// InitStoreManager initializes StoreManager.
func InitStoreManager(storeMgr StoreManager) {
	lock.Lock()
	defer lock.Unlock()

	sManager = storeMgr
}

// GetStoreManager returns the kv store manager singleton instance.
func GetStoreManager() StoreManager {
	if sManager != nil {
		return sManager
	}
	once4StoreManager.Do(func() {
		sManager = newStoreManager()
	})
	return sManager
}

// StoreManager represents a global store manager.
type StoreManager interface {
	// CreateStore creates the store by name/option.
	// NOTE: name need include relatively path based on root path.
	CreateStore(name string, option StoreOption) (Store, error)
	// GetStoreByName returns Store by given name.
	GetStoreByName(name string) (Store, bool)
	// GetStores returns all Store under manager cache.
	GetStores() []Store
	// CloseStore closes the Store, then remove from manager cache.
	CloseStore(name string) error
}

// storeManager implements StoreManager interface.
type storeManager struct {
	stores map[string]Store

	mutex sync.Mutex
}

// newStoreManager creates a StoreManager instance.
func newStoreManager() StoreManager {
	return &storeManager{
		stores: make(map[string]Store),
	}
}

// CreateStore creates the store by name/option.
// NOTE: name need include relatively path based on root path.
func (s *storeManager) CreateStore(name string, option StoreOption) (Store, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	kvLogger.Info("create new kv store", logger.String("kv", name))
	if store, ok := s.stores[name]; ok {
		return store, nil
	}

	// FIXME: remove one name
	store, err := newStoreFunc(name, name, option)
	if err != nil {
		return nil, err
	}
	s.stores[name] = store
	return store, nil
}

// GetStoreByName returns Store by given name.
func (s *storeManager) GetStoreByName(name string) (store Store, ok bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	store, ok = s.stores[name]
	return
}

// GetStores returns all Store under manager cache.
func (s *storeManager) GetStores() (rs []Store) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, store := range s.stores {
		rs = append(rs, store)
	}
	return
}

// CloseStore closes the Store, then remove from manager cache.
func (s *storeManager) CloseStore(name string) error {
	store, ok := s.GetStoreByName(name)
	if !ok {
		return nil
	}
	kvLogger.Info("close kv store", logger.String("kv", name))

	s.mutex.Lock()
	defer s.mutex.Unlock()
	// remove store from cache
	delete(s.stores, name)
	if err := store.close(); err != nil {
		return err
	}
	return nil
}
