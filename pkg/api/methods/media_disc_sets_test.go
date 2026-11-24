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

package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// discSetFixture is a real PSX library behind the API handlers:
//
//	PSX/Saga/Saga (Disc 1|2|3).chd   a three-disc set
//	PSX/Quest/Quest (Disc 1|2).chd   a two-disc set
//	PSX/Solo/Solo.chd                a folder with one game
//	PSX/Collection/Alpha.chd, Beta.chd  two games in one folder
//	PSX/Loose.chd                    a file at the top
type discSetFixture struct {
	userDB database.UserDBI
	ids    map[string]int64
	labels map[string]string
	psx    string
	env    requests.RequestEnv
}

func newDiscSetFixture(t *testing.T) *discSetFixture {
	t.Helper()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	root := t.TempDir()
	psx := filepath.Join(root, "PSX")
	labels := map[string]string{
		"saga1":  filepath.Join(psx, "Saga", "Saga (Disc 1).chd"),
		"saga2":  filepath.Join(psx, "Saga", "Saga (Disc 2).chd"),
		"saga3":  filepath.Join(psx, "Saga", "Saga (Disc 3).chd"),
		"quest1": filepath.Join(psx, "Quest", "Quest (Disc 1).chd"),
		"quest2": filepath.Join(psx, "Quest", "Quest (Disc 2).chd"),
		"solo":   filepath.Join(psx, "Solo", "Solo.chd"),
		"alpha":  filepath.Join(psx, "Collection", "Alpha.chd"),
		"beta":   filepath.Join(psx, "Collection", "Beta.chd"),
		"loose":  filepath.Join(psx, "Loose.chd"),
	}
	paths := make([]string, 0, len(labels))
	for _, path := range labels {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	scantest.IndexMediaPaths(t, mediaDB, "PSX", paths...)
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))

	rows, err := mediaDB.GetMediaBySystemID("PSX")
	require.NoError(t, err)
	byPath := make(map[string]int64, len(rows))
	for _, row := range rows {
		byPath[row.Path] = row.DBID
	}
	f := &discSetFixture{
		userDB: userDB,
		psx:    psx,
		ids:    make(map[string]int64, len(labels)),
		labels: make(map[string]string, len(labels)),
	}
	for label, path := range labels {
		canonical := pathutil.CanonicalMediaPath(path)
		require.NotZero(t, byPath[canonical], "%s is indexed", path)
		f.ids[label] = byPath[canonical]
		f.labels[canonical] = label
	}

	platform := mocks.NewMockPlatform()
	platform.On("RootDirs", mock.Anything).Return([]string{root})
	platform.On("SupportedReaders", mock.Anything).Return(nil)
	launchers := []platforms.Launcher{{ID: "PSX", SystemID: "PSX", Folders: []string{"PSX"}}}
	platform.On("Launchers", mock.Anything).Return(launchers)
	cache := &phelpers.LauncherCache{}
	cache.InitializeFromSlice(launchers)
	f.env = requests.RequestEnv{
		Context:       ctx,
		Database:      &database.Database{MediaDB: mediaDB, UserDB: userDB},
		Platform:      platform,
		Config:        &config.Instance{},
		LauncherCache: cache,
	}
	return f
}

func (f *discSetFixture) path(label string) string {
	return pathutil.CanonicalMediaPath(f.ids2path(label))
}

func (f *discSetFixture) ids2path(label string) string {
	for path, l := range f.labels {
		if l == label {
			return path
		}
	}
	panic("unknown label " + label)
}

func (f *discSetFixture) folder(name string) string {
	return filepath.ToSlash(filepath.Join(f.psx, name))
}

// flag sets flags on one file as a single-file edit would, the way Library
// sync can leave them on any disc.
func (f *discSetFixture) flag(t *testing.T, label string, changes map[database.MediaUserFlag]bool) {
	t.Helper()
	_, err := database.ApplyMediaUserFlags(context.Background(), f.env.Database, "PSX", f.path(label),
		f.ids[label], changes)
	require.NoError(t, err)
}

func (f *discSetFixture) stored(t *testing.T, label string) []string {
	t.Helper()
	row, _, err := f.userDB.GetMediaUserData("PSX", f.path(label))
	require.NoError(t, err)
	var got []string
	for _, flag := range database.MediaUserFlags {
		if row.Flag(flag) {
			got = append(got, string(flag))
		}
	}
	return got
}

func (f *discSetFixture) play(t *testing.T, label string, at time.Time) {
	t.Helper()
	_, err := f.userDB.AddMediaHistory(&database.MediaHistoryEntry{
		SystemID: "PSX", SystemName: "PlayStation", MediaPath: f.path(label), MediaName: label, StartTime: at,
	})
	require.NoError(t, err)
}

func (f *discSetFixture) update(t *testing.T, params string) []string {
	t.Helper()
	result, err := HandleMediaTagsUpdate(withParams(&f.env, params))
	require.NoError(t, err)
	resp, ok := result.(models.TagsResponse)
	require.True(t, ok)
	return userTagValues(resp.Tags)
}

func userTagValues(tagList []database.TagInfo) []string {
	var got []string
	for _, tag := range tagList {
		if tag.Type == "user" {
			got = append(got, tag.Tag)
		}
	}
	slices.Sort(got)
	return got
}

type searchedEntry struct {
	label     string
	tags      []string
	multiDisc bool
}

func (f *discSetFixture) search(t *testing.T, params map[string]any) (entries []searchedEntry, next *string) {
	t.Helper()
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	result, err := HandleMediaSearch(withParams(&f.env, string(raw)))
	require.NoError(t, err)
	searched, ok := result.(models.SearchResults)
	require.True(t, ok)
	entries = make([]searchedEntry, 0, len(searched.Results))
	for i := range searched.Results {
		r := searched.Results[i]
		label := f.labels[pathutil.CanonicalMediaPath(r.Path)]
		require.NotEmpty(t, label, "result %s", r.Path)
		assert.Equal(t, f.ids[label], r.MediaID, "a result launches the disc it names")
		entries = append(entries, searchedEntry{label: label, tags: userTagValues(r.Tags), multiDisc: r.MultiDisc})
	}
	if searched.Pagination != nil && searched.Pagination.HasNextPage {
		next = searched.Pagination.NextCursor
	}
	return entries, next
}

func labelsOf(entries []searchedEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.label)
	}
	return out
}

func (f *discSetFixture) browse(t *testing.T, params map[string]any) []models.BrowseEntry {
	t.Helper()
	params["path"] = f.psx
	params["systems"] = []string{"PSX"}
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	result, err := HandleMediaBrowse(withParams(&f.env, string(raw)))
	require.NoError(t, err)
	browsed, ok := result.(models.BrowseResults)
	require.True(t, ok)
	return browsed.Entries
}

func browseEntryNamed(t *testing.T, entries []models.BrowseEntry, path string) models.BrowseEntry {
	t.Helper()
	for i := range entries {
		if filepath.ToSlash(entries[i].Path) == path {
			return entries[i]
		}
	}
	require.Failf(t, "entry not listed", "%s in %+v", path, entries)
	return models.BrowseEntry{}
}

func TestMediaTagsUpdateFavoritesADiscSetFolder(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)
	f.play(t, "saga2", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	// The folder's flag goes to the disc it launches: the one last played.
	got := f.update(t, fmt.Sprintf(`{"system":"PSX","path":%q,"add":["user:favorite"]}`, f.folder("Saga")))
	assert.Equal(t, []string{"favorite"}, got)
	assert.Empty(t, f.stored(t, "saga1"))
	assert.Equal(t, []string{"favorite"}, f.stored(t, "saga2"))
	assert.Empty(t, f.stored(t, "saga3"))

	// Playing another disc moves the launch target, not the flag.
	f.play(t, "saga3", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	entry := browseEntryNamed(t, f.browse(t, map[string]any{}), f.folder("Saga"))
	assert.True(t, entry.MultiDisc)
	assert.Equal(t, f.ids["saga3"], entry.MediaID)
	assert.Contains(t, userTagValues(entry.Tags), "favorite", "the folder shows a flag any disc holds")

	// Adding it again through the folder writes nothing new.
	f.update(t, fmt.Sprintf(`{"system":"PSX","path":%q,"add":["user:favorite"]}`, f.folder("Saga")))
	assert.Empty(t, f.stored(t, "saga3"))

	// Removing it through any disc clears every disc.
	f.flag(t, "saga1", map[database.MediaUserFlag]bool{database.MediaUserFlagFavorite: true})
	got = f.update(t, fmt.Sprintf(`{"mediaId":%d,"remove":["user:favorite"]}`, f.ids["saga3"]))
	assert.Empty(t, got)
	for _, disc := range []string{"saga1", "saga2", "saga3"} {
		assert.Empty(t, f.stored(t, disc), disc)
	}
	entry = browseEntryNamed(t, f.browse(t, map[string]any{}), f.folder("Saga"))
	assert.NotContains(t, userTagValues(entry.Tags), "favorite")
}

func TestMediaTagsUpdateOnADiscSharesTheSetsFlags(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)

	// A disc named by its mediaId takes a flag no disc holds.
	got := f.update(t, fmt.Sprintf(`{"mediaId":%d,"add":["user:favorite"]}`, f.ids["saga2"]))
	assert.Equal(t, []string{"favorite"}, got)
	assert.Equal(t, []string{"favorite"}, f.stored(t, "saga2"))

	// Another disc does not take a copy, and the response shows the set.
	got = f.update(t, fmt.Sprintf(`{"mediaId":%d,"add":["user:favorite","user:playlater"]}`, f.ids["saga3"]))
	assert.Equal(t, []string{"favorite", "playlater"}, got)
	assert.Equal(t, []string{"playlater"}, f.stored(t, "saga3"))

	// Disliking any disc takes the favorite back from all of them.
	got = f.update(t, fmt.Sprintf(`{"mediaId":%d,"add":["user:disliked"]}`, f.ids["saga1"]))
	assert.Equal(t, []string{"disliked", "playlater"}, got)
	assert.Equal(t, []string{"disliked"}, f.stored(t, "saga1"))
	assert.Empty(t, f.stored(t, "saga2"))

	// Liking it takes the dislike back.
	got = f.update(t, fmt.Sprintf(`{"system":"PSX","path":%q,"add":["user:liked"]}`, f.folder("Saga")))
	assert.Equal(t, []string{"liked", "playlater"}, got)
	assert.Equal(t, []string{"liked"}, f.stored(t, "saga1"))

	// Other games are untouched.
	for _, label := range []string{"quest1", "quest2", "solo", "alpha", "beta", "loose"} {
		assert.Empty(t, f.stored(t, label), label)
	}
}

func TestMediaTagsUpdateHidesAndFavoritesADiscSetFolder(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)
	folder := f.folder("Quest")

	got := f.update(t, fmt.Sprintf(`{"system":"PSX","path":%q,"add":["user:hidden","user:favorite"]}`, folder))
	assert.Equal(t, []string{"favorite", "hidden"}, got)
	assert.Equal(t, []string{"favorite"}, f.stored(t, "quest1"), "hidden is the folder's, not a disc's")
	dir, found, err := f.userDB.GetMediaUserData("PSX", folder)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, dir.IsHidden)

	// Hidden alone still goes to the folder.
	got = f.update(t, fmt.Sprintf(`{"system":"PSX","path":%q,"remove":["user:hidden"]}`, folder))
	assert.Empty(t, got)
	_, found, err = f.userDB.GetMediaUserData("PSX", folder)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, []string{"favorite"}, f.stored(t, "quest1"))

	// A hide on one disc stays on that disc.
	f.update(t, fmt.Sprintf(`{"mediaId":%d,"add":["user:hidden"]}`, f.ids["quest2"]))
	assert.Equal(t, []string{"hidden"}, f.stored(t, "quest2"))
	assert.Equal(t, []string{"favorite"}, f.stored(t, "quest1"))
}

func TestMediaTagsUpdateLeavesOtherFilesAlone(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)

	// A folder with one game resolves to that game, as before.
	got := f.update(t, fmt.Sprintf(`{"system":"PSX","path":%q,"add":["user:favorite"]}`, f.folder("Solo")))
	assert.Contains(t, got, "favorite")
	assert.Equal(t, []string{"favorite"}, f.stored(t, "solo"))

	// A plain file is its own game.
	got = f.update(t, fmt.Sprintf(`{"mediaId":%d,"add":["user:liked"]}`, f.ids["alpha"]))
	assert.Equal(t, []string{"liked"}, got)
	assert.Empty(t, f.stored(t, "beta"))
	got = f.update(t, fmt.Sprintf(`{"mediaId":%d,"add":["user:disliked"]}`, f.ids["beta"]))
	assert.Equal(t, []string{"disliked"}, got)
	assert.Equal(t, []string{"liked"}, f.stored(t, "alpha"), "a game in the same folder is not a disc")
}

// seedFavorites flags saga1 and saga3 (as sync can leave them), quest2, solo,
// alpha and loose, and dislikes beta.
func seedFavorites(t *testing.T, f *discSetFixture) {
	t.Helper()
	for _, label := range []string{"saga1", "saga3", "quest2", "solo", "alpha", "loose"} {
		f.flag(t, label, map[database.MediaUserFlag]bool{database.MediaUserFlagFavorite: true})
	}
	f.flag(t, "beta", map[database.MediaUserFlag]bool{database.MediaUserFlagDisliked: true})
	f.flag(t, "quest1", map[database.MediaUserFlag]bool{database.MediaUserFlagLiked: true})
}

func TestMediaSearchFlaggedListsADiscSetOnce(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)
	seedFavorites(t, f)

	for _, sort := range []string{"", "name-asc", "name-desc", "filename-asc", "filename-desc", "added-desc"} {
		t.Run("sort "+sort, func(t *testing.T) {
			t.Parallel()
			base := map[string]any{"tags": []string{"user:favorite"}}
			if sort != "" {
				base["sort"] = sort
			}
			whole, next := f.search(t, base)
			assert.Nil(t, next)
			require.Len(t, whole, 5, "%v", labelsOf(whole))
			assert.ElementsMatch(t, []string{"saga1", "quest1", "solo", "alpha", "loose"}, labelsOf(whole))
			for _, e := range whole {
				switch e.label {
				case "saga1":
					assert.True(t, e.multiDisc)
					assert.Equal(t, []string{"favorite"}, e.tags)
				case "quest1":
					assert.True(t, e.multiDisc, "a set shows its launch disc even when another disc holds the flag")
					assert.Equal(t, []string{"favorite", "liked"}, e.tags)
				default:
					assert.False(t, e.multiDisc, e.label)
					assert.Equal(t, []string{"favorite"}, e.tags, e.label)
				}
			}

			// Paging walks the same list, each set once, with nothing lost
			// where a page ends.
			for _, size := range []int{1, 2} {
				var walked []string
				params := map[string]any{"tags": []string{"user:favorite"}, "maxResults": size}
				if sort != "" {
					params["sort"] = sort
				}
				for range 20 {
					page, cursor := f.search(t, params)
					require.LessOrEqual(t, len(page), size)
					walked = append(walked, labelsOf(page)...)
					if cursor == nil {
						break
					}
					params["cursor"] = *cursor
				}
				assert.Equal(t, labelsOf(whole), walked, "page size %d", size)
			}
		})
	}
}

func TestMediaSearchFlaggedLaunchesTheLastPlayedDisc(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)
	seedFavorites(t, f)
	f.play(t, "saga2", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	base := map[string]any{"tags": []string{"user:favorite"}, "sort": "name-asc"}
	whole, _ := f.search(t, base)
	assert.Equal(t, []string{"alpha", "loose", "quest1", "saga2", "solo"}, labelsOf(whole))

	// The set keeps its place across pages after another disc is played.
	base["maxResults"] = 3
	first, cursor := f.search(t, base)
	require.NotNil(t, cursor)
	assert.Equal(t, []string{"alpha", "loose", "quest1"}, labelsOf(first))
	f.play(t, "saga3", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	base["cursor"] = *cursor
	rest, _ := f.search(t, base)
	assert.Equal(t, []string{"saga3", "solo"}, labelsOf(rest))
}

func TestMediaSearchFlaggedAddedDescUsesTheEarliestDisc(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)
	seedFavorites(t, f)
	at := func(label string, setAt int64) database.MediaUserFlagTime {
		return database.MediaUserFlagTime{SystemID: "PSX", Path: f.path(label), SetAt: setAt}
	}
	f.env.Database = &database.Database{MediaDB: f.env.Database.MediaDB, UserDB: &flagTimesUserDB{
		UserDBI: f.userDB,
		times: map[database.MediaUserFlag][]database.MediaUserFlagTime{
			database.MediaUserFlagFavorite: {
				at("saga1", 500), at("saga3", 100), at("quest2", 300), at("solo", 400), at("alpha", 200),
				at("loose", 0),
			},
		},
	}}

	whole, _ := f.search(t, map[string]any{"tags": []string{"user:favorite"}, "sort": "added-desc"})
	assert.Equal(t, []string{"solo", "quest1", "alpha", "saga1", "loose"}, labelsOf(whole))
}

func TestMediaSearchOtherFiltersListEveryDisc(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)
	seedFavorites(t, f)

	// Either of two flags is not a flag every result holds, so the discs
	// stay separate, as before.
	either, _ := f.search(t, map[string]any{"tags": []string{"~user:favorite", "~user:liked"}})
	assert.ElementsMatch(t,
		[]string{"saga1", "saga3", "quest1", "quest2", "solo", "alpha", "loose"}, labelsOf(either))

	all, _ := f.search(t, map[string]any{"systems": []string{"PSX"}})
	assert.Len(t, all, 9)
	for _, e := range all {
		assert.False(t, e.multiDisc)
	}
}

func TestMediaSearchFlaggedSkipsAHiddenLaunchDisc(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)
	seedFavorites(t, f)
	f.flag(t, "saga1", map[database.MediaUserFlag]bool{database.MediaUserFlagHidden: true})

	// Asking for a flag lists hidden games too, so the set launches its
	// launch disc though it is hidden.
	shown, _ := f.search(t, map[string]any{"tags": []string{"user:favorite"}, "sort": "name-asc"})
	assert.Equal(t, []string{"alpha", "loose", "quest1", "saga1", "solo"}, labelsOf(shown))

	// Leaving hidden games out shows the set through a disc that is not.
	visible := map[string]any{"tags": []string{"user:favorite", "-user:hidden"}, "sort": "name-asc"}
	whole, _ := f.search(t, visible)
	assert.Equal(t, []string{"alpha", "loose", "quest1", "saga3", "solo"}, labelsOf(whole))
	for _, e := range whole {
		if e.label == "saga3" {
			assert.True(t, e.multiDisc)
			assert.Equal(t, []string{"favorite"}, e.tags, "the hidden disc's flag is not shown")
		}
	}

	// A set whose every flagged disc is hidden is not listed.
	f.flag(t, "saga3", map[database.MediaUserFlag]bool{database.MediaUserFlagHidden: true})
	whole, _ = f.search(t, visible)
	assert.Equal(t, []string{"alpha", "loose", "quest1", "solo"}, labelsOf(whole))
}

func TestMediaBrowseFilteredCollapsesDiscSets(t *testing.T) {
	t.Parallel()
	f := newDiscSetFixture(t)
	seedFavorites(t, f)
	f.play(t, "saga2", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	entries := f.browse(t, map[string]any{"tags": []string{"user:favorite"}})
	saga := browseEntryNamed(t, entries, f.folder("Saga"))
	assert.True(t, saga.MultiDisc)
	assert.Equal(t, f.ids["saga2"], saga.MediaID, "launches the disc last played though it is not a favorite")
	assert.Equal(t, []string{"favorite"}, userTagValues(saga.Tags))
	for _, tag := range saga.DisambiguatingTags {
		assert.NotEqual(t, "disc", tag.Type)
	}

	quest := browseEntryNamed(t, entries, f.folder("Quest"))
	assert.True(t, quest.MultiDisc)
	assert.Equal(t, f.ids["quest1"], quest.MediaID)
	assert.Equal(t, []string{"favorite", "liked"}, userTagValues(quest.Tags))

	// A folder of two games stays a folder.
	collection := browseEntryNamed(t, entries, f.folder("Collection"))
	assert.False(t, collection.MultiDisc)
	assert.Zero(t, collection.MediaID)

	// Unfiltered browse shows the same sets with every disc's flags.
	entries = f.browse(t, map[string]any{})
	quest = browseEntryNamed(t, entries, f.folder("Quest"))
	assert.Equal(t, []string{"favorite", "liked"}, userTagValues(quest.Tags))

	// Browse lists every folder whatever the filter, so a set none of
	// whose discs match is still the same one entry.
	entries = f.browse(t, map[string]any{"tags": []string{"user:disliked"}})
	saga = browseEntryNamed(t, entries, f.folder("Saga"))
	assert.True(t, saga.MultiDisc)
	assert.Equal(t, f.ids["saga2"], saga.MediaID)
	assert.Equal(t, []string{"favorite"}, userTagValues(saga.Tags))
}
