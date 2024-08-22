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
	"encoding/binary"
	"math/bits"
)

const (
	wordSize     = 64
	log2WordSize = 6
)

var endian = binary.LittleEndian

// A precomputed tabled containing the positions of the set bits in the binary
// representations of all 8-bit unsigned integers.
//
// For i: [0, 256) ranging over all 8-bit unsigned integers and for j: [0, 8)
// ranging over all 0-based bit positions in an 8-bit unsigned integer, the
// table entry selectInByteLut[i][j] is the 0-based bit position of the j-th set
// bit in the binary representation of i, or 8 if it has fewer than j set bits.
//
// Example: i: 17 (b00010001), j: [0, 8)
//
//	selectInByteLut[b00010001][0] = 0
//	selectInByteLut[b00010001][1] = 4
//	selectInByteLut[b00010001][2] = 8
//	...
//	selectInByteLut[b00010001][7] = 8
var selectInByteLut [256][8]uint8

func init() {
	for i := 0; i < 256; i++ {
		for j := 0; j < 8; j++ {
			selectInByteLut[i][j] = selectInByte(i, j)
		}
	}
}

func findFirstSet(x int) int {
	return bits.TrailingZeros64(uint64(x)) + 1
}

func selectInByte(i, j int) uint8 {
	r := 0
	for ; j != 0; j-- {
		s := findFirstSet(i)
		r += s
		i >>= s
	}
	if i == 0 {
		return 8
	}
	return uint8(r + findFirstSet(i) - 1)
}

func select64Broadword(x uint64, nth int64) int64 {
	const (
		onesStep4 = uint64(0x1111111111111111)
		onesStep8 = uint64(0x0101010101010101)
		msbsStep8 = uint64(0x80) * onesStep8
	)

	k := uint64(nth - 1)
	s := x
	s -= (s & (0xa * onesStep4)) >> 1
	s = (s & (0x3 * onesStep4)) + ((s >> 2) & (0x3 * onesStep4))
	s = (s + (s >> 4)) & (0xf * onesStep8)
	byteSums := s * onesStep8

	step8 := k * onesStep8
	geqKStep8 := ((step8 | msbsStep8) - byteSums) & msbsStep8
	place := bits.OnesCount64(geqKStep8) * 8
	byteRank := k - (((byteSums << 8) >> place) & uint64(0xff))
	return int64(place + int(selectInByteLut[(x>>place)&0xff][byteRank]))
}

func popcountBlock(bs []uint64, off, nbits uint32) uint32 {
	if nbits == 0 {
		return 0
	}

	lastWord := (nbits - 1) / wordSize
	lastBits := (nbits - 1) % wordSize
	var i, p uint32

	for i = 0; i < lastWord; i++ {
		p += uint32(bits.OnesCount64(bs[off+i]))
	}
	last := bs[off+lastWord] << (wordSize - 1 - lastBits)
	return p + uint32(bits.OnesCount64(last))
}

func readBit(bs []uint64, pos uint32) bool {
	wordOff := pos / wordSize
	bitsOff := pos % wordSize
	return bs[wordOff]&(uint64(1)<<bitsOff) != 0
}

func setBit(bs []uint64, pos uint32) {
	wordOff := pos / wordSize
	bitsOff := pos % wordSize
	bs[wordOff] |= uint64(1) << bitsOff
}
