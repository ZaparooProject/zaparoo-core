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
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func versionKey(system, title int64, keyType, key string) versionKeyRow {
	return versionKeyRow{system: system, title: title, keyType: keyType, key: key}
}

func TestVersionGroupsFromKeys(t *testing.T) {
	t.Parallel()

	t.Run("titles sharing a key form one group named after the lowest", func(t *testing.T) {
		t.Parallel()
		groups := versionGroupsFromKeys([]versionKeyRow{
			versionKey(1, 30, database.VersionKeyMAMESet, "aso"),
			versionKey(1, 20, database.VersionKeyMAMESet, "aso"),
			versionKey(1, 40, database.VersionKeyMAMESet, "other"),
		})
		assert.Equal(t, map[int64]int64{20: 20, 30: 20}, groups)
	})

	t.Run("keys chain", func(t *testing.T) {
		t.Parallel()
		// 10 and 11 share a set, 11 and 12 share a folder: one game.
		groups := versionGroupsFromKeys([]versionKeyRow{
			versionKey(1, 12, database.VersionKeyAltFolder, "aso"),
			versionKey(1, 11, database.VersionKeyAltFolder, "aso"),
			versionKey(1, 11, database.VersionKeyMAMESet, "aso"),
			versionKey(1, 10, database.VersionKeyMAMESet, "aso"),
		})
		assert.Equal(t, map[int64]int64{10: 10, 11: 10, 12: 10}, groups)
	})

	t.Run("a key joins nothing across systems or key types", func(t *testing.T) {
		t.Parallel()
		groups := versionGroupsFromKeys([]versionKeyRow{
			versionKey(1, 10, database.VersionKeyMAMESet, "contra"),
			versionKey(2, 20, database.VersionKeyMAMESet, "contra"),
			versionKey(1, 11, database.VersionKeyGameDatabaseID, "contra"),
		})
		assert.Empty(t, groups)
	})

	t.Run("one title holding a key twice is not a group", func(t *testing.T) {
		t.Parallel()
		groups := versionGroupsFromKeys([]versionKeyRow{
			versionKey(1, 10, database.VersionKeyMAMESet, "aso"),
			versionKey(1, 10, database.VersionKeyMAMESet, "aso"),
		})
		assert.Empty(t, groups)
	})

	t.Run("a key shared too widely is ignored", func(t *testing.T) {
		t.Parallel()
		keys := make([]versionKeyRow, 0, versionKeyMaxTitles+3)
		for title := int64(1); title <= versionKeyMaxTitles+1; title++ {
			keys = append(keys, versionKey(1, title, database.VersionKeyMAMESet, "bios"))
		}
		keys = append(keys,
			versionKey(1, 1, database.VersionKeyAltFolder, "real"),
			versionKey(1, 2, database.VersionKeyAltFolder, "real"),
		)
		assert.Equal(t, map[int64]int64{1: 1, 2: 1}, versionGroupsFromKeys(keys),
			"the generic key joins nothing, the real one still does")
	})

	t.Run("a group stops growing at its bound", func(t *testing.T) {
		t.Parallel()
		// Two full-size keys bridged by a third would make one oversized group.
		keys := make([]versionKeyRow, 0, 2*versionKeyMaxTitles+14)
		for title := int64(1); title <= versionKeyMaxTitles; title++ {
			keys = append(keys, versionKey(1, title, database.VersionKeyMAMESet, "left"))
		}
		for title := int64(1001); title <= 1000+versionKeyMaxTitles; title++ {
			keys = append(keys, versionKey(1, title, database.VersionKeyMAMESet, "right"))
		}
		for title := int64(2001); title <= 2010; title++ {
			keys = append(keys, versionKey(1, title, database.VersionKeyMAMESet, "small"))
		}
		keys = append(keys,
			versionKey(1, 1, database.VersionKeyAltFolder, "bridge-a"),
			versionKey(1, 1001, database.VersionKeyAltFolder, "bridge-a"),
			versionKey(1, 1, database.VersionKeyAltFolder, "bridge-b"),
			versionKey(1, 2001, database.VersionKeyAltFolder, "bridge-b"),
		)
		groups := versionGroupsFromKeys(keys)
		sizes := make(map[int64]int)
		for _, group := range groups {
			sizes[group]++
		}
		for group, size := range sizes {
			assert.LessOrEqual(t, size, versionGroupMaxTitles, "group %d", group)
		}
		assert.Len(t, groups, 2*versionKeyMaxTitles+10, "every title still belongs to a group")
	})
}

func TestNormalizeVersionKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "aso", normalizeVersionKey(database.VersionKeyMAMESet, " ASO "))
	// A romset name is stored unpadded as a property and padded as a tag.
	assert.Equal(t, normalizeVersionKey(database.VersionKeyMAMESet, "720"),
		normalizeVersionKey(database.VersionKeyMAMESet, tags.PadTagValue("720")))
	assert.Equal(t, "0720", normalizeVersionKey(database.VersionKeyAltFolder, "0720"),
		"only romset names are stored padded")
}

type versionTestLibrary struct {
	db     *MediaDB
	media  map[string]*database.Media
	titles map[string]int64
}

// newVersionTestLibrary indexes one arcade descriptor per name, each with its
// own title.
func newVersionTestLibrary(t *testing.T, names ...string) *versionTestLibrary {
	t.Helper()
	db, cleanup := setupTempMediaDB(t)
	t.Cleanup(cleanup)
	files := make([]database.ScanStagedMedia, 0, len(names))
	for _, name := range names {
		files = append(files, stagedLinkTestMedia(arcadeLinkPath(name+".mra"), name, ""))
	}
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, files...)
	lib := &versionTestLibrary{db: db, media: map[string]*database.Media{}, titles: map[string]int64{}}
	for _, name := range names {
		media := linkTestMedia(t, db, arcadeLinkPath(name+".mra"))
		lib.media[name] = media
		lib.titles[name] = media.MediaTitleDBID
	}
	return lib
}

func (l *versionTestLibrary) scrape(t *testing.T, name string, write *database.ScrapeWrite) {
	t.Helper()
	media := l.media[name]
	require.NoError(t, l.db.ApplyScrapeResult(context.Background(), media.DBID, media.MediaTitleDBID, write))
}

func (l *versionTestLibrary) regroup(t *testing.T) {
	t.Helper()
	require.NoError(t, l.db.RecomputeVersionGroups(context.Background(), []string{systemdefs.SystemArcade}))
}

func (l *versionTestLibrary) groups(t *testing.T, names ...string) map[string]int64 {
	t.Helper()
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		ids = append(ids, l.titles[name])
	}
	byTitle, err := l.db.GetTitleVersionGroups(context.Background(), ids)
	require.NoError(t, err)
	out := make(map[string]int64, len(names))
	for _, name := range names {
		out[name] = byTitle[l.titles[name]]
	}
	return out
}

func (l *versionTestLibrary) storedKeys(t *testing.T, name string) []string {
	t.Helper()
	rows, err := l.db.sql.Load().QueryContext(context.Background(), `
		SELECT Source, KeyType, Key FROM MediaVersionKeys WHERE MediaDBID = ?
		ORDER BY Source, KeyType, Key`, l.media[name].DBID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	keys := []string{}
	for rows.Next() {
		var source, keyType, key string
		require.NoError(t, rows.Scan(&source, &keyType, &key))
		keys = append(keys, source+"/"+keyType+"="+key)
	}
	require.NoError(t, rows.Err())
	return keys
}

func setNameWrite(setName, parent string) *database.ScrapeWrite {
	write := &database.ScrapeWrite{
		MediaProps: []database.MediaProperty{{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyMAMESetName), Text: setName,
		}},
	}
	if parent != "" {
		write.MediaTags = []database.TagInfo{{Type: string(tags.TagTypeMameParent), Tag: parent}}
	}
	return write
}

func keysWrite(source string, keys ...database.VersionKey) *database.ScrapeWrite {
	return &database.ScrapeWrite{VersionKeySource: source, VersionKeys: keys}
}

// Differently named releases of one game meet through what scrapers already
// store about them: the set a clone names as its parent, and a shared game ID.
func TestRecomputeVersionGroups_ReadsScrapedIdentity(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission", "Arian Mission", "Contra", "Gryzor", "Unrelated")
	ctx := context.Background()

	lib.scrape(t, "ASO", setNameWrite("aso", ""))
	lib.scrape(t, "Alpha Mission", setNameWrite("alphamis", "aso"))
	lib.scrape(t, "Arian Mission", setNameWrite("arian", "aso"))
	gameID := func(id string) *database.ScrapeWrite {
		return &database.ScrapeWrite{TitleProps: []database.MediaProperty{{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyGameDatabaseID), Text: id,
		}}}
	}
	lib.scrape(t, "Contra", gameID("contra"))
	lib.scrape(t, "Gryzor", gameID("contra"))
	lib.scrape(t, "Unrelated", gameID("unrelated"))

	groups := lib.groups(t, "ASO", "Unrelated")
	assert.Equal(t, lib.titles["ASO"], groups["ASO"], "nothing is grouped before a regroup")

	lib.regroup(t)

	groups = lib.groups(t, "ASO", "Alpha Mission", "Arian Mission", "Contra", "Gryzor", "Unrelated")
	aso := min(lib.titles["ASO"], lib.titles["Alpha Mission"], lib.titles["Arian Mission"])
	assert.Equal(t, aso, groups["ASO"])
	assert.Equal(t, aso, groups["Alpha Mission"])
	assert.Equal(t, aso, groups["Arian Mission"], "two clones meet through the parent they both name")
	contra := min(lib.titles["Contra"], lib.titles["Gryzor"])
	assert.Equal(t, contra, groups["Contra"])
	assert.Equal(t, contra, groups["Gryzor"])
	assert.Equal(t, lib.titles["Unrelated"], groups["Unrelated"], "an unlinked title is its own group")

	members, err := lib.db.GetVersionGroupMembers(ctx, []int64{lib.titles["Alpha Mission"], lib.titles["Unrelated"]})
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]int64{lib.titles["ASO"], lib.titles["Alpha Mission"], lib.titles["Arian Mission"]},
		members[lib.titles["Alpha Mission"]])
	assert.NotContains(t, members, lib.titles["Unrelated"])

	keys, err := lib.db.GetVersionKeysByTitleDBIDs(ctx, []int64{lib.titles["Alpha Mission"], lib.titles["Contra"]})
	require.NoError(t, err)
	assert.ElementsMatch(t, []database.VersionKey{
		{Type: database.VersionKeyMAMESet, Key: "alphamis"},
		{Type: database.VersionKeyMAMESet, Key: "aso"},
	}, keys[lib.titles["Alpha Mission"]])
	assert.Equal(t, []database.VersionKey{{Type: database.VersionKeyGameDatabaseID, Key: "contra"}},
		keys[lib.titles["Contra"]])

	// A second regroup finds every title already where it belongs.
	lib.regroup(t)
	assert.Equal(t, groups,
		lib.groups(t, "ASO", "Alpha Mission", "Arian Mission", "Contra", "Gryzor", "Unrelated"))
}

// A regroup only has a say over the systems it was asked about.
func TestRecomputeVersionGroups_UnknownSystemIsANoOp(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission")
	lib.scrape(t, "ASO", setNameWrite("aso", ""))
	lib.scrape(t, "Alpha Mission", setNameWrite("alphamis", "aso"))
	lib.regroup(t)
	grouped := lib.groups(t, "ASO", "Alpha Mission")

	require.NoError(t, lib.db.RecomputeVersionGroups(context.Background(), []string{"NotIndexed"}))
	assert.Equal(t, grouped, lib.groups(t, "ASO", "Alpha Mission"))

	// Every system at once reaches the same answer.
	require.NoError(t, lib.db.RecomputeVersionGroups(context.Background(), nil))
	assert.Equal(t, grouped, lib.groups(t, "ASO", "Alpha Mission"))
}

func TestRecomputeVersionGroups_StoredKeysAndUngrouping(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "Green Beret", "Rush'n Attack", "Other")
	folder := database.VersionKey{Type: database.VersionKeyAltFolder, Key: "greenberet"}
	lib.scrape(t, "Green Beret", keysWrite("test", folder))
	lib.scrape(t, "Rush'n Attack", keysWrite("test", folder))
	lib.regroup(t)

	groups := lib.groups(t, "Green Beret", "Rush'n Attack", "Other")
	assert.Equal(t, groups["Green Beret"], groups["Rush'n Attack"])
	assert.Equal(t, lib.titles["Other"], groups["Other"])

	// The source withdraws the key: the titles part again.
	lib.scrape(t, "Rush'n Attack", keysWrite("test"))
	lib.regroup(t)
	groups = lib.groups(t, "Green Beret", "Rush'n Attack")
	assert.Equal(t, lib.titles["Green Beret"], groups["Green Beret"])
	assert.Equal(t, lib.titles["Rush'n Attack"], groups["Rush'n Attack"])

	var stillGrouped int
	require.NoError(t, lib.db.sql.Load().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM MediaTitles WHERE VersionGroup IS NOT NULL`).Scan(&stillGrouped))
	assert.Zero(t, stillGrouped, "a title linked to nothing stores no group")
}

// A file that is gone no longer speaks for its title.
func TestRecomputeVersionGroups_IgnoresMissingMedia(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission")
	lib.scrape(t, "ASO", setNameWrite("aso", ""))
	lib.scrape(t, "Alpha Mission", setNameWrite("alphamis", "aso"))
	lib.regroup(t)
	groups := lib.groups(t, "ASO", "Alpha Mission")
	require.Equal(t, groups["ASO"], groups["Alpha Mission"])

	_, err := lib.db.sql.Load().ExecContext(context.Background(),
		`UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, lib.media["Alpha Mission"].DBID)
	require.NoError(t, err)
	lib.regroup(t)
	groups = lib.groups(t, "ASO", "Alpha Mission")
	assert.Equal(t, lib.titles["ASO"], groups["ASO"])
}

// A title whose files are all gone holds no game ID, so it cannot push a
// shared ID past the size bound and part the titles that are present.
func TestRecomputeVersionGroups_MissingTitleDoesNotOversizeGameID(t *testing.T) {
	t.Parallel()
	names := make([]string, versionKeyMaxTitles+1)
	for i := range names {
		names[i] = fmt.Sprintf("Contra %03d", i)
	}
	lib := newVersionTestLibrary(t, names...)
	for _, name := range names {
		lib.scrape(t, name, &database.ScrapeWrite{TitleProps: []database.MediaProperty{{
			TypeTag: tags.PropertyTypeTag(tags.TagPropertyGameDatabaseID), Text: "contra",
		}}})
	}
	gone := names[len(names)-1]
	_, err := lib.db.sql.Load().ExecContext(context.Background(),
		`UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, lib.media[gone].DBID)
	require.NoError(t, err)
	lib.regroup(t)

	groups := lib.groups(t, names...)
	want := groups[names[0]]
	for _, name := range names[:len(names)-1] {
		assert.Equal(t, want, groups[name], name)
	}
	assert.Equal(t, lib.titles[gone], groups[gone])
}

// A scrape replaces exactly the keys its own source wrote for a row.
func TestScrapeVersionKeys_ReplacePerSource(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO")
	set := func(key string) database.VersionKey {
		return database.VersionKey{Type: database.VersionKeyMAMESet, Key: key}
	}

	lib.scrape(t, "ASO", keysWrite("one", set("ASO"), set("aso"), set("parent")))
	lib.scrape(t, "ASO", keysWrite("two", database.VersionKey{Type: database.VersionKeyAltFolder, Key: "aso"}))
	assert.Equal(t, []string{"one/mame-set=aso", "one/mame-set=parent", "two/alt-folder=aso"},
		lib.storedKeys(t, "ASO"), "keys are stored normalised and once")

	// Writing the same keys again, as a resumed run does, changes nothing.
	lib.scrape(t, "ASO", keysWrite("one", set("parent"), set("aso")))
	assert.Equal(t, []string{"one/mame-set=aso", "one/mame-set=parent", "two/alt-folder=aso"},
		lib.storedKeys(t, "ASO"))

	lib.scrape(t, "ASO", keysWrite("one", set("aso")))
	assert.Equal(t, []string{"one/mame-set=aso", "two/alt-folder=aso"}, lib.storedKeys(t, "ASO"),
		"the other source's key is left alone")

	// A write that names no source has no say over stored keys.
	lib.scrape(t, "ASO", &database.ScrapeWrite{VersionKeys: []database.VersionKey{set("ignored")}})
	assert.Equal(t, []string{"one/mame-set=aso", "two/alt-folder=aso"}, lib.storedKeys(t, "ASO"))

	lib.scrape(t, "ASO", keysWrite("one",
		database.VersionKey{Type: "made-up", Key: "x"},
		database.VersionKey{Type: database.VersionKeyMAMESet, Key: ""},
	))
	assert.Equal(t, []string{"two/alt-folder=aso"}, lib.storedKeys(t, "ASO"),
		"an unknown or empty key is refused, and the source's old keys still go")
}

// The batch path a scraper run uses stores keys the same way.
func TestScrapeVersionKeys_BatchApply(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission")
	folder := database.VersionKey{Type: database.VersionKeyAltFolder, Key: "aso"}
	targets := []database.ScrapeWriteTarget{
		{MediaDBID: lib.media["ASO"].DBID, MediaTitleDBID: lib.titles["ASO"], Write: keysWrite("batch", folder)},
		{
			MediaDBID: lib.media["Alpha Mission"].DBID, MediaTitleDBID: lib.titles["Alpha Mission"],
			Write: keysWrite("batch", folder),
		},
	}
	require.NoError(t, lib.db.ApplyScrapeResults(context.Background(), targets))
	assert.Equal(t, []string{"batch/alt-folder=aso"}, lib.storedKeys(t, "ASO"))
	assert.Equal(t, []string{"batch/alt-folder=aso"}, lib.storedKeys(t, "Alpha Mission"))

	lib.regroup(t)
	groups := lib.groups(t, "ASO", "Alpha Mission")
	assert.Equal(t, groups["ASO"], groups["Alpha Mission"])
}

func countVersionKeys(t *testing.T, db *MediaDB) int {
	t.Helper()
	var count int
	require.NoError(t, db.sql.Load().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM MediaVersionKeys`).Scan(&count))
	return count
}

// Media DBIDs are reused, so a key left behind by a deleted row would speak
// for whichever file took its place.
func TestVersionKeys_DeletedWithTheirMedia(t *testing.T) {
	t.Parallel()
	folder := database.VersionKey{Type: database.VersionKeyAltFolder, Key: "aso"}

	t.Run("truncate", func(t *testing.T) {
		t.Parallel()
		lib := newVersionTestLibrary(t, "ASO")
		lib.scrape(t, "ASO", keysWrite("test", folder))
		require.Equal(t, 1, countVersionKeys(t, lib.db))
		require.NoError(t, lib.db.Truncate())
		assert.Zero(t, countVersionKeys(t, lib.db))
	})

	t.Run("truncate system", func(t *testing.T) {
		t.Parallel()
		lib := newVersionTestLibrary(t, "ASO")
		lib.scrape(t, "ASO", keysWrite("test", folder))
		require.NoError(t, lib.db.TruncateSystems([]string{systemdefs.SystemArcade}))
		assert.Zero(t, countVersionKeys(t, lib.db))
	})

	t.Run("missing media cleanup", func(t *testing.T) {
		t.Parallel()
		lib := newVersionTestLibrary(t, "ASO", "Alpha Mission")
		lib.scrape(t, "ASO", keysWrite("test", folder))
		lib.scrape(t, "Alpha Mission", keysWrite("test", folder))
		_, err := lib.db.sql.Load().ExecContext(context.Background(),
			`UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, lib.media["ASO"].DBID)
		require.NoError(t, err)
		_, err = lib.db.CleanMediaOrphans(context.Background())
		require.NoError(t, err)
		assert.Equal(t, []string{"test/alt-folder=aso"}, lib.storedKeys(t, "Alpha Mission"))
		assert.Equal(t, 1, countVersionKeys(t, lib.db), "only the surviving row keeps its key")
	})
}

func TestGetTitleVersionRows(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission", "Other")
	ctx := context.Background()

	rows, err := lib.db.GetTitleVersionRows(ctx,
		[]int64{lib.titles["ASO"], lib.titles["Alpha Mission"]}, &database.SearchFilters{ExcludeHidden: true})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byTitle := map[int64]database.SearchResultWithCursor{}
	for i := range rows {
		byTitle[rows[i].MediaTitleID] = rows[i]
	}
	aso := byTitle[lib.titles["ASO"]]
	assert.Equal(t, lib.media["ASO"].DBID, aso.MediaID)
	assert.Equal(t, arcadeLinkPath("ASO.mra"), aso.Path)
	assert.Equal(t, systemdefs.SystemArcade, aso.SystemID)

	rows, err = lib.db.GetTitleVersionRows(ctx, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, rows)

	// A hidden file is not a version anyone browsing would be offered.
	require.NoError(t, lib.db.UpdateMediaTags(ctx, lib.media["ASO"].DBID, nil,
		[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	titles := []int64{lib.titles["ASO"], lib.titles["Alpha Mission"]}
	rows, err = lib.db.GetTitleVersionRows(ctx, titles, &database.SearchFilters{ExcludeHidden: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, lib.media["Alpha Mission"].DBID, rows[0].MediaID)
	rows, err = lib.db.GetTitleVersionRows(ctx, titles, &database.SearchFilters{})
	require.NoError(t, err)
	assert.Len(t, rows, 2, "a listing that includes hidden media counts it")
}

// A search for several titles at once is how a game's versions are listed
// when they are filed under more than one name.
func TestSearchMediaWithFilters_TitleDBIDs(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission", "Other")
	ctx := context.Background()

	rows, err := lib.db.SearchMediaWithFilters(ctx, &database.SearchFilters{
		TitleDBIDs: []int64{lib.titles["ASO"], lib.titles["Alpha Mission"]}, Limit: 100,
	})
	require.NoError(t, err)
	names := make([]string, 0, len(rows))
	for i := range rows {
		names = append(names, rows[i].Name)
	}
	assert.ElementsMatch(t, []string{"ASO", "Alpha Mission"}, names)

	// TitleDBID and TitleDBIDs together name the union.
	rows, err = lib.db.SearchMediaWithFilters(ctx, &database.SearchFilters{
		TitleDBID: lib.titles["Other"], TitleDBIDs: []int64{lib.titles["ASO"]}, Limit: 100,
	})
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func TestCountTitleVersionRows_NoLinks(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO")
	ctx := context.Background()
	indexLinkTestMedia(t, lib.db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(arcadeLinkPath("ASO.mra"), "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("_alternatives", "_ASO", "ASO (Japan).mra"), "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("ASO", "ASO.cue"), "ASO", ""),
	)
	counts, err := lib.db.CountTitleVersionRows(ctx, []int64{lib.titles["ASO"]}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[int64]database.TitleVersionRowCount{lib.titles["ASO"]: {Rows: 3, Foldable: 1}}, counts)
	assert.NotContains(t, titleVersionRowCountSQL(1, nil, false), "MediaLinks",
		"a library with no links must not probe for them on every row")
}

func TestCountTitleVersionRows(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission")
	ctx := context.Background()
	asoPath := arcadeLinkPath("ASO.mra")
	indexLinkTestMedia(t, lib.db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(asoPath, "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("Alpha Mission.mra"), "Alpha Mission", ""),
		stagedLinkTestMedia(arcadeLinkPath("_alternatives", "_ASO", "ASO (Japan).mra"), "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("ASO", "ASO (Track 1).BIN"), "ASO", ""),
		stagedLinkTestMedia(arcadeLinkPath("_Organized", "_1 A-E", "ASO.mra"), "ASO", asoPath),
	)
	_, err := lib.db.sql.Load().ExecContext(ctx,
		`UPDATE Media SET IsMissing = 1 WHERE DBID = ?`, lib.media["Alpha Mission"].DBID)
	require.NoError(t, err)

	titles := []int64{lib.titles["ASO"], lib.titles["Alpha Mission"], 99_999}
	counts, err := lib.db.CountTitleVersionRows(ctx, titles, nil)
	require.NoError(t, err)
	assert.Equal(t, map[int64]database.TitleVersionRowCount{
		lib.titles["ASO"]: {Rows: 4, Foldable: 2},
	}, counts, "the track file and the alias may fold; a title with no present file is absent")

	// The count must see exactly the rows a version read would return.
	for _, filters := range []*database.SearchFilters{
		nil,
		{ExcludeHidden: true},
		{PathPrefix: arcadeLinkPath("_alternatives")},
	} {
		counts, err := lib.db.CountTitleVersionRows(ctx, titles, filters)
		require.NoError(t, err)
		rows, err := lib.db.GetTitleVersionRows(ctx, titles, filters)
		require.NoError(t, err)
		perTitle := make(map[int64]int)
		for i := range rows {
			perTitle[rows[i].MediaTitleID]++
		}
		got := make(map[int64]int)
		for title, count := range counts {
			got[title] = count.Rows
		}
		assert.Equal(t, perTitle, got, "%+v", filters)
	}

	plan := explainPlan(t, lib.db, titleVersionRowCountSQL(1, nil, true), lib.titles["ASO"])
	t.Logf("title version row count plan:\n%s", plan)
	assert.NotContains(t, plan, "SCAN Media", "the count must enter Media by title")
}

func TestGetMediaLinkTargets(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	targetPath := arcadeLinkPath("ASO.mra")
	aliasPath := arcadeLinkPath("_Organized", "_1 A-E", "ASO.mra")
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{}, stagedLinkTestMedia(targetPath, "ASO", ""))
	target := linkTestMedia(t, db, targetPath)

	none, err := db.GetMediaLinkTargets(ctx, []int64{target.DBID})
	require.NoError(t, err)
	assert.Empty(t, none, "a library with no links answers without reading any")

	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(targetPath, "ASO", ""),
		stagedLinkTestMedia(aliasPath, "ASO", targetPath),
	)
	alias := linkTestMedia(t, db, aliasPath)
	targets, err := db.GetMediaLinkTargets(ctx, []int64{target.DBID, alias.DBID})
	require.NoError(t, err)
	assert.Equal(t, map[int64]int64{alias.DBID: target.DBID}, targets)
}

// Reading a group's members must enter the partial index: it holds only
// linked titles, where the table holds the whole library.
func TestVersionGroupMembersUseGroupIndex(t *testing.T) {
	t.Parallel()
	lib := newVersionTestLibrary(t, "ASO", "Alpha Mission")
	lib.scrape(t, "ASO", setNameWrite("aso", ""))
	lib.scrape(t, "Alpha Mission", setNameWrite("alphamis", "aso"))
	lib.regroup(t)

	plan := explainPlan(t, lib.db, versionGroupMembersSQL+"?) ORDER BY t.DBID, o.DBID", lib.titles["ASO"])
	t.Logf("version group members plan:\n%s", plan)
	assert.Contains(t, plan, versionGroupIndexName)
	assert.NotContains(t, plan, "SCAN o")
}

// The schema change is one a database of any size can take at startup: the
// group index is not part of it.
func TestVersionKeysMigration(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTempMediaDB(t)
	defer cleanup()
	ctx := context.Background()

	var columns int
	require.NoError(t, db.sql.Load().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('MediaTitles') WHERE name = 'VersionGroup'`).Scan(&columns))
	assert.Equal(t, 1, columns)

	indexed := func() int {
		var count int
		require.NoError(t, db.sql.Load().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`,
			versionGroupIndexName).Scan(&count))
		return count
	}
	assert.Zero(t, indexed(), "the migration builds no index over existing titles")

	for _, column := range []string{"SystemDBID", "KeyType", "Key", "MediaDBID", "Source"} {
		var found int
		require.NoError(t, db.sql.Load().QueryRowContext(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('MediaVersionKeys') WHERE name = '%s'`, column),
		).Scan(&found))
		assert.Equal(t, 1, found, column)
	}

	lib := &versionTestLibrary{db: db}
	indexLinkTestMedia(t, db, database.ScanReconcileOpts{},
		stagedLinkTestMedia(arcadeLinkPath("ASO.mra"), "ASO", ""))
	lib.regroup(t)
	assert.Zero(t, indexed(), "a regroup with nothing to write builds nothing")
}
