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

package page

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	commonfileutil "github.com/lindb/common/pkg/fileutil"
	"github.com/lindb/common/pkg/logger"
	"go.uber.org/atomic"
)

//go:generate mockgen -source ./factory.go -destination ./factory_mock.go -package page

// for testing
var (
	mkDirFunc      = commonfileutil.MkDirIfNotExist
	removeFileFunc = commonfileutil.RemoveFile
	listDirFunc    = commonfileutil.ListDir
)

var pageLogger = logger.GetLogger("Queue", "PageFactory")

var errFactoryClosed = errors.New("page factory is closed")

// pageSuffix represents the page file suffix
const pageSuffix = "bat"

// Factory represents mapped page manage factory
type Factory interface {
	io.Closer
	// AcquirePage acquires a mapped page with specific index from the factory
	AcquirePage(index int64) (MappedPage, error)
	// GetPage returns a mapped page with specific index
	GetPage(index int64) (MappedPage, bool)
	// TruncatePages truncates expired page by index(page id).
	TruncatePages(index int64)
	// Size returns the total page size
	Size() int64
}

// factory implements Factory interface
type factory struct {
	logger   logger.Logger
	pages    map[int64]MappedPage
	path     string
	pageSize int
	closed   atomic.Bool
	size     atomic.Int64
	mutex    sync.RWMutex
}

// NewFactory creates page factory based on page size
func NewFactory(path string, pageSize int) (fct Factory, err error) {
	if err := mkDirFunc(path); err != nil {
		return nil, err
	}

	f := &factory{
		path:     path,
		pageSize: pageSize,
		pages:    make(map[int64]MappedPage),
		logger:   logger.GetLogger("Queue", "Page"),
	}

	if err := f.loadPages(); err != nil {
		// if create factory failure, need release the file resources
		_ = f.Close()
		return nil, err
	}

	return f, nil
}

// AcquirePage acquires a mapped page with specific index from the factory
func (f *factory) AcquirePage(index int64) (MappedPage, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.closed.Load() {
		return nil, errFactoryClosed
	}

	page, ok := f.pages[index]
	if ok {
		return page, nil
	}

	page, err := NewMappedPage(f.pageFileName(index), f.pageSize)
	if err != nil {
		return nil, err
	}

	f.pages[index] = page
	f.size.Add(int64(f.pageSize))

	return page, nil
}

// GetPage returns a mapped page with specific index
func (f *factory) GetPage(index int64) (MappedPage, bool) {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	page, ok := f.pages[index]
	return page, ok
}

// TruncatePages truncates expired page by index(page id).
func (f *factory) TruncatePages(index int64) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.closed.Load() {
		return
	}

	for pageID := range f.pages {
		if pageID < index {
			if page, ok := f.pages[pageID]; ok {
				if err := page.Close(); err != nil {
					f.logger.Warn("close page failure",
						logger.String("path", f.path), logger.Any("page", pageID), logger.Error(err))
				}
				if err := removeFileFunc(f.pageFileName(pageID)); err != nil {
					f.logger.Warn("remove page failure",
						logger.String("path", f.path), logger.Any("page", pageID), logger.Error(err))
					continue
				}
				delete(f.pages, pageID)
				f.size.Sub(int64(f.pageSize))

				f.logger.Info("remove page successfully",
					logger.String("path", f.path), logger.Any("page", pageID))
			}
		}
	}
}

// Size returns the total page size
func (f *factory) Size() int64 {
	return f.size.Load()
}

// Close closes all acquire mapped pages
func (f *factory) Close() error {
	if f.closed.CompareAndSwap(false, true) {
		f.mutex.Lock()
		defer f.mutex.Unlock()

		for _, page := range f.pages {
			if err := page.Close(); err != nil {
				pageLogger.Error("close mapped page data err",
					logger.String("path", f.path), logger.Error(err))
			}
		}
	}
	return nil
}

// pageFileName returns the mapped file name
func (f *factory) pageFileName(index int64) string {
	return filepath.Join(f.path, fmt.Sprintf("%d.%s", index, pageSuffix))
}

// loadPages loads exist pages when factory init
func (f *factory) loadPages() error {
	fileNames, err := listDirFunc(f.path)
	if err != nil {
		return err
	}
	if len(fileNames) == 0 {
		// page file not exist
		return nil
	}

	for _, fn := range fileNames {
		seqNumStr := fn[0 : strings.Index(fn, pageSuffix)-1]
		seq, err := strconv.ParseInt(seqNumStr, 10, 64)
		if err != nil {
			return err
		}
		_, err = f.AcquirePage(seq)
		if err != nil {
			return err
		}
	}

	return nil
}
