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

package sql

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	commonconstants "github.com/lindb/common/constants"
	commontimeutil "github.com/lindb/common/pkg/timeutil"

	"github.com/lindb/lindb/aggregation/function"
	"github.com/lindb/lindb/pkg/timeutil"
	"github.com/lindb/lindb/sql/stmt"
)

func TestQueryStmt_validation(t *testing.T) {
	queryStmt := newQueryStmtParse(false)
	// case 1: queryStmt err
	queryStmt.err = fmt.Errorf("err")
	s, err := queryStmt.build()
	assert.Error(t, err)
	assert.Nil(t, s)
	queryStmt.err = nil
	// case 2: metric name is empty
	queryStmt.metricName = ""
	s, err = queryStmt.build()
	assert.Error(t, err)
	assert.Nil(t, s)
	queryStmt.metricName = "cpu"
	// case 3: select item is nil
	queryStmt.selectItems = nil
	s, err = queryStmt.build()
	assert.Error(t, err)
	assert.Nil(t, s)
}

func TestQueryStmt_Namespace(t *testing.T) {
	sql := "select f from cpu on 'ns' where host='1.1.1.1' and time>='2020-10-10 10:00:00' and time<='2020-10-10 11:00:00'"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, "ns", query.Namespace)
	sql2 := "from cpu on 'ns' select f where host='1.1.1.1' and time>='2020-10-10 10:00:00' and time<='2020-10-10 11:00:00'"
	q2, err := Parse(sql2)
	assert.NoError(t, err)
	assert.EqualValues(t, q, q2)
}

func TestExplainStatement(t *testing.T) {
	sql := "explain select f from cpu where host='1.1.1.1'"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.Nil(t, err)
	assert.True(t, query.Explain)
	sql = "explain from cpu select f where host='1.1.1.1'"
	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.Nil(t, err)
	assert.True(t, query.Explain)
	sql = "select f from cpu where host='2.2.2.2'"

	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.Nil(t, err)
	assert.False(t, query.Explain)
	assert.Equal(t, commonconstants.DefaultNamespace, query.Namespace)
}

func TestMetricName(t *testing.T) {
	sql := "select f from cpu where host='1.1.1.1'"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, "cpu", query.MetricName)

	sql = "select f "
	_, err = Parse(sql)
	assert.NotNil(t, err)
}

func TestSingleSelectItem(t *testing.T) {
	sql := "select f from memory"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(query.SelectItems))
	selectItem := (query.SelectItems[0]).(*stmt.SelectItem)
	assert.Equal(t, stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "f"}}, *selectItem)

	sql = " from cpu"
	_, err = Parse(sql)
	assert.NotNil(t, err)

	sql = "select f as f1 from cpu"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.Equal(t, 1, len(query.SelectItems))
	selectItem = (query.SelectItems[0]).(*stmt.SelectItem)
	assert.Equal(t, stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "f"}, Alias: "f1"}, *selectItem)

	sql = "select f,a,sum(d),avg(a) as f1 from cpu"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.Len(t, query.SelectItems, 4)
}

func TestSelectFuncItem(t *testing.T) {
	sql := "select count(f) from memory"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(query.SelectItems))
	selectItem := (query.SelectItems[0]).(*stmt.SelectItem)
	assert.Equal(t, stmt.SelectItem{
		Expr: &stmt.CallExpr{FuncType: function.Count, Params: []stmt.Expr{&stmt.FieldExpr{Name: "f"}}},
	}, *selectItem)

	sql = "select quantile(0.99) from memory"
	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(query.SelectItems))
	selectItem = (query.SelectItems[0]).(*stmt.SelectItem)
	// field name is hacked to transform into quantile value
	assert.Equal(t, stmt.SelectItem{
		Expr: &stmt.CallExpr{FuncType: function.Quantile, Params: []stmt.Expr{&stmt.NumberLiteral{Val: 0.99}}},
	}, *selectItem)

	sql = "select rate(f) from memory"
	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(query.SelectItems))
	selectItem = (query.SelectItems[0]).(*stmt.SelectItem)
	assert.Equal(t, stmt.SelectItem{
		Expr: &stmt.CallExpr{FuncType: function.Rate, Params: []stmt.Expr{&stmt.FieldExpr{Name: "f"}}},
	}, *selectItem)

	sql = "select last(f) from memory"
	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(query.SelectItems))
	selectItem = (query.SelectItems[0]).(*stmt.SelectItem)
	assert.Equal(t, stmt.SelectItem{
		Expr: &stmt.CallExpr{FuncType: function.Last, Params: []stmt.Expr{&stmt.FieldExpr{Name: "f"}}},
	}, *selectItem)

	sql = "select first(f) from memory"
	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(query.SelectItems))
	selectItem = (query.SelectItems[0]).(*stmt.SelectItem)
	assert.Equal(t, stmt.SelectItem{
		Expr: &stmt.CallExpr{FuncType: function.First, Params: []stmt.Expr{&stmt.FieldExpr{Name: "f"}}},
	}, *selectItem)
}

func TestFieldExpression(t *testing.T) {
	q, err := Parse("select f+100 from cpu")
	query := q.(*stmt.Query)
	assert.NoError(t, err)
	num := ((query.SelectItems[0].(*stmt.SelectItem)).Expr.(*stmt.BinaryExpr)).Right.(*stmt.NumberLiteral)
	assert.Equal(t, 100.0, num.Val)

	q, err = Parse("select f-100.1 from cpu")
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	num = ((query.SelectItems[0].(*stmt.SelectItem)).Expr.(*stmt.BinaryExpr)).Right.(*stmt.NumberLiteral)
	assert.Equal(t, 100.1, num.Val)

	q, err = Parse("select sum(f+100.1) from cpu")
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	selectItem := query.SelectItems[0].(*stmt.SelectItem)
	num = ((selectItem.Expr.(*stmt.CallExpr)).Params[0].(*stmt.BinaryExpr)).Right.(*stmt.NumberLiteral)
	assert.Equal(t, 100.1, num.Val)
}

func TestComplexSelectItem(t *testing.T) {
	sql := "select a,b,c from memory"
	q, _ := Parse(sql)
	query := q.(*stmt.Query)
	assert.Equal(t,
		[]stmt.Expr{
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "a"}},
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "b"}},
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "c"}},
		},
		query.SelectItems)

	sql = "select a,b,sum(c) from memory"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.Equal(t,
		[]stmt.Expr{
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "a"}},
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "b"}},
			&stmt.SelectItem{
				Expr: &stmt.CallExpr{
					FuncType: function.Sum,
					Params:   []stmt.Expr{&stmt.FieldExpr{Name: "c"}},
				},
			},
		},
		query.SelectItems)

	sql = "select a,b,max(sum(c)) from memory"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.Equal(t,
		[]stmt.Expr{
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "a"}},
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "b"}},
			&stmt.SelectItem{
				Expr: &stmt.CallExpr{
					FuncType: function.Max,
					Params: []stmt.Expr{&stmt.CallExpr{
						FuncType: function.Sum,
						Params:   []stmt.Expr{&stmt.FieldExpr{Name: "c"}}},
					},
				},
			},
		},
		query.SelectItems)
	sql = "select min(a),avg(b),max(sum(c)) from memory"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.Equal(t,
		[]stmt.Expr{
			&stmt.SelectItem{
				Expr: &stmt.CallExpr{
					FuncType: function.Min,
					Params:   []stmt.Expr{&stmt.FieldExpr{Name: "a"}},
				},
			},
			&stmt.SelectItem{
				Expr: &stmt.CallExpr{
					FuncType: function.Avg,
					Params:   []stmt.Expr{&stmt.FieldExpr{Name: "b"}},
				},
			},
			&stmt.SelectItem{
				Expr: &stmt.CallExpr{
					FuncType: function.Max,
					Params: []stmt.Expr{&stmt.CallExpr{
						FuncType: function.Sum,
						Params:   []stmt.Expr{&stmt.FieldExpr{Name: "c"}}},
					},
				},
			},
		},
		query.SelectItems)

	sql = "select a,b,stddev(max(sum(c))) from memory"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.Equal(t,
		[]stmt.Expr{
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "a"}},
			&stmt.SelectItem{Expr: &stmt.FieldExpr{Name: "b"}},
			&stmt.SelectItem{
				Expr: &stmt.CallExpr{
					FuncType: function.Stddev,
					Params: []stmt.Expr{
						&stmt.CallExpr{
							FuncType: function.Max,
							Params: []stmt.Expr{&stmt.CallExpr{
								FuncType: function.Sum,
								Params:   []stmt.Expr{&stmt.FieldExpr{Name: "c"}}},
							},
						},
					},
				},
			},
		},
		query.SelectItems)
}

func TestMathExpress(t *testing.T) {
	// math expression
	sql := "select max(sum(c)+c*d/e) from memory"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.NoError(t, err)
	expr := []stmt.Expr{
		&stmt.SelectItem{
			Expr: &stmt.CallExpr{
				FuncType: function.Max,
				Params: []stmt.Expr{
					&stmt.BinaryExpr{
						Left: &stmt.CallExpr{
							FuncType: function.Sum,
							Params:   []stmt.Expr{&stmt.FieldExpr{Name: "c"}}},
						Operator: stmt.ADD,
						Right: &stmt.BinaryExpr{
							Left: &stmt.BinaryExpr{
								Left:     &stmt.FieldExpr{Name: "c"},
								Operator: stmt.MUL,
								Right:    &stmt.FieldExpr{Name: "d"},
							},
							Operator: stmt.DIV,
							Right:    &stmt.FieldExpr{Name: "e"},
						},
					},
				},
			},
		},
	}
	assert.Equal(t, expr, query.SelectItems)
	sql = "from memory select max(sum(c)+c*d/e)"
	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, expr, query.SelectItems)
}

func TestLimit(t *testing.T) {
	sql := "select f from cpu limit 10"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, 10, query.Limit)

	sql = "select f from cpu limit abc"
	_, err = Parse(sql)
	assert.Error(t, err)

	// default
	sql = "select f from cpu "
	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.Nil(t, err)
	assert.Equal(t, 20, query.Limit)
}

func TestTimeRange(t *testing.T) {
	sql := "select f from cpu where time>'20190410 00:00:00' and time<'20190410 10:00:00'"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, "cpu", query.MetricName)
	startTime, _ := commontimeutil.ParseTimestamp("20190410 00:00:00")
	assert.Equal(t, startTime, query.TimeRange.Start)
	endTime, _ := commontimeutil.ParseTimestamp("20190410 10:00:00")
	assert.Equal(t, endTime, query.TimeRange.End)

	// error for start > end
	sql = "select f from cpu where time>'20190410 11:00:00' and time<'20190410 10:00:00'"
	_, err = Parse(sql)
	assert.Error(t, err)
}

func TestInterval(t *testing.T) {
	sql := "select f from cpu where region='sh'"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, timeutil.Interval(int64(0)), query.Interval)
	sql = "select f from cpu group by time(100s)"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, timeutil.Interval(100*commontimeutil.OneSecond), query.Interval)
	sql = "select f from cpu group by time(1m)"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, timeutil.Interval(commontimeutil.OneMinute), query.Interval)
	sql = "select f from cpu group by time(1d)"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, timeutil.Interval(commontimeutil.OneDay), query.Interval)
	sql = "select f from cpu group by time(1w)"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, timeutil.Interval(commontimeutil.OneWeek), query.Interval)
	sql = "select f from cpu group by time(1M)"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, timeutil.Interval(commontimeutil.OneMonth), query.Interval)
	sql = "select f from cpu group by time(1y)"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, timeutil.Interval(commontimeutil.OneYear), query.Interval)
	assert.False(t, query.AutoGroupByTime)

	sql = "select f from cpu group by time()"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.True(t, query.AutoGroupByTime)
}

func TestGroupBy(t *testing.T) {
	sql := "select f from cpu where time>now()-1h"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(query.GroupBy))
	sql = "select f from disk group by host,time(100s),'/data'"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(query.GroupBy))
	assert.Equal(t, "host", query.GroupBy[0])
	assert.Equal(t, "/data", query.GroupBy[1])
}

func TestEmptyCondition(t *testing.T) {
	sql := "select f from cpu"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.NoError(t, err)
	assert.Nil(t, query.Condition)
}

func TestEqualsExpr(t *testing.T) {
	// equals
	sql := "select f from cpu where ip='1.1.1.1'"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.NoError(t, err)
	expr := query.Condition.(*stmt.EqualsExpr)
	assert.Equal(t, stmt.EqualsExpr{Key: "ip", Value: "1.1.1.1"}, *expr)
	// not equals
	sql = "select f from cpu where ip!='1.1.1.1'"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	notExpr := query.Condition.(*stmt.NotExpr)
	assert.Equal(t, stmt.NotExpr{Expr: &stmt.EqualsExpr{Key: "ip", Value: "1.1.1.1"}}, *notExpr)

	// not equals
	sql = "select f from cpu where ip<>'1.1.1.1'"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	notExpr = query.Condition.(*stmt.NotExpr)
	assert.Equal(t, stmt.NotExpr{Expr: &stmt.EqualsExpr{Key: "ip", Value: "1.1.1.1"}}, *notExpr)
}

func TestLikeExpr(t *testing.T) {
	sql := "select f from cpu where ip like '1.1.%.1'"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.NoError(t, err)
	expr := query.Condition.(*stmt.LikeExpr)
	assert.Equal(t, stmt.LikeExpr{Key: "ip", Value: "1.1.%.1"}, *expr)

	// not like
	sql = "select f from cpu where ip not like '1.1.%.1'"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	notExpr := query.Condition.(*stmt.NotExpr)
	assert.Equal(t, stmt.NotExpr{Expr: &stmt.LikeExpr{Key: "ip", Value: "1.1.%.1"}}, *notExpr)
}

func TestRegexExpr(t *testing.T) {
	sql := "select f from cpu where ip=~'/1.1.*.1/'"
	q, _ := Parse(sql)
	query := q.(*stmt.Query)
	expr := query.Condition.(*stmt.RegexExpr)
	assert.Equal(t, stmt.RegexExpr{Key: "ip", Regexp: "/1.1.*.1/"}, *expr)

	// not regex
	sql = "select f from cpu where ip!~'/1.1.*.1/'"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	notExpr := query.Condition.(*stmt.NotExpr)
	assert.Equal(t, stmt.NotExpr{Expr: &stmt.RegexExpr{Key: "ip", Regexp: "/1.1.*.1/"}}, *notExpr)
}

func TestInExpr(t *testing.T) {
	sql := "select f from cpu where ip in ('1.1.1.1','2.2.2.2')"
	q, _ := Parse(sql)
	query := q.(*stmt.Query)
	expr := query.Condition.(*stmt.InExpr)
	assert.Equal(t, stmt.InExpr{Key: "ip", Values: []string{"1.1.1.1", "2.2.2.2"}}, *expr)

	sql = "select f from cpu where (ip in ('1.1.1.1','2.2.2.2'))"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	atomExpr := query.Condition.(*stmt.ParenExpr)
	assert.Equal(t, stmt.ParenExpr{Expr: &stmt.InExpr{Key: "ip", Values: []string{"1.1.1.1", "2.2.2.2"}}}, *atomExpr)

	// not in
	sql = "select f from cpu where ip not in ('1.1.1.1','2.2.2.2')"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	notExpr := query.Condition.(*stmt.NotExpr)
	assert.Equal(t, stmt.NotExpr{Expr: &stmt.InExpr{Key: "ip", Values: []string{"1.1.1.1", "2.2.2.2"}}}, *notExpr)
}

func TestTagFilterBinary(t *testing.T) {
	sql := "select f from cpu where ip in ('1.1.1.1','2.2.2.2') and path='/data'"
	q, _ := Parse(sql)
	query := q.(*stmt.Query)
	expr := query.Condition.(*stmt.BinaryExpr)
	assert.Equal(t,
		stmt.BinaryExpr{
			Left:     &stmt.InExpr{Key: "ip", Values: []string{"1.1.1.1", "2.2.2.2"}},
			Operator: stmt.AND,
			Right:    &stmt.EqualsExpr{Key: "path", Value: "/data"},
		}, *expr)

	sql = "select f from cpu where ip in ('1.1.1.1','2.2.2.2') and path='/data' and disk='adc'"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	expr = query.Condition.(*stmt.BinaryExpr)
	assert.Equal(t,
		stmt.BinaryExpr{
			Left: &stmt.BinaryExpr{
				Left:     &stmt.InExpr{Key: "ip", Values: []string{"1.1.1.1", "2.2.2.2"}},
				Operator: stmt.AND,
				Right:    &stmt.EqualsExpr{Key: "path", Value: "/data"},
			},
			Operator: stmt.AND,
			Right:    &stmt.EqualsExpr{Key: "disk", Value: "adc"},
		}, *expr)

	sql = "select f from cpu where ip in ('1.1.1.1','2.2.2.2') and (path='/data' and disk='adc')"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	expr = query.Condition.(*stmt.BinaryExpr)
	assert.Equal(t,
		stmt.BinaryExpr{
			Left:     &stmt.InExpr{Key: "ip", Values: []string{"1.1.1.1", "2.2.2.2"}},
			Operator: stmt.AND,
			Right: &stmt.ParenExpr{Expr: &stmt.BinaryExpr{
				Left:     &stmt.EqualsExpr{Key: "path", Value: "/data"},
				Operator: stmt.AND,
				Right:    &stmt.EqualsExpr{Key: "disk", Value: "adc"},
			}},
		}, *expr)

	sql = "select f from cpu where (ip in ('1.1.1.1','2.2.2.2') and region='sh') and (path='/data' or path='/home')"
	q, _ = Parse(sql)
	query = q.(*stmt.Query)
	expr = query.Condition.(*stmt.BinaryExpr)
	assert.Equal(t,
		stmt.BinaryExpr{
			Left: &stmt.ParenExpr{Expr: &stmt.BinaryExpr{
				Left:     &stmt.InExpr{Key: "ip", Values: []string{"1.1.1.1", "2.2.2.2"}},
				Operator: stmt.AND,
				Right:    &stmt.EqualsExpr{Key: "region", Value: "sh"},
			}},
			Operator: stmt.AND,
			Right: &stmt.ParenExpr{Expr: &stmt.BinaryExpr{
				Left:     &stmt.EqualsExpr{Key: "path", Value: "/data"},
				Operator: stmt.OR,
				Right:    &stmt.EqualsExpr{Key: "path", Value: "/home"},
			}},
		}, *expr)
}

func TestOrderBy(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		rs      []stmt.Expr
		wantErr bool
	}{
		{
			name: "no order by",
			sql:  "select f from cpu",
		},
		{
			name:    "order by field no in select list",
			sql:     "select f from cpu order by b",
			wantErr: true,
		},
		{
			name:    "order by field not in select list",
			sql:     "select ff from cpu order by f desc",
			wantErr: true,
		},
		{
			name:    "function not support",
			sql:     "select ff from cpu order by quantile(f,0.99) desc",
			wantErr: true,
		},
		{
			name:    "function param not support function, not in select list",
			sql:     "select ff from cpu order by max(min(f)) desc",
			wantErr: true,
		},
		{
			name: "function param support function, in select list",
			sql:  "select min(f) from cpu order by max(min(f)) desc",
			rs: []stmt.Expr{&stmt.OrderByExpr{
				Desc: true,
				Expr: &stmt.CallExpr{
					FuncType: function.Max,
					Params: []stmt.Expr{
						&stmt.CallExpr{
							FuncType: function.Min,
							Params:   []stmt.Expr{&stmt.FieldExpr{Name: "f"}},
						},
					},
				},
			}},
		},
		{
			name: "order by field asc",
			sql:  "select f from cpu order by f",
			rs:   []stmt.Expr{&stmt.OrderByExpr{Expr: &stmt.FieldExpr{Name: "f"}}},
		},
		{
			name: "order by field asc with func",
			sql:  "select f from cpu order by max(f)",
			rs: []stmt.Expr{&stmt.OrderByExpr{
				Expr: &stmt.CallExpr{FuncType: function.Max, Params: []stmt.Expr{&stmt.FieldExpr{Name: "f"}}},
			}},
		},
		{
			name: "order by field desc",
			sql:  "select f from cpu order by f desc",
			rs:   []stmt.Expr{&stmt.OrderByExpr{Expr: &stmt.FieldExpr{Name: "f"}, Desc: true}},
		},
		{
			name: "order by field desc(alias)",
			sql:  "select ff from cpu order by ff desc",
			rs:   []stmt.Expr{&stmt.OrderByExpr{Expr: &stmt.FieldExpr{Name: "ff"}, Desc: true}},
		},
		{
			name: "order by multi-field desc(alias)",
			sql:  "select f as ff,bb from cpu order by bb,ff desc",
			rs: []stmt.Expr{
				&stmt.OrderByExpr{Expr: &stmt.FieldExpr{Name: "bb"}, Desc: false},
				&stmt.OrderByExpr{Expr: &stmt.FieldExpr{Name: "ff"}, Desc: true},
			},
		},
		{
			name: "order by multi-field desc(alias) with func",
			sql:  "select min(f) as ff,bb from cpu order by bb,max(ff) desc",
			rs: []stmt.Expr{
				&stmt.OrderByExpr{Expr: &stmt.FieldExpr{Name: "bb"}, Desc: false},
				&stmt.OrderByExpr{Expr: &stmt.CallExpr{FuncType: function.Max, Params: []stmt.Expr{&stmt.FieldExpr{Name: "ff"}}}, Desc: true},
			},
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			q, err := Parse(tt.sql)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				query := q.(*stmt.Query)
				assert.Equal(t, tt.rs, query.OrderByItems)
			}
		})
	}
}

func TestHaving(t *testing.T) {
	sql := "select f from cpu where host='1.1.1.1' group by host,ip having x*2 > 3.5 and m*(n*4.1) < 5"
	q, err := Parse(sql)
	query := q.(*stmt.Query)
	assert.Nil(t, err)

	calc := NewCalc(query.Having)
	result, err := calc.CalcExpr(map[string]float64{
		"x": 2,
		"m": 2,
		"n": 1.1,
	})
	assert.Nil(t, err)
	assert.IsType(t, true, result)
	assert.False(t, result.(bool))

	sql = "select f from cpu where host='1.1.1.1' group by host,ip having (x*2 > 3.5 or m*(n*4.1) < 5) and a*b > 99.7"
	q, err = Parse(sql)
	query = q.(*stmt.Query)
	assert.Nil(t, err)

	calc = NewCalc(query.Having)
	result, err = calc.CalcExpr(map[string]float64{
		"x": 2,
		"m": 2,
		"n": 1.1,
		"a": 9.99,
		"b": 9.99,
	})
	assert.Nil(t, err)
	assert.IsType(t, true, result)
	assert.True(t, result.(bool))
}
