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

package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lindb/lindb/config"
)

func Test_ParseToken(t *testing.T) {
	user := config.User{UserName: "admin", Password: "admin123"}
	claim := parseToken(tokenStr, user)
	assert.Equal(t, user.UserName, claim.UserName)
	assert.Equal(t, user.Password, claim.Password)
}

func TestUserAuthentication_Validate(t *testing.T) {
	req, err := http.NewRequestWithContext(context.TODO(), "GET", "/health-check", bytes.NewReader([]byte("test")))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer abc123")

	rr := httptest.NewRecorder()
	user := config.User{UserName: "admin", Password: "admin123"}
	auth := NewAuthentication(user)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "ok")
	})
	authHandler := auth.Validate(handler)

	authHandler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	req, err = http.NewRequestWithContext(context.TODO(), "GET", "/health-check", bytes.NewReader([]byte("test")))
	assert.NoError(t, err)
	req.Header.Set("Authorization", tokenStr)
	rr = httptest.NewRecorder()

	authHandler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "ok", rr.Body.String())
}
