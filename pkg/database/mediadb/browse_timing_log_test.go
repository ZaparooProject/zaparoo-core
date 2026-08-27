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
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// TestLogBrowseTiming_SplitsWaitFromWork is the guard for the attribution this
// line exists to provide. A browse that is slow because it queued for a pooled
// connection and one that is slow because its SQL is expensive look identical
// in the logs otherwise, which is exactly the ambiguity that made a live
// media.browse timeout un-diagnosable.
func TestLogBrowseTiming_SplitsWaitFromWork(t *testing.T) {
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	t.Cleanup(cleanup)

	out := captureBrowseTiming(t, func() {
		// Backdate the start so the call is over the reporting threshold
		// without the test having to sleep for it.
		started := time.Now().Add(-2 * browseTimingLogThreshold)
		mediaDB.logBrowseTiming("browse files", 21, started, samplePoolWait(mediaDB.sql.Load()))
	})

	fields, found := browseTimingLine(t, out)
	require.True(t, found, "a call over the threshold must be reported; output:\n%s", out)

	for _, key := range []string{"op", "elapsed", "poolConnWait", "poolConnWaitCount", "nonWait", "routes"} {
		assert.Contains(t, fields, key, "timing line must carry %q", key)
	}
	assert.Equal(t, "browse files", fields["op"])
	assert.InDelta(t, 21.0, fields["routes"], 0.001, "route count is the suspected cost multiplier")

	numeric := func(key string) float64 {
		v, ok := fields[key].(float64)
		require.True(t, ok, "%q must be numeric, got %T", key, fields[key])
		return v
	}
	assert.InDelta(t, numeric("elapsed"), numeric("nonWait")+numeric("poolConnWait"), 0.001,
		"wait and work must account for the whole elapsed time, or the split means nothing")
}

// TestLogBrowseTiming_QuietBelowThreshold keeps the line off the hot path: a
// browse served from cache runs many times per second and must not log.
func TestLogBrowseTiming_QuietBelowThreshold(t *testing.T) {
	mediaDB, cleanup := setupBrowsePlanTestDB(t)
	t.Cleanup(cleanup)

	out := captureBrowseTiming(t, func() {
		mediaDB.logBrowseTiming("browse files", 2, time.Now(), samplePoolWait(mediaDB.sql.Load()))
	})

	_, found := browseTimingLine(t, out)
	assert.False(t, found, "a fast browse must not log; output:\n%s", out)
}
