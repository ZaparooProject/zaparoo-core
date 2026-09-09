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

package apidiag

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func receiveReport(t *testing.T, reports <-chan Report) Report {
	t.Helper()
	select {
	case report := <-reports:
		return report
	case <-time.After(5 * time.Second):
		t.Fatal("timeout diagnostic was not emitted")
		return Report{}
	}
}

func TestRecorderCapturesUnfinishedStagesAtDeadline(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	parent, cancel := clockwork.WithTimeout(t.Context(), clock, 30*time.Second)
	defer cancel()
	reports := make(chan Report, 4)
	var snapshots atomic.Int32
	ctx, recorder := New(parent, "MEDIA.SEARCH", WebSocket, 4*time.Second, func() Snapshot {
		snapshots.Add(1)
		return Snapshot{MediaDB: DatabaseSnapshot{Pool: Pool{Available: true, InUse: 2}}}
	}, func(report Report) { reports <- report }, clock)
	defer recorder.Finish()

	endHandler := Begin(ctx, Handler)
	clock.Advance(3 * time.Second)
	endDB := Begin(ctx, Database)
	clock.Advance(27 * time.Second)
	report := receiveReport(t, reports)

	assert.Equal(t, "media.search", report.Method)
	assert.Equal(t, RequestDeadline, report.Kind)
	assert.Equal(t, Database, report.Stage)
	assert.Equal(t, 30*time.Second, report.Budget)
	assert.Equal(t, 30*time.Second, report.Elapsed)
	assert.Equal(t, 4*time.Second, report.QueueWait)
	assert.Equal(t, 30*time.Second, report.Durations[Handler])
	assert.Equal(t, 27*time.Second, report.Durations[Database])
	assert.True(t, report.ActiveStages[Database])
	assert.Equal(t, int32(2), snapshots.Load())
	assert.Equal(t, 2, report.EndSnapshot.MediaDB.Pool.InUse)

	endDB()
	endDB()
	endHandler()
	RecordError(ctx, parent.Err())
	recorder.Finish()
	assert.Empty(t, reports, "callback, error handling and completion must not duplicate the report")
}

func TestRecorderNestedDeadlineDoesNotRetainErrorText(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	reports := make(chan Report, 2)
	ctx, recorder := New(t.Context(), "media.meta", HTTP, 0, nil,
		func(report Report) { reports <- report }, clock)
	defer recorder.Finish()
	end := Begin(ctx, Handler)
	clock.Advance(time.Second)
	privateErr := fmt.Errorf("PRIVATE_TOKEN /home/private/file: %w", context.DeadlineExceeded)
	RecordError(ctx, privateErr)
	end()
	report := receiveReport(t, reports)
	assert.Equal(t, OperationDeadline, report.Kind)
	assert.Equal(t, Handler, report.Stage)
	assert.Zero(t, report.Budget, "unbounded requests still diagnose operation timeouts")
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "PRIVATE_TOKEN")
	assert.NotContains(t, string(encoded), "/home/private")
	assert.Empty(t, reports)
}

func TestRecorderSuccessfulCompletionAndCancellationAreQuiet(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		t.Run(strconv.FormatBool(canceled), func(t *testing.T) {
			t.Parallel()
			clock := clockwork.NewFakeClock()
			parent, cancel := clockwork.WithTimeout(t.Context(), clock, time.Second)
			defer cancel()
			var count atomic.Int32
			ctx, recorder := New(parent, "media.search", HTTP, 0, nil,
				func(Report) { count.Add(1) }, clock)
			end := Begin(ctx, Handler)
			if canceled {
				cancel()
				<-parent.Done()
				RecordError(ctx, context.DeadlineExceeded)
			}
			end()
			recorder.Finish()
			clock.Advance(2 * time.Second)
			recorder.Report(RequestDeadline)
			assert.Zero(t, count.Load())
		})
	}
}

func TestRecorderConcurrentReportsAndRequestIsolation(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"media.search", "media.browse"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			clock := clockwork.NewFakeClock()
			reports := make(chan Report, 32)
			ctx, recorder := New(t.Context(), method, HTTP, 0, nil,
				func(report Report) { reports <- report }, clock)
			defer recorder.Finish()
			end := Begin(ctx, Database)
			var callers sync.WaitGroup
			for range 16 {
				callers.Go(func() { RecordError(ctx, context.DeadlineExceeded) })
			}
			callers.Wait()
			end()
			report := receiveReport(t, reports)
			assert.Equal(t, method, report.Method)
			assert.Equal(t, Database, report.Stage)
			assert.Empty(t, reports)
		})
	}
}

func TestRecorderDeadlineRacingFinishReportsOnce(t *testing.T) {
	t.Parallel()
	for range 25 {
		ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		reports := make(chan Report, 4)
		_, recorder := New(ctx, "media.search", HTTP, 0, nil,
			func(report Report) { reports <- report }, nil)
		recorder.Finish()
		receiveReport(t, reports)
		recorder.Finish()
		cancel()
		assert.Empty(t, reports)
	}
}

func TestRecorderBoundsActiveStages(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	reports := make(chan Report, 1)
	ctx, recorder := New(t.Context(), "media.search", HTTP, 0, nil,
		func(report Report) { reports <- report }, clock)
	defer recorder.Finish()
	ends := make([]func(), 0, 64)
	for range 64 {
		ends = append(ends, Begin(ctx, Database))
	}
	clock.Advance(time.Second)
	RecordError(ctx, context.DeadlineExceeded)
	report := receiveReport(t, reports)
	assert.Equal(t, 16*time.Second, report.Durations[Database])
	for _, end := range ends {
		end()
	}
}

func TestMethodNameAllowsOnlyFixedMethods(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "media.search", MethodName("MEDIA.SEARCH"))
	for _, method := range []string{"", "secret.token", "media.search?token=secret", "/home/private/file"} {
		assert.Equal(t, "unknown", MethodName(method))
	}
	assert.Equal(t, "unknown", Stage(255).String())
	assert.Equal(t, "unknown", Transport(255).String())
	assert.Equal(t, "unknown", DeadlineKind(255).String())
	assert.Equal(t, "unknown", State(255).String())
}

//nolint:staticcheck // Explicitly verify the optional diagnostics API's nil-context contract.
func TestMissingContextDiagnosticsAreNoOps(t *testing.T) {
	t.Parallel()
	assert.Nil(t, FromContext(nil))
	Begin(nil, Database)()
	RecordError(nil, context.DeadlineExceeded)
}
