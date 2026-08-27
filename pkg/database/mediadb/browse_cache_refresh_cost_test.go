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

// Diagnostic for #1279 open item B: the mid-scan browse cache refresh costs a
// flat ~6 s per system on the MiSTer late in a reindex, regardless of whether
// that system holds 7 files or 1,441, and 662,224 ms run-wide (12.7%).
//
// Three explanations have already been proposed and disproved: superlinear
// system-count scaling, a regression in the foreign-key fix, and a full table
// scan from ordering on the rowid (the query plan showed a covering index with
// only a redundant sort). Rather than propose a fourth, this sweep holds the
// refreshed system tiny and grows everything around it, then reports which
// statement moves.
//
// Run with:
//
//	ZAPAROO_REFRESH_COST=1 go test -run TestBrowseCacheRefreshCostSweep -v ./pkg/database/mediadb/

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

// refreshCostBuffer collects zerolog JSON lines from the code under test.
type refreshCostBuffer struct {
	buf strings.Builder
	mu  syncutil.Mutex
}

func (b *refreshCostBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("refresh cost buffer write: %w", err)
	}
	return n, nil
}

func (b *refreshCostBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// refreshTimingLine mirrors the fields on "browse cache refreshed for systems".
type refreshTimingLine struct {
	Message           string  `json:"message"`
	Duration          float64 `json:"duration"`
	VersionCheck      float64 `json:"versionCheck"`
	ClearIncompatible float64 `json:"clearIncompatible"`
	BeginTx           float64 `json:"beginTx"`
	AttachLookup      float64 `json:"attachLookup"`
	MediaScan         float64 `json:"mediaScan"`
	DeleteCounts      float64 `json:"deleteCounts"`
	Inserts           float64 `json:"inserts"`
	Commit            float64 `json:"commit"`
	Unattributed      float64 `json:"unattributed"`
	NewDirs           int     `json:"newDirs"`
	Counts            int     `json:"counts"`
}

func TestBrowseCacheRefreshCostSweep(t *testing.T) {
	if os.Getenv("ZAPAROO_REFRESH_COST") == "" {
		t.Skip("diagnostic sweep; set ZAPAROO_REFRESH_COST=1 to run")
	}

	// Each cell: how many systems are already indexed, and how much media they
	// hold between them. The system being refreshed always holds tinySystemFiles
	// files, so any growth in its refresh cost comes from the surroundings.
	cells := []struct {
		name          string
		priorSystems  int
		mediaPerPrior int
	}{
		{name: "10-systems-x-500", priorSystems: 10, mediaPerPrior: 500},
		{name: "50-systems-x-500", priorSystems: 50, mediaPerPrior: 500},
		{name: "100-systems-x-500", priorSystems: 100, mediaPerPrior: 500},
		{name: "100-systems-x-2000", priorSystems: 100, mediaPerPrior: 2000},
	}

	t.Logf("%-20s %8s %9s | %7s %8s %9s %9s %9s %8s %8s %8s",
		"cell", "media", "total", "verChk", "beginTx", "attachLk", "mediaScan", "delCounts",
		"inserts", "commit", "unattr")

	for _, cell := range cells {
		t.Run(cell.name, func(t *testing.T) {
			ctx := context.Background()
			mediaDB, cleanup := setupBrowsePlanTestDB(t)
			defer cleanup()

			tinyID := seedRefreshCostDB(t, mediaDB, cell.priorSystems, cell.mediaPerPrior)
			require.NoError(t, sqlAnalyze(ctx, mediaDB.sql.Load()))

			// Refresh every prior system first, so BrowseDirs/BrowseDirCounts are
			// populated exactly as they would be part-way through a real index.
			for s := 1; s <= cell.priorSystems; s++ {
				require.NoError(t, mediaDB.PopulateBrowseCacheForSystems(
					ctx, []string{fmt.Sprintf("MiSTer:CostSys%03d", s)}))
			}

			buf := &refreshCostBuffer{}
			prevLogger := log.Logger
			prevLevel := zerolog.GlobalLevel()
			zerolog.SetGlobalLevel(zerolog.InfoLevel)
			log.Logger = zerolog.New(buf).Level(zerolog.InfoLevel)

			err := mediaDB.PopulateBrowseCacheForSystems(ctx, []string{tinyID})

			log.Logger = prevLogger
			zerolog.SetGlobalLevel(prevLevel)
			require.NoError(t, err)

			var line refreshTimingLine
			for _, l := range strings.Split(buf.String(), "\n") {
				if !strings.Contains(l, `"browse cache refreshed for systems"`) {
					continue
				}
				require.NoError(t, json.Unmarshal([]byte(l), &line), "line: %s", l)
			}
			require.NotEmpty(t, line.Message, "expected a refresh timing line")

			totalMedia := cell.priorSystems*cell.mediaPerPrior + tinySystemFiles
			t.Logf("%-20s %8d %8.1fms | %6.1f %7.1f %8.1f %8.1f %8.1f %7.1f %7.1f %7.1f",
				cell.name, totalMedia, line.Duration,
				line.VersionCheck, line.BeginTx, line.AttachLookup, line.MediaScan,
				line.DeleteCounts, line.Inserts, line.Commit, line.Unattributed)
		})
	}
}

// tinySystemFiles matches the small systems that exposed the flat toll on the
// device (UK101 held 7 files and still paid 6,115 ms).
const tinySystemFiles = 7

// seedRefreshCostDB creates priorSystems systems holding mediaPerPrior rows
// each, plus one extra system holding only tinySystemFiles rows. Returns the
// tiny system's SystemID.
func seedRefreshCostDB(t *testing.T, mediaDB *MediaDB, priorSystems, mediaPerPrior int) string {
	t.Helper()
	ctx := context.Background()

	tx, err := mediaDB.sql.Load().BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	sysStmt, err := tx.PrepareContext(ctx,
		"INSERT INTO Systems (DBID, SystemID, Name) VALUES (?, ?, ?)")
	require.NoError(t, err)
	defer func() { require.NoError(t, sysStmt.Close()) }()

	titleStmt, err := tx.PrepareContext(ctx,
		"INSERT INTO MediaTitles (DBID, SystemDBID, Slug, Name) VALUES (?, ?, ?, ?)")
	require.NoError(t, err)
	defer func() { require.NoError(t, titleStmt.Close()) }()

	mediaStmt, err := tx.PrepareContext(ctx,
		"INSERT INTO Media (DBID, MediaTitleDBID, SystemDBID, Path, ParentDir, SortName, IsMissing) "+
			"VALUES (?, ?, ?, ?, ?, ?, 0)")
	require.NoError(t, err)
	defer func() { require.NoError(t, mediaStmt.Close()) }()

	var dbid int64
	seedSystem := func(s int, rows int) {
		systemID := fmt.Sprintf("MiSTer:CostSys%03d", s)
		_, err = sysStmt.ExecContext(ctx, int64(s), systemID, systemID)
		require.NoError(t, err)
		_, err = titleStmt.ExecContext(ctx, int64(s), int64(s),
			fmt.Sprintf("cost-title-%03d", s), systemID)
		require.NoError(t, err)

		// 20 dirs per system under a shared ancestor, mirroring a real library.
		for i := range rows {
			dbid++
			dir := fmt.Sprintf("/roms/shared/sys%03d/dir%02d/", s, i%20)
			_, err = mediaStmt.ExecContext(ctx, dbid, int64(s), int64(s),
				fmt.Sprintf("%sgame%06d.rom", dir, i), dir, fmt.Sprintf("game%06d", i))
			require.NoError(t, err)
		}
	}

	for s := 1; s <= priorSystems; s++ {
		seedSystem(s, mediaPerPrior)
	}
	tinyIdx := priorSystems + 1
	seedSystem(tinyIdx, tinySystemFiles)

	require.NoError(t, tx.Commit())
	return fmt.Sprintf("MiSTer:CostSys%03d", tinyIdx)
}
