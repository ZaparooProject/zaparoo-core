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

package mediadb_test

// The end-to-end guard for the reconcile step-timings line: drive a real
// reconcile, capture what it actually logged, and check the numbers add up.
//
// This lives in the external test package because it needs
// helpers.NewInMemoryMediaDB, which imports mediadb.

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stepTimingsLine finds the single "scan reconcile step timings" record in
// captured zerolog JSONL output and returns its elapsed and steps fields.
func stepTimingsLine(t *testing.T, out string) (elapsedMS float64, steps string) {
	t.Helper()
	found := false
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		var rec struct {
			Message string  `json:"message"`
			Steps   string  `json:"steps"`
			Elapsed float64 `json:"elapsed"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Message != "scan reconcile step timings" {
			continue
		}
		require.False(t, found, "expected exactly one step-timings line, got more")
		found, elapsedMS, steps = true, rec.Elapsed, rec.Steps
	}
	require.True(t, found, "reconcile must emit a step-timings line; captured output:\n%s", out)
	return elapsedMS, steps
}

// TestReconcileStepTimingsLine_AccountsForWallTime is the regression guard for
// the gap that made round 9 of #1279 misread. Before pacing was split out and
// the four untimed steps were named, the entries on this line covered as
// little as 55% of reconcile wall time while reading like a complete
// breakdown.
//
// The assertion is deliberately on the *unattributed* term rather than on any
// individual step: it is the term that catches a step someone adds later and
// forgets to record, which is exactly how the original gap appeared.
func TestReconcileStepTimingsLine_AccountsForWallTime(t *testing.T) {
	// Not parallel: swaps the global logger.
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	// TestMain disables logging for the whole binary; this test reads the log,
	// so it re-enables it for its own duration and restores the suppression.
	var buf bytes.Buffer
	original := log.Logger
	originalLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = original
		zerolog.SetGlobalLevel(originalLevel)
	})

	paths := make([]string, 0, 40)
	for i := range 40 {
		paths = append(paths, filepath.ToSlash(filepath.Join(
			string(filepath.Separator), "roms", "SNES",
			"Game "+strconv.Itoa(i)+" (USA).sfc",
		)))
	}
	stats := scantest.IndexMediaPaths(t, mediaDB, "SNES", paths...)
	require.EqualValues(t, len(paths), stats.MediaUpserted)

	elapsedMS, steps := stepTimingsLine(t, buf.String())
	parsed := map[string]int64{}
	var nameParts []string
	for _, token := range strings.Fields(steps) {
		idx := strings.Index(token, "=")
		if idx < 0 {
			nameParts = append(nameParts, token)
			continue
		}
		ms, err := strconv.ParseInt(token[idx+1:], 10, 64)
		require.NoError(t, err, "step entry %q must carry an integer ms value", token)
		parsed[strings.Join(append(nameParts, token[:idx]), " ")] = ms
		nameParts = nil
	}

	// The steps that must always be present, including the four that used to
	// run untimed and the two accounting terms.
	for _, want := range []string{
		"resolve system", "insert titles", "upsert media",
		"count touched titles", "clear scan stage", "pacing", "unattributed",
	} {
		assert.Contains(t, parsed, want,
			"step-timings line must name %q; steps were: %s", want, steps)
	}

	var sum int64
	for _, ms := range parsed {
		sum += ms
	}
	// Millisecond truncation across ~15 entries is the only legitimate drift.
	assert.InDelta(t, elapsedMS, float64(sum), float64(len(parsed)+2),
		"entries must reconstruct elapsed (%.1f ms); got %d ms from: %s",
		elapsedMS, sum, steps)

	assert.LessOrEqual(t, parsed["unattributed"], int64(float64(elapsedMS)*0.25)+5,
		"unattributed must stay a small share of reconcile; a large value means a "+
			"step is running untimed, which is the #1279 gap reappearing. steps: %s", steps)
}
