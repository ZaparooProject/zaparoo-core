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

// Measures where SQLite's page cache starts spilling dirty pages to the WAL
// during a long single-transaction media insert, as a function of cache_size and
// row width.
//
// Round 7 of #1279 established the mechanism on the MiSTer: walBytes stays
// pinned at the empty-WAL value, then walDelta and writeBytes jump together
// while reads stay flat, and the per-chunk elapsed steps at exactly that point.
// It also established that the onset is a stable per-system constant but NOT a
// fixed row count:
//
//	C64        66,011 files   onset 16,000   (reproduced R6, R7)
//	SNESMusic  31,232 files   onset 16,000   (R7)
//	NES        17,123 files   onset 16,000   (R7)
//	SNES       13,591 files   onset 10,000   (reproduced R5, R6, R7)
//	ZXSpectrum 11,723 files   no spill through all 11,723 rows
//
// Two explanations were proposed and both were falsified by later data: "the
// onset is exactly 16,000 rows" (SNES) and "residual staged rows drives it"
// (SNESMusic, which has the smallest residual and an ordinary 16,000 onset).
// The surviving hypothesis is that the cache fills by BYTES, so wider rows
// cross the limit at fewer rows — SNES holds long scene-release ROM names while
// SNESMusic holds short .spc names. This sweep tests it directly by asking
// whether onset x bytes-per-row is roughly constant.
//
// Run:
//
//	go test ./pkg/database/mediadb/ -run TestSpillOnsetSweep -v -timeout 30m
//
// Skipped by default: it writes hundreds of MB and takes minutes.
// A desktop's page cache and fast storage understate the device, so the value
// here is the SHAPE and the ONSET LOCATION, never an absolute saving to quote
// for MiSTer hardware.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	spillChunkRows = 2000    // matches the device's upsert chunk size
	spillMaxRows   = 120_000 // enough headroom for a 64MB cache to spill
)

// spillResult is one (cacheSize, rowWidth) cell of the sweep.
type spillResult struct {
	cacheKiB     int
	pathLen      int
	onsetRows    int // 0 means no spill was observed
	bytesPerRow  float64
	totalWALSize int64
}

// spillWALSize returns the current size of the database's WAL file.
func spillWALSize(t *testing.T, dbPath string) int64 {
	t.Helper()
	info, err := os.Stat(dbPath + "-wal")
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return info.Size()
}

// measureSpillOnset inserts rows into Media inside a single transaction, in
// chunks, and returns the cumulative row count at which the WAL first grows.
//
// The WAL only grows mid-transaction when SQLite evicts dirty pages it can no
// longer hold, so its first movement is a direct observation of the spill —
// this is the same signal the device telemetry captured as walDelta.
func measureSpillOnset(t *testing.T, cacheKiB, pathLen int) spillResult {
	t.Helper()
	ctx := context.Background()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	sqlDB := mediaDB.sql.Load()
	dbPath := mediaDB.GetDBPath()

	// Pin one connection: cache_size is per-connection, and the transaction
	// below must run on the connection that received the pragma.
	conn, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.ExecContext(ctx, fmt.Sprintf("PRAGMA cache_size = -%d", cacheKiB))
	require.NoError(t, err)
	// Match indexing: no automatic checkpoint, so WAL growth is only ever the
	// spill and never a checkpoint artefact.
	_, err = conn.ExecContext(ctx, "PRAGMA wal_autocheckpoint = 0")
	require.NoError(t, err)

	// One system and one title to satisfy the foreign keys.
	_, err = conn.ExecContext(ctx,
		"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'sweep', 'sweep')")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx,
		"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'sweep', 'Sweep')")
	require.NoError(t, err)

	// Pad the path to the requested width. Row width is the variable under test.
	pad := strings.Repeat("x", maxInt(pathLen-64, 0))

	tx, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing, SortName)
		 VALUES (?, 1, 1, ?, ?, 0, ?)`)
	require.NoError(t, err)
	defer func() { _ = stmt.Close() }()

	baseWAL := spillWALSize(t, dbPath)
	res := spillResult{cacheKiB: cacheKiB, pathLen: pathLen}

	for row := 1; row <= spillMaxRows; row++ {
		dir := fmt.Sprintf("/media/fat/games/sweep/d%03d", row%256)
		path := fmt.Sprintf("%s/%s_%08d.rom", dir, pad, row)
		sortName := fmt.Sprintf("%s_%08d", pad, row)
		if _, execErr := stmt.ExecContext(ctx, row, path, dir, sortName); execErr != nil {
			t.Fatalf("insert row %d: %v", row, execErr)
		}

		if row%spillChunkRows != 0 {
			continue
		}
		current := spillWALSize(t, dbPath)
		if res.onsetRows == 0 && current > baseWAL {
			res.onsetRows = row
			res.bytesPerRow = float64(current-baseWAL) / float64(spillChunkRows)
		}
		res.totalWALSize = current
		// Once the onset is found, a few more chunks confirm sustained growth
		// rather than a one-off, then stop — the rest is just wall clock.
		if res.onsetRows > 0 && row >= res.onsetRows+2*spillChunkRows {
			break
		}
	}
	return res
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TestSpillOnsetSweep answers the open question from #1279 round 7: what sets
// the spill onset? It sweeps cache_size against row width and prints the onset
// for each combination.
//
// Reading the result:
//   - onset roughly proportional to cache_size at fixed width -> the cache fills
//     by bytes, and raising cache_size buys proportional headroom.
//   - onset roughly inversely proportional to width at fixed cache -> row width
//     explains why SNES (long ROM names) spills earlier than its file count
//     suggests, which is the surviving hypothesis.
//   - onset x bytes-per-row constant across the grid -> both hold, and the
//     device onsets become predictable from row width alone.
func TestSpillOnsetSweep(t *testing.T) {
	if os.Getenv("ZAPAROO_SPILL_SWEEP") == "" {
		t.Skip("set ZAPAROO_SPILL_SWEEP=1 to run the spill onset sweep (writes hundreds of MB)")
	}

	cacheSizes := []int{8192, 16384, 32768, 65536} // KiB: 8MB (default), 16, 32 (indexing), 64
	pathLens := []int{64, 128, 256}                // short / medium / long scene-release style

	var results []spillResult
	for _, cacheKiB := range cacheSizes {
		for _, pathLen := range pathLens {
			name := fmt.Sprintf("cache%dMB_path%d", cacheKiB/1024, pathLen)
			t.Run(name, func(t *testing.T) {
				res := measureSpillOnset(t, cacheKiB, pathLen)
				results = append(results, res)
				if res.onsetRows == 0 {
					t.Logf("cache=%dMB pathLen=%d: NO SPILL through %d rows",
						cacheKiB/1024, pathLen, spillMaxRows)
					return
				}
				t.Logf("cache=%dMB pathLen=%d: onset=%d rows, %.0f WAL bytes/row, onset*bytes=%.1f MB",
					cacheKiB/1024, pathLen, res.onsetRows, res.bytesPerRow,
					float64(res.onsetRows)*res.bytesPerRow/(1<<20))
			})
		}
	}

	t.Log("=== spill onset sweep ===")
	t.Logf("%-10s %-9s %-12s %-14s", "cache", "pathLen", "onset(rows)", "WAL bytes/row")
	for _, r := range results {
		onset := "none"
		if r.onsetRows > 0 {
			onset = strconv.Itoa(r.onsetRows)
		}
		t.Logf("%-10s %-9d %-12s %-14.0f",
			fmt.Sprintf("%dMB", r.cacheKiB/1024), r.pathLen, onset, r.bytesPerRow)
	}
}

// measureSpillCost inserts a fixed number of rows in one transaction at a given
// cache_size, then commits, and reports the wall time and the total WAL bytes
// produced.
//
// This is the question the onset sweep cannot answer: whether spilling costs
// anything. Dirty pages reach the WAL at commit regardless, so if a small cache
// merely writes them earlier then eliminating the spill saves nothing. It is
// only a real saving if a spilled page is re-dirtied and written again — which
// random index insertion makes plausible but which has to be measured, not
// assumed. Equal WAL totals mean redistribution; a larger total at a smaller
// cache means genuine waste.
func measureSpillCost(t *testing.T, cacheKiB, pathLen, rows int) (insert, commit time.Duration, walBytes int64) {
	t.Helper()
	ctx := context.Background()

	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	dbPath := mediaDB.GetDBPath()

	conn, err := mediaDB.sql.Load().Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.ExecContext(ctx, fmt.Sprintf("PRAGMA cache_size = -%d", cacheKiB))
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, "PRAGMA wal_autocheckpoint = 0")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx,
		"INSERT INTO Systems (DBID, SystemID, Name) VALUES (1, 'sweep', 'sweep')")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx,
		"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (1, 1, 'sweep', 'Sweep')")
	require.NoError(t, err)
	// Checkpoint the setup writes so the WAL measured below is only this workload.
	_, err = conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	require.NoError(t, err)

	pad := strings.Repeat("x", maxInt(pathLen-64, 0))
	tx, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, IsMissing, SortName)
		 VALUES (?, 1, 1, ?, ?, 0, ?)`)
	require.NoError(t, err)
	defer func() { _ = stmt.Close() }()

	insertStart := time.Now()
	for row := 1; row <= rows; row++ {
		dir := fmt.Sprintf("/media/fat/games/sweep/d%03d", row%256)
		if _, execErr := stmt.ExecContext(ctx, row,
			fmt.Sprintf("%s/%s_%08d.rom", dir, pad, row), dir,
			fmt.Sprintf("%s_%08d", pad, row)); execErr != nil {
			t.Fatalf("insert row %d: %v", row, execErr)
		}
	}
	insert = time.Since(insertStart)

	commitStart := time.Now()
	require.NoError(t, tx.Commit())
	commit = time.Since(commitStart)

	return insert, commit, spillWALSize(t, dbPath)
}

// TestSpillCostByCacheSize measures what the spill actually costs, so a decision
// about raising cache_size rests on a number rather than on the onset alone.
func TestSpillCostByCacheSize(t *testing.T) {
	if os.Getenv("ZAPAROO_SPILL_SWEEP") == "" {
		t.Skip("set ZAPAROO_SPILL_SWEEP=1 to run the spill cost measurement")
	}

	const (
		pathLen  = 128
		attempts = 3
	)
	// 100k rows puts 32MB deep into the spilled regime (onset 34,000) while 64MB
	// spills only late (onset 68,000) — the C64-shaped case where a bigger cache
	// should pay off if it ever does. 40k is the control: 32MB barely spills there.
	for _, rows := range []int{40_000, 100_000} {
		t.Logf("=== %d rows, pathLen=%d, best of %d ===", rows, pathLen, attempts)
		t.Logf("%-8s %-12s %-12s %-12s %-14s", "cache", "insert", "commit", "total", "WAL bytes")
		for _, cacheKiB := range []int{8192, 32768, 65536} {
			var bestTotal time.Duration
			var bi, bc time.Duration
			var bw int64
			for attempt := range attempts {
				ins, com, wal := measureSpillCost(t, cacheKiB, pathLen, rows)
				if attempt == 0 || ins+com < bestTotal {
					bestTotal, bi, bc, bw = ins+com, ins, com, wal
				}
			}
			t.Logf("%-8s %-12s %-12s %-12s %-14d",
				fmt.Sprintf("%dMB", cacheKiB/1024),
				bi.Round(time.Millisecond), bc.Round(time.Millisecond),
				bestTotal.Round(time.Millisecond), bw)
		}
	}
}
