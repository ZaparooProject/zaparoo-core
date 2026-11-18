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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countRow(dbid, title int64, path string) versionCountRow {
	return versionCountRow{dbid: dbid, title: title, path: path, parentDir: ParentDirForMediaPath(path)}
}

func TestVersionCountsFromRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		groups map[int64]int64
		links  map[int64]int64
		want   map[int64]int
		name   string
		rows   []versionCountRow
	}{
		{
			name: "single versions store nothing",
			rows: []versionCountRow{
				countRow(1, 10, "/roms/A.sfc"),
				countRow(2, 20, "/roms/B.sfc"),
			},
			want: map[int64]int{},
		},
		{
			name: "files of one title",
			rows: []versionCountRow{
				countRow(1, 10, "/roms/A (USA).sfc"),
				countRow(2, 10, "/roms/A (Japan).sfc"),
				countRow(3, 20, "/roms/B.sfc"),
			},
			want: map[int64]int{10: 2},
		},
		{
			name: "a disc's tracks fold into its cue",
			rows: []versionCountRow{
				countRow(1, 10, "/roms/A (USA)/A (USA).cue"),
				countRow(2, 10, "/roms/A (USA)/A (USA) (Track 1).bin"),
				countRow(3, 10, "/roms/A (USA)/A (USA) (Track 2).bin"),
				countRow(4, 10, "/roms/A (Japan)/A (Japan).cue"),
				countRow(5, 10, "/roms/A (Japan)/A (Japan) (Track 1).bin"),
			},
			want: map[int64]int{10: 2},
		},
		{
			name: "a folder with media below it is browsed, so it folds nothing",
			rows: []versionCountRow{
				countRow(1, 10, "/roms/A/A.cue"),
				countRow(2, 10, "/roms/A/A (Track 1).bin"),
				countRow(3, 30, "/roms/A/extra/B.cue"),
			},
			want: map[int64]int{10: 2},
		},
		{
			name: "a folder whose launch row belongs to another game folds nothing",
			rows: []versionCountRow{
				countRow(1, 20, "/roms/A/A.cue"),
				countRow(2, 10, "/roms/A/A (Track 1).bin"),
				countRow(3, 10, "/roms/A/A (Track 2).bin"),
			},
			want: map[int64]int{10: 2},
		},
		{
			name: "an alias folds into its target",
			rows: []versionCountRow{
				countRow(1, 10, "/roms/A.mra"),
				countRow(2, 10, "/roms/_Organized/A.mra"),
				countRow(3, 10, "/roms/_alternatives/_A/A (Japan).mra"),
			},
			links: map[int64]int64{2: 1},
			want:  map[int64]int{10: 2},
		},
		{
			name: "an alias whose target is gone counts",
			rows: []versionCountRow{
				countRow(2, 10, "/roms/_Organized/A.mra"),
				countRow(3, 10, "/roms/_alternatives/_A/A (Japan).mra"),
			},
			links: map[int64]int64{2: 1},
			want:  map[int64]int{10: 2},
		},
		{
			name: "every title of a group carries the group's count",
			rows: []versionCountRow{
				countRow(1, 10, "/roms/Contra.nes"),
				countRow(2, 20, "/roms/Probotector.nes"),
				countRow(3, 20, "/roms/Probotector (Rev 1).nes"),
				countRow(4, 30, "/roms/Other.nes"),
			},
			groups: map[int64]int64{10: 10, 20: 10},
			want:   map[int64]int{10: 3, 20: 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, versionCountsFromRows(tt.rows, tt.groups, tt.links))
		})
	}
}

func storedVersionCounts(t *testing.T, db *MediaDB, titles ...int64) database.StoredVersionCounts {
	t.Helper()
	got, err := db.GetStoredVersionCounts(context.Background(), titles, false)
	require.NoError(t, err)
	return got
}

func TestRecomputeVersionCounts(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission", "Ikari")
	ctx := context.Background()
	aso, alpha, ikari := lib.titles["ASO"], lib.titles["Alpha Mission"], lib.titles["Ikari"]

	assert.Empty(t, storedVersionCounts(t, lib.db, aso, alpha, ikari).Counts,
		"a system no regroup has counted has no counts")

	asoPath := arcadeLinkPath("ASO.mra")
	indexLinkTestMedia(t, lib.db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(asoPath, "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("Alpha Mission.mra"), "Alpha Mission", ""),
		stagedLinkTestMedia(arcadeLinkPath("Ikari.mra"), "Ikari", ""),
		stagedLinkTestMedia(arcadeLinkPath("_alternatives", "_ASO", "ASO (Japan).mra"), "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("_Organized", "_1 A-E", "ASO.mra"), "ASO", asoPath),
	)
	lib.scrape(t, "ASO", setNameWrite("aso", ""))
	lib.scrape(t, "Alpha Mission", setNameWrite("alphamis", "aso"))
	lib.regroup(t)

	assert.Equal(t, map[int64]int{aso: 3, alpha: 3, ikari: 1},
		storedVersionCounts(t, lib.db, aso, alpha, ikari).Counts,
		"ASO, its alternate and Alpha Mission; the alias is not a version")

	var stored int
	require.NoError(t, lib.db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MediaTitles WHERE VersionCount IS NOT NULL`).Scan(&stored))
	assert.Equal(t, 2, stored, "a single-version title stores nothing")

	// Nothing changed since, so a regroup counts nothing again.
	conn := lib.db.sql.Load()
	counted, written, err := sqlRecomputeVersionCounts(ctx, conn, nil)
	require.NoError(t, err)
	assert.Zero(t, counted)
	assert.Zero(t, written)

	// A regroup with no group change leaves the counts alone: only a
	// reconcile or a group change marks a system's counts stale.
	_, err = conn.ExecContext(ctx, `UPDATE MediaTitles SET VersionCount = 9 WHERE DBID = ?`, ikari)
	require.NoError(t, err)
	lib.regroup(t)
	assert.Equal(t, 9, storedVersionCounts(t, lib.db, ikari).Counts[ikari])

	// An index that loses a file recounts the system.
	indexLinkTestMedia(t, lib.db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(asoPath, "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("Alpha Mission.mra"), "Alpha Mission", ""),
		stagedLinkTestMedia(arcadeLinkPath("Ikari.mra"), "Ikari", ""),
		stagedLinkTestMedia(arcadeLinkPath("_Organized", "_1 A-E", "ASO.mra"), "ASO", asoPath),
	)
	assert.Equal(t, map[int64]int{aso: 3, alpha: 3, ikari: 9},
		storedVersionCounts(t, lib.db, aso, alpha, ikari).Counts,
		"stale counts are read until the regroup")
	lib.regroup(t)
	assert.Equal(t, map[int64]int{aso: 2, alpha: 2, ikari: 1},
		storedVersionCounts(t, lib.db, aso, alpha, ikari).Counts)

	// A scrape that changes a group recounts the system.
	_, err = conn.ExecContext(ctx, `UPDATE MediaTitles SET VersionCount = 9 WHERE DBID = ?`, ikari)
	require.NoError(t, err)
	lib.scrape(t, "Ikari", setNameWrite("ikari", "aso"))
	lib.regroup(t)
	assert.Equal(t, map[int64]int{aso: 3, alpha: 3, ikari: 3},
		storedVersionCounts(t, lib.db, aso, alpha, ikari).Counts)

	// Truncating a system forgets that it was counted.
	require.NoError(t, lib.db.TruncateSystems([]string{systemdefs.SystemArcade}))
	var countedSystems int
	require.NoError(t, conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM Systems WHERE VersionCountsState <> 0`).Scan(&countedSystems))
	assert.Zero(t, countedSystems)
}

// Re-indexing a system whose files did not change leaves its counts
// current, so the next regroup does not recount it.
func TestRecomputeVersionCounts_UnchangedReindexKeepsCounts(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission")
	ctx := context.Background()
	files := func() []database.ScanStagedMedia {
		return []database.ScanStagedMedia{
			stagedLinkTestMedia(arcadeLinkPath("ASO.mra"), "ASO", ""),
			stagedLinkTestMedia(arcadeLinkPath("Alpha Mission.mra"), "Alpha Mission", ""),
		}
	}
	indexLinkTestMedia(t, lib.db, database.ScanReconcileOpts{}, files()...)
	lib.scrape(t, "ASO", setNameWrite("aso", ""))
	lib.scrape(t, "Alpha Mission", setNameWrite("alphamis", "aso"))
	lib.regroup(t)

	conn := lib.db.sql.Load()
	state := func() int {
		var value int
		require.NoError(t, conn.QueryRowContext(ctx,
			`SELECT VersionCountsState FROM Systems WHERE SystemID = ?`, systemdefs.SystemArcade).Scan(&value))
		return value
	}
	require.Equal(t, 1, state())

	stats := indexLinkTestMedia(t, lib.db, database.ScanReconcileOpts{}, files()...)
	require.Zero(t, stats.LibraryRowsChanged, "the re-index changed nothing")
	assert.Equal(t, 1, state(), "an unchanged system's counts stay current")
}

func TestGetStoredVersionCounts_HiddenVersionsAreRecounted(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission", "Ikari", "Rygar")
	ctx := context.Background()
	aso, alpha, ikari, rygar := lib.titles["ASO"], lib.titles["Alpha Mission"],
		lib.titles["Ikari"], lib.titles["Rygar"]

	indexLinkTestMedia(t, lib.db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(arcadeLinkPath("ASO.mra"), "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("Alpha Mission.mra"), "Alpha Mission", ""),
		stagedLinkTestMedia(arcadeLinkPath("Ikari.mra"), "Ikari", ""),
		stagedLinkTestMedia(arcadeLinkPath("Rygar.mra"), "Rygar", ""),
		stagedLinkTestMedia(arcadeLinkPath("_alternatives", "_Rygar", "Rygar (Japan).mra"), "Rygar", ""),
	)
	lib.scrape(t, "ASO", setNameWrite("aso", ""))
	lib.scrape(t, "Alpha Mission", setNameWrite("alphamis", "aso"))
	lib.regroup(t)

	titles := []int64{aso, alpha, ikari, rygar}
	got, err := lib.db.GetStoredVersionCounts(ctx, titles, true)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int{aso: 2, alpha: 2, ikari: 1, rygar: 2}, got.Counts)
	assert.Empty(t, got.Recount, "nothing is hidden")

	// Hiding Alpha Mission's file sends both titles of its group to a live
	// count; the other titles keep their stored counts.
	require.NoError(t, lib.db.UpdateMediaTags(ctx, lib.media["Alpha Mission"].DBID, nil,
		[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	got, err = lib.db.GetStoredVersionCounts(ctx, titles, true)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int{ikari: 1, rygar: 2}, got.Counts)
	assert.Equal(t, []int64{aso, alpha}, got.Recount)

	got, err = lib.db.GetStoredVersionCounts(ctx, titles, false)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int{aso: 2, alpha: 2, ikari: 1, rygar: 2}, got.Counts,
		"a browse that shows hidden media reads the stored count")
	assert.Empty(t, got.Recount)

	// A hidden folder holding one of Rygar's versions does the same.
	_, err = lib.db.ReplaceHiddenDirectories(ctx, []database.HiddenDirectory{{
		SystemID: systemdefs.SystemArcade, Path: arcadeLinkPath("_alternatives"),
	}})
	require.NoError(t, err)
	got, err = lib.db.GetStoredVersionCounts(ctx, titles, true)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int{ikari: 1}, got.Counts)
	assert.ElementsMatch(t, []int64{aso, alpha, rygar}, got.Recount)
}
