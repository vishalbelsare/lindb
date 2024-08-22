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

package encoding

import "github.com/lindb/roaring"

// for testing
var (
	BitmapMarshal   = bitmapMarshal
	BitmapUnmarshal = bitmapUnmarshal
)

// bitmapMarshal marshals the bitmap data for testing
func bitmapMarshal(bitmap *roaring.Bitmap) ([]byte, error) {
	return bitmap.MarshalBinary()
}

// bitmapUnmarshal unmarshal the bitmap from data for testing
func bitmapUnmarshal(bitmap *roaring.Bitmap, data []byte) (int64, error) {
	return bitmap.FromBuffer(data)
}
