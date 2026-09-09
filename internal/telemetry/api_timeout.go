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
	"runtime/metrics"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/getsentry/sentry-go"
)

var diagnosticsStarted = time.Now()

// CaptureAPITimeout uses a fresh scope, not the current request's scope. The
// BeforeSend boundary rebuilds the event from this typed, content-free report.
//
//nolint:gocritic // The callback owns an immutable value snapshot, not recorder state.
func CaptureAPITimeout(report apidiag.Report) {
	if !Enabled() {
		return
	}
	client := sentry.CurrentHub().Client()
	if client == nil {
		return
	}
	event := sentry.NewEvent()
	event.Message = "API request timed out"
	event.Level = sentry.LevelError
	client.CaptureEvent(event, &sentry.EventHint{Data: report}, sentry.NewScope())
}

func apiTimeoutEvent(original *sentry.Event, report *apidiag.Report) *sentry.Event {
	event := sentry.NewEvent()
	// These are SDK/build metadata, not values copied from the API request.
	event.EventID = original.EventID
	event.Timestamp = original.Timestamp
	event.Release = original.Release
	event.Environment = original.Environment
	event.Platform = "go"
	event.Level = sentry.LevelError
	event.Logger = "api"
	event.Message = "API request timed out"
	method := apidiag.MethodName(report.Method)
	event.Fingerprint = []string{"api-timeout-v1", method, report.Stage.String(), report.Kind.String()}
	event.Tags = map[string]string{
		"method": method, "transport": report.Transport.String(),
		"timeout_stage": report.Stage.String(), "deadline_kind": report.Kind.String(),
	}
	durations := make(map[string]int64, len(report.Durations))
	active := make([]string, 0, len(report.ActiveStages))
	for stage, duration := range report.Durations {
		name := apidiag.Stage(stage).String()
		durations[name] = diagnosticMillis(duration)
		if report.ActiveStages[stage] {
			active = append(active, name)
		}
	}
	event.Contexts = map[string]sentry.Context{
		"api_timeout": {
			"schema": 1, "budget_ms": diagnosticMillis(report.Budget),
			"elapsed_ms": diagnosticMillis(report.Elapsed), "queue_wait_ms": diagnosticMillis(report.QueueWait),
			"stage_ms": durations, "active_stages": active,
		},
		"activity_start":        activityContext(&report.StartSnapshot),
		"activity_timeout":      activityContext(&report.EndSnapshot),
		"media_pool_start":      poolContext(report.StartSnapshot.MediaDB.Pool),
		"media_pool_timeout":    poolContext(report.EndSnapshot.MediaDB.Pool),
		"media_pool_wait_delta": poolDeltaContext(report.StartSnapshot.MediaDB.Pool, report.EndSnapshot.MediaDB.Pool),
		"user_pool_start":       poolContext(report.StartSnapshot.UserDB.Pool),
		"user_pool_timeout":     poolContext(report.EndSnapshot.UserDB.Pool),
		"user_pool_wait_delta":  poolDeltaContext(report.StartSnapshot.UserDB.Pool, report.EndSnapshot.UserDB.Pool),
		"resource_pressure":     runtimeDiagnosticContext(),
	}
	// No inherited user, request, exception, breadcrumbs, stack, or extra data.
	return event
}

func diagnosticMillis(duration time.Duration) int64 {
	return min(max(duration, 0), 24*time.Hour).Milliseconds()
}

func poolContext(pool apidiag.Pool) sentry.Context {
	if !pool.Available {
		return sentry.Context{"available": false}
	}
	return sentry.Context{
		"available": true, "max": boundedCount(pool.Max), "open": boundedCount(pool.Open),
		"in_use": boundedCount(pool.InUse), "idle": boundedCount(pool.Idle),
	}
}

func poolDeltaContext(start, end apidiag.Pool) sentry.Context {
	if !start.Available || !end.Available {
		return sentry.Context{"available": false}
	}
	reset := end.WaitCount < start.WaitCount || end.WaitDuration < start.WaitDuration
	if reset {
		return sentry.Context{"available": false, "counters_reset": true}
	}
	return sentry.Context{
		"available": true, "shared_pool_counters": true,
		"wait_count": min(max(end.WaitCount-start.WaitCount, 0), 1_000_000),
		"wait_ms":    diagnosticMillis(end.WaitDuration - start.WaitDuration),
	}
}

func boundedCount(count int) int {
	return min(max(count, 0), 1_000_000)
}

func activityContext(snapshot *apidiag.Snapshot) sentry.Context {
	return sentry.Context{
		"indexing": snapshot.MediaDB.Indexing.String(), "optimizing": snapshot.MediaDB.Optimizing.String(),
		"database_recovery": snapshot.MediaDB.Recovery.String(), "service_recovery": snapshot.Recovery.String(),
		"scraping": snapshot.Scraping.String(), "media_playing": snapshot.MediaPlaying.String(),
		"media_transaction": snapshot.MediaDB.Transaction.String(),
	}
}

// Runtime metrics are in-memory counters. Avoid ReadMemStats, filesystem probes,
// SQL, or profiling on an already overloaded request's timeout path.
func runtimeDiagnosticContext() sentry.Context {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/sched/goroutines:goroutines"},
	}
	metrics.Read(samples)
	uptime := uint64(time.Since(diagnosticsStarted).Seconds())
	result := sentry.Context{"process_age_seconds_bucket": countBucket(uptime)}
	if samples[0].Value.Kind() == metrics.KindUint64 {
		result["heap_mib_bucket"] = countBucket(samples[0].Value.Uint64() / (1024 * 1024))
	}
	if samples[1].Value.Kind() == metrics.KindUint64 {
		result["goroutines_bucket"] = countBucket(samples[1].Value.Uint64())
	}
	return result
}

func countBucket(value uint64) uint64 {
	if value == 0 {
		return 0
	}
	bucket := uint64(1)
	for bucket < value && bucket < 1<<30 {
		bucket <<= 1
	}
	return bucket
}
