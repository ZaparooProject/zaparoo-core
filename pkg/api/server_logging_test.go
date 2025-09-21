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
	"bytes"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func TestLogSafeResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		result         any
		name           string
		expectTruncate bool
	}{
		{
			name: "non-LogDownloadResponse should log normally",
			result: map[string]string{
				"test": "normal response",
			},
			expectTruncate: false,
		},
		{
			name: "small LogDownloadResponse should not truncate",
			result: models.LogDownloadResponse{
				Filename: "test.log",
				Size:     50,
				Content:  "small content",
			},
			expectTruncate: false,
		},
		{
			name: "large LogDownloadResponse should truncate",
			result: models.LogDownloadResponse{
				Filename: "test.log",
				Size:     200,
				Content:  strings.Repeat("a", 150),
			},
			expectTruncate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Capture log output
			var buf bytes.Buffer
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)

			// Test the function
			logSafeResponse(tt.result)

			// Restore original logger
			log.Logger = originalLogger

			logOutput := buf.String()

			if tt.expectTruncate {
				assert.Contains(t, logOutput, "truncated")
				assert.Contains(t, logOutput, "more chars")
				// Should not contain the full original content
				if resp, ok := tt.result.(models.LogDownloadResponse); ok {
					assert.NotContains(t, logOutput, resp.Content)
				}
			} else {
				assert.NotContains(t, logOutput, "truncated")
			}

			// Should always contain the debug message
			assert.Contains(t, logOutput, "sending response")
		})
	}
}

func TestLogSafeRequest(t *testing.T) {
	t.Parallel()

	testID := uuid.New()

	tests := []struct {
		name              string
		request           models.RequestObject
		expectMethodOnly  bool
		expectFullRequest bool
	}{
		{
			name: "logs download request should log method only",
			request: models.RequestObject{
				Method:  models.MethodSettingsLogsDownload,
				ID:      &testID,
				JSONRPC: "2.0",
			},
			expectMethodOnly:  true,
			expectFullRequest: false,
		},
		{
			name: "other requests should log full request",
			request: models.RequestObject{
				Method:  models.MethodSettings,
				ID:      &testID,
				JSONRPC: "2.0",
			},
			expectMethodOnly:  false,
			expectFullRequest: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Capture log output
			var buf bytes.Buffer
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)

			// Test the function
			logSafeRequest(tt.request)

			// Restore original logger
			log.Logger = originalLogger

			logOutput := buf.String()

			if tt.expectMethodOnly {
				assert.Contains(t, logOutput, "received logs download request")
				assert.Contains(t, logOutput, tt.request.Method)
				// Should not contain full request data
				assert.NotContains(t, logOutput, "jsonrpc")
			}

			if tt.expectFullRequest {
				assert.Contains(t, logOutput, "received request")
				assert.Contains(t, logOutput, tt.request.Method)
				assert.Contains(t, logOutput, "jsonrpc")
			}
		})
	}
}
