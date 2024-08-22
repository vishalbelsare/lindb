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

// StateType represents state statement type.
type StateType uint8

const (
	// Master represents show master statement.
	Master StateType = iota + 1
	// RootAlive represents show root alive(node)  statement.
	RootAlive
	// BrokerAlive represents show broker alive(node)  statement.
	BrokerAlive
	// StorageAlive represents show storage alive(node) statement.
	StorageAlive
	// Replication represents show replication statement.
	Replication
	// RootMetric represents show current root's metric statement
	RootMetric
	// BrokerMetric represents show current broker's metric statement
	BrokerMetric
	// StorageMetric represents show current storage's metric statement
	StorageMetric
	// MemoryDatabase represents show memory database statement.
	MemoryDatabase
)

// State represents show state statement.
type State struct {
	Type     StateType
	Database string

	MetricNames []string
}

// StatementType returns state query type.
func (q *State) StatementType() StatementType {
	return StateStatement
}
