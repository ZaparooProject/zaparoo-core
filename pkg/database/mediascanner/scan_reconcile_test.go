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

package mediascanner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mediaTagStrings returns a media row's tags as natural "type:value" strings.
func mediaTagStrings(t *testing.T, db *mediadb.MediaDB, mediaDBID int64) []string {
	t.Helper()
	rows, err := db.UnsafeGetSQLDb().QueryContext(context.Background(), `
		SELECT TagTypes.Type, Tags.Tag
		FROM MediaTags
		JOIN Tags ON Tags.DBID = MediaTags.TagDBID
		JOIN TagTypes ON TagTypes.DBID = Tags.TypeDBID
		WHERE MediaTags.MediaDBID = ?`, mediaDBID)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var got []string
	for rows.Next() {
		var typ, val string
		require.NoError(t, rows.Scan(&typ, &val))
		got = append(got, typ+":"+tags.UnpadTagValue(val))
	}
	require.NoError(t, rows.Err())
	return got
}

// mediaBySystem returns the system's media rows keyed by path.
func mediaBySystem(t *testing.T, db *mediadb.MediaDB, systemID string) map[string]database.MediaWithFullPath {
	t.Helper()
	rows, err := db.GetMediaBySystemID(systemID)
	require.NoError(t, err)
	byPath := make(map[string]database.MediaWithFullPath, len(rows))
	for _, row := range rows {
		byPath[row.Path] = row
	}
	return byPath
}

func titleDisambiguation(t *testing.T, db *mediadb.MediaDB, titleDBID int64) string {
	t.Helper()
	var types string
	require.NoError(t, db.UnsafeGetSQLDb().QueryRowContext(context.Background(),
		"SELECT DisambiguationTypes FROM MediaTitles WHERE DBID = ?", titleDBID).Scan(&types))
	return types
}

func TestReconcile_NewSystemInsertsTitlesMediaAndTags(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Super Game (USA) (Rev 2).sfc")
	stats := indexMediaPaths(t, mediaDB, "SNES", gamePath)

	assert.True(t, stats.SystemKnown)
	assert.Equal(t, int64(1), stats.TitlesInserted)
	assert.Equal(t, int64(1), stats.MediaUpserted)
	assert.Equal(t, int64(0), stats.MediaMissing)
	assert.Equal(t, int64(0), stats.TouchedTitles, "new singleton titles do not need disambiguation")

	byPath := mediaBySystem(t, mediaDB, "SNES")
	require.Len(t, byPath, 1)
	row := byPath[gamePath]
	require.NotZero(t, row.DBID)
	assert.False(t, row.IsMissing)
	assert.Equal(t, mediadb.ParentDirForMediaPath(gamePath), row.ParentDir)
	assert.Equal(t, "Super Game", row.SortName)

	got := mediaTagStrings(t, mediaDB, row.DBID)
	assert.Contains(t, got, "region:us")
	assert.Contains(t, got, "rev:2")
	assert.Contains(t, got, "extension:sfc")
}

// TestReconcile_MultiDiscSharesOneTitleAcrossMidScanCommit pins the multi-disc
// invariant the old in-memory pipeline could fragment: two files of one title
// staged either side of a mid-system commit must still share one MediaTitle row.
// The set-based title insert dedupes by (SystemDBID, Slug) in SQL, so the commit
// boundary is irrelevant by construction.
func TestReconcile_MultiDiscSharesOneTitleAcrossMidScanCommit(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	disc1 := filepath.Join(string(filepath.Separator), "roms", "PSX", "Final Fantasy VII (Disc 1).cue")
	disc2 := filepath.Join(string(filepath.Separator), "roms", "PSX", "Final Fantasy VII (Disc 2).cue")

	require.NoError(t, SeedCanonicalTags(ctx, mediaDB))
	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, mediaDB.ClearScanStage())
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "PSX", Path: disc1}))
	// Mid-system file-limit commit: staged rows become durable, scan continues.
	require.NoError(t, mediaDB.CommitTransaction())
	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "PSX", Path: disc2}))
	stats, err := mediaDB.ReconcileStagedSystem(ctx, "PSX", database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	assert.Equal(t, int64(1), stats.TitlesInserted, "both discs share one title")
	byPath := mediaBySystem(t, mediaDB, "PSX")
	require.Len(t, byPath, 2)
	assert.Equal(t, byPath[disc1].MediaTitleDBID, byPath[disc2].MediaTitleDBID,
		"both discs must reference the same MediaTitle row")
}

func TestReconcile_NewMultiMediaTitleComputesDisambiguation(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	usaPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Super Game (USA).sfc")
	eurPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Super Game (Europe).sfc")
	stats := indexMediaPaths(t, mediaDB, "SNES", usaPath, eurPath)

	assert.Equal(t, int64(1), stats.TouchedTitles, "new sibling media title needs disambiguation")
	byPath := mediaBySystem(t, mediaDB, "SNES")
	require.Equal(t, byPath[usaPath].MediaTitleDBID, byPath[eurPath].MediaTitleDBID)
	assert.Contains(t, titleDisambiguation(t, mediaDB, byPath[usaPath].MediaTitleDBID), "region")
}

func TestReconcile_UnchangedReindexIsNoOp(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	paths := []string{
		filepath.Join(string(filepath.Separator), "roms", "SNES", "Alpha (USA).sfc"),
		// Leading-zero numeric segment: "Rev 02" must collapse onto the stored
		// "rev:2" tag instead of churning a phantom re-insert every re-index.
		filepath.Join(string(filepath.Separator), "roms", "SNES", "Beta (Europe) (Rev 02).sfc"),
	}
	indexMediaPaths(t, mediaDB, "SNES", paths...)

	stats := indexMediaPaths(t, mediaDB, "SNES", paths...)
	assert.Equal(t, int64(0), stats.TitlesInserted)
	assert.Equal(t, int64(0), stats.TitlesRenamed)
	assert.Equal(t, int64(0), stats.MediaUpserted)
	assert.Equal(t, int64(0), stats.MediaMissing)
	assert.Equal(t, int64(0), stats.TagsInserted)
	assert.Equal(t, int64(0), stats.TagLinksAdded)
	assert.Equal(t, int64(0), stats.TagLinksDeleted)
	assert.Equal(t, int64(0), stats.TouchedTitles,
		"an unchanged re-index must not touch any title")
}

// TestReconcile_MultiVariantSystemUnchangedReindexTouchesNothing exercises
// C64-style data — numeric disc/rev tags (which exercise PadTagValue), region
// tags, and multiple media sharing one title — and pins that a byte-identical
// re-index touches nothing, so the disambiguation recompute and tags-cache
// rebuild are skipped.
func TestReconcile_MultiVariantSystemUnchangedReindexTouchesNothing(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	dir := filepath.Join(string(filepath.Separator), "media", "fat", "games", "C64")
	paths := []string{
		filepath.Join(dir, "Commando (USA).d64"),
		filepath.Join(dir, "Commando (Europe).d64"),
		filepath.Join(dir, "Great Giana Sisters (Germany) (Disk 1 of 2).d64"),
		filepath.Join(dir, "Great Giana Sisters (Germany) (Disk 2 of 2).d64"),
		filepath.Join(dir, "Boulder Dash (USA) (Rev 1).d64"),
		filepath.Join(dir, "Boulder Dash (USA) (Rev 2).d64"),
	}
	indexMediaPaths(t, mediaDB, "C64", paths...)

	stats := indexMediaPaths(t, mediaDB, "C64", paths...)
	assert.Equal(t, int64(0), stats.MediaUpserted)
	assert.Equal(t, int64(0), stats.TagLinksAdded)
	assert.Equal(t, int64(0), stats.TagLinksDeleted)
	assert.Equal(t, int64(0), stats.TouchedTitles,
		"unchanged re-index of a multi-variant system must not touch any title")
}

func TestReconcile_DynamicTagTypesPersisted(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(t, SeedCanonicalTags(ctx, mediaDB))

	arcadePath := filepath.Join("_Arcade", "Super Street Fighter II The New Challengers (World 931005).mra")
	trackPath := filepath.Join("SNESMusic", "Star Fox [01].spc")

	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, mediaDB.ClearScanStage())
	require.NoError(t, StageMediaPath(&StageMediaPathParams{
		DB: mediaDB, SystemID: "Arcade", Path: arcadePath, MediaType: slugs.MediaTypeGame,
	}))
	_, err := mediaDB.ReconcileStagedSystem(ctx, "Arcade", database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, StageMediaPath(&StageMediaPathParams{
		DB: mediaDB, SystemID: "SNESMusic", Path: trackPath, MediaType: slugs.MediaTypeMusic,
	}))
	_, err = mediaDB.ReconcileStagedSystem(ctx, "SNESMusic", database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	arcadeRows := mediaBySystem(t, mediaDB, "Arcade")
	require.Len(t, arcadeRows, 1)
	got := mediaTagStrings(t, mediaDB, arcadeRows[arcadePath].DBID)
	assert.Contains(t, got, "builddate:1993-10-05", "build date tag should persist through indexing")
	assert.Contains(t, got, "region:world")

	musicRows := mediaBySystem(t, mediaDB, "SNESMusic")
	require.Len(t, musicRows, 1)
	got = mediaTagStrings(t, mediaDB, musicRows[trackPath].DBID)
	assert.Contains(t, got, "track:1", "track tag should persist through indexing")
}

func TestReconcile_NonScannerTagsSurviveStaleScannerTagDeleted(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	gamePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Gamma (USA).sfc")
	indexMediaPaths(t, mediaDB, "SNES", gamePath)

	row := mediaBySystem(t, mediaDB, "SNES")[gamePath]
	require.NotZero(t, row.DBID)

	// Plant a user tag and a scraper-owned genre tag (non-scanner types), plus
	// a scanner-owned region tag the filename does not carry (stale).
	require.NoError(t, mediaDB.UpsertMediaTags(ctx, row.DBID, []database.TagInfo{
		{Type: "user", Tag: "favourite-shelf"},
		{Type: "genre", Tag: "platformer"},
		{Type: "region", Tag: "eu"},
	}))

	stats := indexMediaPaths(t, mediaDB, "SNES", gamePath)

	got := mediaTagStrings(t, mediaDB, row.DBID)
	assert.Contains(t, got, "user:favourite-shelf", "user tags must survive re-index")
	assert.Contains(t, got, "genre:platformer", "scraper-owned tags must survive re-index")
	assert.Contains(t, got, "region:us")
	assert.NotContains(t, got, "region:eu", "stale scanner-owned tag must be deleted")
	assert.Equal(t, int64(1), stats.TagLinksDeleted)
	assert.Equal(t, int64(0), stats.TouchedTitles, "singleton tag changes do not need disambiguation")
}

func TestReconcile_MissingFlagLifecycle(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	keptPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Kept (USA).sfc")
	lostPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Lost (USA).sfc")
	indexMediaPaths(t, mediaDB, "SNES", keptPath, lostPath)

	// Re-index without the second file: it flips to missing, touching its title.
	stats := indexMediaPaths(t, mediaDB, "SNES", keptPath)
	assert.Equal(t, int64(1), stats.MediaMissing)
	assert.Equal(t, int64(1), stats.TouchedTitles)
	byPath := mediaBySystem(t, mediaDB, "SNES")
	assert.False(t, byPath[keptPath].IsMissing)
	assert.True(t, byPath[lostPath].IsMissing, "unscanned media must be flagged missing, not deleted")

	missing, err := mediaDB.GetMissingMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, missing)

	// The file comes back: flag clears, title touched again.
	stats = indexMediaPaths(t, mediaDB, "SNES", keptPath, lostPath)
	assert.Equal(t, int64(0), stats.MediaMissing)
	assert.Equal(t, int64(1), stats.MediaUpserted, "re-found media is updated in place")
	assert.Equal(t, int64(1), stats.TouchedTitles)
	byPath = mediaBySystem(t, mediaDB, "SNES")
	assert.False(t, byPath[lostPath].IsMissing)

	missing, err = mediaDB.GetMissingMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 0, missing)
}

// TestReconcile_IncompleteScanKeepsMissingState pins the incomplete-scan
// contract: when file collection errored (unmounted path, failed scanner), a
// file's absence from the stage proves nothing, so no missing flags are set —
// but staged files still upsert and re-found rows still clear their flag.
func TestReconcile_IncompleteScanKeepsMissingState(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	keptPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Kept (USA).sfc")
	unreadPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Unread (USA).sfc")
	gonePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Gone (USA).sfc")
	indexMediaPaths(t, mediaDB, "SNES", keptPath, unreadPath, gonePath)

	// A prior complete scan legitimately flagged one row missing.
	indexMediaPaths(t, mediaDB, "SNES", keptPath, unreadPath)
	require.True(t, mediaBySystem(t, mediaDB, "SNES")[gonePath].IsMissing)

	// Incomplete scan that only collected keptPath: unreadPath must keep its
	// present state, gonePath must keep its missing state.
	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, mediaDB.ClearScanStage())
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "SNES", Path: keptPath}))
	stats, err := mediaDB.ReconcileStagedSystem(ctx, "SNES", database.ScanReconcileOpts{IncompleteScan: true})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	assert.Equal(t, int64(0), stats.MediaMissing, "incomplete scan must not flag missing media")
	assert.Equal(t, int64(0), stats.TouchedTitles)
	byPath := mediaBySystem(t, mediaDB, "SNES")
	assert.False(t, byPath[keptPath].IsMissing)
	assert.False(t, byPath[unreadPath].IsMissing, "absence during an incomplete scan proves nothing")
	assert.True(t, byPath[gonePath].IsMissing, "previously missing rows keep their state")

	// A re-found file during an incomplete scan is still definitive evidence
	// of presence: its flag clears.
	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, mediaDB.ClearScanStage())
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "SNES", Path: gonePath}))
	_, err = mediaDB.ReconcileStagedSystem(ctx, "SNES", database.ScanReconcileOpts{IncompleteScan: true})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())
	assert.False(t, mediaBySystem(t, mediaDB, "SNES")[gonePath].IsMissing,
		"a staged file always clears its missing flag, even on an incomplete scan")
}

func TestReconcile_EmptyScanMarksSystemMissing(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Solo (USA).sfc")
	indexMediaPaths(t, mediaDB, "SNES", gamePath)

	stats := indexMediaPaths(t, mediaDB, "SNES")
	assert.True(t, stats.SystemKnown, "existing system reconciles even with nothing staged")
	assert.Equal(t, int64(1), stats.MediaMissing)
	assert.True(t, mediaBySystem(t, mediaDB, "SNES")[gamePath].IsMissing)
}

func TestReconcile_InvalidatesCachedMissingCount(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Solo (USA).sfc")
	indexMediaPaths(t, mediaDB, "SNES", gamePath)
	_, err := mediaDB.UnsafeGetSQLDb().ExecContext(context.Background(),
		"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, '0')", mediadb.DBConfigMediaMissingCount)
	require.NoError(t, err)

	indexMediaPaths(t, mediaDB, "SNES")
	missing, err := mediaDB.GetMissingMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 1, missing)
}

func TestReconcile_UnknownSystemWithNoFilesIsNoop(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	stats := indexMediaPaths(t, mediaDB, "NeverSeen")
	assert.False(t, stats.SystemKnown)

	_, err := mediaDB.FindSystemBySystemID("NeverSeen")
	require.Error(t, err, "no Systems row may be created for an empty unknown system")
}

func TestReconcile_TitleRenameRefreshesName(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Delta Quest (USA).sfc")
	indexMediaPaths(t, mediaDB, "SNES", gamePath)

	row := mediaBySystem(t, mediaDB, "SNES")[gamePath]
	// Simulate a stale stored name (e.g. produced by an older parser version).
	_, err := mediaDB.UnsafeGetSQLDb().ExecContext(context.Background(),
		"UPDATE MediaTitles SET Name = 'Stale Name' WHERE DBID = ?", row.MediaTitleDBID)
	require.NoError(t, err)

	stats := indexMediaPaths(t, mediaDB, "SNES", gamePath)
	assert.Equal(t, int64(1), stats.TitlesRenamed)

	var name string
	require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(context.Background(),
		"SELECT Name FROM MediaTitles WHERE DBID = ?", row.MediaTitleDBID).Scan(&name))
	assert.Equal(t, "Delta Quest", name)
}

func TestReconcile_RepairsParentDirAndSortName(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	gamePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Epsilon (USA).sfc")
	indexMediaPaths(t, mediaDB, "SNES", gamePath)

	row := mediaBySystem(t, mediaDB, "SNES")[gamePath]
	_, err := mediaDB.UnsafeGetSQLDb().ExecContext(context.Background(),
		"UPDATE Media SET ParentDir = 'wrong', SortName = '' WHERE DBID = ?", row.DBID)
	require.NoError(t, err)

	stats := indexMediaPaths(t, mediaDB, "SNES", gamePath)
	assert.Equal(t, int64(1), stats.MediaUpserted)

	repaired := mediaBySystem(t, mediaDB, "SNES")[gamePath]
	assert.Equal(t, mediadb.ParentDirForMediaPath(gamePath), repaired.ParentDir)
	assert.Equal(t, "Epsilon", repaired.SortName)
}

func TestReconcile_TitleReassignmentTouchesBothTitles(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	firstPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "First Game (USA).sfc")
	secondPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Second Game (USA).sfc")
	indexMediaPaths(t, mediaDB, "SNES", firstPath, secondPath)

	byPath := mediaBySystem(t, mediaDB, "SNES")
	firstTitle := byPath[firstPath].MediaTitleDBID
	secondTitle := byPath[secondPath].MediaTitleDBID
	require.NotEqual(t, firstTitle, secondTitle)

	// Simulate a stale assignment: point the first media at the second title.
	_, err := mediaDB.UnsafeGetSQLDb().ExecContext(ctx,
		"UPDATE Media SET MediaTitleDBID = ? WHERE DBID = ?", secondTitle, byPath[firstPath].DBID)
	require.NoError(t, err)

	stats := indexMediaPaths(t, mediaDB, "SNES", firstPath, secondPath)
	assert.Equal(t, int64(1), stats.MediaUpserted, "reassigned media is re-pointed at its slug's title")
	assert.Equal(t, int64(2), stats.TouchedTitles, "both the losing and gaining title are touched")

	repaired := mediaBySystem(t, mediaDB, "SNES")
	assert.Equal(t, firstTitle, repaired[firstPath].MediaTitleDBID)
}

func TestReconcile_DisambiguationRecomputedForSiblings(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	usaPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Zeta Quest (USA).sfc")
	eurPath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Zeta Quest (Europe).sfc")
	indexMediaPaths(t, mediaDB, "SNES", usaPath, eurPath)

	byPath := mediaBySystem(t, mediaDB, "SNES")
	require.Equal(t, byPath[usaPath].MediaTitleDBID, byPath[eurPath].MediaTitleDBID)
	assert.Contains(t, titleDisambiguation(t, mediaDB, byPath[usaPath].MediaTitleDBID), "region",
		"siblings differing only by region must be region-disambiguated after reconcile")
}

func TestReconcile_ProvidedNameUsedAsTitle(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(t, SeedCanonicalTags(ctx, mediaDB))

	virtualPath := "steam://12345/Custom%20Game"
	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, mediaDB.ClearScanStage())
	require.NoError(t, StageMediaPath(&StageMediaPathParams{
		DB: mediaDB, SystemID: "PC", Path: virtualPath, ProvidedName: "My Custom Game", NoExt: true,
	}))
	_, err := mediaDB.ReconcileStagedSystem(ctx, "PC", database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	titles, err := mediaDB.GetTitlesBySystemID("PC")
	require.NoError(t, err)
	require.Len(t, titles, 1)
	assert.Equal(t, "My Custom Game", titles[0].Name)
}

// TestReconcile_DuplicatePathStagedOnceKeepsSingleRow: the same path scanned
// twice in one run (e.g. surfaced by both the filesystem walk and a launcher
// scanner) must produce exactly one Media row. The ScanStage primary key
// dedupes at staging time.
func TestReconcile_DuplicatePathStagedOnceKeepsSingleRow(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(t, SeedCanonicalTags(ctx, mediaDB))

	gamePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Twice (USA).sfc")
	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, mediaDB.ClearScanStage())
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "SNES", Path: gamePath}))
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "SNES", Path: gamePath}))
	stats, err := mediaDB.ReconcileStagedSystem(ctx, "SNES", database.ScanReconcileOpts{})
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	assert.Equal(t, int64(1), stats.MediaUpserted)
	assert.Len(t, mediaBySystem(t, mediaDB, "SNES"), 1)
}

// TestReconcile_CrashLeftoverStagingIsIsolated ensures rows left staged by a
// crashed run (durable via a mid-system commit) cannot bleed into the next
// system's reconcile: staging is cleared at the start of every system scan.
func TestReconcile_CrashLeftoverStagingIsIsolated(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	require.NoError(t, SeedCanonicalTags(ctx, mediaDB))

	// Simulate the crash: stage a file, commit, never reconcile.
	stalePath := filepath.Join(string(filepath.Separator), "roms", "SNES", "Stale (USA).sfc")
	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "SNES", Path: stalePath}))
	require.NoError(t, mediaDB.CommitTransaction())

	// Next run scans a different system; the leftover row must not surface.
	genesisPath := filepath.Join(string(filepath.Separator), "roms", "Genesis", "Fresh (USA).md")
	indexMediaPaths(t, mediaDB, "Genesis", genesisPath)

	rows := mediaBySystem(t, mediaDB, "Genesis")
	require.Len(t, rows, 1)
	assert.NotContains(t, rows, stalePath)
}
