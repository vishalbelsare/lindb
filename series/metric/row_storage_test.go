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

package metric

import (
	"math"
	"sort"
	"strconv"
	"testing"
	"time"

	xxhash "github.com/cespare/xxhash/v2"
	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/lindb/common/proto/gen/v1/flatMetricsV1"
	"github.com/stretchr/testify/assert"

	"github.com/lindb/lindb/series/field"
)

const (
	testCounter = "counter"
)

func buildFlatMetric(builder *flatbuffers.Builder, ns bool) {
	builder.Reset()

	var (
		keys       [10]flatbuffers.UOffsetT
		values     [10]flatbuffers.UOffsetT
		fieldNames [10]flatbuffers.UOffsetT
		kvs        [10]flatbuffers.UOffsetT
		fields     [10]flatbuffers.UOffsetT
	)
	for i := 0; i < 10; i++ {
		keys[i] = builder.CreateString("key" + strconv.Itoa(i))
		values[i] = builder.CreateString("value" + strconv.Itoa(i))
		fieldNames[i] = builder.CreateString(testCounter + strconv.Itoa(i))
	}
	for i := 9; i >= 0; i-- {
		flatMetricsV1.KeyValueStart(builder)
		flatMetricsV1.KeyValueAddKey(builder, keys[i])
		flatMetricsV1.KeyValueAddValue(builder, values[i])
		kvs[i] = flatMetricsV1.KeyValueEnd(builder)
	}

	// serialize field names
	for i := 0; i < 10; i++ {
		flatMetricsV1.SimpleFieldStart(builder)
		flatMetricsV1.SimpleFieldAddName(builder, fieldNames[i])
		switch i {
		case 0:
			flatMetricsV1.SimpleFieldAddType(builder, flatMetricsV1.SimpleFieldTypeLast)
		case 1:
			flatMetricsV1.SimpleFieldAddType(builder, flatMetricsV1.SimpleFieldTypeFirst)
		case 2:
			flatMetricsV1.SimpleFieldAddType(builder, flatMetricsV1.SimpleFieldTypeMin)
		case 3:
			flatMetricsV1.SimpleFieldAddType(builder, flatMetricsV1.SimpleFieldTypeMax)
		case 4:
			flatMetricsV1.SimpleFieldAddType(builder, flatMetricsV1.SimpleFieldTypeUnSpecified)
		default:
			flatMetricsV1.SimpleFieldAddType(builder, flatMetricsV1.SimpleFieldTypeDeltaSum)
		}
		flatMetricsV1.SimpleFieldAddValue(builder, float64(i))
		fields[i] = flatMetricsV1.SimpleFieldEnd(builder)
	}

	flatMetricsV1.MetricStartKeyValuesVector(builder, 10)
	for i := 9; i >= 0; i-- {
		builder.PrependUOffsetT(kvs[i])
	}
	kvsAt := builder.EndVector(10)

	flatMetricsV1.MetricStartSimpleFieldsVector(builder, 10)
	for i := 9; i >= 0; i-- {
		builder.PrependUOffsetT(fields[i])
	}
	fieldsAt := builder.EndVector(10)

	// add compound buckets
	flatMetricsV1.CompoundFieldStartValuesVector(builder, 10)
	for i := 9; i >= 0; i-- {
		builder.PrependFloat64(float64(i))
	}
	compoundFieldValues := builder.EndVector(10)
	// add explicit bounds
	flatMetricsV1.CompoundFieldStartExplicitBoundsVector(builder, 10)
	for i := 9; i >= 0; i-- {
		builder.PrependFloat64(float64(i))
	}
	compoundFieldBounds := builder.EndVector(10)
	flatMetricsV1.CompoundFieldStart(builder)
	flatMetricsV1.CompoundFieldAddCount(builder, 1024)
	flatMetricsV1.CompoundFieldAddSum(builder, 1024*1024)
	flatMetricsV1.CompoundFieldAddMin(builder, 10)
	flatMetricsV1.CompoundFieldAddMax(builder, 2048)
	flatMetricsV1.CompoundFieldAddValues(builder, compoundFieldValues)
	flatMetricsV1.CompoundFieldAddExplicitBounds(builder, compoundFieldBounds)
	compoundField := flatMetricsV1.CompoundFieldEnd(builder)

	// serialize metric
	metricName := builder.CreateString("hello")
	namespace := builder.CreateString("default-ns")
	flatMetricsV1.MetricStart(builder)
	if ns {
		flatMetricsV1.MetricAddNamespace(builder, namespace)
	}
	flatMetricsV1.MetricAddName(builder, metricName)
	flatMetricsV1.MetricAddTimestamp(builder, time.Now().UnixNano())
	flatMetricsV1.MetricAddKeyValues(builder, kvsAt)
	flatMetricsV1.MetricAddKvsHash(builder, xxhash.Sum64String("hello"))
	flatMetricsV1.MetricAddNameHash(builder, xxhash.Sum64String("hello"))
	flatMetricsV1.MetricAddSimpleFields(builder, fieldsAt)
	flatMetricsV1.MetricAddCompoundField(builder, compoundField)

	end := flatMetricsV1.MetricEnd(builder)
	builder.Finish(end)
}

func Test_MetricRow_WithSimpleFields(t *testing.T) {
	builder := flatbuffers.NewBuilder(1024)
	buildFlatMetric(builder, true)

	var mr StorageRow
	mr.Unmarshal(builder.FinishedBytes())

	assert.Equal(t, "hello", string(mr.Name()))

	assert.Equal(t, "default-ns", string(mr.NameSpace()))
	assert.NotZero(t, mr.NameHash())
	assert.NotZero(t, mr.TagsHash())
	assert.Equal(t, 10, mr.TagsLen())
	assert.Equal(t, 10, mr.SimpleFieldsLen())
	assert.NotZero(t, mr.Timestamp())

	kvItr := mr.NewKeyValueIterator()
	assert.Equal(t, 10, kvItr.Len())

	for i := 0; i < 10; i++ {
		kvItr.Reset()
		var count int
		for kvItr.HasNext() {
			assert.Equal(t, "key"+strconv.Itoa(count), string(kvItr.NextKey()))
			assert.Equal(t, "value"+strconv.Itoa(count), string(kvItr.NextValue()))
			count++
		}
		assert.Equal(t, 10, count)
	}

	sfItr := mr.NewSimpleFieldIterator()
	assert.Equal(t, 10, sfItr.Len())
	for i := 0; i < 10; i++ {
		sfItr.Reset()
		var count int
		for sfItr.HasNext() {
			assert.Equal(t, testCounter+strconv.Itoa(count), string(sfItr.NextName()))
			assert.Equal(t, testCounter+strconv.Itoa(count), string(sfItr.NextRawName()))
			switch count {
			case 0:
				assert.Equal(t, field.LastField, sfItr.NextType())
				assert.Equal(t, flatMetricsV1.SimpleFieldTypeLast, sfItr.NextRawType())
			case 1:
				assert.Equal(t, field.FirstField, sfItr.NextType())
				assert.Equal(t, flatMetricsV1.SimpleFieldTypeFirst, sfItr.NextRawType())
			case 2:
				assert.Equal(t, field.MinField, sfItr.NextType())
				assert.Equal(t, flatMetricsV1.SimpleFieldTypeMin, sfItr.NextRawType())
			case 3:
				assert.Equal(t, field.MaxField, sfItr.NextType())
				assert.Equal(t, flatMetricsV1.SimpleFieldTypeMax, sfItr.NextRawType())
			case 4:
				assert.Equal(t, field.Unknown, sfItr.NextType())
				assert.Equal(t, flatMetricsV1.SimpleFieldTypeUnSpecified, sfItr.NextRawType())
			default:
				assert.Equal(t, field.SumField, sfItr.NextType())
				assert.Equal(t, flatMetricsV1.SimpleFieldTypeDeltaSum, sfItr.NextRawType())
			}
			assert.InDelta(t, float64(count), sfItr.NextValue(), 1e-6)
			count++
		}
		assert.Equal(t, 10, count)
	}
}

func Test_MetricRow_WithCompoundField(t *testing.T) {
	builder := flatbuffers.NewBuilder(1024)
	buildFlatMetric(builder, false)

	var mr StorageRow
	mr.Unmarshal(builder.FinishedBytes())

	assert.Equal(t, "default-ns", string(mr.NameSpace()))
	itr, ok := mr.NewCompoundFieldIterator()
	assert.True(t, ok)
	assert.NotNil(t, itr)

	assert.Equal(t, 10, itr.BucketLen())
	assert.InDelta(t, 10, itr.Min(), 1e-6)
	assert.InDelta(t, 2048, itr.Max(), 1e-6)
	assert.InDelta(t, 1024, itr.Count(), 1e-6)
	assert.InDelta(t, 1024*1024, itr.Sum(), 1e-6)

	_ = itr.HistogramSumFieldName()
	_ = itr.HistogramCountFieldName()
	_ = itr.HistogramMaxFieldName()
	_ = itr.HistogramMinFieldName()

	for i := 0; i < 10; i++ {
		itr.Reset()
		var count int
		for itr.HasNextBucket() {
			assert.InDelta(t, float64(count), itr.NextExplicitBound(), 1e-6)
			assert.InDelta(t, float64(count), itr.NextValue(), 1e-6)
			_ = itr.BucketName()
			count++
		}
		assert.Equal(t, 10, count)
	}
}

func Test_HistogramConverter(t *testing.T) {
	v := math.Inf(1) + 1
	assert.Equal(t, "__bucket_+Inf", BucketNameOfHistogramExplicitBound(v))
	assert.Equal(t, "__bucket_0.025", BucketNameOfHistogramExplicitBound(0.025))
	assert.Equal(t, "__bucket_0.5", BucketNameOfHistogramExplicitBound(0.500))
	assert.Equal(t, "__bucket_5000", BucketNameOfHistogramExplicitBound(5000))

	f, err := UpperBound("__bucket_5000")
	assert.Nil(t, err)
	assert.Equal(t, float64(5000), f)

	f, err = UpperBound("__bucket_0.025")
	assert.Nil(t, err)
	assert.Equal(t, 0.025, f)

	_, err = UpperBound("_bucket_0.025")
	assert.NotNil(t, err)

	_, err = UpperBound("__bucket_x")
	assert.NotNil(t, err)
}

func TestStorageBatchRows_Sorts(t *testing.T) {
	builder := flatbuffers.NewBuilder(1024)
	buildFlatMetric(builder, true)

	var mr1 StorageRow
	mr1.Unmarshal(builder.FinishedBytes())
	var mr2 StorageRow
	buildFlatMetric(builder, true)
	mr2.Unmarshal(builder.FinishedBytes())
	rows := NewStorageBatchRows()
	rows.appendIndex = 4
	rows.rows = []*StorageRow{&mr1, &mr2, &mr1, &mr1, &mr2}
	sort.Sort(rows)
}
