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

package mediadb

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// browseTimingLine returns the single "browse call timing" record in captured
// zerolog output, or reports that none was emitted.
func browseTimingLine(t *testing.T, out string) (fields map[string]any, found bool) {
	t.Helper()
	for line := range strings.Lines(out) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["message"] == "browse call timing" {
			require.False(t, found, "expected at most one timing line, got more")
			fields, found = rec, true
		}
	}
	return fields, found
}

func captureBrowseTiming(t *testing.T, fn func()) string {
	t.Helper()
	var buf strings.Builder
	prevLogger := log.Logger
	prevLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = prevLogger
		zerolog.SetGlobalLevel(prevLevel)
	})
	fn()
	return buf.String()
}

// browseTimingNumeric reads a numeric field off a captured timing line.
func browseTimingNumeric(t *testing.T, fields map[string]any, key string) float64 {
	t.Helper()
	v, ok := fields[key].(float64)
	require.True(t, ok, "%q must be numeric, got %T", key, fields[key])
	return v
}

// TestLogBrowseTiming_ReportsRealPoolContention drives a real BrowseFiles call
// through the instrumented entry point with the pool deliberately saturated,
// and asserts the reported wait actually reflects it.
//
// This is the assertion the line exists for. A browse blocked waiting for a
// pooled connection and a browse executing expensive SQL are indistinguishable
// in the logs without it, which is what made a live media.browse timeout
// un-diagnosable. Asserting only that elapsed == nonWait + poolConnWait would
// pass trivially when poolConnWait is zero, so the contended and uncontended
// cases are compared against each other instead.
func TestLogBrowseTiming_ReportsRealPoolContention(t *testing.T) {
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	t.Cleanup(cleanup)
	parentDir := seedBrowsePlanTestDB(t, mediaDB, 50)

	sqlDB := mediaDB.sql.Load()
	// One connection, so a second caller must block until the first releases.
	sqlDB.SetMaxOpenConns(1)

	const holdFor = 400 * time.Millisecond
	browse := func() map[string]any {
		var fields map[string]any
		out := captureBrowseTiming(t, func() {
			_, err := mediaDB.BrowseFiles(context.Background(),
				&database.BrowseFilesOptions{PathPrefix: parentDir, Limit: 10})
			require.NoError(t, err)
		})
		f, found := browseTimingLine(t, out)
		if found {
			fields = f
		}
		return fields
	}

	// Contended: hold the only connection, start the browse, release after a
	// known delay. The browse cannot begin work until the hold ends.
	waitedBefore := sqlDB.Stats().WaitCount
	hog, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)

	done := make(chan map[string]any, 1)
	go func() { done <- browse() }()

	// Start the clock only once the browse is genuinely queued for the
	// connection. Sleeping straight away instead assumed the goroutine reaches
	// sqlDB.Conn within holdFor; on a loaded machine it need not, and the
	// browse then never blocks and reports a connWait near zero.
	require.Eventually(t, func() bool {
		return sqlDB.Stats().WaitCount > waitedBefore
	}, 10*time.Second, time.Millisecond, "browse never queued for the only connection")

	time.Sleep(holdFor)
	require.NoError(t, hog.Close())

	contended := <-done
	require.NotNil(t, contended, "a browse blocked for %v must be over the reporting threshold", holdFor)

	waitMS := browseTimingNumeric(t, contended, "connWait")
	elapsedMS := browseTimingNumeric(t, contended, "elapsed")
	workMS := browseTimingNumeric(t, contended, "work")

	assert.Greater(t, waitMS, float64(holdFor.Milliseconds())*0.5,
		"connWait must reflect the ~%v spent queued for the only connection; "+
			"got %.1f ms, so the instrumentation is not measuring contention at all", holdFor, waitMS)
	assert.Less(t, workMS, waitMS,
		"a browse that spent most of its time queued must attribute more to wait than to work")
	assert.InDelta(t, elapsedMS, workMS+waitMS, 1.0,
		"wait and work must account for the whole elapsed time")

	// Uncontended: the same query with the pool free must not report a
	// comparable wait, or the field would be meaningless as a signal.
	if uncontended := browse(); uncontended != nil {
		assert.Less(t, browseTimingNumeric(t, uncontended, "connWait"), waitMS*0.5,
			"an uncontended browse must not report a wait like the contended one")
	}
}

// TestLogBrowseTiming_CarriesRouteCount pins the route count on the line. Route
// count is the suspected cost multiplier for the overlay query, and having it
// beside the wait is what allows the two to be compared without matching
// timestamps across separate log entries.
func TestLogBrowseTiming_CarriesRouteCount(t *testing.T) {
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	t.Cleanup(cleanup)
	parentDir := seedBrowsePlanTestDB(t, mediaDB, 50)

	// Two overlay sources, so the line must report routes=2.
	overlay := &database.BrowseOverlay{Sources: []database.BrowseSource{
		{PathPrefix: parentDir, IncludeDirs: true},
		{PathPrefix: parentDir, IncludeDirs: false},
	}}

	// A 50-row browse finishes well under the production threshold on a
	// desktop, so lower it rather than contriving a slow query.
	origThreshold := browseTimingLogThreshold
	browseTimingLogThreshold = 0
	t.Cleanup(func() { browseTimingLogThreshold = origThreshold })

	out := captureBrowseTiming(t, func() {
		_, err := mediaDB.BrowseFiles(context.Background(),
			&database.BrowseFilesOptions{PathPrefix: parentDir, Overlay: overlay, Limit: 50})
		require.NoError(t, err)
	})
	fields, found := browseTimingLine(t, out)
	require.True(t, found, "with the threshold at zero every browse must log; output:\n%s", out)

	for _, key := range []string{"op", "elapsed", "connWait", "work", "routes"} {
		assert.Contains(t, fields, key, "timing line must carry %q", key)
	}
	assert.Equal(t, "browse files", fields["op"])
	assert.InDelta(t, 2.0, browseTimingNumeric(t, fields, "routes"), 0.001)
}

// TestLogBrowseTiming_QuietBelowThreshold keeps the line off the hot path: a
// browse served quickly runs many times per second and must not log.
func TestLogBrowseTiming_QuietBelowThreshold(t *testing.T) {
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	t.Cleanup(cleanup)
	parentDir := seedBrowsePlanTestDB(t, mediaDB, 5)

	out := captureBrowseTiming(t, func() {
		_, err := mediaDB.BrowseFiles(context.Background(),
			&database.BrowseFilesOptions{PathPrefix: parentDir, Limit: 5})
		require.NoError(t, err)
	})

	_, found := browseTimingLine(t, out)
	assert.False(t, found, "a fast browse must not log; output:\n%s", out)
}
