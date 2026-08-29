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
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the staged-set fingerprint that lets reconcile skip a system
// whose staged input and stored state are unchanged since its last reconcile
// (#1317). A false skip hides real media changes and a user would only see
// missing media, so most of the cases here are the ways the skip must NOT
// happen.

func snesPath(name string) string {
	return filepath.Join(string(filepath.Separator), "roms", "SNES", name)
}

// stagedSNESMedia builds a staged row by hand so a test can vary the fields
// reconcile consumes — tags, properties, slug — while keeping the path fixed,
// which StageMediaPath (deriving everything from the filename) cannot do.
func stagedSNESMedia(path string, stagedTags ...database.ScanStagedTag) database.ScanStagedMedia {
	return database.ScanStagedMedia{
		Path:          path,
		ParentDir:     mediadb.ParentDirForMediaPath(filepath.ToSlash(path)),
		Slug:          "somegame",
		TitleName:     "Some Game",
		SortName:      "Some Game",
		SlugLength:    8,
		SlugWordCount: 2,
		Tags:          stagedTags,
	}
}

// stageAndReconcile runs pre-built staged rows through seeding, staging,
// reconcile and commit, rolling back on a reconcile error so the caller can
// assert on the state a failed run leaves behind.
func stageAndReconcile(
	t *testing.T, db *mediadb.MediaDB, systemID string, opts database.ScanReconcileOpts,
	media ...database.ScanStagedMedia,
) (*database.ScanReconcileStats, error) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, SeedCanonicalTags(ctx, db))
	require.NoError(t, db.BeginTransaction(true))
	require.NoError(t, db.ClearScanStage())
	for i := range media {
		require.NoError(t, db.StageScannedMedia(&media[i]))
	}
	stats, err := db.ReconcileStagedSystem(ctx, systemID, opts)
	if err != nil {
		require.NoError(t, db.RollbackTransaction())
		return &stats, fmt.Errorf("reconcile staged system: %w", err)
	}
	require.NoError(t, db.CommitTransaction())
	return &stats, nil
}

func fingerprintRowCount(t *testing.T, db database.MediaDBI) int {
	t.Helper()
	var n int
	require.NoError(t, db.UnsafeGetSQLDb().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM ScanSystemFingerprints").Scan(&n))
	return n
}

// reindex is indexMediaPaths returning its stats by pointer, so the assertion
// helpers below can take them without copying the struct per call.
func reindex(t *testing.T, db database.MediaDBI, systemID string, paths ...string) *database.ScanReconcileStats {
	t.Helper()
	stats := indexMediaPaths(t, db, systemID, paths...)
	return &stats
}

func assertReconcileRan(t *testing.T, stats *database.ScanReconcileStats) {
	t.Helper()
	assert.False(t, stats.Unchanged, "reconcile must not be skipped")
}

func assertReconcileSkipped(t *testing.T, stats *database.ScanReconcileStats) {
	t.Helper()
	assert.True(t, stats.Unchanged, "an unchanged system must be skipped")
	assert.Zero(t, stats.TitlesInserted+stats.TitlesRenamed+stats.MediaUpserted+stats.MediaMissing+
		stats.TagsInserted+stats.TagLinksAdded+stats.TagLinksDeleted+stats.TouchedTitles,
		"a skipped reconcile reports no work")
}

func TestScanFingerprint_UnchangedReindexIsSkipped(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	paths := []string{snesPath("Alpha (USA).sfc"), snesPath("Beta (Europe) (Rev 2).sfc")}
	first := reindex(t, mediaDB, "SNES", paths...)
	assertReconcileRan(t, first)
	assert.Equal(t, 1, fingerprintRowCount(t, mediaDB), "a successful reconcile stores its fingerprint")
	before := mediaBySystem(t, mediaDB, "SNES")

	second := reindex(t, mediaDB, "SNES", paths...)
	assertReconcileSkipped(t, second)
	assert.Equal(t, before, mediaBySystem(t, mediaDB, "SNES"), "a skipped reconcile leaves the rows as they were")
	assert.Subset(t, mediaTagStrings(t, mediaDB, before[paths[0]].DBID), []string{"region:us", "extension:sfc"})

	// Skipping is stable: nothing about a skipped run changes the next decision.
	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", paths...))
}

func TestScanFingerprint_SkipClearsStagingTables(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	path := snesPath("Alpha (USA).sfc")
	reindex(t, mediaDB, "SNES", path)
	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", path))

	for _, table := range []string{"ScanStage", "ScanStageTags", "ScanStageProperties"} {
		var n int
		require.NoError(t, mediaDB.UnsafeGetSQLDb().QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM "+table).Scan(&n))
		assert.Zero(t, n, "%s must be cleared by a skipped reconcile", table)
	}
}

// The digest is over rows in primary-key order, not staging order, so the
// order the filesystem walk happens to yield files in cannot defeat it.
func TestScanFingerprint_StagingOrderDoesNotMatter(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	a, b, c := snesPath("Alpha (USA).sfc"), snesPath("Beta (USA).sfc"), snesPath("Gamma (Japan).sfc")
	reindex(t, mediaDB, "SNES", a, b, c)
	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", c, a, b))
}

func TestScanFingerprint_AddedFileReconciles(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	a, b := snesPath("Alpha (USA).sfc"), snesPath("Beta (USA).sfc")
	reindex(t, mediaDB, "SNES", a)

	stats := reindex(t, mediaDB, "SNES", a, b)
	assertReconcileRan(t, stats)
	assert.Equal(t, int64(1), stats.MediaUpserted)
	require.Len(t, mediaBySystem(t, mediaDB, "SNES"), 2)

	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", a, b))
}

func TestScanFingerprint_RemovedFileReconciles(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	a, b := snesPath("Alpha (USA).sfc"), snesPath("Beta (USA).sfc")
	reindex(t, mediaDB, "SNES", a, b)

	stats := reindex(t, mediaDB, "SNES", a)
	assertReconcileRan(t, stats)
	assert.Equal(t, int64(1), stats.MediaMissing)
	assert.True(t, mediaBySystem(t, mediaDB, "SNES")[b].IsMissing)

	// The reduced set is the new steady state and skips like any other.
	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", a))

	// The file coming back is a staged-set change again.
	stats = reindex(t, mediaDB, "SNES", a, b)
	assertReconcileRan(t, stats)
	assert.False(t, mediaBySystem(t, mediaDB, "SNES")[b].IsMissing)
}

// A parser or configuration change that alters what is derived from an
// unchanged filename must move the fingerprint: the tags are part of the
// staged set, not just the paths.
func TestScanFingerprint_StagedTagChangeReconciles(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	path := snesPath("Some Game.sfc")
	usa := database.ScanStagedTag{Type: string(tags.TagTypeRegion), Value: "us"}
	europe := database.ScanStagedTag{Type: string(tags.TagTypeRegion), Value: "eu"}

	_, err := stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{}, stagedSNESMedia(path, usa))
	require.NoError(t, err)
	stats, err := stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{}, stagedSNESMedia(path, usa))
	require.NoError(t, err)
	assertReconcileSkipped(t, stats)

	stats, err = stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{}, stagedSNESMedia(path, europe))
	require.NoError(t, err)
	assertReconcileRan(t, stats)
	assert.Equal(t, int64(1), stats.TagLinksAdded)
	assert.Equal(t, int64(1), stats.TagLinksDeleted)
	row := mediaBySystem(t, mediaDB, "SNES")[path]
	assert.ElementsMatch(t, []string{"region:eu"}, mediaTagStrings(t, mediaDB, row.DBID))
}

// The same path staged with a different title derivation (slug, name, sort
// name) is a changed set even though no file was added or removed.
func TestScanFingerprint_StagedTitleChangeReconciles(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	path := snesPath("Some Game.sfc")
	original := stagedSNESMedia(path)
	_, err := stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{}, original)
	require.NoError(t, err)

	renamed := original
	renamed.TitleName = "Some Game Deluxe"
	renamed.SortName = "Some Game Deluxe"
	stats, err := stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{}, renamed)
	require.NoError(t, err)
	assertReconcileRan(t, stats)
	assert.Equal(t, int64(1), stats.TitlesRenamed)
	assert.Equal(t, "Some Game Deluxe", mediaBySystem(t, mediaDB, "SNES")[path].SortName)
}

// The scanner only stages a property it could not find in the database, so a
// staged property is new work by construction and can never be skipped over.
func TestScanFingerprint_StagedPropertyForcesReconcile(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	path := snesPath("Some Game.sfc")
	plain := stagedSNESMedia(path)
	_, err := stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{}, plain)
	require.NoError(t, err)

	withProperty := plain
	withProperty.Properties = []database.ScanStagedProperty{{
		Type: string(tags.TagTypeProperty),
		Name: string(tags.TagPropertyGameID),
		Text: "SNS-XX-USA",
	}}
	for range 2 {
		stats, runErr := stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{}, withProperty)
		require.NoError(t, runErr)
		assertReconcileRan(t, stats)
	}

	stats, err := stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{}, plain)
	require.NoError(t, err)
	assertReconcileSkipped(t, stats)
}

// Anything that edits the rows reconcile owns behind its back — here a
// scanner-owned tag written through the same API the scraper and tag editing
// use, and a media row deleted outright — must fail open into a reconcile
// even though the staged set is identical. This is the stored-state half of
// the fingerprint; the media-count guard alone would miss the first case.
func TestScanFingerprint_OutOfBandStateChangeReconciles(t *testing.T) {
	t.Parallel()

	t.Run("scanner-owned tag link added", func(t *testing.T) {
		t.Parallel()
		mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
		t.Cleanup(cleanup)

		path := snesPath("Alpha (USA).sfc")
		reindex(t, mediaDB, "SNES", path)
		row := mediaBySystem(t, mediaDB, "SNES")[path]
		require.NoError(t, mediaDB.UpsertMediaTags(context.Background(), row.DBID, []database.TagInfo{
			{Type: string(tags.TagTypeRegion), Tag: "eu"},
		}))

		stats := reindex(t, mediaDB, "SNES", path)
		assertReconcileRan(t, stats)
		assert.Equal(t, int64(1), stats.TagLinksDeleted, "the stale link is removed by the reconcile that ran")
		assert.NotContains(t, mediaTagStrings(t, mediaDB, row.DBID), "region:eu")

		assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", path))
	})

	t.Run("media row deleted", func(t *testing.T) {
		t.Parallel()
		mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
		t.Cleanup(cleanup)

		a, b := snesPath("Alpha (USA).sfc"), snesPath("Beta (USA).sfc")
		reindex(t, mediaDB, "SNES", a, b)
		_, err := mediaDB.UnsafeGetSQLDb().ExecContext(context.Background(),
			"DELETE FROM Media WHERE DBID = ?", mediaBySystem(t, mediaDB, "SNES")[b].DBID)
		require.NoError(t, err)

		stats := reindex(t, mediaDB, "SNES", a, b)
		assertReconcileRan(t, stats)
		assert.Equal(t, int64(1), stats.MediaUpserted, "the deleted row is re-inserted")
		require.Len(t, mediaBySystem(t, mediaDB, "SNES"), 2)
	})

	t.Run("media flagged missing", func(t *testing.T) {
		t.Parallel()
		mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
		t.Cleanup(cleanup)

		path := snesPath("Alpha (USA).sfc")
		reindex(t, mediaDB, "SNES", path)
		_, err := mediaDB.UnsafeGetSQLDb().ExecContext(context.Background(),
			"UPDATE Media SET IsMissing = 1 WHERE DBID = ?", mediaBySystem(t, mediaDB, "SNES")[path].DBID)
		require.NoError(t, err)

		stats := reindex(t, mediaDB, "SNES", path)
		assertReconcileRan(t, stats)
		assert.False(t, mediaBySystem(t, mediaDB, "SNES")[path].IsMissing, "the file is on disk, so it is re-found")
	})
}

// A stored fingerprint from a different generation — a release whose
// reconcile, schema, tag vocabulary or disambiguation version differs — never
// matches. Modelled by rewriting the stored digest, which is what any of those
// changes does to the comparison.
func TestScanFingerprint_GenerationMismatchReconciles(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	path := snesPath("Alpha (USA).sfc")
	reindex(t, mediaDB, "SNES", path)
	_, err := mediaDB.UnsafeGetSQLDb().ExecContext(context.Background(),
		"UPDATE ScanSystemFingerprints SET Fingerprint = 'written-by-another-generation'")
	require.NoError(t, err)

	assertReconcileRan(t, reindex(t, mediaDB, "SNES", path))
	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", path))
}

// An incomplete scan stages a subset of the library, so it must neither be
// compared against a full scan's fingerprint nor leave one behind: the first
// full scan after it reconciles, and only then does skipping resume.
func TestScanFingerprint_IncompleteScanNeverSkipsAndForgetsFingerprint(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	a, b := snesPath("Alpha (USA).sfc"), snesPath("Beta (USA).sfc")
	reindex(t, mediaDB, "SNES", a, b)
	require.Equal(t, 1, fingerprintRowCount(t, mediaDB))

	incomplete := database.ScanReconcileOpts{IncompleteScan: true}
	ctx := context.Background()
	require.NoError(t, mediaDB.BeginTransaction(true))
	require.NoError(t, mediaDB.ClearScanStage())
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "SNES", Path: a}))
	require.NoError(t, StageMediaPath(&StageMediaPathParams{DB: mediaDB, SystemID: "SNES", Path: b}))
	stats, err := mediaDB.ReconcileStagedSystem(ctx, "SNES", incomplete)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())
	assertReconcileRan(t, &stats)
	assert.Zero(t, fingerprintRowCount(t, mediaDB), "an incomplete scan clears the stored fingerprint")

	assertReconcileRan(t, reindex(t, mediaDB, "SNES", a, b))
	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", a, b))
}

// The fingerprint is written inside the reconcile's transaction, so a run that
// is rolled back — here cancelled through the pacing hook, the path the
// scanner's own cancellation takes — leaves the previous fingerprint in place
// and the rows it describes untouched. If the aborted run's fingerprint leaked,
// the next scan of the same set would be skipped with the new file missing.
func TestScanFingerprint_RolledBackReconcileLeavesNoFingerprint(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	a, b := snesPath("Alpha (USA).sfc"), snesPath("Beta (USA).sfc")
	reindex(t, mediaDB, "SNES", a)

	cancelled := errors.New("indexing cancelled")
	yields := 0
	_, err := stageAndReconcile(t, mediaDB, "SNES", database.ScanReconcileOpts{
		Yield: func() error {
			yields++
			if yields > 3 {
				return cancelled
			}
			return nil
		},
	}, stagedSNESMedia(a), stagedSNESMedia(b))
	require.ErrorIs(t, err, cancelled)
	require.Len(t, mediaBySystem(t, mediaDB, "SNES"), 1, "the rolled-back reconcile wrote nothing")

	stats := reindex(t, mediaDB, "SNES", a, b)
	assertReconcileRan(t, stats)
	assert.Equal(t, int64(1), stats.MediaUpserted)
	require.Len(t, mediaBySystem(t, mediaDB, "SNES"), 2)
}

func TestScanFingerprint_TruncatedSystemReconcilesFresh(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	path := snesPath("Alpha (USA).sfc")
	reindex(t, mediaDB, "SNES", path)
	require.NoError(t, mediaDB.TruncateSystems([]string{"SNES"}))
	assert.Zero(t, fingerprintRowCount(t, mediaDB), "truncating a system drops its fingerprint")

	stats := reindex(t, mediaDB, "SNES", path)
	assertReconcileRan(t, stats)
	assert.Equal(t, int64(1), stats.MediaUpserted)
	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", path))
}

// A forced rebuild recreates the database from scratch; nothing survives to
// be matched against.
func TestScanFingerprint_RecreatedDatabaseReconcilesFresh(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	path := snesPath("Alpha (USA).sfc")
	reindex(t, mediaDB, "SNES", path)
	require.NoError(t, mediaDB.Recreate(false))
	assert.Zero(t, fingerprintRowCount(t, mediaDB))

	stats := reindex(t, mediaDB, "SNES", path)
	assertReconcileRan(t, stats)
	require.Len(t, mediaBySystem(t, mediaDB, "SNES"), 1)
}

// Fingerprints are per system: a change in one must not disturb another's
// skip, and each system's row is keyed to its own Systems row.
func TestScanFingerprint_SystemsAreIndependent(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	snes := snesPath("Alpha (USA).sfc")
	nes := filepath.Join(string(filepath.Separator), "roms", "NES", "Beta (USA).nes")
	nes2 := filepath.Join(string(filepath.Separator), "roms", "NES", "Gamma (USA).nes")
	reindex(t, mediaDB, "SNES", snes)
	reindex(t, mediaDB, "NES", nes)
	require.Equal(t, 2, fingerprintRowCount(t, mediaDB))

	assertReconcileRan(t, reindex(t, mediaDB, "NES", nes, nes2))
	assertReconcileSkipped(t, reindex(t, mediaDB, "SNES", snes))
	assertReconcileSkipped(t, reindex(t, mediaDB, "NES", nes, nes2))
}

// TestNewNamesIndex_SecondRunSkipsUnchangedSystems drives the whole scanner
// twice over the same files and checks that every system is skipped on the
// second run, that the media are still all there, and that the skip is
// reported where an operator would look for it.
func TestNewNamesIndex_SecondRunSkipsUnchangedSystems(t *testing.T) {
	// Cannot use t.Parallel() - modifies shared GlobalLauncherCache and log.Logger.
	db, cleanup := testhelpers.NewTestDatabase(t)
	defer cleanup()

	systemFiles := map[string][]string{
		systemdefs.SystemGameboy: {"pocket_quest.bin", "pocket_quest_2.bin"},
		systemdefs.SystemSNES:    {"super_quest.bin"},
	}
	platform, cfg, systems := setupCustomLauncherSystems(t, systemFiles)
	ctx := context.Background()

	var buf strings.Builder
	prevLogger := log.Logger
	prevLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = zerolog.New(&buf).Level(zerolog.InfoLevel)
	t.Cleanup(func() {
		log.Logger = prevLogger
		zerolog.SetGlobalLevel(prevLevel)
	})
	const skipped = "scan reconcile skipped"

	first, err := NewNamesIndex(ctx, platform, cfg, systems, db, func(_ IndexStatus) {}, nil)
	require.NoError(t, err)
	require.Equal(t, 3, first)
	assert.Zero(t, strings.Count(buf.String(), skipped), "a fresh index reconciles every system")
	assert.Equal(t, 2, fingerprintRowCount(t, db.MediaDB))

	buf.Reset()
	second, err := NewNamesIndex(ctx, platform, cfg, systems, db, func(_ IndexStatus) {}, nil)
	require.NoError(t, err)
	assert.Equal(t, first, second, "a skipped system still counts its files")
	assert.Equal(t, 2, strings.Count(buf.String(), skipped), "every unchanged system is skipped")

	total, err := db.MediaDB.GetTotalMediaCount()
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	status, err := db.MediaDB.GetIndexingStatus()
	require.NoError(t, err)
	assert.Equal(t, mediadb.IndexingStatusCompleted, status)
}
