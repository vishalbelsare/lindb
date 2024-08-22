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

package api

import (
	"github.com/gin-gonic/gin"

	"github.com/lindb/common/pkg/http"

	"github.com/lindb/lindb/query"
)

var (
	RequestsPath = "/state/requests"
)

// RequestAPI represents request state related api.
type RequestAPI struct {
}

// NewRequestAPI creates a RequestAPI instance.
func NewRequestAPI() *RequestAPI {
	return &RequestAPI{}
}

// Register adds request state url route.
func (api *RequestAPI) Register(route gin.IRoutes) {
	route.GET(RequestsPath, api.GetAllAliveRequests)
}

// GetAllAliveRequests returns all alive request.
func (api *RequestAPI) GetAllAliveRequests(c *gin.Context) {
	http.OK(c, query.GetRequestManager().GetAliveRequests())
}
