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

package linmetric

import (
	"math"

	"go.uber.org/atomic"

	"github.com/lindb/common/proto/gen/v1/flatMetricsV1"
)

type BoundMin struct {
	value     atomic.Float64
	fieldName string
}

func newMin(fieldName string) *BoundMin {
	return &BoundMin{
		fieldName: fieldName,
		value:     *atomic.NewFloat64(math.Inf(1)),
	}
}

// Update updates Min with a new value
// Skip updating when newValue is biger than v
func (m *BoundMin) Update(newValue float64) {
	for {
		v := m.value.Load()
		if newValue > v {
			return
		}
		if m.value.CompareAndSwap(v, newValue) {
			return
		}
	}
}

// Get returns the current Min value
func (m *BoundMin) Get() float64 {
	return m.value.Load()
}

func (m *BoundMin) gather() float64 { return m.value.Load() }

func (m *BoundMin) name() string { return m.fieldName }

func (m *BoundMin) flatType() flatMetricsV1.SimpleFieldType {
	return flatMetricsV1.SimpleFieldTypeMin
}
