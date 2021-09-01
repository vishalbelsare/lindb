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

package metricsdata

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/lindb/roaring"

	"github.com/lindb/lindb/flow"
	"github.com/lindb/lindb/pkg/encoding"
	"github.com/lindb/lindb/pkg/stream"
	"github.com/lindb/lindb/pkg/timeutil"
	"github.com/lindb/lindb/series/field"
)

//go:generate mockgen -source ./reader.go -destination=./reader_mock.go -package metricsdata

const (
	dataFooterSize = 2 + // start time slot
		2 + // end time slot
		4 + // field metas position
		4 + // series ids position
		4 + // high offsets position
		4 // crc32 checksum
)

// MetricReader represents the metric block metricReader
type MetricReader interface {
	// Path returns file path
	Path() string
	// GetSeriesIDs returns the series ids in this sst file
	GetSeriesIDs() *roaring.Bitmap
	// GetFields returns the field metas in this sst file
	GetFields() field.Metas
	// GetTimeRange returns the time range in this sst file
	GetTimeRange() timeutil.SlotRange
	// Load loads the data from sst file, then returns the file metric scanner.
	Load(highKey uint16, seriesID roaring.Container, fields field.Metas) flow.DataLoader
	// readSeriesData reads series data from file by seriesEntryBlock
	readSeriesData(seriesEntryBlock []byte) [][]byte
}

// metricReader implements MetricReader interface that reads metric block
type metricReader struct {
	path           string
	metricBlock    []byte
	seriesBucket   []byte
	highKeyOffsets *encoding.FixedOffsetDecoder
	seriesIDs      *roaring.Bitmap
	fields         field.Metas
	crc32CheckSum  uint32
	timeRange      timeutil.SlotRange

	readFieldIndexes []int // read field indexes be used when query metric data
}

// NewReader creates a metric block metricReader
func NewReader(path string, metricBlock []byte) (MetricReader, error) {
	r := &metricReader{
		path:        path,
		metricBlock: metricBlock,
	}
	if err := r.initReader(); err != nil {
		return nil, err
	}
	return r, nil
}

// Path returns the file path
func (r *metricReader) Path() string {
	return r.path
}

// GetSeriesIDs returns the series ids in this sst file
func (r *metricReader) GetSeriesIDs() *roaring.Bitmap {
	return r.seriesIDs
}

// GetFields returns the field metas in this sst file
func (r *metricReader) GetFields() field.Metas {
	return r.fields
}

// GetTimeRange returns the time range in this sst file
func (r *metricReader) GetTimeRange() timeutil.SlotRange {
	return r.timeRange
}

// prepare prepares the field aggregator based on query condition
func (r *metricReader) prepare(fields field.Metas) (found bool) {
	fieldMap := make(map[field.ID]int)
	for idx, fieldMeta := range r.fields {
		fieldMap[fieldMeta.ID] = idx
	}
	r.readFieldIndexes = make([]int, len(fields))
	for idx, f := range fields { // sort by field ids
		fieldIdx, ok := fieldMap[f.ID]
		if !ok {
			r.readFieldIndexes[idx] = -1
		} else {
			r.readFieldIndexes[idx] = fieldIdx
			found = true
		}
	}
	return
}

// Load loads the data from sst file, then returns the file metric scanner.
func (r *metricReader) Load(highKey uint16, seriesID roaring.Container, fields field.Metas) flow.DataLoader {
	// 1. get high container index by the high key of series ID
	highContainerIdx := r.seriesIDs.GetContainerIndex(highKey)
	if highContainerIdx < 0 {
		// if high container index < 0(series IDs not exist) return it
		return nil
	}
	// 2. get low container include all low keys by the high container index, delete op will clean empty low container
	lowContainer := r.seriesIDs.GetContainerAtIndex(highContainerIdx)
	foundSeriesIDs := lowContainer.And(seriesID)
	if foundSeriesIDs.GetCardinality() == 0 {
		return nil
	}
	level3Block, err := r.highKeyOffsets.GetBlock(highContainerIdx, r.seriesBucket)
	if err != nil {
		return nil
	}
	// shorter than footer
	if len(level3Block) <= 4 {
		return nil
	}
	// out of range
	lowKeyOffsetsAt := binary.LittleEndian.Uint32(level3Block[len(level3Block)-4:])
	if lowKeyOffsetsAt+4 >= uint32(len(level3Block)) {
		return nil
	}

	lowKeyOffsetsDecoder := encoding.NewFixedOffsetDecoder()
	if _, err = lowKeyOffsetsDecoder.Unmarshal(level3Block[lowKeyOffsetsAt:]); err != nil {
		return nil
	}

	if !r.prepare(fields) {
		// field not found
		return nil
	}
	seriesEntriesBlock := level3Block[:lowKeyOffsetsAt]
	// must use lowContainer from store, because get series index based on container
	return newMetricLoader(r, seriesEntriesBlock, lowContainer, lowKeyOffsetsDecoder)
}

// readSeriesData reads series data from file by given position.
func (r *metricReader) readSeriesData(seriesEntryBlock []byte) [][]byte {
	fieldCount := r.fields.Len()
	if fieldCount == 1 {
		// metric has one field, just read the data
		return [][]byte{seriesEntryBlock}
	}

	// seriesEntry length too short or out of range
	fieldOffsetsBlockLen, uVariantEncodingLen := stream.UvarintLittleEndian(seriesEntryBlock)
	fieldOffsetsAt := len(seriesEntryBlock) - int(fieldOffsetsBlockLen) - uVariantEncodingLen
	if uVariantEncodingLen <= 0 || fieldOffsetsAt <= 0 || fieldOffsetsAt >= len(seriesEntryBlock) {
		return nil
	}

	// read data for multi-fields
	fieldOffsetsDecoder := encoding.NewFixedOffsetDecoder()
	_, _ = fieldOffsetsDecoder.Unmarshal(seriesEntryBlock[fieldOffsetsAt:])

	rs := make([][]byte, len(r.readFieldIndexes))
	for i, idx := range r.readFieldIndexes {
		if idx == -1 {
			continue
		}
		fieldBlock, err := fieldOffsetsDecoder.GetBlock(idx, seriesEntryBlock[:fieldOffsetsAt])
		if err == nil {
			// read field data
			rs[i] = fieldBlock
		}
	}
	return rs
}

// initReader initializes the metricReader context includes tag value ids/high offsets
func (r *metricReader) initReader() error {
	if len(r.metricBlock) <= dataFooterSize {
		return fmt.Errorf("metric block's length too small: %d <= %d", len(r.metricBlock), dataFooterSize)
	}
	// read footer(2+2+4+4+4+4)
	footerPos := len(r.metricBlock) - dataFooterSize
	r.timeRange.Start = stream.ReadUint16(r.metricBlock, footerPos)
	r.timeRange.End = stream.ReadUint16(r.metricBlock, footerPos+2)

	fieldMetaStartPos := int(stream.ReadUint32(r.metricBlock, footerPos+4))
	seriesIDsStartPos := int(stream.ReadUint32(r.metricBlock, footerPos+8))
	highKeyOffsetsPos := int(stream.ReadUint32(r.metricBlock, footerPos+12))
	r.crc32CheckSum = stream.ReadUint32(r.metricBlock, footerPos+16)
	// validate offsets
	if !sort.IntsAreSorted([]int{
		0, fieldMetaStartPos, fieldMetaStartPos + 2, seriesIDsStartPos, highKeyOffsetsPos, footerPos,
	}) {
		return fmt.Errorf("invalid footer format")
	}

	// read field metas
	fieldCount := r.metricBlock[fieldMetaStartPos]
	cursor := fieldMetaStartPos + 1
	r.fields = make(field.Metas, fieldCount)
	for i := uint8(0); i < fieldCount; i++ {
		if cursor+1 >= seriesIDsStartPos {
			return fmt.Errorf("corruted field metas, field count: %d", fieldCount)
		}
		r.fields[i] = field.Meta{
			ID:   field.ID(r.metricBlock[cursor]),
			Type: field.Type(r.metricBlock[cursor+1]),
		}
		cursor += 2
	}
	if fieldCount == 0 {
		return fmt.Errorf("field count is zero")
	}
	// read series ids
	seriesIDs := roaring.New()
	if err := encoding.BitmapUnmarshal(seriesIDs, r.metricBlock[seriesIDsStartPos:]); err != nil {
		return err
	}
	r.seriesBucket = r.metricBlock[:fieldMetaStartPos]
	r.seriesIDs = seriesIDs
	// read high offsets
	r.highKeyOffsets = encoding.NewFixedOffsetDecoder()
	_, err := r.highKeyOffsets.Unmarshal(r.metricBlock[highKeyOffsetsPos:])
	return err
}

// fieldIndexes returns field indexes of metric level
func (r *metricReader) fieldIndexes() map[field.ID]int {
	result := make(map[field.ID]int)
	for idx, f := range r.fields {
		result[f.ID] = idx
	}
	return result
}

// dataScanner represents the metric data scanner which scans the series data when merge operation
type dataScanner struct {
	reader        *metricReader
	container     roaring.Container
	lowKeyOffsets *encoding.FixedOffsetDecoder
	seriesEntries []byte

	highKeys         []uint16
	highKey          uint16
	highContainerIdx int
}

// newDataScanner creates a data scanner for data merge
func newDataScanner(r MetricReader) (*dataScanner, error) {
	reader := r.(*metricReader)
	s := &dataScanner{
		reader:        reader,
		highKeys:      reader.seriesIDs.GetHighKeys(),
		lowKeyOffsets: encoding.NewFixedOffsetDecoder(),
	}
	if len(s.highKeys) == 0 {
		return nil, fmt.Errorf("seriesID bitmap are empty")
	}
	return s, s.nextContainer()
}

// fieldIndexes returns field indexes of metric level
func (s *dataScanner) fieldIndexes() map[field.ID]int {
	return s.reader.fieldIndexes()
}

// nextContainer goes next container context for scanner
func (s *dataScanner) nextContainer() error {
	s.highKey = s.highKeys[s.highContainerIdx]
	s.container = s.reader.seriesIDs.GetContainerAtIndex(s.highContainerIdx)
	level3Block, err := s.reader.highKeyOffsets.GetBlock(s.highContainerIdx, s.reader.seriesBucket)
	if err != nil {
		return err
	}
	if len(level3Block) <= 4 {
		return fmt.Errorf("series entries length too short: %d", len(level3Block))
	}
	lowKeyOffsetsAt := binary.LittleEndian.Uint32(level3Block[len(level3Block)-4:])
	if lowKeyOffsetsAt+4 >= uint32(len(level3Block)) {
		return fmt.Errorf("lowKeyOffsetsAt: %d is out or range: %d-4", lowKeyOffsetsAt, len(level3Block))
	}
	if _, err := s.lowKeyOffsets.Unmarshal(level3Block[lowKeyOffsetsAt:]); err != nil {
		return err
	}
	s.seriesEntries = level3Block[:lowKeyOffsetsAt]
	s.highContainerIdx++
	return nil
}

// slotRange returns the slot range of metric level in current sst file
func (s *dataScanner) slotRange() timeutil.SlotRange {
	return s.reader.GetTimeRange()
}

// scan scans the data and returns the seriesEntry if series id exist,
// else returns nil
func (s *dataScanner) scan(highKey, lowSeriesID uint16) []byte {
	if s.highKey < highKey {
		if s.highContainerIdx >= len(s.highKeys) {
			// current tag inverted no data can read
			return nil
		}
		if err := s.nextContainer(); err != nil {
			return nil
		}
	}
	if highKey != s.highKey {
		// high key not match, return it
		return nil
	}
	// find data by low series id
	if s.container.Contains(lowSeriesID) {
		// get the index of low series id in container
		idx := s.container.Rank(lowSeriesID)
		// get series data data position
		seriesEntry, _ := s.lowKeyOffsets.GetBlock(idx-1, s.seriesEntries)
		return seriesEntry
	}
	return nil
}
