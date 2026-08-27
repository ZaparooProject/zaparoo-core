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

// The reconcile step-timings line is a measurement instrument, and an
// instrument that quietly loses time is worse than none: round 9 of #1279 read
// its numbers as a phase breakdown when they in fact covered only 78% of
// reconcile wall time on aggregate, and as little as 55% on some systems. The
// missing time was four untimed steps plus pacing folded into a step that
// yields inside its own loop.
//
// These tests pin the two properties that make the line trustworthy: the
// entries reconstruct wall time, and pacing is never billed as SQL work.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseStepTimings turns the joined "name=ms name=ms" field back into a map.
//
// Step names contain spaces ("insert tag links"), so the separator between
// entries and the separator within a name are the same character. Entries are
// recovered by accumulating tokens until one carries the "=": that token ends
// the entry, and everything before it is part of the name.
func parseStepTimings(t *testing.T, steps string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	var nameParts []string
	for _, token := range strings.Fields(steps) {
		idx := strings.Index(token, "=")
		if idx < 0 {
			nameParts = append(nameParts, token)
			continue
		}
		ms, err := strconv.ParseInt(token[idx+1:], 10, 64)
		require.NoError(t, err, "step entry %q must carry an integer ms value", token)
		name := strings.Join(append(nameParts, token[:idx]), " ")
		out[name] = ms
		nameParts = nil
	}
	require.Empty(t, nameParts, "trailing tokens with no value in %q", steps)
	return out
}

// TestChunkedStepTiming_PacingIsNotBilledAsSQL is the core guarantee for the
// two steps that yield inside their own loops. Both call the scanner's pauser
// between chunks; on the MiSTer that pauser sleeps roughly as long as the work
// it follows, so a step that counted it would report double its real cost and
// the time would also be counted a second time in the scanner's throttle
// total.
func TestChunkedStepTiming_PacingIsNotBilledAsSQL(t *testing.T) {
	t.Parallel()

	const (
		rows        = scanUpsertMediaBatchSize + 25 // forces a second chunk, so yield runs twice
		pausePerRun = 40 * time.Millisecond         // the MiSTer ThrottleBackground work window
	)

	ctx := context.Background()
	sqlDB := newUpsertStagedMediaTestDB(t)
	stageSyntheticMedia(t, sqlDB, 1, rows)

	yields := 0
	yield := func() error {
		yields++
		time.Sleep(pausePerRun)
		return nil
	}

	started := time.Now()
	affected, timing, err := sqlUpsertStagedMedia(ctx, sqlDB, "C64", 1, yield)
	wall := time.Since(started)
	require.NoError(t, err)
	require.EqualValues(t, rows, affected)
	require.GreaterOrEqual(t, yields, 2, "test needs at least two chunks to be meaningful")

	assert.GreaterOrEqual(t, timing.pacing, time.Duration(yields)*pausePerRun,
		"every yield must be captured in timing.pacing; otherwise the sleep is "+
			"billed as SQL work and double-counted against the scanner's throttle total")

	sqlTime := wall - timing.bounds - timing.pacing
	assert.Less(t, sqlTime, wall-timing.pacing+time.Millisecond,
		"reported SQL time must exclude pacing")
	assert.Positive(t, timing.bounds,
		"the per-chunk bounds lookup is a real statement and must be reported separately")
}

// TestChunkedStepTiming_PacingRecordedBeforeErrorReturn guards the error path.
// A yield that fails still consumed wall time, and losing it would make the
// failing system's numbers unreconstructable — exactly when they matter most.
func TestChunkedStepTiming_PacingRecordedBeforeErrorReturn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB := newUpsertStagedMediaTestDB(t)
	stageSyntheticMedia(t, sqlDB, 1, 10)

	sentinel := errors.New("pauser cancelled")
	yield := func() error {
		time.Sleep(20 * time.Millisecond)
		return sentinel
	}

	_, timing, err := sqlUpsertStagedMedia(ctx, sqlDB, "C64", 1, yield)
	require.ErrorIs(t, err, sentinel)
	assert.GreaterOrEqual(t, timing.pacing, 20*time.Millisecond,
		"pacing before a failed yield must still be reported")
}

// TestFlagMissingMediaTiming_ExcludesPacing covers the same contract for the
// other chunked step. It has no bounds query — its loop is self-draining — so
// only pacing applies.
func TestFlagMissingMediaTiming_ExcludesPacing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB := newUpsertStagedMediaTestDB(t)
	stageSyntheticMedia(t, sqlDB, 1, 5)
	_, _, err := sqlUpsertStagedMedia(ctx, sqlDB, "C64", 1, nil)
	require.NoError(t, err)

	// Empty ScanStage so every Media row now counts as missing.
	_, err = sqlDB.ExecContext(ctx, "DELETE FROM ScanStage")
	require.NoError(t, err)

	yield := func() error {
		time.Sleep(25 * time.Millisecond)
		return nil
	}
	affected, timing, err := sqlFlagMissingMedia(ctx, sqlDB, "C64", 1, yield)
	require.NoError(t, err)
	require.EqualValues(t, 5, affected)

	assert.GreaterOrEqual(t, timing.pacing, 25*time.Millisecond,
		"flag missing media yields between chunks and must report that time as pacing")
	assert.Zero(t, timing.bounds,
		"flag missing media has no bounds query; reporting one would invent a statement")
}

// TestParseStepTimings_HandlesMultiWordStepNames pins the parsing assumption
// the reconstruction test relies on, since step names legitimately contain
// spaces ("upsert media bounds").
func TestParseStepTimings_HandlesMultiWordStepNames(t *testing.T) {
	t.Parallel()

	parsed := parseStepTimings(t, "upsert media=12 upsert media bounds=3 pacing=0 unattributed=-1")
	assert.Equal(t, map[string]int64{
		"upsert media":        12,
		"upsert media bounds": 3,
		"pacing":              0,
		"unattributed":        -1,
	}, parsed)
}
