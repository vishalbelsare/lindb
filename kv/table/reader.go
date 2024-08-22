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
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/lindb/common/pkg/logger"
	"github.com/lindb/roaring"

	"github.com/lindb/lindb/metrics"
	"github.com/lindb/lindb/pkg/encoding"
	"github.com/lindb/lindb/pkg/fileutil"
)

//go:generate mockgen -source ./reader.go -destination=./reader_mock.go -package table

// for testing
var (
	ErrKeyNotExist           = errors.New("key not exist in kv table")
	openFileFn               = os.Open
	mapFunc                  = fileutil.Map
	unmapFunc                = fileutil.Unmap
	unmarshalFixedOffsetFunc = unmarshalFixedOffset
	uint64Func               = binary.LittleEndian.Uint64
	intsAreSortedFunc        = sort.IntsAreSorted
)

// Reader represents reader which reads k/v pair from store file.
type Reader interface {
	// Path returns the file path.
	Path() string
	// FileName returns the file name of reader.
	FileName() string
	// Get returns value for giving key,
	// if key not exist, return nil, ErrKeyNotExist.
	Get(key uint32) ([]byte, error)
	// Iterator iterates over a store's key/value pairs in key order.
	Iterator() Iterator
	// Close closes reader, release related resources.
	Close() error
}

// storeMMapReader represents mmap store file reader.
type storeMMapReader struct {
	f            *os.File
	keys         *roaring.Bitmap
	offsets      *encoding.FixedOffsetDecoder
	path         string
	fileName     string
	fullBlock    []byte
	entriesBlock []byte
}

// newMMapStoreReader creates mmap store file reader.
func newMMapStoreReader(path, fileName string) (r Reader, err error) {
	f, err := openFileFn(path)
	if err != nil {
		return nil, err
	}
	data, err := mapFunc(f)
	defer func() {
		if err != nil && len(data) > 0 {
			defer func() {
				_ = f.Close()
			}()
			// if init err and map data exist, need unmap it
			if e := unmapFunc(f, data); e != nil {
				metrics.TableReadStatistics.UnMMapFailures.Incr()
				tableLogger.Warn("unmap error when new store reader fail",
					logger.String("path", path), logger.Error(err))
			} else {
				metrics.TableReadStatistics.UnMMaps.Incr()
			}
		}
	}()
	if err != nil {
		metrics.TableReadStatistics.MMapFailures.Incr()
		return nil, err
	}
	metrics.TableReadStatistics.MMaps.Incr()

	if len(data) < sstFileFooterSize {
		err = fmt.Errorf("length of sstfile:%s length is too short", path)
		return nil, err
	}
	reader := &storeMMapReader{
		path:      path,
		fileName:  fileName,
		f:         f,
		fullBlock: data,
		keys:      roaring.New(),
	}

	if err := reader.initialize(); err != nil {
		return nil, err
	}

	return reader, nil
}

// initialize store reader, reads index block(keys,offset etc.), then caches it.
func (r *storeMMapReader) initialize() error {
	// decode footer
	footerStart := len(r.fullBlock) - sstFileFooterSize
	// validate magic-number
	if uint64Func(r.fullBlock[footerStart+magicNumberAtFooter:]) != magicNumberOffsetFile {
		return fmt.Errorf("verify magic-number of sstfile:%s failure", r.path)
	}
	posOfOffset := int(binary.LittleEndian.Uint32(r.fullBlock[footerStart : footerStart+4]))
	posOfKeys := int(binary.LittleEndian.Uint32(r.fullBlock[footerStart+4 : footerStart+8]))
	if !intsAreSortedFunc([]int{
		0, posOfOffset, posOfKeys, footerStart,
	}) {
		return fmt.Errorf("bad footer data, posOfOffsets: %d posOfKeys: %d,"+
			" footerStart: %d", posOfOffset, posOfKeys, footerStart)
	}
	// decode offsets
	offsetsBlock := r.fullBlock[posOfOffset:posOfKeys]
	r.offsets = encoding.NewFixedOffsetDecoder()
	if err := unmarshalFixedOffsetFunc(r.offsets, offsetsBlock); err != nil {
		return fmt.Errorf("unmarshal fixed-offsets decoder with error: %s", err)
	}
	// decode keys
	if _, err := encoding.BitmapUnmarshal(r.keys, r.fullBlock[posOfKeys:]); err != nil {
		return fmt.Errorf("unmarshal keys data from file[%s] error:%s", r.path, err)
	}
	// validate keys and offsets
	if r.offsets.Size() != int(r.keys.GetCardinality()) {
		return fmt.Errorf("num. of keys != num. of offsets in file[%s]", r.path)
	}
	// read entries block
	r.entriesBlock = r.fullBlock[:posOfOffset]
	return nil
}

func unmarshalFixedOffset(decoder *encoding.FixedOffsetDecoder, data []byte) error {
	_, err := decoder.Unmarshal(data)
	return err
}

// Path returns the file path.
func (r *storeMMapReader) Path() string {
	return r.path
}

// FileName returns the file name of reader.
func (r *storeMMapReader) FileName() string {
	return r.fileName
}

// Get return value for key, if not exist return nil, false.
func (r *storeMMapReader) Get(key uint32) ([]byte, error) {
	if !r.keys.Contains(key) {
		return nil, ErrKeyNotExist
	}
	// bitmap data's index from 1, so idx= get index - 1
	idx := r.keys.Rank(key)
	return r.getBlock(int(idx) - 1)
}

func (r *storeMMapReader) getBlock(idx int) ([]byte, error) {
	block, err := r.offsets.GetBlock(idx, r.entriesBlock)
	if err == nil {
		metrics.TableReadStatistics.Gets.Incr()
		metrics.TableReadStatistics.ReadBytes.Add(float64(len(block)))
	} else {
		metrics.TableReadStatistics.GetFailures.Get()
	}
	return block, err
}

// Iterator iterates over a store's key/value pairs in key order.
func (r *storeMMapReader) Iterator() Iterator {
	return newMMapIterator(r)
}

// Close store reader, release resource
func (r *storeMMapReader) Close() error {
	defer func() {
		_ = r.f.Close()
	}()
	r.entriesBlock = nil
	err := unmapFunc(r.f, r.fullBlock)
	if err != nil {
		metrics.TableReadStatistics.UnMMapFailures.Incr()
	} else {
		metrics.TableReadStatistics.UnMMaps.Incr()
	}
	return err
}

// storeMMapIterator iterates k/v pair using mmap store reader
type storeMMapIterator struct {
	reader *storeMMapReader
	keyIt  roaring.IntIterable

	idx int
}

// newMMapIterator creates store iterator using mmap store reader
func newMMapIterator(reader *storeMMapReader) Iterator {
	return &storeMMapIterator{
		reader: reader,
		keyIt:  reader.keys.Iterator(),
	}
}

// HasNext returns if the iteration has more element.
// It returns false if the iterator is exhausted.
func (it *storeMMapIterator) HasNext() bool {
	return it.keyIt.HasNext()
}

// Key returns the key of the current key/value pair
func (it *storeMMapIterator) Key() uint32 {
	key := it.keyIt.Next()
	return key
}

// Value returns the value of the current key/value pair
func (it *storeMMapIterator) Value() []byte {
	block, _ := it.reader.getBlock(it.idx)
	it.idx++
	return block
}
