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

// TestCollectTouchedTitleDBIDs exercises the pure missing-state logic that
// decides which titles finalize must recompute. The subtle cases are the
// missing-state transitions: MissingMedia is seeded with every existing media
// at load and trimmed as files are re-found, so "in MissingMedia" alone does
// not mean a change — only a flip relative to PreviouslyMissing does.
func TestCollectTouchedTitleDBIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		touchedTitles     []int
		mediaTitleIDs     map[int]int
		missingMedia      []int
		previouslyMissing []int
		want              []int64
	}{
		{
			name: "nothing changed returns nil",
			want: nil,
		},
		{
			name:          "explicitly touched title",
			touchedTitles: []int{5},
			want:          []int64{5},
		},
		{
			name:              "still missing is not a change",
			mediaTitleIDs:     map[int]int{10: 3},
			missingMedia:      []int{10},
			previouslyMissing: []int{10},
			want:              nil,
		},
		{
			name:          "newly missing touches its title",
			mediaTitleIDs: map[int]int{10: 3},
			missingMedia:  []int{10},
			want:          []int64{3},
		},
		{
			name:              "re-found media touches its title",
			mediaTitleIDs:     map[int]int{10: 7},
			previouslyMissing: []int{10},
			want:              []int64{7},
		},
		{
			name:              "explicit touch and missing dedupe to one title",
			touchedTitles:     []int{4},
			mediaTitleIDs:     map[int]int{20: 4},
			missingMedia:      []int{20},
			previouslyMissing: nil,
			want:              []int64{4},
		},
		{
			name:          "non-positive title index is excluded",
			touchedTitles: []int{0},
			want:          nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ss := &database.ScanState{
				TouchedTitles:     make(map[int]struct{}),
				MediaTitleIDs:     tt.mediaTitleIDs,
				MissingMedia:      make(map[int]struct{}),
				PreviouslyMissing: make(map[int]struct{}),
			}
			for _, ti := range tt.touchedTitles {
				ss.TouchedTitles[ti] = struct{}{}
			}
			for _, m := range tt.missingMedia {
				ss.MissingMedia[m] = struct{}{}
			}
			for _, m := range tt.previouslyMissing {
				ss.PreviouslyMissing[m] = struct{}{}
			}

			got := collectTouchedTitleDBIDs(ss)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

// TestTouchedTitles_NewMediaThenUnchangedReindex verifies the core Fix 4 claim:
// a first index touches every inserted title, but an unchanged re-index of the
// same media touches nothing — so finalize can skip the whole-system
// disambiguation recompute and the tags-cache rebuild.
func TestTouchedTitles_NewMediaThenUnchangedReindex(t *testing.T) {
	t.Parallel()

	const systemID = "SNES"
	path := filepath.Join(string(filepath.Separator), "roms", systemID, "Super Metroid (USA).sfc")

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	// First index: the title is newly inserted, so it must be touched.
	firstState := newIndexingPipelineScanState()
	require.NoError(t, SeedCanonicalTags(mediaDB, firstState))
	require.NoError(t, mediaDB.BeginTransaction(true))
	titleIndex, _, err := AddMediaPath(mediaDB, firstState, systemID, path, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	assert.Contains(t, firstState.TouchedTitles, titleIndex, "new media must touch its title")
	assert.Equal(t, []int64{int64(titleIndex)}, collectTouchedTitleDBIDs(firstState))

	// Unchanged re-index: the same media re-found, no mutation, so nothing is touched.
	reindexState := newIndexingPipelineScanState()
	require.NoError(t, PopulateScanStateFromDB(context.Background(), mediaDB, reindexState))
	require.NoError(t, PopulatePersistentScanStateForSystem(context.Background(), mediaDB, reindexState, systemID))
	require.NoError(t, mediaDB.BeginTransaction(true))
	_, _, err = AddMediaPath(mediaDB, reindexState, systemID, path, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	assert.Empty(t, reindexState.TouchedTitles, "unchanged re-index must not touch any title")
	assert.Nil(t, collectTouchedTitleDBIDs(reindexState), "unchanged re-index yields an empty recompute set")
}

// TestTouchedTitles_SelectiveReindexUnchangedTouchesNothing reproduces the real
// on-device selective-reindex path (PopulateScanStateForSelectiveIndexing +
// per-system PopulatePersistentScanStateForSystem) with C64-style data: numeric
// tags (disc/rev, which exercise PadTagValue), region tags, and multiple media
// sharing a title. A byte-identical re-index must touch nothing.
func TestTouchedTitles_SelectiveReindexUnchangedTouchesNothing(t *testing.T) {
	t.Parallel()

	const systemID = "C64"
	dir := filepath.Join(string(filepath.Separator), "media", "fat", "games", systemID)
	paths := []string{
		filepath.Join(dir, "Commando (USA).d64"),
		filepath.Join(dir, "Commando (Europe).d64"),
		filepath.Join(dir, "Great Giana Sisters (Germany) (Disk 1 of 2).d64"),
		filepath.Join(dir, "Great Giana Sisters (Germany) (Disk 2 of 2).d64"),
		filepath.Join(dir, "Boulder Dash (USA) (Rev 1).d64"),
		filepath.Join(dir, "Boulder Dash (USA) (Rev 2).d64"),
	}

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	// First index: fresh DB, every title newly inserted (all touched).
	firstState := newIndexingPipelineScanState()
	require.NoError(t, SeedCanonicalTags(mediaDB, firstState))
	require.NoError(t, mediaDB.BeginTransaction(true))
	for _, p := range paths {
		_, _, err := AddMediaPath(mediaDB, firstState, systemID, p, "", false, false, nil, slugs.MediaTypeGame)
		require.NoError(t, err)
	}
	require.NoError(t, mediaDB.CommitTransaction())

	// Selective re-index of the same paths, using the exact production loaders.
	reindexState := newIndexingPipelineScanState()
	require.NoError(t, PopulateScanStateForSelectiveIndexing(
		context.Background(), mediaDB, reindexState, []string{}))
	require.NoError(t, PopulatePersistentScanStateForSystem(
		context.Background(), mediaDB, reindexState, systemID))
	require.NoError(t, mediaDB.BeginTransaction(true))
	for _, p := range paths {
		_, _, err := AddMediaPath(mediaDB, reindexState, systemID, p, "", false, false, nil, slugs.MediaTypeGame)
		require.NoError(t, err)
	}
	require.NoError(t, mediaDB.CommitTransaction())

	assert.Empty(t, reindexState.TouchedTitles,
		"unchanged selective re-index must not touch any title")
	assert.Nil(t, collectTouchedTitleDBIDs(reindexState),
		"unchanged selective re-index yields an empty recompute set")
}

// TestTouchedTitles_LeadingZeroNumericTagDoesNotChurn reproduces the residual
// on-device churn: a filename revision spelled with a leading zero ("(Rev 02)")
// parses to the raw value "rev:02", but the storage layer pads then unpads it to
// the natural form "rev:2". Because ss.TagIDs is keyed by the natural form, the
// raw "rev:02" lookup used to miss, re-inserting the tag every re-index and
// touching the title — forcing a full tags-cache rebuild. Canonicalizing the
// lookup key must make an unchanged re-index touch nothing.
func TestTouchedTitles_LeadingZeroNumericTagDoesNotChurn(t *testing.T) {
	t.Parallel()

	const systemID = "C64"
	dir := filepath.Join(string(filepath.Separator), "media", "fat", "games", systemID)
	// "(Rev 02)" parses to raw "rev:02"; "(Rev 2)" parses to "rev:2". Both collapse
	// to the same stored tag, so they must resolve to a single tag DBID with no churn.
	pathPadded := filepath.Join(dir, "Boulder Dash (USA) (Rev 02).d64")
	pathBare := filepath.Join(dir, "Pitfall (USA) (Rev 2).d64")

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	firstState := newIndexingPipelineScanState()
	require.NoError(t, SeedCanonicalTags(mediaDB, firstState))
	require.NoError(t, mediaDB.BeginTransaction(true))
	for _, p := range []string{pathPadded, pathBare} {
		_, _, err := AddMediaPath(mediaDB, firstState, systemID, p, "", false, false, nil, slugs.MediaTypeGame)
		require.NoError(t, err)
	}
	require.NoError(t, mediaDB.CommitTransaction())

	// Both spellings must be stored as the same natural-form rev tag.
	assert.Contains(t, mediaTagStringsForPath(t, mediaDB, pathPadded), "rev:2",
		"'(Rev 02)' must store as natural-form rev:2")
	assert.Contains(t, mediaTagStringsForPath(t, mediaDB, pathBare), "rev:2",
		"'(Rev 2)' must store as natural-form rev:2")

	// Unchanged selective re-index via the production loaders: no churn expected.
	reindexState := newIndexingPipelineScanState()
	require.NoError(t, PopulateScanStateForSelectiveIndexing(
		context.Background(), mediaDB, reindexState, []string{}))
	require.NoError(t, PopulatePersistentScanStateForSystem(
		context.Background(), mediaDB, reindexState, systemID))
	require.NoError(t, mediaDB.BeginTransaction(true))
	for _, p := range []string{pathPadded, pathBare} {
		_, _, err := AddMediaPath(mediaDB, reindexState, systemID, p, "", false, false, nil, slugs.MediaTypeGame)
		require.NoError(t, err)
	}
	require.NoError(t, mediaDB.CommitTransaction())

	assert.Empty(t, reindexState.TouchedTitles,
		"a leading-zero numeric tag must not churn on an unchanged re-index")
	assert.Nil(t, collectTouchedTitleDBIDs(reindexState),
		"unchanged re-index yields an empty recompute set")
}

// TestTouchedTitles_ScraperTagsSurviveUnchangedReindex reproduces the real
// on-device C64 case: media carry non-filename tags written by scrapers/cover
// resolution — a cover "property" tag and a "scraper-run" marker, both stored in
// MediaTags. An unchanged re-index must preserve those tags and must NOT touch
// the owning title. The scanner only owns filename-derived tags; reconcile must
// never delete tags it did not create.
func TestTouchedTitles_ScraperTagsSurviveUnchangedReindex(t *testing.T) {
	t.Parallel()

	const systemID = "C64"
	dir := filepath.Join(string(filepath.Separator), "media", "fat", "games", systemID)
	path := filepath.Join(dir, "Commando (USA).d64")
	storedPath := filepath.ToSlash(path)

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	firstState := newIndexingPipelineScanState()
	require.NoError(t, SeedCanonicalTags(mediaDB, firstState))
	require.NoError(t, mediaDB.BeginTransaction(true))
	titleIndex, _, err := AddMediaPath(mediaDB, firstState, systemID, path, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	// Simulate scraper/cover writes: a cover "property" tag and a "scraper-run"
	// marker linked to the media, exactly as the localmedia scraper does.
	addNonScannerMediaTag(t, mediaDB, storedPath, string(tags.TagTypeProperty), "image:box-2d")
	addNonScannerMediaTag(t, mediaDB, storedPath, string(tags.ScraperRunType("localmedia")), "run-123")

	before := mediaTagStringsForPath(t, mediaDB, path)
	require.Contains(t, before, "property:image:box-2d", "cover property tag should be present pre-reindex")

	// Unchanged selective re-index via the production loaders.
	reindexState := newIndexingPipelineScanState()
	require.NoError(t, PopulateScanStateForSelectiveIndexing(
		context.Background(), mediaDB, reindexState, []string{}))
	require.NoError(t, PopulatePersistentScanStateForSystem(
		context.Background(), mediaDB, reindexState, systemID))
	require.NoError(t, mediaDB.BeginTransaction(true))
	_, _, err = AddMediaPath(mediaDB, reindexState, systemID, path, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	after := mediaTagStringsForPath(t, mediaDB, path)
	assert.Contains(t, after, "property:image:box-2d",
		"cover property tag must survive an unchanged re-index")
	assert.Contains(t, after, "scraper-run.localmedia:run-123",
		"scraper-run marker must survive an unchanged re-index")
	assert.NotContains(t, reindexState.TouchedTitles, titleIndex,
		"non-scanner tags must not cause a spurious title touch")
}

// addNonScannerMediaTag inserts a tag of an arbitrary (non-filename) type and
// links it to the media at storedPath, mirroring how scrapers/cover resolution
// write directly into Tags/MediaTags outside the filename scanner.
func addNonScannerMediaTag(t *testing.T, mediaDB *mediadb.MediaDB, storedPath, tagType, tagValue string) {
	t.Helper()
	sqlDB := mediaDB.UnsafeGetSQLDb()
	ctx := context.Background()

	_, err := sqlDB.ExecContext(ctx, `INSERT OR IGNORE INTO TagTypes (Type) VALUES (?)`, tagType)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx,
		`INSERT OR IGNORE INTO Tags (Tag, TypeDBID) VALUES (?, (SELECT DBID FROM TagTypes WHERE Type = ?))`,
		tags.PadTagValue(tagValue), tagType)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, `
		INSERT OR IGNORE INTO MediaTags (MediaDBID, TagDBID)
		VALUES (
			(SELECT DBID FROM Media WHERE Path = ?),
			(SELECT t.DBID FROM Tags t JOIN TagTypes tt ON t.TypeDBID = tt.DBID
			 WHERE tt.Type = ? AND t.Tag = ?)
		)`, storedPath, tagType, tags.PadTagValue(tagValue))
	require.NoError(t, err)
}

// TestTouchedTitles_MissingMediaTouchesTitle verifies that a media which was
// present last run but is not re-found this run (goes missing) touches its
// title, since a sibling going missing can change the title's disambiguation.
func TestTouchedTitles_MissingMediaTouchesTitle(t *testing.T) {
	t.Parallel()

	const systemID = "SNES"
	pathA := filepath.Join(string(filepath.Separator), "roms", systemID, "Chrono Trigger (USA).sfc")
	pathB := filepath.Join(string(filepath.Separator), "roms", systemID, "Chrono Trigger (Japan).sfc")

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	firstState := newIndexingPipelineScanState()
	require.NoError(t, SeedCanonicalTags(mediaDB, firstState))
	require.NoError(t, mediaDB.BeginTransaction(true))
	titleIndex, _, err := AddMediaPath(mediaDB, firstState, systemID, pathA, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	_, _, err = AddMediaPath(mediaDB, firstState, systemID, pathB, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	// Re-index but only re-find pathA. pathB stays in MissingMedia (not re-found)
	// and was not previously missing, so it is a newly-missing transition.
	reindexState := newIndexingPipelineScanState()
	require.NoError(t, PopulateScanStateFromDB(context.Background(), mediaDB, reindexState))
	require.NoError(t, PopulatePersistentScanStateForSystem(context.Background(), mediaDB, reindexState, systemID))
	require.NoError(t, mediaDB.BeginTransaction(true))
	_, _, err = AddMediaPath(mediaDB, reindexState, systemID, pathA, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	touched := collectTouchedTitleDBIDs(reindexState)
	assert.Contains(t, touched, int64(titleIndex), "a media going missing must touch its title")
}

// TestTouchedTitles_TagChangeTouchesTitle verifies that when an existing
// media's tags diverge from the stored set (reconcileExistingMediaTags inserts
// or deletes a link) the owning title is touched.
func TestTouchedTitles_TagChangeTouchesTitle(t *testing.T) {
	t.Parallel()

	const systemID = "SNES"
	path := filepath.Join(string(filepath.Separator), "roms", systemID, "Super Mario World (USA).sfc")
	storedPath := filepath.ToSlash(path)

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	firstState := newIndexingPipelineScanState()
	require.NoError(t, SeedCanonicalTags(mediaDB, firstState))
	require.NoError(t, mediaDB.BeginTransaction(true))
	titleIndex, _, err := AddMediaPath(mediaDB, firstState, systemID, path, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	// Confirm the media has tags, then delete them from the DB so the next
	// index sees the desired tags as missing and re-inserts them.
	require.NotEmpty(t, mediaTagStringsForPath(t, mediaDB, path), "seed media should have tags")
	_, err = mediaDB.UnsafeGetSQLDb().ExecContext(context.Background(),
		`DELETE FROM MediaTags WHERE MediaDBID = (SELECT DBID FROM Media WHERE Path = ?)`, storedPath)
	require.NoError(t, err)

	reindexState := newIndexingPipelineScanState()
	require.NoError(t, PopulateScanStateFromDB(context.Background(), mediaDB, reindexState))
	require.NoError(t, PopulatePersistentScanStateForSystem(context.Background(), mediaDB, reindexState, systemID))
	require.NoError(t, mediaDB.BeginTransaction(true))
	_, _, err = AddMediaPath(mediaDB, reindexState, systemID, path, "", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	assert.Contains(t, reindexState.TouchedTitles, titleIndex, "a tag change must touch the media's title")
}

// TestTouchedTitles_ReassignmentTouchesBothTitles verifies that moving a media
// from one title to another (its resolved title changed between runs) touches
// both the old and new title.
func TestTouchedTitles_ReassignmentTouchesBothTitles(t *testing.T) {
	t.Parallel()

	const systemID = "SNES"
	path := filepath.Join(string(filepath.Separator), "roms", systemID, "game.sfc")

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	firstState := newIndexingPipelineScanState()
	require.NoError(t, SeedCanonicalTags(mediaDB, firstState))
	require.NoError(t, mediaDB.BeginTransaction(true))
	oldTitleIndex, _, err := AddMediaPath(
		mediaDB, firstState, systemID, path, "Alpha Quest", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	// Re-index the same path with a different provided name, which resolves to a
	// different title slug and reassigns the media.
	reindexState := newIndexingPipelineScanState()
	require.NoError(t, PopulateScanStateFromDB(context.Background(), mediaDB, reindexState))
	require.NoError(t, PopulatePersistentScanStateForSystem(context.Background(), mediaDB, reindexState, systemID))
	require.NoError(t, mediaDB.BeginTransaction(true))
	newTitleIndex, _, err := AddMediaPath(
		mediaDB, reindexState, systemID, path, "Beta Saga", false, false, nil, slugs.MediaTypeGame)
	require.NoError(t, err)
	require.NoError(t, mediaDB.CommitTransaction())

	require.NotEqual(t, oldTitleIndex, newTitleIndex, "provided name change should reassign to a new title")
	assert.Contains(t, reindexState.TouchedTitles, oldTitleIndex, "reassignment must touch the old title")
	assert.Contains(t, reindexState.TouchedTitles, newTitleIndex, "reassignment must touch the new title")
}
