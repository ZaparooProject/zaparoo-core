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
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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

// slowBoundsDB delays the per-chunk bounds lookup, the one QueryRowContext
// sqlUpsertStagedMedia issues, so that its cost is far larger than any
// platform's clock granularity. Windows' timer ticks at ~15.6 ms and a bounds
// query against a warm SQLite file finishes well inside one tick, which read
// back as exactly 0 and failed a bare positivity assertion there (#1379).
type slowBoundsDB struct {
	sqlQueryable
	delay time.Duration
	calls int
}

func (db *slowBoundsDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db.calls++
	time.Sleep(db.delay)
	return db.sqlQueryable.QueryRowContext(ctx, query, args...)
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
		boundsDelay = 40 * time.Millisecond
	)

	ctx := context.Background()
	sqlDB := newUpsertStagedMediaTestDB(t)
	stageSyntheticMedia(t, sqlDB, 1, rows)
	boundsDB := &slowBoundsDB{sqlQueryable: sqlDB, delay: boundsDelay}

	yields := 0
	yield := func() error {
		yields++
		time.Sleep(pausePerRun)
		return nil
	}

	affected, timing, err := sqlUpsertStagedMedia(ctx, boundsDB, clockwork.NewRealClock(), "C64", 1, yield)
	require.NoError(t, err)
	require.EqualValues(t, rows, affected)
	require.GreaterOrEqual(t, yields, 2, "test needs at least two chunks to be meaningful")

	assert.GreaterOrEqual(t, timing.pacing, time.Duration(yields)*pausePerRun,
		"every yield must be captured in timing.pacing; otherwise the sleep is "+
			"billed as SQL work and double-counted against the scanner's throttle total")

	// Two claims, neither of them a race against the clock: the lookup runs once
	// per chunk (counted, not timed), and the delay injected into it lands in
	// timing.bounds, so the timer really does wrap the bounds statement. The
	// requirement stays at one delay rather than one per call because a coarse
	// clock can read each measured interval up to a tick short, and scaling it by
	// the call count would put the granularity back in.
	require.GreaterOrEqual(t, boundsDB.calls, 2, "each chunk must run its own bounds lookup")
	assert.GreaterOrEqual(t, timing.bounds, boundsDelay,
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

	_, timing, err := sqlUpsertStagedMedia(ctx, sqlDB, clockwork.NewRealClock(), "C64", 1, yield)
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
	_, _, err := sqlUpsertStagedMedia(ctx, sqlDB, clockwork.NewRealClock(), "C64", 1, nil)
	require.NoError(t, err)

	// Empty ScanStage so every Media row now counts as missing.
	_, err = sqlDB.ExecContext(ctx, "DELETE FROM ScanStage")
	require.NoError(t, err)

	yield := func() error {
		time.Sleep(25 * time.Millisecond)
		return nil
	}
	affected, timing, err := sqlFlagMissingMedia(ctx, sqlDB, clockwork.NewRealClock(), "C64", 1, yield)
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

// tickingDB charges a fixed cost to a fake clock for every statement issued
// through it. With that clock as reconcile's only time source, each step's
// figure is an exact count of its statements, and a statement that no step
// covers lands in unattributed as a whole tick rather than hiding in scheduler
// noise.
type tickingDB struct {
	sqlQueryable
	clock      *clockwork.FakeClock
	statements int
}

const (
	tickedStatementCost = time.Millisecond
	tickedPacingCost    = 3 * time.Millisecond
)

func (db *tickingDB) tick() {
	db.statements++
	db.clock.Advance(tickedStatementCost)
}

func (db *tickingDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	db.tick()
	res, err := db.sqlQueryable.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ticked exec: %w", err)
	}
	return res, nil
}

func (db *tickingDB) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	db.tick()
	stmt, err := db.sqlQueryable.PrepareContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ticked prepare: %w", err)
	}
	return stmt, nil
}

func (db *tickingDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	db.tick()
	rows, err := db.sqlQueryable.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ticked query: %w", err)
	}
	return rows, nil
}

func (db *tickingDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	db.tick()
	return db.sqlQueryable.QueryRowContext(ctx, query, args...)
}

// tickedReconcile stages files for SNES and reconciles them through a
// tickingDB, returning the statements it charged and the times it paced.
func tickedReconcile(
	t *testing.T, mediaDB *MediaDB, clock *clockwork.FakeClock, files []database.ScanStagedMedia,
) (statements, yields int) {
	t.Helper()
	require.NoError(t, mediaDB.BeginTransaction(true))
	for i := range files {
		require.NoError(t, mediaDB.StageScannedMedia(&files[i]))
	}
	require.NoError(t, mediaDB.FlushBatchInserters())

	db := &tickingDB{sqlQueryable: mediaDB.conn(), clock: clock}
	yield := func() error {
		yields++
		clock.Advance(tickedPacingCost)
		return nil
	}
	_, err := sqlReconcileStagedSystem(context.Background(), db, clock, "SNES",
		database.ScanReconcileOpts{Yield: yield})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())
	return db.statements, yields
}

// readStepTimings returns the elapsed ms and steps field of the single
// step-timings record in captured zerolog JSONL output.
func readStepTimings(t *testing.T, out string) (elapsedMS float64, steps string) {
	t.Helper()
	found := false
	for _, line := range strings.Split(out, "\n") {
		var rec struct {
			Message string  `json:"message"`
			Steps   string  `json:"steps"`
			Elapsed float64 `json:"elapsed"`
		}
		if line == "" || json.Unmarshal([]byte(line), &rec) != nil ||
			rec.Message != "scan reconcile step timings" {
			continue
		}
		require.False(t, found, "expected exactly one step-timings line, got more")
		found, elapsedMS, steps = true, rec.Elapsed, rec.Steps
	}
	require.True(t, found, "reconcile must emit a step-timings line; captured output:\n%s", out)
	return elapsedMS, steps
}

func snesStagedFiles(names ...string) []database.ScanStagedMedia {
	files := make([]database.ScanStagedMedia, 0, len(names))
	for _, name := range names {
		files = append(files, database.ScanStagedMedia{
			Path:          "/roms/SNES/" + name + " (USA).sfc",
			ParentDir:     "/roms/SNES",
			Slug:          name,
			TitleName:     name,
			SortName:      name,
			SlugLength:    len(name),
			SlugWordCount: 1,
			Tags: []database.ScanStagedTag{
				{Type: string(tags.TagTypeRegion), Value: "us"},
				{Type: string(tags.TagTypeRev), Value: "1"},
			},
		})
	}
	return files
}

// TestReconcileStepTimings_EveryStatementIsAttributed is the regression guard
// for the gap that made round 9 of #1279 misread: entries that covered as
// little as 55% of reconcile wall time while reading like a complete
// breakdown. The guard is on unattributed rather than on any one step, since
// that is the term a step someone adds later and forgets to record ends up in.
//
// It runs on a fake clock that only statements and pacing advance, so the
// check is exact. Measured on a real clock, a reconcile this small is a few
// milliseconds, and a scheduler stall between two steps read as an untimed
// step.
func TestReconcileStepTimings_EveryStatementIsAttributed(t *testing.T) {
	// Not parallel: swaps the global logger.
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "media.db")
	sqlDB, err := sql.Open(sqliteDriverName(), dbPath+"?_foreign_keys=ON")
	require.NoError(t, err)
	mediaDB := &MediaDB{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("test-platform")
	require.NoError(t, mediaDB.SetSQLForTesting(ctx, sqlDB, mockPlatform))
	mediaDB.SetDBPathForTesting(dbPath)
	t.Cleanup(func() { require.NoError(t, mediaDB.Close()) })

	// TestMain disables logging for the whole binary; this test reads the log,
	// so it re-enables it for its own duration and restores the suppression.
	var buf bytes.Buffer
	originalLogger := log.Logger
	originalLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	t.Cleanup(func() {
		log.Logger = originalLogger
		zerolog.SetGlobalLevel(originalLevel)
	})

	names := make([]string, 0, 40)
	for i := range 40 {
		names = append(names, fmt.Sprintf("game%02d", i))
	}
	clock := clockwork.NewFakeClock()

	// The first index creates the system and skips the steps that only
	// reconcile against existing rows. The rescan drops one file and renames
	// another so those steps run too.
	rescan := snesStagedFiles(names[1:]...)
	rescan[0].TitleName = "Renamed"
	runs := []struct {
		name     string
		wantStep []string
		files    []database.ScanStagedMedia
	}{
		{
			name:     "fresh system",
			files:    snesStagedFiles(names...),
			wantStep: []string{"resolve system", "insert titles", "upsert media", "count touched titles"},
		},
		{
			name:  "rescan",
			files: rescan,
			wantStep: []string{
				"resolve system", "rename titles", "upsert media", "flag missing media",
				"delete stale tag links", "count touched titles", "disambiguation",
			},
		},
	}
	for _, run := range runs {
		buf.Reset()
		statements, yields := tickedReconcile(t, mediaDB, clock, run.files)
		elapsedMS, steps := readStepTimings(t, buf.String())
		parsed := parseStepTimings(t, steps)

		for _, want := range append(run.wantStep, "clear scan stage", "pacing", "unattributed") {
			assert.Contains(t, parsed, want, "%s: steps must name %q; steps were: %s", run.name, want, steps)
		}

		wantElapsed := time.Duration(statements)*tickedStatementCost + time.Duration(yields)*tickedPacingCost
		assert.InDelta(t, float64(wantElapsed.Milliseconds()), elapsedMS, 0,
			"%s: the fake clock must be the only time source reconcile measures", run.name)
		assert.Equal(t, int64(yields)*tickedPacingCost.Milliseconds(), parsed["pacing"],
			"%s: every yield must be reported as pacing; steps: %s", run.name, steps)
		assert.Zero(t, parsed["unattributed"],
			"%s: a statement ran outside every named step, the #1279 gap reappearing. steps: %s",
			run.name, steps)

		var sum int64
		for _, ms := range parsed {
			sum += ms
		}
		assert.Equal(t, wantElapsed.Milliseconds(), sum,
			"%s: entries must reconstruct elapsed exactly; steps: %s", run.name, steps)
	}
}
