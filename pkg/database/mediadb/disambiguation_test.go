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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// disambTitleMedia describes one media row of a title for the test helper: its
// path and the (type, value) tags to attach.
type disambTitleMedia struct {
	tags map[string]string
	path string
}

// setupDisambTitle inserts a system, one title, and its media+tags, returning the
// system DBID, title DBID, and the inserted media DBIDs in input order.
func setupDisambTitle(
	t *testing.T, mediaDB *MediaDB, systemID, titleName string, media []disambTitleMedia,
) (systemDBID, titleDBID int64, mediaDBIDs []int64) {
	t.Helper()

	system, err := mediaDB.FindOrInsertSystem(database.System{SystemID: systemID, Name: systemID})
	require.NoError(t, err)

	// Tag types must exist before the write transaction (mirrors other tests).
	typeDBIDs := make(map[string]int64)
	for i := range media {
		for tagType := range media[i].tags {
			if _, ok := typeDBIDs[tagType]; ok {
				continue
			}
			tt, ttErr := mediaDB.FindOrInsertTagType(database.TagType{Type: tagType})
			require.NoError(t, ttErr)
			typeDBIDs[tagType] = tt.DBID
		}
	}

	require.NoError(t, mediaDB.BeginTransaction(false))
	title, err := mediaDB.InsertMediaTitle(&database.MediaTitle{
		SystemDBID: system.DBID,
		Slug:       slugs.Slugify(slugs.MediaTypeGame, titleName),
		Name:       titleName,
	})
	require.NoError(t, err)

	mediaDBIDs = make([]int64, len(media))
	for i := range media {
		row, mErr := mediaDB.InsertMedia(database.Media{
			SystemDBID:     system.DBID,
			MediaTitleDBID: title.DBID,
			Path:           media[i].path,
			ParentDir:      ParentDirForMediaPath(media[i].path),
			SortName:       titleName,
		})
		require.NoError(t, mErr)
		mediaDBIDs[i] = row.DBID

		for tagType, value := range media[i].tags {
			tag, tErr := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: typeDBIDs[tagType], Tag: value})
			require.NoError(t, tErr)
			_, tErr = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: row.DBID, TagDBID: tag.DBID})
			require.NoError(t, tErr)
		}
	}
	require.NoError(t, mediaDB.CommitTransaction())

	return system.DBID, title.DBID, mediaDBIDs
}

func titleDisambiguationTypes(t *testing.T, mediaDB *MediaDB, titleDBID int64) string {
	t.Helper()
	var types string
	err := mediaDB.sql.Load().QueryRowContext(
		context.Background(), `SELECT DisambiguationTypes FROM MediaTitles WHERE DBID = ?`, titleDBID,
	).Scan(&types)
	require.NoError(t, err)
	return types
}

func TestRecomputeSystemDisambiguation_DifferingTagDisambiguates(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "NES", "Sonic", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "sonic-usa.nes"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "nes", "sonic-eur.nes"), tags: map[string]string{"release": "Europe"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "release", titleDisambiguationTypes(t, mediaDB, titleDBID))

	// The main browse/search query supplies DisambiguationTypes; simulate that.
	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Sonic", SystemID: "NES", DisambiguationTypes: "release"},
		{MediaID: mediaIDs[1], Name: "Sonic", SystemID: "NES", DisambiguationTypes: "release"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql.Load(), results))
	require.Len(t, results[0].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "release", Tag: "USA"}, results[0].ZapScriptTags[0])
	require.Len(t, results[1].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "release", Tag: "Europe"}, results[1].ZapScriptTags[0])

	// Search paths carrying title IDs reuse the already fetched media tags
	// instead of issuing a second disambiguation query.
	fetchedResults := []database.SearchResultWithCursor{
		{
			MediaID: mediaIDs[0], MediaTitleID: titleDBID, Name: "Sonic", SystemID: "NES",
			DisambiguationTypes: "release",
		},
		{
			MediaID: mediaIDs[1], MediaTitleID: titleDBID, Name: "Sonic", SystemID: "NES",
			DisambiguationTypes: "release",
		},
	}
	require.NoError(t, attachTagsAndDisambiguation(ctx, mediaDB.sql.Load(), fetchedResults))
	assert.Equal(t, results[0].ZapScriptTags, fetchedResults[0].ZapScriptTags)
	assert.Equal(t, results[1].ZapScriptTags, fetchedResults[1].ZapScriptTags)
}

// TestDisambiguationBackfill_RecomputesStaleTitlesAndStamps covers the one-time
// algorithm-version backfill: indexing only recomputes DisambiguationTypes for
// titles whose data changed, so values written by an older algorithm are never
// revisited by reindexing alone. A missing/outdated stamp with titles present
// must report pending, recompute every title, and stamp the current version.
func TestDisambiguationBackfill_RecomputesStaleTitlesAndStamps(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	_, titleDBID, _ := setupDisambTitle(t, mediaDB, "NES", "Sonic", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "sonic-usa.nes"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "nes", "sonic-eur.nes"), tags: map[string]string{"release": "Europe"}},
	})
	// No recompute has run: the stored value stands in for one computed by an
	// older algorithm that would now disagree with the current one.
	require.Empty(t, titleDisambiguationTypes(t, mediaDB, titleDBID))

	pending, err := mediaDB.disambiguationBackfillPending(ctx)
	require.NoError(t, err)
	assert.True(t, pending, "titles without a current stamp must be pending backfill")

	require.NoError(t, mediaDB.runDisambiguationBackfill(ctx, nil))

	assert.Equal(t, "release", titleDisambiguationTypes(t, mediaDB, titleDBID),
		"backfill must recompute stored disambiguation with the current algorithm")

	pending, err = mediaDB.disambiguationBackfillPending(ctx)
	require.NoError(t, err)
	assert.False(t, pending, "a completed backfill must stamp the current version")

	repairPending, err := mediaDB.TemporaryRepairJobsPending(ctx)
	require.NoError(t, err)
	assert.False(t, repairPending)
}

// TestRecreate_StampsDisambiguationVersion verifies a recreated database is
// stamped current immediately: everything indexed into it is computed by the
// current algorithm, so the first optimization pass after a rebuild must not
// re-run a full backfill over freshly computed values.
func TestRecreate_StampsDisambiguationVersion(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	require.NoError(t, mediaDB.Recreate(false))

	var version string
	err := mediaDB.sql.Load().QueryRowContext(context.Background(),
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigDisambiguationVersion,
	).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, disambiguationAlgoVersion, version)
}

// TestDisambiguationBackfill_EmptyDatabaseStampsWithoutWork verifies a fresh
// database is stamped current immediately: the first index computes
// disambiguation with the current algorithm, so a boot-time backfill pass would
// be pure waste.
func TestDisambiguationBackfill_EmptyDatabaseStampsWithoutWork(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	pending, err := mediaDB.disambiguationBackfillPending(ctx)
	require.NoError(t, err)
	assert.False(t, pending, "an empty database has nothing to backfill")

	var version string
	err = mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigDisambiguationVersion,
	).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, disambiguationAlgoVersion, version,
		"the empty-database check must stamp so later titles indexed under the current algorithm stay stamped")
}

// TestDisambiguationBackfill_ResumesFromCheckpoint verifies an interrupted
// backfill does not restart from the first system: systems at or below the
// persisted cursor are skipped, and completion stamps the version and clears
// the cursor. Restart-from-scratch is what wedged devices with short power
// sessions in an endless minutes-per-system walk.
func TestDisambiguationBackfill_ResumesFromCheckpoint(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	firstSystemDBID, firstTitleDBID, _ := setupDisambTitle(t, mediaDB, "NES", "Sonic", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "sonic-usa.nes"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "nes", "sonic-eur.nes"), tags: map[string]string{"release": "Europe"}},
	})
	_, secondTitleDBID, _ := setupDisambTitle(t, mediaDB, "SNES", "Mario", []disambTitleMedia{
		{path: browseTestPath("roms", "snes", "mario-usa.sfc"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "snes", "mario-jpn.sfc"), tags: map[string]string{"release": "Japan"}},
	})

	// Simulate a previous walk interrupted after the first system committed.
	require.NoError(t, sqlSetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load(), firstSystemDBID))

	require.NoError(t, mediaDB.runDisambiguationBackfill(ctx, nil))

	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, firstTitleDBID),
		"systems at or below the checkpoint must not be recomputed again")
	assert.Equal(t, "release", titleDisambiguationTypes(t, mediaDB, secondTitleDBID),
		"systems past the checkpoint must be recomputed")

	pending, err := mediaDB.disambiguationBackfillPending(ctx)
	require.NoError(t, err)
	assert.False(t, pending, "a resumed walk that reaches the end must stamp the version")

	cursor, err := sqlGetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load())
	require.NoError(t, err)
	assert.Zero(t, cursor, "completion must clear the checkpoint")
}

// TestDisambiguationBackfill_InterruptionPersistsProgress is the regression
// test for devices wedged in an endless backfill: an interruption mid-walk
// must leave a durable checkpoint for the completed system, and the next run
// must resume past it instead of restarting from the first system.
//
// Not parallel: it mutates the package-level disambiguationBackfillAfterSystem
// test seam. Sequential tests run while parallel tests are paused, so the
// mutation cannot race their reads.
func TestDisambiguationBackfill_InterruptionPersistsProgress(t *testing.T) {
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()

	firstSystemDBID, firstTitleDBID, _ := setupDisambTitle(t, mediaDB, "NES", "Sonic", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "sonic-usa.nes"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "nes", "sonic-eur.nes"), tags: map[string]string{"release": "Europe"}},
	})
	secondSystemDBID, secondTitleDBID, _ := setupDisambTitle(t, mediaDB, "SNES", "Mario", []disambTitleMedia{
		{path: browseTestPath("roms", "snes", "mario-usa.sfc"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "snes", "mario-jpn.sfc"), tags: map[string]string{"release": "Japan"}},
	})
	require.Less(t, firstSystemDBID, secondSystemDBID,
		"the walk visits systems in DBID order; the test relies on NES running first")

	// Cancel as soon as the first system commits, as a power-off would.
	interruptCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disambiguationBackfillAfterSystem = func(int64) { cancel() }
	defer func() { disambiguationBackfillAfterSystem = nil }()

	ctx := context.Background()
	require.Error(t, mediaDB.runDisambiguationBackfill(interruptCtx, nil),
		"an interrupted walk must report the cancellation")

	cursor, err := sqlGetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load())
	require.NoError(t, err)
	assert.Equal(t, firstSystemDBID, cursor, "the completed system must be checkpointed durably")
	assert.Equal(t, "release", titleDisambiguationTypes(t, mediaDB, firstTitleDBID),
		"the system completed before the interruption keeps its recomputed value")
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, secondTitleDBID),
		"the interrupted walk must not have reached the second system")

	pending, err := mediaDB.disambiguationBackfillPending(ctx)
	require.NoError(t, err)
	assert.True(t, pending, "an interrupted walk must not stamp the version")

	// Blank the first system's value: the resumed walk must skip it, so the
	// blank surviving the resume proves it was not recomputed again.
	_, err = mediaDB.sql.Load().ExecContext(ctx,
		"UPDATE MediaTitles SET DisambiguationTypes = '' WHERE DBID = ?", firstTitleDBID)
	require.NoError(t, err)

	disambiguationBackfillAfterSystem = nil
	require.NoError(t, mediaDB.runDisambiguationBackfill(ctx, nil))

	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, firstTitleDBID),
		"resume must skip the checkpointed system instead of restarting from the first")
	assert.Equal(t, "release", titleDisambiguationTypes(t, mediaDB, secondTitleDBID),
		"resume must recompute the remaining systems")

	pending, err = mediaDB.disambiguationBackfillPending(ctx)
	require.NoError(t, err)
	assert.False(t, pending, "the resumed walk must stamp the version on completion")

	cursor, err = sqlGetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load())
	require.NoError(t, err)
	assert.Zero(t, cursor, "completion must clear the checkpoint")
}

func TestDisambiguationBackfillCursor_Roundtrip(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	cursor, err := sqlGetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load())
	require.NoError(t, err)
	assert.Zero(t, cursor, "missing cursor reads as zero")

	require.NoError(t, sqlSetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load(), 42))
	cursor, err = sqlGetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load())
	require.NoError(t, err)
	assert.Equal(t, int64(42), cursor)

	require.NoError(t, sqlClearDisambiguationBackfillCursor(ctx, mediaDB.sql.Load()))
	cursor, err = sqlGetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load())
	require.NoError(t, err)
	assert.Zero(t, cursor, "cleared cursor reads as zero")
}

// TestDisambiguationBackfillCursor_IgnoresOtherAlgoVersions verifies a cursor
// persisted by a different algorithm version never truncates the current
// walk: a new algorithm must revisit every system.
func TestDisambiguationBackfillCursor_IgnoresOtherAlgoVersions(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	for _, raw := range []string{"0:42", "999:42", "garbage", "1:notanumber"} {
		_, err := mediaDB.sql.Load().ExecContext(ctx,
			"INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES (?, ?)",
			DBConfigDisambiguationBackfillCursor, raw,
		)
		require.NoError(t, err)

		cursor, err := sqlGetDisambiguationBackfillCursor(ctx, mediaDB.sql.Load())
		require.NoError(t, err)
		assert.Zero(t, cursor, "cursor %q must be ignored", raw)
	}
}

// TestMigrateUp_StampsEmptyDatabase verifies the boot path stamps a database
// with no titles before the first index writes any. Without this, the stamp
// check first runs during post-index optimization — after titles exist — and
// a fresh install pays a full pointless backfill over values the index just
// computed.
func TestMigrateUp_StampsEmptyDatabase(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, mediaDB.MigrateUp())

	var version string
	err := mediaDB.sql.Load().QueryRowContext(ctx,
		"SELECT Value FROM DBConfig WHERE Name = ?",
		DBConfigDisambiguationVersion,
	).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, disambiguationAlgoVersion, version)
}

// TestMigrateUp_LegacyDatabaseStaysPending verifies the boot-time stamp check
// never stamps a database that already holds titles: those may carry values
// from an older algorithm and must keep the backfill pending.
func TestMigrateUp_LegacyDatabaseStaysPending(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	setupDisambTitle(t, mediaDB, "NES", "Sonic", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "sonic-usa.nes"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "nes", "sonic-eur.nes"), tags: map[string]string{"release": "Europe"}},
	})

	require.NoError(t, mediaDB.MigrateUp())

	pending, err := mediaDB.disambiguationBackfillPending(ctx)
	require.NoError(t, err)
	assert.True(t, pending, "a database with titles must not be stamped by the boot check")
}

func TestRecomputeSystemDisambiguation_IdenticalTagsDoNotDisambiguate(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "NES", "Tetris", []disambTitleMedia{
		{path: "/roms/nes/tetris-a.nes", tags: map[string]string{"year": "1989"}},
		{path: "/roms/nes/tetris-b.nes", tags: map[string]string{"year": "1989"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, titleDBID))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Tetris", SystemID: "NES"},
		{MediaID: mediaIDs[1], Name: "Tetris", SystemID: "NES"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql.Load(), results))
	assert.Empty(t, results[0].ZapScriptTags)
	assert.NotNil(t, results[0].ZapScriptTags, "ZapScriptTags should be a non-nil empty slice")
	assert.Empty(t, results[1].ZapScriptTags)
}

func TestRecomputeSystemDisambiguation_OnlyDifferingTypeSelected(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	// Same year, different players — only players disambiguates.
	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "Arcade", "Street Fighter", []disambTitleMedia{
		{path: "/roms/arcade/sf-2p.zip", tags: map[string]string{"year": "1992", "players": "2"}},
		{path: "/roms/arcade/sf-4p.zip", tags: map[string]string{"year": "1992", "players": "4"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "players", titleDisambiguationTypes(t, mediaDB, titleDBID))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Street Fighter", SystemID: "Arcade", DisambiguationTypes: "players"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql.Load(), results))
	require.Len(t, results[0].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "players", Tag: "2"}, results[0].ZapScriptTags[0])
}

func TestRecomputeSystemDisambiguation_SingleMediaTitle(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, _ := setupDisambTitle(t, mediaDB, "NES", "Solo", []disambTitleMedia{
		{path: "/roms/nes/solo.nes", tags: map[string]string{"release": "USA"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, titleDBID))
}

func TestRecomputeSystemDisambiguation_MissingMediaExcluded(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "NES", "Castlevania", []disambTitleMedia{
		{path: "/roms/nes/cv-usa.nes", tags: map[string]string{"release": "USA"}},
		{path: "/roms/nes/cv-eur.nes", tags: map[string]string{"release": "Europe"}},
	})

	// Mark the Europe variant missing: only one present variant remains, so the
	// title no longer disambiguates.
	_, err := mediaDB.sql.Load().ExecContext(ctx, `UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, mediaIDs[1])
	require.NoError(t, err)

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, titleDBID))
}

// addExtraTag attaches one more (type, value) tag to each of the given media,
// used to build multi-valued tag sets the map-based setupDisambTitle helper can't
// express (one value per type).
func addExtraTag(t *testing.T, mediaDB *MediaDB, mediaIDs []int64, tagType, value string) {
	t.Helper()
	tt, err := mediaDB.FindOrInsertTagType(database.TagType{Type: tagType})
	require.NoError(t, err)
	require.NoError(t, mediaDB.BeginTransaction(false))
	tag, err := mediaDB.FindOrInsertTag(database.Tag{TypeDBID: tt.DBID, Tag: value})
	require.NoError(t, err)
	for _, mid := range mediaIDs {
		_, err = mediaDB.InsertMediaTag(database.MediaTag{MediaDBID: mid, TagDBID: tag.DBID})
		require.NoError(t, err)
	}
	require.NoError(t, mediaDB.CommitTransaction())
}

// TestRecomputeSystemDisambiguation_IdenticalMultiValueSetsDoNotDisambiguate proves
// that a multi-valued type identical on every sibling is not flagged: both discs
// carry the same region set {us, eu}, so only the differing disc number
// disambiguates. (Pooling distinct values across media would wrongly flag region.)
func TestRecomputeSystemDisambiguation_IdenticalMultiValueSetsDoNotDisambiguate(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "PSX", "Final Fantasy VII", []disambTitleMedia{
		{path: browseTestPath("roms", "psx", "ff7-disc1.chd"), tags: map[string]string{"region": "us", "disc": "1"}},
		{path: browseTestPath("roms", "psx", "ff7-disc2.chd"), tags: map[string]string{"region": "us", "disc": "2"}},
	})

	// Give both discs the identical second region value so each holds the set {us, eu}.
	addExtraTag(t, mediaDB, mediaIDs, "region", "eu")

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "disc", titleDisambiguationTypes(t, mediaDB, titleDBID))
}

// TestRecomputeSystemDisambiguation_DifferingMultiValueSetsDisambiguate is the
// counterpart: when the per-media region sets differ ({us, eu} vs {us}), region
// disambiguates.
func TestRecomputeSystemDisambiguation_DifferingMultiValueSetsDisambiguate(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "SNES", "Secret of Mana", []disambTitleMedia{
		{path: browseTestPath("roms", "snes", "som-multi.sfc"), tags: map[string]string{"region": "us"}},
		{path: browseTestPath("roms", "snes", "som-us.sfc"), tags: map[string]string{"region": "us"}},
	})

	// Only the first media also carries region:eu → its set {us, eu} differs from {us}.
	addExtraTag(t, mediaDB, mediaIDs[:1], "region", "eu")

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "region", titleDisambiguationTypes(t, mediaDB, titleDBID))
}

// TestRecomputeSystemDisambiguation_PresenceAbsenceDisambiguates covers the arcade
// "Jackal" case: three siblings share region:world, but one adds an input tag
// (Rotary) and another an unlicensed tag ([bl] → bootleg). Neither tag is shared, so
// presence vs absence must distinguish them; region (identical on all) must not. The
// tag values mirror what the filename parser actually emits for these names.
func TestRecomputeSystemDisambiguation_PresenceAbsenceDisambiguates(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "Arcade", "Jackal", []disambTitleMedia{
		{path: browseTestPath("roms", "arcade", "jackal-w.mra"), tags: map[string]string{"region": "world"}},
		{
			path: browseTestPath("roms", "arcade", "jackal-w-rotary.mra"),
			tags: map[string]string{"region": "world", "input": "joystick:rotary"},
		},
		{
			path: browseTestPath("roms", "arcade", "jackal-w-bl.mra"),
			tags: map[string]string{"region": "world", "unlicensed": "bootleg"},
		},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "input,unlicensed", titleDisambiguationTypes(t, mediaDB, titleDBID))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Jackal", SystemID: "Arcade", DisambiguationTypes: "input,unlicensed"},
		{MediaID: mediaIDs[1], Name: "Jackal", SystemID: "Arcade", DisambiguationTypes: "input,unlicensed"},
		{MediaID: mediaIDs[2], Name: "Jackal", SystemID: "Arcade", DisambiguationTypes: "input,unlicensed"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql.Load(), results))
	assert.Empty(t, results[0].ZapScriptTags, "plain (W) sibling has no distinguishing tag")
	require.Len(t, results[1].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "input", Tag: "joystick:rotary"}, results[1].ZapScriptTags[0])
	require.Len(t, results[2].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "unlicensed", Tag: "bootleg"}, results[2].ZapScriptTags[0])
}

// TestRecomputeSystemDisambiguation_MultipleTitlesInOnePass verifies the single-pass
// recompute handles several titles in one call: a title that disambiguates and one that
// does not must both get the correct value from the same system-scoped recompute.
func TestRecomputeSystemDisambiguation_MultipleTitlesInOnePass(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	sysA, titleA, _ := setupDisambTitle(t, mediaDB, "Arcade", "Contra", []disambTitleMedia{
		{path: browseTestPath("roms", "arcade", "contra-w.mra"), tags: map[string]string{"region": "world"}},
		{path: browseTestPath("roms", "arcade", "contra-jp.mra"), tags: map[string]string{"region": "jp"}},
	})
	sysB, titleB, _ := setupDisambTitle(t, mediaDB, "Arcade", "Gradius", []disambTitleMedia{
		{path: browseTestPath("roms", "arcade", "gradius-1.mra"), tags: map[string]string{"region": "world"}},
		{path: browseTestPath("roms", "arcade", "gradius-2.mra"), tags: map[string]string{"region": "world"}},
	})
	require.Equal(t, sysA, sysB, "both titles must share one system")

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{sysA}))
	assert.Equal(t, "region", titleDisambiguationTypes(t, mediaDB, titleA), "differing region disambiguates")
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, titleB), "identical siblings do not disambiguate")
}

// TestRecomputeSystemDisambiguation_ClearsStaleValue verifies the reset step: a title
// carrying a stale DisambiguationTypes whose media no longer disagree is cleared to ”.
func TestRecomputeSystemDisambiguation_ClearsStaleValue(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	sysID, titleID, _ := setupDisambTitle(t, mediaDB, "Arcade", "Gradius", []disambTitleMedia{
		{path: browseTestPath("roms", "arcade", "gradius-1.mra"), tags: map[string]string{"region": "world"}},
		{path: browseTestPath("roms", "arcade", "gradius-2.mra"), tags: map[string]string{"region": "world"}},
	})
	_, err := mediaDB.sql.Load().ExecContext(
		ctx, `UPDATE MediaTitles SET DisambiguationTypes = 'region' WHERE DBID = ?`, titleID)
	require.NoError(t, err)

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{sysID}))
	assert.Empty(t, titleDisambiguationTypes(t, mediaDB, titleID), "stale value must be cleared")
}

// TestRecomputeTitleDisambiguation_Success exercises the title-scoped entry point
// (RecomputeTitleDisambiguation) directly, complementing the system-scoped tests.
func TestRecomputeTitleDisambiguation_Success(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	_, titleDBID, _ := setupDisambTitle(t, mediaDB, "NES", "Contra", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "contra-usa.nes"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "nes", "contra-jpn.nes"), tags: map[string]string{"release": "Japan"}},
	})

	require.NoError(t, mediaDB.RecomputeTitleDisambiguation(ctx, []int64{titleDBID}))
	assert.Equal(t, "release", titleDisambiguationTypes(t, mediaDB, titleDBID))
}

// TestRecomputeTitleDisambiguation_NullSQL verifies the nil-DB guard.
func TestRecomputeTitleDisambiguation_NullSQL(t *testing.T) {
	t.Parallel()
	db := &MediaDB{}
	err := db.RecomputeTitleDisambiguation(context.Background(), []int64{1})
	require.ErrorIs(t, err, ErrNullSQL)
}

// TestAttachZapScriptTags_TitleGlobalAcrossPages proves the page-independence fix:
// a single result on its own page still receives its disambiguating tag because
// the type set is stored per title, not derived from the current page.
func TestAttachZapScriptTags_TitleGlobalAcrossPages(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	parentDir := browseTestDir("roms", "nes")
	systemDBID, _, _ := setupDisambTitle(t, mediaDB, "NES", "Double Dragon", []disambTitleMedia{
		{path: browseTestPath("roms", "nes", "dd-usa.nes"), tags: map[string]string{"release": "USA"}},
		{path: browseTestPath("roms", "nes", "dd-jpn.nes"), tags: map[string]string{"release": "Japan"}},
	})
	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))

	// Limit 1 → only the first sibling is on this page; old page-scoped grouping
	// would have found no sibling and emitted no disambiguating tag.
	results, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{PathPrefix: parentDir, Limit: 1})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0].ZapScriptTags, 1, "lone sibling on a page must still be disambiguated")
	assert.Equal(t, "release", results[0].ZapScriptTags[0].Type)
}

func TestGetZapScriptTagsBySystemAndPath_Integration(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	usaPath := "/roms/nes/contra-usa.nes"
	systemDBID, _, _ := setupDisambTitle(t, mediaDB, "NES", "Contra", []disambTitleMedia{
		{path: usaPath, tags: map[string]string{"release": "USA", "year": "1988"}},
		{path: "/roms/nes/contra-jpn.nes", tags: map[string]string{"release": "Japan", "year": "1988"}},
	})
	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))

	got, err := mediaDB.GetZapScriptTagsBySystemAndPath(ctx, "NES", usaPath)
	require.NoError(t, err)
	require.Len(t, got, 1, "only release differs across the two variants")
	assert.Equal(t, database.TagInfo{Type: "release", Tag: "USA"}, got[0])
}

// TestRecomputeSystemDisambiguation_RegionDisambiguates exercises a newly eligible tag
// type: region (us vs jp) now disambiguates same-named regional variants.
func TestRecomputeSystemDisambiguation_RegionDisambiguates(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "Genesis", "Sonic The Hedgehog", []disambTitleMedia{
		{path: "/roms/genesis/sonic-usa.md", tags: map[string]string{"region": "us"}},
		{path: "/roms/genesis/sonic-jpn.md", tags: map[string]string{"region": "jp"}},
	})

	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	assert.Equal(t, "region", titleDisambiguationTypes(t, mediaDB, titleDBID))

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Sonic The Hedgehog", SystemID: "Genesis", DisambiguationTypes: "region"},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql.Load(), results))
	require.Len(t, results[0].ZapScriptTags, 1)
	assert.Equal(t, database.TagInfo{Type: "region", Tag: "us"}, results[0].ZapScriptTags[0])
}

// TestAttachZapScriptTags_OrdersByDisplayPriority verifies emitted tags come back in
// display-importance order (unfinished › region › rev), not alphabetical, so clients can
// render-and-truncate left to right.
func TestAttachZapScriptTags_OrdersByDisplayPriority(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	systemDBID, titleDBID, mediaIDs := setupDisambTitle(t, mediaDB, "Genesis", "Streets of Rage", []disambTitleMedia{
		{path: "/roms/genesis/sor-a.md", tags: map[string]string{"region": "us", "unfinished": "beta", "rev": "a"}},
		{path: "/roms/genesis/sor-b.md", tags: map[string]string{"region": "jp", "unfinished": "proto", "rev": "b"}},
	})
	require.NoError(t, mediaDB.RecomputeSystemDisambiguation(ctx, []int64{systemDBID}))
	stored := titleDisambiguationTypes(t, mediaDB, titleDBID)

	results := []database.SearchResultWithCursor{
		{MediaID: mediaIDs[0], Name: "Streets of Rage", SystemID: "Genesis", DisambiguationTypes: stored},
	}
	require.NoError(t, attachZapScriptTags(ctx, mediaDB.sql.Load(), results))
	require.Len(t, results[0].ZapScriptTags, 3)
	gotOrder := []string{
		results[0].ZapScriptTags[0].Type,
		results[0].ZapScriptTags[1].Type,
		results[0].ZapScriptTags[2].Type,
	}
	assert.Equal(t, []string{"unfinished", "region", "rev"}, gotOrder)
}
