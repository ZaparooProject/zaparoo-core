// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPrivateNetworkAccessMiddleware verifies that the middleware correctly
// handles Private Network Access preflight requests as specified in:
// https://wicg.github.io/private-network-access/
func TestPrivateNetworkAccessMiddleware(t *testing.T) {
	t.Parallel()

	handler := privateNetworkAccessMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name                    string
		method                  string
		requestPNAHeader        string
		expectPNAResponseHeader bool
	}{
		{
			name:                    "OPTIONS_with_PNA_request_header",
			method:                  http.MethodOptions,
			requestPNAHeader:        "true",
			expectPNAResponseHeader: true,
		},
		{
			name:                    "OPTIONS_without_PNA_request_header",
			method:                  http.MethodOptions,
			requestPNAHeader:        "",
			expectPNAResponseHeader: false,
		},
		{
			name:                    "GET_with_PNA_request_header",
			method:                  http.MethodGet,
			requestPNAHeader:        "true",
			expectPNAResponseHeader: false,
		},
		{
			name:                    "POST_with_PNA_request_header",
			method:                  http.MethodPost,
			requestPNAHeader:        "true",
			expectPNAResponseHeader: false,
		},
		{
			name:                    "OPTIONS_with_PNA_false",
			method:                  http.MethodOptions,
			requestPNAHeader:        "false",
			expectPNAResponseHeader: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, "/api", http.NoBody)
			if tt.requestPNAHeader != "" {
				req.Header.Set("Access-Control-Request-Private-Network", tt.requestPNAHeader)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			pnaResponse := rec.Header().Get("Access-Control-Allow-Private-Network")
			if tt.expectPNAResponseHeader {
				assert.Equal(t, "true", pnaResponse,
					"expected Access-Control-Allow-Private-Network: true header")
			} else {
				assert.Empty(t, pnaResponse,
					"expected no Access-Control-Allow-Private-Network header")
			}
		})
	}
}
