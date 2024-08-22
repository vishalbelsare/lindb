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
	"testing"

	commontimeutil "github.com/lindb/common/pkg/timeutil"
	"github.com/stretchr/testify/assert"

	"github.com/lindb/lindb/pkg/timeutil"
	"github.com/lindb/lindb/series/field"
)

func TestNewFieldAggregates(t *testing.T) {
	agg := NewFieldAggregates(
		timeutil.Interval(commontimeutil.OneSecond),
		1,
		timeutil.TimeRange{
			Start: 10,
			End:   20,
		},
		AggregatorSpecs{
			NewAggregatorSpec("b", field.SumField),
			NewAggregatorSpec("a", field.SumField),
		})
	assert.Equal(t, field.Name("b"), agg[0].FieldName())
	assert.Equal(t, field.Name("a"), agg[1].FieldName())
	assert.Equal(t, field.SumField, agg[0].GetFieldType())
	assert.Equal(t, field.SumField, agg[1].GetFieldType())

	agg = NewFieldAggregates(
		timeutil.Interval(commontimeutil.OneSecond),
		1,
		timeutil.TimeRange{
			Start: 10,
			End:   20,
		},
		AggregatorSpecs{
			NewAggregatorSpec("a", field.SumField),
			NewAggregatorSpec("b", field.SumField),
		})
	assert.Equal(t, field.Name("a"), agg[0].FieldName())
	assert.Equal(t, field.Name("b"), agg[1].FieldName())

	it := agg.ResultSet("")
	assert.True(t, it.HasNext())
	sIt := it.Next()
	assert.Equal(t, field.Name("a"), sIt.FieldName())
	assert.Equal(t, field.SumField, sIt.FieldType())
	assert.True(t, it.HasNext())
	sIt = it.Next()
	assert.Equal(t, field.Name("b"), sIt.FieldName())
	assert.Equal(t, field.SumField, sIt.FieldType())
	assert.False(t, it.HasNext())

	agg.Reset()
}

func TestNewSeriesAggregator(t *testing.T) {
	now, _ := commontimeutil.ParseTimestamp("20190702 19:10:00", "20060102 15:04:05")
	familyTime, _ := commontimeutil.ParseTimestamp("20190702 19:00:00", "20060102 15:04:05")
	agg := NewSeriesAggregator(
		timeutil.Interval(commontimeutil.OneSecond),
		1,
		timeutil.TimeRange{
			Start: now,
			End:   now + 3*commontimeutil.OneHour,
		},
		NewAggregatorSpec("b", field.SumField),
	)

	fAgg := agg.GetAggregator(familyTime)
	assert.NotNil(t, fAgg)

	fAgg = agg.GetAggregator(familyTime + 3*commontimeutil.OneHour)
	assert.NotNil(t, fAgg)

	rs := agg.ResultSet()
	assert.Equal(t, field.Name("b"), rs.FieldName())
	assert.True(t, rs.HasNext())
	startTime, fIt := rs.Next()
	assert.Equal(t, now, startTime)
	assert.NotNil(t, fIt)
	assert.True(t, rs.HasNext())
	startTime, fIt = rs.Next()
	assert.Equal(t, now, startTime)
	assert.NotNil(t, fIt)
	assert.False(t, rs.HasNext())
	rs = agg.ResultSet()
	d, err := rs.MarshalBinary()
	assert.NoError(t, err)
	assert.True(t, len(d) > 0)

	agg.Reset()
}

func TestNewMergeSeriesAggregator(t *testing.T) {
	now, _ := commontimeutil.ParseTimestamp("20190702 19:10:00", "20060102 15:04:05")
	familyTime, _ := commontimeutil.ParseTimestamp("20190702 19:00:00", "20060102 15:04:05")
	agg := NewMergeSeriesAggregator(
		timeutil.Interval(commontimeutil.OneSecond),
		1,
		timeutil.TimeRange{
			Start: now,
			End:   now + 3*commontimeutil.OneHour,
		},
		NewAggregatorSpec("b", field.SumField),
	)

	fAgg := agg.getAggregator(familyTime)
	assert.NotNil(t, fAgg)

	rs := agg.ResultSet()
	assert.Equal(t, field.Name("b"), rs.FieldName())
	assert.True(t, rs.HasNext())
	startTime, fIt := rs.Next()
	assert.Equal(t, now, startTime)
	assert.NotNil(t, fIt)
	assert.False(t, rs.HasNext())
}
