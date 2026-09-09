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

package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type timeoutCaptureTransport struct {
	events chan *sentry.Event
}

func (*timeoutCaptureTransport) Configure(sentry.ClientOptions)        {}
func (*timeoutCaptureTransport) Flush(time.Duration) bool              { return true }
func (*timeoutCaptureTransport) FlushWithContext(context.Context) bool { return true }
func (*timeoutCaptureTransport) Close()                                {}
func (t *timeoutCaptureTransport) SendEvent(event *sentry.Event)       { t.events <- event }

func TestAPITimeoutEventDropsPrivateData(t *testing.T) {
	t.Parallel()
	const secret = "PRIVATE_CANARY_4839"
	event := sentry.NewEvent()
	event.Release = "zaparoo-core@test"
	event.Environment = "mister"
	event.Message = secret
	event.ServerName = secret
	event.Transaction = secret
	event.User = sentry.User{ID: secret, Username: secret, IPAddress: secret}
	event.Tags["request_id"] = secret
	event.Contexts["private"] = sentry.Context{"params": secret}
	event.Request = &sentry.Request{URL: "https://example.invalid/" + secret, Data: secret}
	event.Exception = []sentry.Exception{{Value: secret}}
	event.Breadcrumbs = []*sentry.Breadcrumb{{Message: secret}}
	event.Attachments = []*sentry.Attachment{{Filename: secret, Payload: []byte(secret)}}

	report := apidiag.Report{
		Method: secret, Transport: apidiag.WebSocket, Stage: apidiag.Database,
		Budget: 30 * time.Second, Elapsed: 30 * time.Second, QueueWait: time.Second,
		StartSnapshot: apidiag.Snapshot{MediaDB: apidiag.DatabaseSnapshot{
			Pool: apidiag.Pool{Available: true, WaitCount: 4, WaitDuration: time.Second},
		}},
		EndSnapshot: apidiag.Snapshot{MediaDB: apidiag.DatabaseSnapshot{
			Pool:     apidiag.Pool{Available: true, InUse: 3, WaitCount: 7, WaitDuration: 5 * time.Second},
			Indexing: apidiag.Active, Transaction: apidiag.Unknown,
		}},
	}
	report.ActiveStages[apidiag.Database] = true
	report.Durations[apidiag.Database] = 20 * time.Second
	got := beforeSendEvent(event, &sentry.EventHint{Data: report})
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), secret)
	assert.NotContains(t, string(encoded), "example.invalid")
	assert.Nil(t, got.Request)
	assert.Empty(t, got.Exception)
	assert.Empty(t, got.Threads)
	assert.Empty(t, got.User)
	assert.Empty(t, got.Attachments)
	assert.Empty(t, got.Breadcrumbs)
	assert.Equal(t, "API request timed out", got.Message)
	assert.Equal(t, "unknown", got.Tags["method"])
	assert.Equal(t, "zaparoo-core@test", got.Release)
	assert.Equal(t, "mister", got.Environment)
	assert.Equal(t, int64(30_000), got.Contexts["api_timeout"]["elapsed_ms"])
	assert.Equal(t, int64(3), got.Contexts["media_pool_wait_delta"]["wait_count"])
	assert.Equal(t, int64(4_000), got.Contexts["media_pool_wait_delta"]["wait_ms"])
	assert.Equal(t, "unknown", got.Contexts["activity_timeout"]["media_transaction"])
}

func TestCaptureAPITimeoutUsesCleanScopeAndConsent(t *testing.T) {
	// Global hub/client and consent state are isolated from parallel tests.
	const secret = "PRIVATE_SCOPE_CANARY"
	transport := &timeoutCaptureTransport{events: make(chan *sentry.Event, 4)}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: "https://public@example.invalid/1", Transport: transport,
		Release: "zaparoo-core@test", Environment: "mister", AttachStacktrace: true,
		BeforeSend: beforeSendEvent,
	})
	require.NoError(t, err)
	hub := sentry.CurrentHub()
	oldClient, oldEnabled := hub.Client(), enabled
	scope := hub.PushScope()
	hub.BindClient(client)
	t.Cleanup(func() {
		hub.PopScope()
		hub.BindClient(oldClient)
		enabled = oldEnabled
	})
	scope.SetUser(sentry.User{ID: secret, Username: secret})
	scope.SetTag("private", secret)
	scope.SetContext("params", sentry.Context{"body": secret})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"https://example.invalid/"+secret, http.NoBody)
	require.NoError(t, err)
	request.Header.Set("Authorization", secret)
	scope.SetRequest(request)
	scope.SetRequestBody([]byte(secret))
	scope.AddBreadcrumb(&sentry.Breadcrumb{Message: secret}, 10)
	scope.AddAttachment(&sentry.Attachment{Filename: secret, Payload: []byte(secret)})

	report := apidiag.Report{Method: "media.search", Stage: apidiag.Database}
	enabled = false
	CaptureAPITimeout(report)
	assert.Empty(t, transport.events)
	enabled = true
	CaptureAPITimeout(report)
	select {
	case event := <-transport.events:
		encoded, marshalErr := json.Marshal(event)
		require.NoError(t, marshalErr)
		assert.NotContains(t, string(encoded), secret)
		assert.Empty(t, event.User)
		assert.Nil(t, event.Request)
		assert.Empty(t, event.Attachments)
		assert.Equal(t, "media.search", event.Tags["method"])
		assert.Equal(t, []string{"api-timeout-v1", "media.search", "database", "request"}, event.Fingerprint)
	default:
		t.Fatal("fake transport did not receive timeout event")
	}
	assert.Empty(t, transport.events)
}

func TestAPITimeoutNumericBoundsAndResetCounters(t *testing.T) {
	t.Parallel()
	assert.Zero(t, diagnosticMillis(-time.Second))
	assert.Equal(t, int64(86_400_000), diagnosticMillis(48*time.Hour))
	assert.Zero(t, boundedCount(-1))
	assert.Equal(t, 1_000_000, boundedCount(2_000_000))
	assert.Equal(t, uint64(8), countBucket(5))
	assert.Equal(t, uint64(1<<30), countBucket(^uint64(0)))
	assert.Equal(t, sentry.Context{"available": false}, poolContext(apidiag.Pool{}))
	reset := poolDeltaContext(
		apidiag.Pool{Available: true, WaitCount: 10}, apidiag.Pool{Available: true, WaitCount: 1})
	assert.Equal(t, false, reset["available"])
	assert.Equal(t, true, reset["counters_reset"])
}
