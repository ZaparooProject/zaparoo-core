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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func receiveAPIDiagnostic(t *testing.T, reports <-chan apidiag.Report) apidiag.Report {
	t.Helper()
	select {
	case report := <-reports:
		return report
	case <-time.After(5 * time.Second):
		t.Fatal("API timeout diagnostic not emitted")
		return apidiag.Report{}
	}
}

func TestAPIDiagnosticMethodAllowlistCoversRegisteredMethods(t *testing.T) {
	t.Parallel()
	for _, method := range NewMethodMap().ListMethods() {
		assert.Equal(t, method, apidiag.MethodName(method), "review privacy before adding a method to diagnostics")
	}
}

func TestHTTPTimeoutDiagnosticsPreserveFailureWithoutPrivateInputs(t *testing.T) {
	t.Parallel()
	handler, methodMap, tracker := createTestPostHandler(t)
	reports := make(chan apidiag.Report, 2)
	methodMap.timeoutReporter = func(report apidiag.Report) { reports <- report }
	methodMap.Store(models.MethodMediaSearch, methodDefinition{
		handler: func(requests.RequestEnv) (any, error) {
			return nil, fmt.Errorf("PRIVATE_ERROR /home/private/rom: %w", context.DeadlineExceeded)
		}, legacyAllowed: true,
	})
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api", strings.NewReader(
		`{"jsonrpc":"2.0","method":"media.search","id":"PRIVATE_ID","params":{"query":"PRIVATE_QUERY"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(response, request)
	report := receiveAPIDiagnostic(t, reports)
	assert.Equal(t, apidiag.HTTP, report.Transport)
	assert.Equal(t, apidiag.Handler, report.Stage)
	assert.Equal(t, apidiag.OperationDeadline, report.Kind)
	assert.Equal(t, "media.search", report.Method)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "PRIVATE_")
	assert.NotContains(t, string(encoded), "/home/private")
	assert.Contains(t, response.Body.String(), "PRIVATE_ERROR", "existing API error response must be preserved")
	assert.Equal(t, int64(1), tracker.ended.Load())
	assert.Empty(t, reports)
}

func TestHTTPDiagnosticsDeadlineAndDisconnect(t *testing.T) {
	t.Parallel()
	for _, deadline := range []bool{true, false} {
		t.Run(strconv.FormatBool(deadline), func(t *testing.T) {
			t.Parallel()
			handler, methodMap, _ := createTestPostHandler(t)
			reports := make(chan apidiag.Report, 2)
			methodMap.timeoutReporter = func(report apidiag.Report) { reports <- report }
			started := make(chan struct{})
			methodMap.Store(models.MethodMediaSearch, methodDefinition{
				handler: func(env requests.RequestEnv) (any, error) {
					close(started)
					<-env.Context.Done()
					return nil, env.Context.Err()
				}, legacyAllowed: true,
			})
			clock := clockwork.NewFakeClock()
			parent, cancel := clockwork.WithTimeout(t.Context(), clock, time.Second)
			defer cancel()
			request := httptest.NewRequestWithContext(parent, http.MethodPost, "/api", strings.NewReader(
				`{"jsonrpc":"2.0","method":"media.search","id":1}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { handler(response, request); close(done) }()
			<-started
			if deadline {
				clock.Advance(time.Second)
				report := receiveAPIDiagnostic(t, reports)
				assert.Equal(t, apidiag.RequestDeadline, report.Kind)
				assert.Equal(t, apidiag.Handler, report.Stage)
			} else {
				cancel()
			}
			<-done
			assert.Empty(t, reports, "disconnect must stay quiet; deadline must only report once")
		})
	}
}

type diagnosticWriteTimeout struct{ *httptest.ResponseRecorder }

func (*diagnosticWriteTimeout) Write([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }

func TestHTTPDiagnosticsResponseWriteDeadline(t *testing.T) {
	t.Parallel()
	handler, methodMap, _ := createTestPostHandler(t)
	reports := make(chan apidiag.Report, 2)
	methodMap.timeoutReporter = func(report apidiag.Report) { reports <- report }
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api", strings.NewReader(
		`{"jsonrpc":"2.0","method":"test.echo","id":1}`))
	request.Header.Set("Content-Type", "application/json")
	handler(&diagnosticWriteTimeout{httptest.NewRecorder()}, request)
	report := receiveAPIDiagnostic(t, reports)
	assert.Equal(t, "unknown", report.Method, "custom method names must not escape")
	assert.Equal(t, apidiag.ResponseWrite, report.Stage)
	assert.Equal(t, apidiag.OperationDeadline, report.Kind)
	assert.Empty(t, reports)
}

type diagnosticMarshalTimeout struct{}

func (diagnosticMarshalTimeout) MarshalJSON() ([]byte, error) { return nil, context.DeadlineExceeded }

func TestWebSocketDiagnosticsSeparateResponseBuild(t *testing.T) {
	t.Parallel()
	for _, encrypted := range []bool{false, true} {
		t.Run(strconv.FormatBool(encrypted), func(t *testing.T) {
			t.Parallel()
			reports := make(chan apidiag.Report, 2)
			ctx, recorder := apidiag.New(t.Context(), "media.search", apidiag.WebSocket, 0, nil,
				func(report apidiag.Report) { reports <- report }, nil)
			defer recorder.Finish()
			var session *apimiddleware.ClientSession
			if encrypted {
				session = &apimiddleware.ClientSession{}
			}
			err := sendWSEncryptedResponse(ctx, nil, session, models.NullRPCID, diagnosticMarshalTimeout{})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			report := receiveAPIDiagnostic(t, reports)
			assert.Equal(t, apidiag.ResponseBuild, report.Stage)
			assert.Equal(t, apidiag.OperationDeadline, report.Kind)
			assert.False(t, report.ActiveStages[apidiag.ResponseWrite])
			assert.Empty(t, reports)
		})
	}
}

func TestWebSocketDiagnosticsStayAliveThroughResponseQueue(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	parent, cancel := clockwork.WithTimeout(t.Context(), clock, time.Second)
	defer cancel()
	dispatcher := &wsSessionDispatcher{ctx: parent, responses: make(chan *wsResponseJob, 1)}
	methodMap := NewMethodMap()
	reports := make(chan apidiag.Report, 2)
	methodMap.timeoutReporter = func(report apidiag.Report) { reports <- report }
	methodMap.Store(models.MethodMediaSearch, methodDefinition{
		handler: func(requests.RequestEnv) (any, error) { return "ok", nil }, legacyAllowed: true,
	})
	tracker := &fakeRequestTracker{}
	tracker.RequestStarted()
	job := &wsRequestJob{
		methodMap: methodMap, env: &requests.RequestEnv{IsLocal: true},
		method: models.MethodMediaSearch, enqueuedAt: time.Now().Add(-2 * time.Second), tracker: tracker,
		msg: []byte(`{"jsonrpc":"2.0","method":"media.search","id":"PRIVATE_ID","params":{"query":"PRIVATE_QUERY"}}`),
	}
	dispatcher.runJob(job)
	assert.Empty(t, reports, "successful handler is not itself a timeout")
	clock.Advance(time.Second)
	report := receiveAPIDiagnostic(t, reports)
	assert.Equal(t, apidiag.WebSocket, report.Transport)
	assert.Equal(t, apidiag.ResponseQueue, report.Stage)
	assert.True(t, report.ActiveStages[apidiag.ResponseQueue])
	assert.False(t, report.ActiveStages[apidiag.Handler])
	assert.GreaterOrEqual(t, report.QueueWait, 2*time.Second)
	assert.LessOrEqual(t, report.Budget, time.Second, "queue wait precedes request deadline")
	dispatcher.drainQueuedResponses()
	assert.Equal(t, int64(1), tracker.ended.Load())
	assert.Empty(t, reports)
}

func TestWebSocketDiagnosticsCancellationAndCleanup(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	dispatcher := &wsSessionDispatcher{ctx: parent, responses: make(chan *wsResponseJob, 1)}
	methodMap := NewMethodMap()
	reports := make(chan apidiag.Report, 2)
	methodMap.timeoutReporter = func(report apidiag.Report) { reports <- report }
	started := make(chan struct{})
	methodMap.Store(models.MethodMediaSearch, methodDefinition{
		handler: func(env requests.RequestEnv) (any, error) {
			close(started)
			<-env.Context.Done()
			return nil, env.Context.Err()
		}, legacyAllowed: true,
	})
	tracker := &fakeRequestTracker{}
	tracker.RequestStarted()
	job := &wsRequestJob{
		methodMap: methodMap, env: &requests.RequestEnv{IsLocal: true}, tracker: tracker,
		method: models.MethodMediaSearch,
		msg:    []byte(`{"jsonrpc":"2.0","method":"media.search","id":1}`),
	}
	done := make(chan struct{})
	go func() { dispatcher.runJob(job); close(done) }()
	<-started
	cancel()
	<-done
	dispatcher.drainQueuedResponses()
	assert.Equal(t, int64(1), tracker.ended.Load())
	assert.Empty(t, reports)
}
