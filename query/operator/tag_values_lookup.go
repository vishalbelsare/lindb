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

	"github.com/lindb/roaring"

	"github.com/lindb/lindb/constants"
	"github.com/lindb/lindb/flow"
	"github.com/lindb/lindb/index"
	"github.com/lindb/lindb/series/tag"
	"github.com/lindb/lindb/sql/stmt"
	"github.com/lindb/lindb/tsdb"
)

// tagValuesLookup represents tag values lookup operator.
type tagValuesLookup struct {
	executeCtx *flow.StorageExecuteContext
	metaDB     index.MetricMetaDatabase

	err error
}

// NewTagValuesLookup creates a tagValuesLookup instance.
func NewTagValuesLookup(executeCtx *flow.StorageExecuteContext, database tsdb.Database) Operator {
	return &tagValuesLookup{
		executeCtx: executeCtx,
		metaDB:     database.MetaDB(),
	}
}

// Execute executes tag value ids lookup based on tag filter expr.
func (op *tagValuesLookup) Execute() error {
	op.executeCtx.TagFilterResult = make(map[string]*flow.TagFilterResult)
	op.findTagValueIDsByExpr(op.executeCtx.Query.Condition)
	return op.err
}

// findTagValueIDsByExpr finds tag value ids by expr, recursion filter for expr
func (op *tagValuesLookup) findTagValueIDsByExpr(expr stmt.Expr) {
	if expr == nil {
		return
	}
	if op.err != nil {
		return
	}
	switch expr := expr.(type) {
	case stmt.TagFilter:
		tagKeyID, err := op.getTagKeyID(expr.TagKey())
		if err != nil {
			op.err = err
			return
		}
		tagValueIDs, err := op.metaDB.FindTagValueDsByExpr(tagKeyID, expr)
		if err != nil {
			op.err = err
			return
		}
		if tagValueIDs == nil {
			tagValueIDs = roaring.New()
		}
		// save atomic tag filter result
		op.executeCtx.TagFilterResult[expr.Rewrite()] = &flow.TagFilterResult{
			TagKeyID:    tagKeyID,
			TagValueIDs: tagValueIDs,
		}
	case *stmt.ParenExpr:
		op.findTagValueIDsByExpr(expr.Expr)
	case *stmt.NotExpr:
		// find tag value id by expr => (not tag filter) => tag filter
		op.findTagValueIDsByExpr(expr.Expr)
	case *stmt.BinaryExpr:
		if expr.Operator != stmt.AND && expr.Operator != stmt.OR {
			op.err = fmt.Errorf("wrong binary operator in tag filter: %s", stmt.BinaryOPString(expr.Operator))
			return
		}
		op.findTagValueIDsByExpr(expr.Left)
		op.findTagValueIDsByExpr(expr.Right)
	}
}

// getTagKeyID returns the tag key id by tag key
func (op *tagValuesLookup) getTagKeyID(tagKey string) (tag.KeyID, error) {
	tagMeta, ok := op.executeCtx.Schema.TagKeys.Find(tagKey)
	if !ok {
		return 0, fmt.Errorf("%w, tag key: %s", constants.ErrTagKeyIDNotFound, tagKey)
	}
	return tagMeta.ID, nil
}

// Identifier returns identifier value of tag value lookup operator.
func (op *tagValuesLookup) Identifier() string {
	return "Tag Value Lookup"
}
