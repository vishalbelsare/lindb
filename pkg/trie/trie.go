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

package trie

import (
	"io"
	"sync"
)

var (
	triePool = sync.Pool{
		New: func() any {
			return NewTrie()
		},
	}
)

func GetTrie() SuccinctTrie {
	return triePool.Get().(SuccinctTrie)
}

func PutTrie(trie SuccinctTrie) {
	if trie != nil {
		triePool.Put(trie)
	}
}

type trie struct {
	totalKeys uint32
	height    uint32

	labelVec    labelVector
	hasChildVec rankVectorSparse
	loudsVec    selectVector
	values      valueVector
	prefixVec   prefixVector
	suffixVec   suffixVector
}

// NewTrie returns a new empty SuccinctTrie
func NewTrie() SuccinctTrie {
	return &trie{}
}

func (tree *trie) Init(builder *builder) *trie {
	tree.height = uint32(builder.height)
	tree.totalKeys = uint32(builder.totalCount)

	tree.labelVec.Init(builder.levels, tree.sparseLevels())
	tree.hasChildVec.Init(builder.levels, HasChild)
	tree.loudsVec.Init(builder.levels, Louds)
	tree.prefixVec.Init(builder.levels, HasPrefix)
	tree.suffixVec.Init(builder.levels, HasSuffix)
	tree.values.Init(builder.levels)
	return tree
}

func (tree *trie) Get(key []byte) (value uint32, ok bool) {
	var (
		nodeID    uint32
		pos       = tree.firstLabelPos(nodeID)
		depth     uint32
		prefixLen uint32
		exhausted bool
	)
	for depth = 0; depth < uint32(len(key)); depth++ {
		prefixLen, ok = tree.prefixVec.CheckPrefix(key, depth, tree.prefixID(nodeID))
		if !ok {
			return 0, false
		}
		depth += prefixLen

		if depth >= uint32(len(key)) {
			exhausted = true
			break
		}

		if pos, ok = tree.labelVec.Search(key[depth], pos, tree.nodeSize(pos)); !ok {
			return 0, false
		}
		if !tree.hasChildVec.IsSet(pos) {
			if ok = tree.suffixVec.CheckSuffix(key, depth, pos); ok {
				valPos := tree.valuePos(pos)
				value = tree.values.Get(valPos)
				ok = true
			}
			return value, ok
		}

		nodeID = tree.childNodeID(pos)
		pos = tree.firstLabelPos(nodeID)
	}
	// key is not exhausted, re-check the prefix
	if !exhausted {
		_, ok = tree.prefixVec.CheckPrefix(key, depth, tree.prefixID(nodeID))
		if !ok {
			return 0, false
		}
	}

	if tree.labelVec.GetLabel(pos) == labelTerminator && !tree.hasChildVec.IsSet(pos) {
		if ok = tree.suffixVec.CheckSuffix(key, depth, pos); ok {
			valPos := tree.valuePos(pos)
			value = tree.values.Get(valPos)
		}
		return value, ok
	}

	return 0, false
}

func (tree *trie) Size() int {
	return int(tree.totalKeys)
}

func (tree *trie) Values() []uint32 {
	return tree.values.values
}

func (tree *trie) UnmarshalBinary(buf []byte) (err error) {
	if len(buf) <= 8 {
		return io.EOF
	}
	buf1 := buf
	tree.totalKeys = endian.Uint32(buf1)
	buf1 = buf1[4:]
	tree.height = endian.Uint32(buf1)
	buf1 = buf1[4:]

	if buf1, err = tree.labelVec.Unmarshal(buf1); err != nil {
		return err
	}
	if buf1, err = tree.hasChildVec.Unmarshal(buf1); err != nil {
		return err
	}
	if buf1, err = tree.loudsVec.Unmarshal(buf1); err != nil {
		return err
	}
	if buf1, err = tree.prefixVec.Unmarshal(buf1); err != nil {
		return err
	}
	if buf1, err = tree.suffixVec.Unmarshal(buf1); err != nil {
		return err
	}
	if _, err = tree.values.Unmarshal(int(tree.totalKeys), buf1); err != nil {
		return err
	}
	return nil
}

func (tree *trie) NewIterator() *Iterator {
	itr := new(Iterator)
	itr.init(tree)
	return itr
}

func (tree *trie) NewPrefixIterator(prefix []byte) *PrefixIterator {
	rawItr := tree.NewIterator()
	rawItr.Seek(prefix)
	return &PrefixIterator{prefix: prefix, it: rawItr}
}

func (tree *trie) valuePos(pos uint32) uint32 {
	return pos - tree.hasChildVec.Rank(pos)
}

func (tree *trie) firstLabelPos(nodeID uint32) uint32 {
	return tree.loudsVec.Select(nodeID + 1)
}

func (tree *trie) sparseLevels() uint32 {
	return tree.height
}

func (tree *trie) prefixID(nodeID uint32) uint32 {
	return nodeID
}

func (tree *trie) lastLabelPos(nodeID uint32) uint32 {
	nextRank := nodeID + 2
	if nextRank > tree.loudsVec.numOnes {
		return tree.loudsVec.numBits - 1
	}
	return tree.loudsVec.Select(nextRank) - 1
}

func (tree *trie) childNodeID(pos uint32) uint32 {
	return tree.hasChildVec.Rank(pos)
}

func (tree *trie) nodeSize(pos uint32) uint32 {
	return tree.loudsVec.DistanceToNextSetBit(pos)
}

func (tree *trie) isEndOfNode(pos uint32) bool {
	return pos == tree.loudsVec.numBits-1 || tree.loudsVec.IsSet(pos+1)
}
