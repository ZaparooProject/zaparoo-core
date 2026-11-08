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
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func arcadeLinkPath(parts ...string) string {
	all := append([]string{string(filepath.Separator), "media", "fat", "_Arcade"}, parts...)
	return filepath.ToSlash(filepath.Join(all...))
}

func stagedLinkTestMedia(path, title, linkTarget string) database.ScanStagedMedia {
	slug := slugs.Slugify(slugs.MediaTypeGame, title)
	return database.ScanStagedMedia{
		Path:          path,
		ParentDir:     ParentDirForMediaPath(path),
		Slug:          slug,
		TitleName:     title,
		SortName:      title,
		SlugLength:    len(slug),
		SlugWordCount: len(strings.Fields(title)),
		LinkTarget:    linkTarget,
	}
}

func indexLinkTestMedia(
	t *testing.T, db *MediaDB, opts database.ScanReconcileOpts, files ...database.ScanStagedMedia,
) database.ScanReconcileStats {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, db.BeginTransaction(false))
	require.NoError(t, db.SeedCanonicalTagDefinitions(ctx))
	require.NoError(t, db.CommitTransaction())
	require.NoError(t, db.BeginTransaction(true))
	require.NoError(t, db.ClearScanStage())
	for i := range files {
		require.NoError(t, db.StageScannedMedia(&files[i]))
	}
	stats, err := db.ReconcileStagedSystem(ctx, systemdefs.SystemArcade, opts)
	require.NoError(t, err)
	require.NoError(t, db.CommitTransaction())
	return stats
}

func linkTestMedia(t *testing.T, db *MediaDB, path string) *database.Media {
	t.Helper()
	system, err := db.FindSystemBySystemID(systemdefs.SystemArcade)
	require.NoError(t, err)
	media, err := db.FindMediaBySystemAndPath(context.Background(), system.DBID, path)
	require.NoError(t, err)
	require.NotNil(t, media, path)
	return media
}

func linkTestTarget(t *testing.T, db *MediaDB, mediaDBID int64) (int64, bool) {
	t.Helper()
	target, ok, err := db.GetMediaLinkTarget(context.Background(), mediaDBID)
	require.NoError(t, err)
	return target, ok
}

// linkTestProperties lists a row's media-scope properties as "value=text".
func linkTestProperties(t *testing.T, db *MediaDB, mediaDBID int64) []string {
	t.Helper()
	rows, err := db.sql.Load().QueryContext(context.Background(), `
		SELECT t.Tag, mp.Text FROM MediaProperties mp JOIN Tags t ON t.DBID = mp.TypeTagDBID
		WHERE mp.MediaDBID = ?`, mediaDBID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var props []string
	for rows.Next() {
		var tag, text string
		require.NoError(t, rows.Scan(&tag, &text))
		props = append(props, tag+"="+text)
	}
	require.NoError(t, rows.Err())
	sort.Strings(props)
	return props
}

// linkTestTags lists a row's media-scope tags as "type:tag".
func linkTestTags(t *testing.T, db *MediaDB, mediaDBID int64) []string {
	t.Helper()
	tagRows, err := db.GetMediaTagsByMediaDBID(context.Background(), mediaDBID)
	require.NoError(t, err)
	names := make([]string, 0, len(tagRows))
	for _, tag := range tagRows {
		names = append(names, tag.Type+":"+tag.Tag)
	}
	sort.Strings(names)
	return names
}

var (
	linkTestBoxart     = filepath.ToSlash(filepath.Join("docs", "Arcade", "Artwork", "aso.png"))
	linkTestScreenshot = filepath.ToSlash(filepath.Join("docs", "Arcade", "Snaps", "aso.png"))
)

// scrapeLinkTestTarget writes what a scraper writes for an arcade descriptor:
// title artwork, an exact media-scope image, and genre and year metadata.
func scrapeLinkTestTarget(t *testing.T, db *MediaDB, target *database.Media) {
	t.Helper()
	require.NoError(t, db.ApplyScrapeResult(context.Background(), target.DBID, target.MediaTitleDBID,
		&database.ScrapeWrite{
			Sentinel: database.TagInfo{Type: "scraper.test", Tag: "scraped"},
			MediaProps: []database.MediaProperty{{
				TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageScreenshot), Text: linkTestScreenshot,
			}},
			TitleProps: []database.MediaProperty{{
				TypeTag: tags.PropertyTypeTag(tags.TagPropertyImageBoxart), Text: linkTestBoxart,
			}},
			MediaTags: []database.TagInfo{{Type: string(tags.TagTypeGenre), Tag: string(tags.TagGameGenreAction)}},
			TitleTags: []database.TagInfo{{Type: string(tags.TagTypeYear), Tag: string(tags.TagYear1985)}},
		}))
}

func TestLinkedMediaCopiesEveryPropertyDecided(t *testing.T) {
	t.Parallel()
	for _, value := range tags.CanonicalTagDefinitions[tags.TagTypeProperty] {
		_, decided := linkedMediaCopiesProperty[value]
		assert.True(t, decided, "property %q must be classified for linked media", value)
	}
}

// TestMediaLinks_RenamedOrganizerAliasShowsTargetArt is the #1717 regression:
// an Arcade Organizer alias indexed under scan_duplicates is renamed, so it has
// a title of its own and no scraper ever matches it. Once linked, it must show
// its target's art and metadata like the target does.
func TestMediaLinks_RenamedOrganizerAliasShowsTargetArt(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	// The alias is staged first: the walk can reach either one first.
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath),
		stagedLinkTestMedia(targetPath, "ASO", ""),
	)
	target := linkTestMedia(t, db, targetPath)
	alias := linkTestMedia(t, db, aliasPath)
	require.NotEqual(t, target.MediaTitleDBID, alias.MediaTitleDBID, "a renamed alias has its own title")
	linkedTo, linked := linkTestTarget(t, db, alias.DBID)
	require.True(t, linked)
	assert.Equal(t, target.DBID, linkedTo)

	// Cache "no cover" for the alias before the scrape.
	before := []database.SearchResultWithCursor{{MediaID: alias.DBID, MediaTitleID: alias.MediaTitleDBID}}
	require.NoError(t, fetchAndAttachCoverFlags(ctx, db.sql.Load(), before))
	require.False(t, before[0].HasCover)
	db.WaitForBackgroundOperations()

	scrapeLinkTestTarget(t, db, target)
	require.NoError(t, db.SyncLinkedMedia(ctx, []string{systemdefs.SystemArcade}, nil))

	assert.Equal(t, []string{
		string(tags.TagPropertyImageBoxart) + "=" + linkTestBoxart,
		string(tags.TagPropertyImageScreenshot) + "=" + linkTestScreenshot,
	}, linkTestProperties(t, db, alias.DBID), "title and media artwork land on the alias's own row")
	aliasTags := linkTestTags(t, db, alias.DBID)
	assert.Contains(t, aliasTags, string(tags.TagTypeGenre)+":"+string(tags.TagGameGenreAction))
	assert.Contains(t, aliasTags, string(tags.TagTypeYear)+":"+string(tags.TagYear1985))
	assert.Contains(t, aliasTags, "scraper.test:scraped", "the alias counts as scraped like its target")

	statuses, err := db.GetMediaCoverStatus(ctx, []database.MediaRef{
		{MediaDBID: alias.DBID, MediaTitleDBID: alias.MediaTitleDBID},
	})
	require.NoError(t, err)
	assert.Equal(t, map[int64]bool{alias.DBID: true}, statuses, "warm cover index sees the alias art")
	clearCoverAvailabilityCacheFor(db.sql.Load())
	cold := []database.SearchResultWithCursor{{MediaID: alias.DBID, MediaTitleID: alias.MediaTitleDBID}}
	require.NoError(t, fetchAndAttachCoverFlags(ctx, db.sql.Load(), cold))
	assert.True(t, cold[0].HasCover, "cold browse cover flags see the alias art")

	_, targetLinked := linkTestTarget(t, db, target.DBID)
	assert.False(t, targetLinked, "the target is not a link")

	tx, err := db.sql.Load().BeginTx(ctx, nil)
	require.NoError(t, err)
	result, err := sqlSyncMediaLinks(ctx, tx, nil, nil, false)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	assert.False(t, result.changed(), "a second sync has nothing to write")
}

// A link that keeps its target's name shares its title, so the title's data
// already applies and is not flattened onto the row, where it would start
// disambiguating the shared title.
func TestMediaLinks_SharedTitleCopiesOnlyMediaScope(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO.mra")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(targetPath, "ASO", ""),
		stagedLinkTestMedia(aliasPath, "ASO", targetPath),
	)
	target := linkTestMedia(t, db, targetPath)
	alias := linkTestMedia(t, db, aliasPath)
	require.Equal(t, target.MediaTitleDBID, alias.MediaTitleDBID)

	scrapeLinkTestTarget(t, db, target)
	require.NoError(t, db.SyncLinkedMedia(ctx, nil, nil))

	assert.Equal(t, []string{string(tags.TagPropertyImageScreenshot) + "=" + linkTestScreenshot},
		linkTestProperties(t, db, alias.DBID))
	aliasTags := linkTestTags(t, db, alias.DBID)
	assert.Contains(t, aliasTags, string(tags.TagTypeGenre)+":"+string(tags.TagGameGenreAction))
	assert.NotContains(t, aliasTags, string(tags.TagTypeYear)+":"+string(tags.TagYear1985))
}

// The sync replaces scraper data only. The user's launcher override, the
// user's tags, and the indexer's own value for a single-value type stay.
func TestMediaLinks_SyncKeepsRowOwnData(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(targetPath, "ASO", ""),
		stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath),
	)
	target := linkTestMedia(t, db, targetPath)
	alias := linkTestMedia(t, db, aliasPath)

	conn := db.sql.Load()
	override := tags.PropertyTypeTag(tags.TagPropertyLauncherOverride)
	_, err := conn.ExecContext(ctx, `
		INSERT INTO MediaProperties (MediaDBID, TypeTagDBID, Text)
		SELECT ?, t.DBID, 'Arcade' FROM Tags t JOIN TagTypes tt ON tt.DBID = t.TypeDBID
		WHERE tt.Type = ? AND t.Tag = ?`,
		alias.DBID, string(tags.TagTypeProperty), strings.TrimPrefix(override, "property:"))
	require.NoError(t, err)
	insertLink := func(mediaDBID int64, tagType tags.TagType, value string, scraped int) {
		t.Helper()
		_, insErr := conn.ExecContext(ctx, `
			INSERT INTO Tags (TypeDBID, Tag)
			SELECT tt.DBID, ? FROM TagTypes tt WHERE tt.Type = ?
			  AND NOT EXISTS (SELECT 1 FROM Tags t WHERE t.TypeDBID = tt.DBID AND t.Tag = ?)`,
			value, string(tagType), value)
		require.NoError(t, insErr)
		_, insErr = conn.ExecContext(ctx, `
			INSERT INTO MediaTags (MediaDBID, TagDBID, Scraped)
			SELECT ?, t.DBID, ? FROM Tags t JOIN TagTypes tt ON tt.DBID = t.TypeDBID
			WHERE tt.Type = ? AND t.Tag = ?`, mediaDBID, scraped, string(tagType), value)
		require.NoError(t, insErr)
	}
	insertLink(alias.DBID, tags.TagTypeUser, "favorite", 0)
	insertLink(alias.DBID, tags.TagTypeRev, string(tags.TagRev1), 0)
	insertLink(target.DBID, tags.TagTypeRev, string(tags.TagRev2), 1)

	scrapeLinkTestTarget(t, db, target)
	require.NoError(t, db.SyncLinkedMedia(ctx, nil, nil))

	assert.Contains(t, linkTestProperties(t, db, alias.DBID), string(tags.TagPropertyLauncherOverride)+"=Arcade")
	aliasTags := linkTestTags(t, db, alias.DBID)
	assert.Contains(t, aliasTags, string(tags.TagTypeUser)+":favorite")
	assert.Contains(t, aliasTags, string(tags.TagTypeRev)+":"+string(tags.TagRev1))
	assert.NotContains(t, aliasTags, string(tags.TagTypeRev)+":"+string(tags.TagRev2),
		"the indexer's single value is not replaced by the target's")
}

// A file that stops being an alias is its own media again: it loses the link
// and every value copied from its former target, and keeps its own data.
func TestMediaLinks_UnlinkedRowLosesCopies(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	files := []database.ScanStagedMedia{
		stagedLinkTestMedia(targetPath, "ASO", ""),
		stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath),
	}
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, files...)
	target := linkTestMedia(t, db, targetPath)
	alias := linkTestMedia(t, db, aliasPath)
	scrapeLinkTestTarget(t, db, target)
	require.NoError(t, db.SyncLinkedMedia(ctx, nil, nil))
	require.NotEmpty(t, linkTestProperties(t, db, alias.DBID))

	// An incomplete scan that never reached the alias proves nothing.
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{IncompleteScan: true}, files[0])
	_, linked := linkTestTarget(t, db, alias.DBID)
	assert.True(t, linked, "an incomplete scan keeps links it did not walk")

	// The alias is now a real file. Losing the copied tags marks its title
	// for the disambiguation recompute.
	files[1].LinkTarget = ""
	stats := indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, files...)
	assert.Equal(t, int64(1), stats.TouchedTitles)
	_, linked = linkTestTarget(t, db, alias.DBID)
	assert.False(t, linked)
	assert.Empty(t, linkTestProperties(t, db, alias.DBID))
	for _, tag := range linkTestTags(t, db, alias.DBID) {
		assert.False(t, strings.HasPrefix(tag, "genre:") || strings.HasPrefix(tag, "scraper."), tag)
	}
}

// A link repointed at a target the upsert refuses, here another link, is no
// longer a link to the game it used to show.
func TestMediaLinks_RepointedToRefusedTargetUnlinks(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	otherPath := arcadeLinkPath("_1 A-E", "ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	target := stagedLinkTestMedia(targetPath, "ASO", "")
	other := stagedLinkTestMedia(otherPath, "ASO", targetPath)
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target, other, stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath))
	scrapeLinkTestTarget(t, db, linkTestMedia(t, db, targetPath))
	require.NoError(t, db.SyncLinkedMedia(ctx, nil, nil))
	alias := linkTestMedia(t, db, aliasPath)
	require.NotEmpty(t, linkTestProperties(t, db, alias.DBID))

	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target, other, stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", otherPath))
	_, linked := linkTestTarget(t, db, alias.DBID)
	assert.False(t, linked)
	assert.Empty(t, linkTestProperties(t, db, alias.DBID), "the old target's art must not stay")
}

// A folder of the Organizer holds only aliases. Scraping it refreshes the
// games they show, never the aliases themselves, and leaves media outside
// the folder that no alias in it points at.
func TestMediaLinks_SubtreeScopeSelectsAliasTargets(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	asoPath := arcadeLinkPath("ASO.mra")
	otherPath := arcadeLinkPath("Other.mra")
	folder := arcadeLinkPath("_Organized", "_2 Year", "1985")
	aliasPath := arcadeLinkPath("_Organized", "_2 Year", "1985", "ASO.mra")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(asoPath, "ASO", ""),
		stagedLinkTestMedia(otherPath, "Other", ""),
		stagedLinkTestMedia(aliasPath, "ASO", asoPath),
	)

	rows, err := db.GetScrapeMedia(ctx, database.ScrapeScope{
		SystemID: systemdefs.SystemArcade, Path: folder, Subtree: true,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, asoPath, rows[0].Path)

	rows, err = db.GetScrapeMedia(ctx, database.ScrapeScope{
		SystemID: systemdefs.SystemArcade, Path: arcadeLinkPath(), Subtree: true,
	})
	require.NoError(t, err)
	paths := make([]string, 0, len(rows))
	for i := range rows {
		paths = append(paths, rows[i].Path)
	}
	assert.Equal(t, []string{asoPath, otherPath}, paths, "the whole tree lists each game once")
}

// A scrape of one game syncs that game's aliases and leaves the aliases of
// every other game untouched, even when their targets have unsynced data.
func TestMediaLinks_ScopedSyncTouchesOnlyScopedTargets(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	asoPath := arcadeLinkPath("ASO.mra")
	otherPath := arcadeLinkPath("Other.mra")
	asoAlias := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	otherAlias := arcadeLinkPath("_Organized", "_4 N-Z", "Other - The Game.mra")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(asoPath, "ASO", ""),
		stagedLinkTestMedia(otherPath, "Other", ""),
		stagedLinkTestMedia(asoAlias, "ASO - Armored Scrum Object", asoPath),
		stagedLinkTestMedia(otherAlias, "Other - The Game", otherPath),
	)
	aso := linkTestMedia(t, db, asoPath)
	scrapeLinkTestTarget(t, db, aso)
	scrapeLinkTestTarget(t, db, linkTestMedia(t, db, otherPath))

	require.NoError(t, db.SyncLinkedMedia(ctx, []string{systemdefs.SystemArcade}, &database.ScrapeScope{
		SystemID: systemdefs.SystemArcade, Path: asoPath, MediaID: aso.DBID,
	}))
	assert.NotEmpty(t, linkTestProperties(t, db, linkTestMedia(t, db, asoAlias).DBID))
	assert.Empty(t, linkTestProperties(t, db, linkTestMedia(t, db, otherAlias).DBID),
		"an alias of a game outside the scope is not synced")

	require.NoError(t, db.SyncLinkedMedia(ctx, []string{systemdefs.SystemArcade}, nil))
	assert.NotEmpty(t, linkTestProperties(t, db, linkTestMedia(t, db, otherAlias).DBID),
		"an unscoped sync covers every alias")
}

// A target that has itself become a link is refused for an existing link the
// same way it is for a new one, so a stored link never turns into a chain.
func TestMediaLinks_TargetBecameLinkUnlinks(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	targetPath := arcadeLinkPath("ASO.mra")
	middlePath := arcadeLinkPath("_1 A-E", "ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	target := stagedLinkTestMedia(targetPath, "ASO", "")
	alias := stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", middlePath)
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target, stagedLinkTestMedia(middlePath, "ASO", ""), alias)
	aliasRow := linkTestMedia(t, db, aliasPath)
	_, linked := linkTestTarget(t, db, aliasRow.DBID)
	require.True(t, linked)

	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target, stagedLinkTestMedia(middlePath, "ASO", targetPath), alias)
	_, linked = linkTestTarget(t, db, aliasRow.DBID)
	assert.False(t, linked, "a link whose target became a link is dropped")
	got, linked := linkTestTarget(t, db, linkTestMedia(t, db, middlePath).DBID)
	assert.True(t, linked)
	assert.Equal(t, linkTestMedia(t, db, targetPath).DBID, got)
}

// An incomplete scan that did not reach a stored link cannot tell whether it
// is still a link, so it is never accepted as a new link's target.
func TestMediaLinks_IncompleteScanRefusesUnwalkedLinkTarget(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	targetPath := arcadeLinkPath("ASO.mra")
	middlePath := arcadeLinkPath("_1 A-E", "ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	target := stagedLinkTestMedia(targetPath, "ASO", "")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target,
		stagedLinkTestMedia(middlePath, "ASO", targetPath),
		stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", ""))

	indexLinkTestMedia(t, db, database.ScanReconcileOpts{IncompleteScan: true},
		target, stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", middlePath))
	_, linked := linkTestTarget(t, db, linkTestMedia(t, db, aliasPath).DBID)
	assert.False(t, linked, "a stored link the scan did not reach is not a target")
	_, linked = linkTestTarget(t, db, linkTestMedia(t, db, middlePath).DBID)
	assert.True(t, linked, "the unwalked link itself is kept")
}

// A walked alias whose target went missing is its own media again, and the
// art copied from the gone target goes with the link.
func TestMediaLinks_MissingTargetUnlinks(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	alias := stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath)
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(targetPath, "ASO", ""), alias)
	scrapeLinkTestTarget(t, db, linkTestMedia(t, db, targetPath))
	require.NoError(t, db.SyncLinkedMedia(ctx, nil, nil))
	aliasRow := linkTestMedia(t, db, aliasPath)
	require.NotEmpty(t, linkTestProperties(t, db, aliasRow.DBID))

	stats := indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, alias)
	assert.Equal(t, int64(1), stats.MediaLinksDeleted)
	_, linked := linkTestTarget(t, db, aliasRow.DBID)
	assert.False(t, linked)
	assert.Empty(t, linkTestProperties(t, db, aliasRow.DBID))
}

// Unlinking purges the alias's copied data, so it needs a scrape: the library
// revision must move even when nothing else about the rows changed, or an
// index-triggered scrape would skip the system as unchanged.
func TestMediaLinks_UnlinkBumpsLibraryRevision(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO.mra")
	target := stagedLinkTestMedia(targetPath, "ASO", "")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target, stagedLinkTestMedia(aliasPath, "ASO", targetPath))
	before, err := db.LibraryRevision(ctx, systemdefs.SystemArcade)
	require.NoError(t, err)

	stats := indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target, stagedLinkTestMedia(aliasPath, "ASO", ""))
	require.Zero(t, stats.MediaUpserted, "the rows themselves are unchanged")
	assert.Equal(t, int64(1), stats.MediaLinksDeleted)
	after, err := db.LibraryRevision(ctx, systemdefs.SystemArcade)
	require.NoError(t, err)
	assert.Greater(t, after, before)

	stats = indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target, stagedLinkTestMedia(aliasPath, "ASO", ""))
	assert.Zero(t, stats.MediaLinksDeleted)
	unchanged, err := db.LibraryRevision(ctx, systemdefs.SystemArcade)
	require.NoError(t, err)
	assert.Equal(t, after, unchanged, "no unlink, no bump")
}

// Reconcile copies the target's existing data onto a new link, so it does
// not wait for the next scrape.
func TestMediaLinks_NewLinkTakesExistingData(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	target := stagedLinkTestMedia(targetPath, "ASO", "")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, target)
	scrapeLinkTestTarget(t, db, linkTestMedia(t, db, targetPath))

	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		target, stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath))
	alias := linkTestMedia(t, db, aliasPath)
	assert.Contains(t, linkTestProperties(t, db, alias.DBID),
		string(tags.TagPropertyImageBoxart)+"="+linkTestBoxart)
}

func TestMediaLinks_RefusedTargets(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()

	targetPath := arcadeLinkPath("ASO.mra")
	chainPath := arcadeLinkPath("_1 A-E", "ASO.mra")
	viaChainPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO (chain).mra")
	strayPath := arcadeLinkPath("_Organized", "_1 A-E", "Gone.mra")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(targetPath, "ASO", ""),
		stagedLinkTestMedia(chainPath, "ASO", targetPath),
		stagedLinkTestMedia(viaChainPath, "ASO (chain)", chainPath),
		stagedLinkTestMedia(strayPath, "Gone", arcadeLinkPath("Gone.mra")),
	)

	_, linked := linkTestTarget(t, db, linkTestMedia(t, db, chainPath).DBID)
	assert.True(t, linked)
	_, linked = linkTestTarget(t, db, linkTestMedia(t, db, viaChainPath).DBID)
	assert.False(t, linked, "a link to a link is refused")
	_, linked = linkTestTarget(t, db, linkTestMedia(t, db, strayPath).DBID)
	assert.False(t, linked, "a target that is not indexed links nothing")
}

func TestMediaLinks_ScraperStreamsSkipLinks(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	gonePath := arcadeLinkPath("Gone.mra")
	target := stagedLinkTestMedia(targetPath, "ASO", "")
	link := stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath)
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, target, link, stagedLinkTestMedia(gonePath, "Gone", ""))
	// Gone.mra is deleted, so its title's only row is missing.
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, target, link)
	targetRow := linkTestMedia(t, db, targetPath)
	alias := linkTestMedia(t, db, aliasPath)
	gone := linkTestMedia(t, db, gonePath)
	require.True(t, gone.IsMissing)

	var mediaPaths []string
	require.NoError(t, db.ForEachMediaBySystemID(ctx, systemdefs.SystemArcade,
		func(m *database.MediaWithFullPath) error {
			mediaPaths = append(mediaPaths, m.Path)
			return nil
		}))
	assert.Equal(t, []string{targetPath, gonePath}, mediaPaths)

	page, err := db.GetMediaPageBySystemID(ctx, systemdefs.SystemArcade, "", 10)
	require.NoError(t, err)
	require.Len(t, page, 2)
	assert.Equal(t, targetPath, page[0].Path)

	var titleIDs []int64
	require.NoError(t, db.ForEachTitleBySystemID(ctx, systemdefs.SystemArcade,
		func(title *database.TitleWithSystem) error {
			titleIDs = append(titleIDs, title.DBID)
			return nil
		}))
	assert.ElementsMatch(t, []int64{targetRow.MediaTitleDBID, gone.MediaTitleDBID}, titleIDs,
		"a title whose rows are only missing stays, so forced cleanup still reaches it")

	system, err := db.FindSystemBySystemID(systemdefs.SystemArcade)
	require.NoError(t, err)
	var sentinelTitles []int64
	require.NoError(t, db.ForEachMediaTitleWithoutSentinel(ctx, system.DBID, "scraper.test:scraped",
		func(title *database.MediaTitle) error {
			sentinelTitles = append(sentinelTitles, title.DBID)
			return nil
		}))
	assert.ElementsMatch(t, []int64{targetRow.MediaTitleDBID, gone.MediaTitleDBID}, sentinelTitles)

	scoped, err := db.GetScrapeMedia(ctx, database.ScrapeScope{
		SystemID: systemdefs.SystemArcade, Path: aliasPath, MediaID: alias.DBID,
	})
	require.NoError(t, err)
	assert.Empty(t, scoped, "a scoped scrape never selects a link")
}

// An alias carries a copy of its target's sentinel. A title it shares with a
// game of its own that was never scraped still needs scraping.
func TestMediaLinks_AliasSentinelDoesNotHideTitle(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	ownPath := arcadeLinkPath("ASO Bootleg.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO Bootleg.mra")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(targetPath, "Armored Scrum Object", ""),
		stagedLinkTestMedia(ownPath, "ASO Bootleg", ""),
		stagedLinkTestMedia(aliasPath, "ASO Bootleg", targetPath),
	)
	target := linkTestMedia(t, db, targetPath)
	own := linkTestMedia(t, db, ownPath)
	require.Equal(t, own.MediaTitleDBID, linkTestMedia(t, db, aliasPath).MediaTitleDBID)
	scrapeLinkTestTarget(t, db, target)
	require.NoError(t, db.SyncLinkedMedia(ctx, nil, nil))

	system, err := db.FindSystemBySystemID(systemdefs.SystemArcade)
	require.NoError(t, err)
	var titles []int64
	require.NoError(t, db.ForEachMediaTitleWithoutSentinel(ctx, system.DBID, "scraper.test:scraped",
		func(title *database.MediaTitle) error {
			titles = append(titles, title.DBID)
			return nil
		}))
	assert.Equal(t, []int64{own.MediaTitleDBID}, titles)
}

func TestMediaLinks_TouchedTitlesCaptured(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(targetPath, "ASO", ""),
		stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath),
	)
	scrapeLinkTestTarget(t, db, linkTestMedia(t, db, targetPath))
	alias := linkTestMedia(t, db, aliasPath)

	tx, err := db.sql.Load().BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	require.NoError(t, sqlEnsureScanStagingTables(ctx, tx))
	result, err := sqlSyncMediaLinks(ctx, tx, nil, nil, true)
	require.NoError(t, err)
	assert.Equal(t, []int64{alias.MediaTitleDBID}, result.tagTitles)
	touched, err := queryInt64s(ctx, tx, "SELECT TitleDBID FROM ScanTouchedTitles")
	require.NoError(t, err)
	assert.Equal(t, []int64{alias.MediaTitleDBID}, touched)
}

func TestMediaLinks_TruncateRemovesLinks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	countLinks := func(t *testing.T, db *MediaDB) int {
		t.Helper()
		var n int
		require.NoError(t, db.sql.Load().QueryRowContext(ctx, "SELECT COUNT(*) FROM MediaLinks").Scan(&n))
		return n
	}
	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO - Armored Scrum Object.mra")
	files := []database.ScanStagedMedia{
		stagedLinkTestMedia(targetPath, "ASO", ""),
		stagedLinkTestMedia(aliasPath, "ASO - Armored Scrum Object", targetPath),
	}

	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, files...)
	require.Equal(t, 1, countLinks(t, db))
	require.NoError(t, db.TruncateSystems([]string{systemdefs.SystemArcade}))
	assert.Zero(t, countLinks(t, db))

	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, files...)
	require.Equal(t, 1, countLinks(t, db))
	require.NoError(t, db.Truncate())
	assert.Zero(t, countLinks(t, db))

	// A missing target purged by orphan cleanup takes its link with it.
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, files...)
	require.Equal(t, 1, countLinks(t, db))
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, files[1])
	_, err := db.CleanMediaOrphans(ctx)
	require.NoError(t, err)
	assert.Zero(t, countLinks(t, db))
}

// Media DBIDs are reused once deleted, and every reset path runs with foreign
// keys off, so each must delete the rows other tables key by a media DBID.
// Otherwise a later media row inherits another game's sources, cover colour
// or slug resolution.
func TestMediaResetsDeleteRowsKeyedByMedia(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tables := []string{"MediaSources", "MediaCoverThumbs", "SlugResolutionCache"}
	gamePath := arcadeLinkPath("ASO.mra")
	otherPath := arcadeLinkPath("Pooyan.mra")

	seed := func(t *testing.T, db *MediaDB, mediaDBID int64) {
		t.Helper()
		conn := db.sql.Load()
		_, err := conn.ExecContext(ctx, `
			INSERT INTO MediaSources (MediaDBID, SourcePath, SourceKey, SourceRoot, SourceKind)
			VALUES (?, '/src/aso', '/src/aso', '/src', 'file')`, mediaDBID)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx,
			`INSERT INTO MediaCoverThumbs (MediaDBID, TypeTag, Color) VALUES (?, 'property:image-boxart', 1)`,
			mediaDBID)
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, `
			INSERT INTO SlugResolutionCache
				(CacheKey, SystemID, Slug, TagFilters, MediaDBID, Strategy, Confidence, LastUpdated)
			VALUES ('aso', 'Arcade', 'aso', '[]', ?, 'exact', 1, 0)`, mediaDBID)
		require.NoError(t, err)
	}
	count := func(t *testing.T, db *MediaDB, table string) int {
		t.Helper()
		var n int
		require.NoError(t, db.sql.Load().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n))
		return n
	}

	resets := map[string]func(t *testing.T, db *MediaDB){
		"truncate": func(t *testing.T, db *MediaDB) {
			t.Helper()
			require.NoError(t, db.Truncate())
		},
		"truncate systems": func(t *testing.T, db *MediaDB) {
			t.Helper()
			require.NoError(t, db.TruncateSystems([]string{systemdefs.SystemArcade}))
		},
		"clean orphans": func(t *testing.T, db *MediaDB) {
			t.Helper()
			indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, stagedLinkTestMedia(otherPath, "Pooyan", ""))
			_, err := db.CleanMediaOrphans(ctx)
			require.NoError(t, err)
		},
	}
	for name, reset := range resets {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, cleanup := setupTempMediaDB(t)
			defer cleanup()
			indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
				stagedLinkTestMedia(gamePath, "ASO", ""), stagedLinkTestMedia(otherPath, "Pooyan", ""))
			seed(t, db, linkTestMedia(t, db, gamePath).DBID)
			for _, table := range tables {
				require.Equal(t, 1, count(t, db, table), table)
			}
			reset(t, db)
			for _, table := range tables {
				assert.Zero(t, count(t, db, table), table)
			}
		})
	}
}
