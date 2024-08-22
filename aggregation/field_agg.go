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

package aggregation

import (
	"math"
	"sort"

	"github.com/lindb/lindb/pkg/collections"
	"github.com/lindb/lindb/series"
	"github.com/lindb/lindb/series/field"
)

//go:generate mockgen -source=./field_agg.go -destination=./field_agg_mock.go -package=aggregation

// FieldAggregator represents a field aggregator, aggregator the field series which with same field id.
type FieldAggregator interface {
	// Aggregate aggregates the field series into current aggregator.
	Aggregate(it series.FieldIterator)
	// AggregateBySlot aggregates the field series into current aggregator.
	AggregateBySlot(slot int, value float64)
	// ResultSet returns the result set of field aggregator.
	ResultSet() (startTime int64, it series.FieldIterator)
	// reset aggregator context for reusing.
	reset()
}

// fieldAggregator implements field aggregator interface, aggregator field series based on aggregator spec.
type fieldAggregator struct {
	aggTypes         []field.AggType
	segmentStartTime int64
	start, end       int // slot range based on query interval and time range

	fieldSeriesList []*collections.FloatArray
}

// NewFieldAggregator creates a field aggregator,
// time range 's start and end is index based on segment start time and interval.
// e.g. segment start time = 20190905 10:00:00, start = 10, end = 50, interval = 10 seconds,
// real query time range {20190905 10:01:40 ~ 20190905 10:08:20}
func NewFieldAggregator(aggSpec AggregatorSpec, segmentStartTime int64, start, end int) FieldAggregator {
	var aggTypes []field.AggType
	for f := range aggSpec.Functions() {
		aggTypes = append(aggTypes, aggSpec.GetFieldType().GetFuncFieldParams(f)...)
	}

	aggTypes = uniqueAggTypes(aggTypes)

	agg := &fieldAggregator{
		aggTypes:         aggTypes,
		segmentStartTime: segmentStartTime,
		start:            start,
		end:              end,
		fieldSeriesList:  make([]*collections.FloatArray, len(aggTypes)),
	}
	return agg
}

// ResultSet returns the result set of field aggregator
func (a *fieldAggregator) ResultSet() (startTime int64, it series.FieldIterator) {
	return a.segmentStartTime, newFieldIterator(a.start, a.aggTypes, a.fieldSeriesList)
}

// Aggregate aggregates the field series into current aggregator
func (a *fieldAggregator) Aggregate(it series.FieldIterator) {
	for it.HasNext() {
		pIt := it.Next()
		for pIt.HasNext() {
			slot, value := pIt.Next()
			a.AggregateBySlot(slot, value)
		}
	}
}

// AggregateBySlot aggregates the field series into current aggregator
func (a *fieldAggregator) AggregateBySlot(slot int, value float64) {
	// drop inf value
	if math.IsInf(value, 1) {
		return
	}
	pos := slot - a.start
	for idx, aggType := range a.aggTypes {
		values := a.fieldSeriesList[idx]
		if values == nil {
			values = collections.NewFloatArray(a.end - a.start + 1)
			values.SetValue(pos, value)
			a.fieldSeriesList[idx] = values
		} else {
			// slot too large for last family
			if values.HasValue(pos) {
				values.SetValue(pos, aggType.Aggregate(values.GetValue(pos), value))
			} else {
				values.SetValue(pos, value)
			}
		}
	}
}

// reset aggregator context for reusing.
func (a *fieldAggregator) reset() {
	for idx := range a.fieldSeriesList {
		if a.fieldSeriesList[idx] == nil {
			continue
		}
		a.fieldSeriesList[idx].Reset()
	}
}

// uniqueAggTypes removes duplicate elements from types
func uniqueAggTypes(types []field.AggType) []field.AggType {
	if len(types) <= 1 {
		return types
	}

	sort.Slice(types, func(i, j int) bool {
		return types[i] < types[j]
	})

	var index = 0
	for i := 1; i < len(types); i++ {
		if types[i] != types[index] {
			index++
			types[index] = types[i]
		}
	}

	return types[:index+1]
}
