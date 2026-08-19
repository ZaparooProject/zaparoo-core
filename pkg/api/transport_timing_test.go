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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findTransportLogEvent(t *testing.T, output, message string) map[string]any {
	t.Helper()
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event["message"] == message {
			return event
		}
	}
	t.Fatalf("log event %q not found in %s", message, output)
	return nil
}

func TestLogWebSocketTransportTimingFields(t *testing.T) {
	tests := []struct {
		writeErr     error
		name         string
		responseType string
		encrypted    bool
	}{
		{name: "plaintext result success", responseType: "result"},
		{name: "plaintext error failure", responseType: "error", writeErr: errors.New("write failed")},
		{name: "encrypted result success", responseType: "result", encrypted: true},
		{
			name: "encrypted error failure", responseType: "error", encrypted: true,
			writeErr: errors.New("write failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf logCapture
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
			defer func() { log.Logger = originalLogger }()

			logWebSocketTransportTiming(
				models.NewStringID("request-1"), tt.responseType, tt.encrypted, 321,
				2*time.Millisecond, 3*time.Millisecond, tt.writeErr,
			)

			event := findTransportLogEvent(t, buf.String(), "websocket response transport timing")
			assert.Equal(t, requestIDForLog(models.NewStringID("request-1")), event["requestId"])
			assert.Equal(t, tt.responseType, event["responseType"])
			assert.Equal(t, tt.encrypted, event["encrypted"])
			assert.InDelta(t, 321, event["responseBytes"], 0)
			assert.Contains(t, event, "marshalDuration")
			assert.Contains(t, event, "writeDuration")
			if tt.writeErr != nil {
				assert.Equal(t, tt.writeErr.Error(), event["error"])
			} else {
				assert.NotContains(t, event, "error")
			}
		})
	}
}

func TestHTTPResponseTransportTimingFields(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		responseType string
	}{
		{name: "result", method: "test.echo", responseType: "result"},
		{name: "error", method: "test.error", responseType: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _, _ := createTestPostHandler(t)
			var buf logCapture
			originalLogger := log.Logger
			log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
			defer func() { log.Logger = originalLogger }()

			body := `{"jsonrpc":"2.0","id":"transport-id","method":"` + tt.method + `"}`
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			handler(recorder, req)
			require.Equal(t, http.StatusOK, recorder.Code)

			event := findTransportLogEvent(t, buf.String(), "http response transport timing")
			assert.Equal(t, tt.method, event["method"])
			assert.Equal(t, requestIDForLog(models.NewStringID("transport-id")), event["requestId"])
			assert.Equal(t, tt.responseType, event["responseType"])
			assert.Positive(t, event["responseBytes"])
			assert.Equal(t, event["responseBytes"], event["writtenBytes"])
			assert.Contains(t, event, "marshalDuration")
			assert.Contains(t, event, "writeDuration")
			assert.Equal(t, false, event["writeError"])
		})
	}
}

func TestWebSocketDispatcherQueueMetadata(t *testing.T) {
	assert.Zero(t, queueDuration(time.Time{}))
	assert.Positive(t, queueDuration(time.Now().Add(-time.Millisecond)))

	d := &wsSessionDispatcher{
		ctx:       t.Context(),
		high:      make(chan *wsRequestJob, 1),
		normal:    make(chan *wsRequestJob, 1),
		low:       make(chan *wsRequestJob, 1),
		responses: make(chan *wsResponseJob, 1),
	}
	var methodMap MethodMap
	env := &requests.RequestEnv{Context: context.Background()}
	require.NoError(t, enqueueWSRequest(
		d, &methodMap, env,
		[]byte(`{"jsonrpc":"2.0","method":"media.meta","id":"queue-id"}`),
		nil, nil,
	))
	requestJob := <-d.normal
	assert.Equal(t, models.MethodMediaMeta, requestJob.method)
	assert.Equal(t, models.NewStringID("queue-id"), requestJob.requestID)
	assert.False(t, requestJob.enqueuedAt.IsZero())

	responseJob := &wsResponseJob{method: requestJob.method, result: requestResult{ShouldReply: false}}
	d.enqueueResponse(responseJob)
	queuedResponse := <-d.responses
	assert.Equal(t, models.MethodMediaMeta, queuedResponse.method)
	assert.False(t, queuedResponse.enqueuedAt.IsZero())

	require.NoError(t, d.enqueuePong(nil, nil))
	pong := <-d.responses
	assert.True(t, pong.pong)
	assert.Equal(t, "ping", pong.method)
	assert.False(t, pong.enqueuedAt.IsZero())
}

func TestWebSocketDispatcherQueueTimingLogs(t *testing.T) {
	var buf logCapture
	originalLogger := log.Logger
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	defer func() { log.Logger = originalLogger }()

	var methodMap MethodMap
	require.NoError(t, methodMap.AddMethod("test.queue", func(requests.RequestEnv) (any, error) {
		return map[string]bool{"ok": true}, nil
	}))
	d := &wsSessionDispatcher{ctx: t.Context(), responses: make(chan *wsResponseJob, 1)}
	job := &wsRequestJob{
		methodMap:  &methodMap,
		env:        &requests.RequestEnv{Context: t.Context()},
		enqueuedAt: time.Now().Add(-time.Millisecond),
		requestID:  models.NewStringID("queue-log-id"),
		method:     "test.queue",
		msg:        []byte(`{"jsonrpc":"2.0","method":"test.queue","id":"queue-log-id"}`),
	}
	d.runJob(job)
	response := <-d.responses
	assert.False(t, response.enqueuedAt.IsZero())

	requestEvent := findTransportLogEvent(t, buf.String(), "websocket request dequeued")
	assert.Equal(t, "test.queue", requestEvent["method"])
	assert.Equal(t, requestIDForLog(models.NewStringID("queue-log-id")), requestEvent["requestId"])
	assert.Contains(t, requestEvent, "queueWaitDuration")

	response.result.ShouldReply = false
	d.writeResponse(response)
	responseEvent := findTransportLogEvent(t, buf.String(), "websocket response dequeued")
	assert.Equal(t, "test.queue", responseEvent["method"])
	assert.Equal(t, requestIDForLog(models.NewStringID("queue-log-id")), responseEvent["requestId"])
	assert.Contains(t, responseEvent, "responseQueueDuration")
}
