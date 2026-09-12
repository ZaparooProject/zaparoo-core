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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/getsentry/sentry-go"
)

// Serialize offline to include payload encoding without sending to Sentry. Real
// network transport queues/encodes asynchronously, so this is not network latency.
type benchmarkTimeoutTransport struct{ events atomic.Int64 }

func (*benchmarkTimeoutTransport) Configure(sentry.ClientOptions)        {}
func (*benchmarkTimeoutTransport) Flush(time.Duration) bool              { return true }
func (*benchmarkTimeoutTransport) FlushWithContext(context.Context) bool { return true }
func (*benchmarkTimeoutTransport) Close()                                {}
func (t *benchmarkTimeoutTransport) SendEvent(event *sentry.Event) {
	if _, err := json.Marshal(event); err != nil {
		panic(err)
	}
	t.events.Add(1)
}

func setupTimeoutBenchmark(b *testing.B) *benchmarkTimeoutTransport {
	b.Helper()
	transport := &benchmarkTimeoutTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: "https://public@example.invalid/1", Transport: transport,
		Release: "benchmark", Environment: "mister", AttachStacktrace: true, BeforeSend: beforeSendEvent,
	})
	if err != nil {
		b.Fatal(err)
	}
	hub := sentry.CurrentHub()
	oldClient, oldEnabled := hub.Client(), enabled
	hub.BindClient(client)
	enabled = true
	b.Cleanup(func() { hub.BindClient(oldClient); enabled = oldEnabled })
	return transport
}

func BenchmarkAPITimeoutCapture(b *testing.B) {
	transport := setupTimeoutBenchmark(b)
	report := apidiag.Report{Method: "media.search", Stage: apidiag.Database, Budget: 30 * time.Second}
	b.ReportAllocs()
	for b.Loop() {
		CaptureAPITimeout(report)
	}
	if transport.events.Load() != int64(b.N) {
		b.Fatal("capture count mismatch")
	}
}

func BenchmarkAPITimeoutBurst(b *testing.B) {
	transport := setupTimeoutBenchmark(b)
	const width = 64
	b.ReportAllocs()
	for b.Loop() {
		var done, workers sync.WaitGroup
		done.Add(width)
		start := make(chan struct{})
		for range width {
			workers.Go(func() {
				<-start
				ctx, cancel := context.WithDeadline(b.Context(), time.Now().Add(-time.Second))
				ctx, recorder := apidiag.New(ctx, "media.search", apidiag.WebSocket, 0, nil,
					func(report apidiag.Report) { CaptureAPITimeout(report); done.Done() }, nil)
				end := apidiag.Begin(ctx, apidiag.DatabaseLock)
				apidiag.RecordError(ctx, ctx.Err())
				end()
				recorder.Finish()
				cancel()
			})
		}
		close(start)
		workers.Wait()
		done.Wait()
	}
	b.ReportMetric(width, "events/op")
	if transport.events.Load() != int64(b.N)*width {
		b.Fatal("burst capture count mismatch")
	}
}
