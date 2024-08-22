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

package operator

import (
	"fmt"

	"github.com/lindb/lindb/constants"
	"github.com/lindb/lindb/query/context"
)

// tagKeySuggest represents tag key suggest operator.
type tagKeySuggest struct {
	ctx *context.LeafMetadataContext
}

// NewTagKeySuggest create a tagKeySuggest instance.
func NewTagKeySuggest(ctx *context.LeafMetadataContext) Operator {
	return &tagKeySuggest{
		ctx: ctx,
	}
}

// Execute returns tag key list by given namespace/metric name.
func (op *tagKeySuggest) Execute() error {
	req := op.ctx.Request
	metricID, err := op.ctx.Database.MetaDB().GetMetricID(req.Namespace, req.MetricName)
	if err != nil {
		return err
	}
	schema, err := op.ctx.Database.MetaDB().GetSchema(metricID)
	if err != nil {
		return err
	}
	if schema == nil {
		return fmt.Errorf("%w, metric: %s", constants.ErrMetricIDNotFound, req.MetricName)
	}
	var result []string
	for _, tagKey := range schema.TagKeys {
		result = append(result, tagKey.Key)
	}
	op.ctx.ResultSet = result
	return nil
}

// Identifier returns identifier value of tag key suggest operator.
func (op *tagKeySuggest) Identifier() string {
	return "Tag Key Suggest"
}
