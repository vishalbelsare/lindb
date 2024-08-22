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

package stmt

import (
	"encoding/json"

	"github.com/lindb/common/pkg/encoding"
)

// MetricMetadataType represents metric metadata suggest type
type MetricMetadataType uint8

// Defines all types of metric metadata suggest
const (
	Namespace MetricMetadataType = iota + 1
	Metric
	TagKey
	TagValue
	Field
)

// String returns string value of metadata type
func (m MetricMetadataType) String() string {
	switch m {
	case Namespace:
		return "namespace"
	case Metric:
		return "metric"
	case Field:
		return field
	case TagKey:
		return "tagKey"
	case TagValue:
		return "tagValue"
	default:
		return unknown
	}
}

// MetricMetadata represents search metric metadata statement
type MetricMetadata struct {
	Namespace  string             // namespace
	MetricName string             // like table name
	Type       MetricMetadataType // metadata suggest type
	TagKey     string
	Prefix     string
	Condition  Expr // tag filter condition expression
	Limit      int  // result set limit
}

// StatementType returns metadata query type.
func (q *MetricMetadata) StatementType() StatementType {
	return MetricMetadataStatement
}

// innerMetadata represents a wrapper of metadata for json encoding
type innerMetadata struct {
	Namespace  string             `json:"namespace,omitempty"`
	MetricName string             `json:"metricName,omitempty"`
	Type       MetricMetadataType `json:"type,omitempty"`
	TagKey     string             `json:"tagKey,omitempty"`
	Condition  json.RawMessage    `json:"condition,omitempty"`
	Prefix     string             `json:"prefix,omitempty"`
	Limit      int                `json:"limit,omitempty"`
}

// MarshalJSON returns json data of query
func (q *MetricMetadata) MarshalJSON() ([]byte, error) {
	inner := innerMetadata{
		MetricName: q.MetricName,
		Namespace:  q.Namespace,
		Condition:  Marshal(q.Condition),
		TagKey:     q.TagKey,
		Type:       q.Type,
		Prefix:     q.Prefix,
		Limit:      q.Limit,
	}
	return encoding.JSONMarshal(&inner), nil
}

// UnmarshalJSON parses json data to metadata
func (q *MetricMetadata) UnmarshalJSON(value []byte) error {
	inner := innerMetadata{}
	if err := encoding.JSONUnmarshal(value, &inner); err != nil {
		return err
	}
	if inner.Condition != nil {
		condition, err := Unmarshal(inner.Condition)
		if err != nil {
			return err
		}
		q.Condition = condition
	}
	q.Namespace = inner.Namespace
	q.MetricName = inner.MetricName
	q.Type = inner.Type
	q.TagKey = inner.TagKey
	q.Prefix = inner.Prefix
	q.Limit = inner.Limit
	return nil
}
