// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"errors"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/google/uuid"
	"github.com/olahol/melody"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogSafeResponse(t *testing.T) {
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

// TestLogSafeResponse_BatchRedaction verifies batch-response branches log only
// an item count and never include per-item details.
func TestLogSafeResponse_BatchRedaction(t *testing.T) {
	const secretBlob = "AAAABASE64SECRETPAYLOAD=="
	textValue := secretBlob
	metaProp := models.MediaMetaPropertyItem{
		Text:        textValue,
		ContentType: "image/png",
	}
	metaItem := models.MediaMetaMediaResponse{
		Path: "/roms/snes/game.sfc",
		Properties: map[string]models.MediaMetaPropertyItem{
			"image-boxart": metaProp,
		},
	}

	tests := []struct {
		result         any
		name           string
		expectMsgFrag  string
		expectKeyFrag  string
		expectItemsVal string
	}{
		{
			name: "MediaMetaBatchResponse logs items count only",
			result: models.MediaMetaBatchResponse{
				Items: []models.MediaMetaBatchItemResponse{
					{Media: &metaItem},
				},
			},
			expectMsgFrag:  "media.meta batch",
			expectKeyFrag:  `"items":1`,
			expectItemsVal: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
			defer func() { log.Logger = originalLogger }()

			logSafeResponse(tt.result)

			out := buf.String()
			assert.Contains(t, out, tt.expectMsgFrag)
			assert.Contains(t, out, tt.expectKeyFrag)
			// The base64 payload from any per-item field must never reach the log.
			assert.NotContains(t, out, secretBlob, "batch redaction must not leak per-item payload")
		})
	}
}

// TestLogSafeResponse_DefaultOmitsBody verifies the default case logs only
// the result type and never serializes unknown response bodies, so a forgotten
// future response shape cannot leak base64 blobs.
func TestLogSafeResponse_DefaultOmitsBody(t *testing.T) {
	type secretShape struct {
		Marker string `json:"marker"`
		Blob   string `json:"blob"`
	}

	var buf bytes.Buffer
	originalLogger := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	defer func() { log.Logger = originalLogger }()

	logSafeResponse(secretShape{Marker: "should-not-appear", Blob: "AAAABASE64=="})

	out := buf.String()
	assert.Contains(t, out, "sending response")
	assert.Contains(t, out, "secretShape", "default case must include the type name")
	assert.NotContains(t, out, "should-not-appear", "default case must omit body")
	assert.NotContains(t, out, "AAAABASE64==", "default case must omit body")
}

// TestHandleResponse covers inbound-response logging: that resp.Result is
// never serialized, that the error block is included only when present, and
// that long error messages are truncated with metadata indicating it.
func TestHandleResponse(t *testing.T) {
	const longMsg = "x"
	tests := []struct {
		name              string
		resp              models.ResponseObject
		mustNotContain    []string
		mustContain       []string
		expectTruncatedOn bool
	}{
		{
			name: "result is never logged",
			resp: models.ResponseObject{
				JSONRPC: "2.0",
				ID:      models.NewStringID("req-1"),
				Result:  map[string]any{"data": "AAAABASE64BLOB==", "secret": "hush"},
			},
			mustContain:    []string{"received response", "req-1"},
			mustNotContain: []string{"AAAABASE64BLOB==", "hush"},
		},
		{
			name: "no error means no error fields",
			resp: models.ResponseObject{
				JSONRPC: "2.0",
				ID:      models.NewStringID("req-2"),
			},
			mustContain:    []string{"received response", "req-2"},
			mustNotContain: []string{"errorCode", "errorMessage"},
		},
		{
			name: "short error message is logged in full",
			resp: models.ResponseObject{
				JSONRPC: "2.0",
				ID:      models.NewStringID("req-3"),
				Error: &models.ErrorObject{
					Code:    -32600,
					Message: "invalid request",
				},
			},
			mustContain: []string{
				"errorCode", "-32600",
				"errorMessage", "invalid request",
			},
			mustNotContain: []string{"errorMessageTruncated", "errorMessageLen"},
		},
		{
			name: "long error message is truncated",
			resp: models.ResponseObject{
				JSONRPC: "2.0",
				ID:      models.NewStringID("req-4"),
				Error: &models.ErrorObject{
					Code:    -32000,
					Message: strings.Repeat(longMsg, 5000),
				},
			},
			mustContain: []string{
				"errorCode",
				"errorMessageTruncated",
				"errorMessageLen",
			},
			expectTruncatedOn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
			defer func() { log.Logger = originalLogger }()

			require.NoError(t, handleResponse(tt.resp))

			out := buf.String()
			for _, s := range tt.mustContain {
				assert.Contains(t, out, s)
			}
			for _, s := range tt.mustNotContain {
				assert.NotContains(t, out, s)
			}
			if tt.expectTruncatedOn {
				// Truncated body must not contain the full repeated payload.
				assert.NotContains(t, out, strings.Repeat(longMsg, 5000))
				assert.Contains(t, out, `"errorMessageTruncated":true`)
			}
		})
	}
}

func TestLogSafeRequest(t *testing.T) {
	testID := models.NewStringID(uuid.New().String())

	tests := []struct {
		name    string
		request models.RequestObject
	}{
		{
			name: "logs download request should log method only",
			request: models.RequestObject{
				Method:  models.MethodSettingsLogsDownload,
				ID:      testID,
				JSONRPC: "2.0",
			},
		},
		{
			name: "other requests should log method only",
			request: models.RequestObject{
				Method:  models.MethodSettings,
				ID:      testID,
				JSONRPC: "2.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture log output
			var buf bytes.Buffer
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)

			// Test the function
			logSafeRequest(&tt.request)

			// Restore original logger
			log.Logger = originalLogger

			logOutput := buf.String()

			assert.Contains(t, logOutput, "received request")
			assert.Contains(t, logOutput, tt.request.Method)
			// Should not contain full request params
			assert.NotContains(t, logOutput, "jsonrpc")
		})
	}
}

func TestLogWSWriteError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		expectLevel   string
		unexpectLevel string
	}{
		{
			name:          "session closed logs at warn level",
			err:           melody.ErrSessionClosed,
			expectLevel:   `"level":"warn"`,
			unexpectLevel: `"level":"error"`,
		},
		{
			name:          "wrapped session closed logs at warn level",
			err:           errors.Join(errors.New("write failed"), melody.ErrSessionClosed),
			expectLevel:   `"level":"warn"`,
			unexpectLevel: `"level":"error"`,
		},
		{
			name:          "other error logs at error level",
			err:           errors.New("unexpected write failure"),
			expectLevel:   `"level":"error"`,
			unexpectLevel: `"level":"warn"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf)

			logWSWriteError(tt.err, "test message")

			log.Logger = originalLogger

			logOutput := buf.String()
			assert.Contains(t, logOutput, tt.expectLevel)
			assert.NotContains(t, logOutput, tt.unexpectLevel)
			assert.Contains(t, logOutput, "test message")
		})
	}
}
