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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMediaHiddenMutationAvailableToEveryRole(t *testing.T) {
	t.Parallel()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	path := filepath.Join("roms", "NES", "Game.nes")
	id := addTestMediaPaths(t, mediaDB, path)[0]
	for _, role := range []string{"", "legacy", "member", "admin"} {
		env := requests.RequestEnv{
			Context: context.Background(), ClientRole: role,
			Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
		}
		_, err := HandleMediaTagsUpdate(withParams(&env, fmt.Sprintf(`{"mediaId":%d,"add":["user:hidden"]}`, id)))
		require.NoError(t, err, role)
		result, err := HandleMediaSearch(withParams(&env, `{"includeHidden":true}`))
		require.NoError(t, err, role)
		searched, ok := result.(models.SearchResults)
		require.True(t, ok)
		require.Len(t, searched.Results, 1)
		_, err = HandleMediaTagsUpdate(withParams(&env, fmt.Sprintf(`{"mediaId":%d,"remove":["user:hidden"]}`, id)))
		require.NoError(t, err, role)
	}
}

func TestHiddenVirtualSchemesAndWeightedRandom(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	ids := addTestMediaPaths(t, mediaDB, "steam://1/Hidden", "steam://2/Visible", "mame-arcade://hidden")
	scantest.IndexMediaPaths(t, mediaDB, "SNES", filepath.Join("roms", "SNES", "Hidden.sfc"))
	snes, err := mediaDB.GetMediaBySystemID("SNES")
	require.NoError(t, err)
	require.Len(t, snes, 1)
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))
	_, err = mediaDB.SystemMediaCounts(ctx, nil, false)
	require.NoError(t, err)
	for _, id := range []int64{ids[0], ids[2], snes[0].DBID} {
		require.NoError(t, mediaDB.UpdateMediaTags(ctx, id, nil, []database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	}
	for _, systems := range [][]systemdefs.System{nil, {{ID: "NES"}}} {
		schemes, queryErr := mediaDB.BrowseVirtualSchemes(ctx, database.BrowseVirtualSchemesOptions{
			Systems: systems, ExcludeHidden: true,
		})
		require.NoError(t, queryErr)
		require.Len(t, schemes, 1)
		assert.Equal(t, "steam://", schemes[0].Scheme)
		assert.Equal(t, 1, schemes[0].FileCount)
	}
	files, err := mediaDB.BrowseFiles(ctx, &database.BrowseFilesOptions{
		PathPrefix: "steam://", ExcludeHidden: true, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, ids[1], files[0].MediaID)
	for range 20 {
		game, randomErr := mediaDB.RandomGameWithQuery(ctx, &database.MediaQuery{Systems: []string{"NES", "SNES"}})
		require.NoError(t, randomErr)
		assert.Equal(t, ids[1], game.MediaID, "hidden-only systems must have zero random weight")
	}
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, ids[1], nil, []database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	_, err = mediaDB.RandomGameWithQuery(ctx, &database.MediaQuery{Systems: []string{"NES", "SNES"}})
	require.Error(t, err, "all-hidden libraries have no random candidate")
}

func TestMediaVisibilityDiscoveryAndRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "Alpha.nes"), filepath.Join(root, "Beta.nes"),
		filepath.Join(root, "Gamma.nes"), filepath.Join(root, "Hidden folder", "Only.nes"),
	}
	ids := addTestMediaPaths(t, mediaDB, paths...)
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))
	_, err := mediaDB.SystemMediaCounts(ctx, nil, false)
	require.NoError(t, err)
	_, err = mediaDB.RandomGameWithQuery(ctx, &database.MediaQuery{Systems: []string{"NES"}})
	require.NoError(t, err)

	platform := mocks.NewMockPlatform()
	platform.On("RootDirs", mock.Anything).Return([]string{root})
	platform.On("SupportedReaders", mock.Anything).Return(nil)
	cache := &phelpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{{ID: "NES", SystemID: "NES", Folders: []string{root}}})
	env := requests.RequestEnv{
		Context: ctx, Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
		Platform: platform, Config: &config.Instance{}, LauncherCache: cache,
	}
	// An unpaired request can edit preferences; no privileged role is needed.
	_, err = HandleMediaTagsUpdate(withParams(&env, fmt.Sprintf(
		`{"mediaId":%d,"add":["user:favorite","user:hidden"]}`, ids[0])))
	require.NoError(t, err)
	_, err = HandleMediaTagsUpdate(withParams(&env, fmt.Sprintf(`{"mediaId":%d,"add":["user:hidden"]}`, ids[3])))
	require.NoError(t, err)

	browse := func(params map[string]any) models.BrowseResults {
		t.Helper()
		encoded, marshalErr := json.Marshal(params)
		require.NoError(t, marshalErr)
		result, browseErr := HandleMediaBrowse(withParams(&env, string(encoded)))
		require.NoError(t, browseErr)
		response, ok := result.(models.BrowseResults)
		require.True(t, ok)
		return response
	}
	page := browse(map[string]any{"path": root, "maxResults": 1})
	require.Len(t, page.Entries, 1)
	assert.Equal(t, ids[1], page.Entries[0].MediaID)
	assert.Equal(t, 2, page.TotalFiles)
	assert.Zero(t, page.TotalDirs, "hidden-only directories disappear before pagination")
	require.NotNil(t, page.Pagination)
	require.NotNil(t, page.Pagination.NextCursor)
	cursor := *page.Pagination.NextCursor
	next := browse(map[string]any{"path": root, "maxResults": 1, "cursor": cursor})
	require.Len(t, next.Entries, 1)
	assert.Equal(t, ids[2], next.Entries[0].MediaID)
	assert.Equal(t, 2, next.TotalFiles)

	shown := browse(map[string]any{"path": root, "includeHidden": true})
	assert.Equal(t, 3, shown.TotalFiles)
	assert.Equal(t, 1, shown.TotalDirs)
	var hiddenTags []database.TagInfo
	for _, entry := range shown.Entries {
		if entry.MediaID == ids[0] {
			hiddenTags = entry.Tags
		}
	}
	assert.Contains(t, hiddenTags, database.TagInfo{Type: "user", Tag: "hidden"})
	_, err = HandleMediaBrowse(withParams(&env, fmt.Sprintf(
		`{"path":%q,"cursor":%q,"includeHidden":true}`, root, cursor)))
	require.ErrorContains(t, err, "visibility changed")

	index, err := HandleMediaBrowseIndex(withParams(&env, fmt.Sprintf(`{"path":%q}`, root)))
	require.NoError(t, err)
	indexResult, ok := index.(models.BrowseIndexResults)
	require.True(t, ok)
	assert.Equal(t, 2, indexResult.TotalFiles)
	require.Len(t, indexResult.Groups, 2)
	assert.Equal(t, 0, indexResult.Groups[0].Offset)
	assert.Equal(t, 1, indexResult.Groups[1].Offset)
	indexedPage := browse(map[string]any{"path": root, "cursor": indexResult.Groups[1].Cursor})
	require.NotEmpty(t, indexedPage.Entries)
	assert.Equal(t, ids[2], indexedPage.Entries[0].MediaID)

	roots := browse(map[string]any{})
	require.Len(t, roots.Entries, 1)
	require.NotNil(t, roots.Entries[0].FileCount)
	assert.Equal(t, 2, *roots.Entries[0].FileCount)
	systemRoots := browse(map[string]any{"systems": []string{"NES"}})
	require.NotEmpty(t, systemRoots.Entries)
	for _, entry := range systemRoots.Entries {
		require.NotNil(t, entry.FileCount)
		assert.Equal(t, 2, *entry.FileCount)
	}

	search, err := HandleMediaSearch(withParams(&env, `{"maxResults":1}`))
	require.NoError(t, err)
	searched, ok := search.(models.SearchResults)
	require.True(t, ok)
	require.Len(t, searched.Results, 1)
	assert.Equal(t, ids[1], searched.Results[0].MediaID)
	search, err = HandleMediaSearch(withParams(&env, `{"includeHidden":true}`))
	require.NoError(t, err)
	searched, ok = search.(models.SearchResults)
	require.True(t, ok)
	assert.Len(t, searched.Results, 4)
	favorites := searchByTags(t, &env, []string{"user:favorite"})
	require.Len(t, favorites.Results, 1)
	assert.Equal(t, ids[0], favorites.Results[0].MediaID)
	assert.Contains(t, favorites.Results[0].Tags, database.TagInfo{Type: "user", Tag: "hidden"})

	counts, err := mediaDB.SystemMediaCounts(ctx, nil, true)
	require.NoError(t, err)
	require.Len(t, counts, 1)
	assert.Equal(t, 2, counts[0].Count)
	for range 20 {
		random, randomErr := mediaDB.RandomGameWithQuery(ctx, &database.MediaQuery{Systems: []string{"NES"}})
		require.NoError(t, randomErr)
		assert.Contains(t, []int64{ids[1], ids[2]}, random.MediaID)
	}
	// Shared search remains available to explicit ZapScript launch resolution.
	resolved, err := mediaDB.SearchMediaWithFilters(ctx, &database.SearchFilters{
		Systems: []systemdefs.System{{ID: "NES"}}, Query: "Alpha", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, ids[0], resolved[0].MediaID)

	_, err = userDB.AddMediaHistory(&database.MediaHistoryEntry{
		SystemID: "NES", SystemName: "NES", MediaPath: paths[0], MediaName: "Alpha",
		StartTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	history, err := HandleMediaHistory(withParams(&env, `{}`))
	require.NoError(t, err)
	historyResponse, ok := history.(models.MediaHistoryResponse)
	require.True(t, ok)
	historyEntries := historyResponse.Entries
	require.Len(t, historyEntries, 1)
	assert.Contains(t, historyEntries[0].Tags, database.TagInfo{Type: "user", Tag: "hidden"})

	updates := make(chan models.Notification, 1)
	env.State = &state.State{Notifications: updates}
	_, err = HandleMediaTagsUpdate(withParams(&env, fmt.Sprintf(`{"mediaId":%d,"remove":["user:hidden"]}`, ids[0])))
	require.NoError(t, err)
	_, err = HandleMediaBrowse(withParams(&env, fmt.Sprintf(`{"path":%q,"cursor":%q}`, root, cursor)))
	require.ErrorContains(t, err, "visibility changed")
	page = browse(map[string]any{"path": root})
	assert.Equal(t, 3, page.TotalFiles)
	row, found, err := userDB.GetMediaUserData("NES", paths[0])
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsFavorite)
	assert.False(t, row.IsHidden)
	select {
	case update := <-updates:
		assert.Equal(t, models.NotificationMediaVisibility, update.Method)
	default:
		t.Fatal("unhide did not notify connected clients")
	}

	// A browse can run between the durable write and its MediaDB projection
	// on HTTP. Finishing the projection must invalidate that intermediate cursor.
	require.NoError(t, userDB.SetMediaUserHidden("NES", paths[1], true))
	intermediate := browse(map[string]any{"path": root, "maxResults": 1})
	require.NotNil(t, intermediate.Pagination.NextCursor)
	require.NoError(t, mediaDB.UpdateMediaTags(ctx, ids[1], nil,
		[]database.MediaTagRef{{Type: "user", Tag: "hidden"}}))
	_, err = HandleMediaBrowse(withParams(&env, fmt.Sprintf(`{"path":%q,"cursor":%q}`,
		root, *intermediate.Pagination.NextCursor)))
	require.ErrorContains(t, err, "visibility changed")
}

// A search cursor is only valid for the visibility it was taken under. Both a
// mode change and a preference edit move the result set, so replaying a stale
// cursor would skip or repeat rows rather than continue the list.
// Hiding a folder is one preference against its path. The folder leaves its
// parent's listing and its media leave search, includeHidden brings both back
// with the folder tagged, and the folder still browses by its own path.
func TestHiddenFolderDiscoveryAndRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	root := t.TempDir()
	folder := filepath.Join(root, "_alternatives")
	addTestMediaPaths(t, mediaDB,
		filepath.Join(root, "Alpha.nes"),
		filepath.Join(folder, "Game", "Alt One.nes"),
		filepath.Join(folder, "Game", "Alt Two.nes"),
	)
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))

	platform := mocks.NewMockPlatform()
	platform.On("RootDirs", mock.Anything).Return([]string{root})
	platform.On("SupportedReaders", mock.Anything).Return(nil)
	cache := &phelpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{{ID: "NES", SystemID: "NES", Folders: []string{root}}})
	env := requests.RequestEnv{
		Context: ctx, Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
		Platform: platform, Config: &config.Instance{}, LauncherCache: cache,
	}
	encode := func(params map[string]any) string {
		t.Helper()
		encoded, err := json.Marshal(params)
		require.NoError(t, err)
		return string(encoded)
	}
	update := func(verb string) []database.TagInfo {
		t.Helper()
		result, err := HandleMediaTagsUpdate(withParams(&env, encode(map[string]any{
			"system": "NES", "path": folder, verb: []string{"user:hidden"},
		})))
		require.NoError(t, err)
		response, ok := result.(models.TagsResponse)
		require.True(t, ok)
		return response.Tags
	}
	browse := func(params map[string]any) models.BrowseResults {
		t.Helper()
		params["systems"] = []string{"NES"}
		result, err := HandleMediaBrowse(withParams(&env, encode(params)))
		require.NoError(t, err)
		response, ok := result.(models.BrowseResults)
		require.True(t, ok)
		return response
	}
	searchCount := func(params map[string]any) int {
		t.Helper()
		params["systems"] = []string{"NES"}
		params["query"] = ""
		result, err := HandleMediaSearch(withParams(&env, encode(params)))
		require.NoError(t, err)
		response, ok := result.(models.SearchResults)
		require.True(t, ok)
		return len(response.Results)
	}
	folderEntry := func(page models.BrowseResults) *models.BrowseEntry {
		for i := range page.Entries {
			if page.Entries[i].Type == "directory" {
				return &page.Entries[i]
			}
		}
		return nil
	}

	hidden := database.TagInfo{Type: "user", Tag: "hidden"}
	assert.Equal(t, []database.TagInfo{hidden}, update("add"))
	stored, err := userDB.ListHiddenDirectories()
	require.NoError(t, err)
	assert.Equal(t, []database.HiddenDirectory{
		{SystemID: "NES", Path: pathutil.CanonicalMediaPath(folder)},
	}, stored, "one row for the folder, none for its files")
	rows, err := userDB.ListMediaUserData()
	require.NoError(t, err)
	assert.Empty(t, rows)

	listed := browse(map[string]any{"path": root})
	assert.Nil(t, folderEntry(listed))
	assert.Zero(t, listed.TotalDirs)
	assert.Equal(t, 1, searchCount(map[string]any{}))

	shown := browse(map[string]any{"path": root, "includeHidden": true})
	entry := folderEntry(shown)
	require.NotNil(t, entry)
	assert.Equal(t, []database.TagInfo{hidden}, entry.Tags)
	assert.Equal(t, 1, shown.TotalDirs)
	assert.Equal(t, 3, searchCount(map[string]any{"includeHidden": true}))

	// Addressed directly, the folder lists and searches as any other.
	inside := browse(map[string]any{"path": folder})
	require.NotNil(t, folderEntry(inside))
	assert.Empty(t, folderEntry(inside).Tags, "what is inside carries no tag of its own")
	assert.Equal(t, 2, searchCount(map[string]any{"pathPrefix": folder}))

	assert.Empty(t, update("remove"))
	assert.NotNil(t, folderEntry(browse(map[string]any{"path": root})))
	assert.Equal(t, 3, searchCount(map[string]any{}))
}

// Any other tag on a folder still means its single launch target, and a path
// that holds no media is still not found.
func TestHiddenFolderLeavesOtherRequestsAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	root := t.TempDir()
	folder := filepath.Join(root, "Game")
	ids := addTestMediaPaths(t, mediaDB, filepath.Join(folder, "Game.nes"))
	env := requests.RequestEnv{
		Context: ctx, Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
		Platform: mocks.NewMockPlatform(), Config: &config.Instance{},
	}
	request := func(path, tag string) (any, error) {
		encoded, err := json.Marshal(map[string]any{"system": "NES", "path": path, "add": []string{tag}})
		require.NoError(t, err)
		return HandleMediaTagsUpdate(withParams(&env, string(encoded)))
	}

	_, err := request(folder, "user:favorite")
	require.NoError(t, err)
	row, found, err := userDB.GetMediaUserData("NES", filepath.Join(folder, "Game.nes"))
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, row.IsFavorite, "the favorite lands on the folder's launch target")
	dirs, err := userDB.ListHiddenDirectories()
	require.NoError(t, err)
	assert.Empty(t, dirs)

	_, err = request(filepath.Join(root, "Missing"), "user:hidden")
	require.ErrorContains(t, err, "media not found")

	// A file is hidden as a file, never as a folder.
	_, err = HandleMediaTagsUpdate(withParams(&env, fmt.Sprintf(`{"mediaId":%d,"add":["user:hidden"]}`, ids[0])))
	require.NoError(t, err)
	dirs, err = userDB.ListHiddenDirectories()
	require.NoError(t, err)
	assert.Empty(t, dirs)
}

func TestSearchCursorRejectedAfterVisibilityChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	root := t.TempDir()
	ids := addTestMediaPaths(t, mediaDB,
		filepath.Join(root, "Alpha.nes"),
		filepath.Join(root, "Beta.nes"),
		filepath.Join(root, "Gamma.nes"),
	)
	env := requests.RequestEnv{
		Context:  ctx,
		Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
	}

	search := func(params string) models.SearchResults {
		t.Helper()
		result, err := HandleMediaSearch(withParams(&env, params))
		require.NoError(t, err)
		response, ok := result.(models.SearchResults)
		require.True(t, ok)
		return response
	}

	first := search(`{"maxResults":1}`)
	require.Len(t, first.Results, 1)
	require.NotNil(t, first.Pagination)
	require.NotNil(t, first.Pagination.NextCursor)
	cursor := *first.Pagination.NextCursor

	// The same mode continues normally.
	next := search(fmt.Sprintf(`{"maxResults":1,"cursor":%q}`, cursor))
	require.Len(t, next.Results, 1)
	assert.NotEqual(t, first.Results[0].MediaID, next.Results[0].MediaID)

	// Switching mode mid-list must not silently reshape the page.
	_, err := HandleMediaSearch(withParams(&env,
		fmt.Sprintf(`{"maxResults":1,"cursor":%q,"includeHidden":true}`, cursor)))
	require.ErrorContains(t, err, "visibility changed")

	_, err = HandleMediaTagsUpdate(withParams(&env,
		fmt.Sprintf(`{"mediaId":%d,"add":["user:hidden"]}`, ids[0])))
	require.NoError(t, err)
	_, err = HandleMediaSearch(withParams(&env, fmt.Sprintf(`{"maxResults":1,"cursor":%q}`, cursor)))
	require.ErrorContains(t, err, "visibility changed")

	// A fresh search still pages, and the hidden row is gone from it.
	restarted := search(`{"maxResults":10}`)
	assert.Len(t, restarted.Results, 2)
	for _, result := range restarted.Results {
		assert.NotEqual(t, ids[0], result.MediaID)
	}
}

// A directory that collapses to one launch target is promoted to that media
// entry. When the target itself is hidden the directory stays a plain
// directory, so hiding a game cannot promote it back into discovery.
func TestBuildBrowseResponse_HiddenSingletonStaysDirectory(t *testing.T) {
	t.Parallel()

	nesSystem := database.System{DBID: 1, SystemID: "NES"}
	systems := []systemdefs.System{{ID: "NES"}}
	path := filepath.ToSlash(filepath.Join("roms", "NES"))
	dirName := "Game.zip"
	dirPath := filepath.ToSlash(filepath.Join(path, dirName))
	alias := []database.SingletonContainerAlias{{
		ChildDir: dirPath + "/",
		Row: database.MediaFullRow{
			Media: database.Media{
				DBID:      20,
				Path:      filepath.ToSlash(filepath.Join(dirPath, "Game.nes")),
				ParentDir: dirPath + "/",
			},
			Title:  database.MediaTitle{DBID: 30, Name: "Game"},
			System: nesSystem,
		},
		Tags:          []database.TagInfo{{Type: "user", Tag: "hidden"}},
		ZapScriptTags: []database.TagInfo{},
	}}

	mockMediaDB := testhelpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Maybe()
	// A rejected alias never reaches ZapScript construction, so the root lookup
	// it would need is optional here.
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{"roms"}).Maybe()
	mockMediaDB.On("FindSystemBySystemID", "NES").Return(nesSystem, nil).Once()
	mockMediaDB.On("ResolveSingletonContainerAliases", mock.Anything, nesSystem.DBID,
		[]database.SingletonAliasCandidate{{ChildDir: dirPath + "/", FileCount: 1}}).
		Return(alias, nil).Once()

	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice([]platforms.Launcher{{
		ID: "NES", SystemID: "NES", Folders: []string{"NES"},
	}})
	env := &requests.RequestEnv{
		Context:       context.Background(),
		Database:      &database.Database{MediaDB: mockMediaDB},
		Platform:      mockPlatform,
		Config:        &config.Instance{},
		LauncherCache: launcherCache,
		ExcludeHidden: true,
	}
	result, err := buildBrowseResponse(env, path,
		[]database.BrowseDirectoryResult{{Name: dirName, FileCount: 1, SystemIDs: []string{"NES"}}},
		nil, defaultMaxResults, 0, 0, nil, false, systems)
	require.NoError(t, err)
	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 1)
	entry := browseResults.Entries[0]
	assert.Equal(t, "directory", entry.Type)
	assert.Zero(t, entry.MediaID, "a hidden launch target must not be promoted")
	assert.Nil(t, entry.ZapScript)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

// browseVisibilityInjector wraps a real MediaDBI and, on selected calls, runs
// a real preference edit before delegating. That reproduces a visibility
// change landing while one browse or browse.index request is already
// running, rather than only between two separate handler calls.
type browseVisibilityInjector struct {
	database.MediaDBI
	onFiles    map[int]func()
	onIndex    map[int]func()
	filesCalls int
	indexCalls int
}

func (w *browseVisibilityInjector) BrowseFiles(
	ctx context.Context, opts *database.BrowseFilesOptions,
) ([]database.SearchResultWithCursor, error) {
	w.filesCalls++
	if change, ok := w.onFiles[w.filesCalls]; ok {
		change()
	}
	results, err := w.MediaDBI.BrowseFiles(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("wrapped browse files: %w", err)
	}
	return results, nil
}

//nolint:gocritic // Value options match the MediaDBI method being wrapped.
func (w *browseVisibilityInjector) BrowseIndex(
	ctx context.Context, opts database.BrowseIndexOptions,
) (database.BrowseIndexResult, error) {
	w.indexCalls++
	if change, ok := w.onIndex[w.indexCalls]; ok {
		change()
	}
	result, err := w.MediaDBI.BrowseIndex(ctx, opts)
	if err != nil {
		return database.BrowseIndexResult{}, fmt.Errorf("wrapped browse index: %w", err)
	}
	return result, nil
}

// visibilityInjectorFixture is a small filesystem library, browsable at
// root, whose MediaDB is wrapped so a test can inject a real hide/unhide
// partway through a request.
type visibilityInjectorFixture struct {
	env      *requests.RequestEnv
	injector *browseVisibilityInjector
	root     string
	ids      []int64
	paths    []string
}

func newVisibilityInjectorFixture(t *testing.T) *visibilityInjectorFixture {
	t.Helper()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "Alpha.nes"), filepath.Join(root, "Beta.nes"), filepath.Join(root, "Gamma.nes"),
	}
	ids := addTestMediaPaths(t, mediaDB, paths...)
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))

	platform := mocks.NewMockPlatform()
	platform.On("RootDirs", mock.Anything).Return([]string{root})
	platform.On("SupportedReaders", mock.Anything).Return(nil)
	cache := &phelpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{{ID: "NES", SystemID: "NES", Folders: []string{root}}})

	injector := &browseVisibilityInjector{
		MediaDBI: mediaDB, onFiles: map[int]func(){}, onIndex: map[int]func(){},
	}
	env := &requests.RequestEnv{
		Context: ctx, Database: &database.Database{MediaDB: injector, UserDB: userDB},
		Platform: platform, Config: &config.Instance{}, LauncherCache: cache,
	}
	return &visibilityInjectorFixture{env: env, injector: injector, root: root, ids: ids, paths: paths}
}

// toggleHidden returns a closure that sets one entry's hidden flag through
// both UserDB and MediaDB, exactly as ApplyMediaUserFlags does, so injecting
// it mid-browse is a real listing-affecting change, not a synthetic one.
func (f *visibilityInjectorFixture) toggleHidden(ctx context.Context, t *testing.T, index int, hidden bool) func() {
	t.Helper()
	return func() {
		userDB, ok := f.env.Database.UserDB.(*userdb.UserDB)
		require.True(t, ok)
		require.NoError(t, userDB.SetMediaUserHidden("NES", f.paths[index], hidden))
		mediaDB, ok := f.injector.MediaDBI.(*mediadb.MediaDB)
		require.True(t, ok)
		ref := database.MediaTagRef{Type: "user", Tag: "hidden"}
		if hidden {
			require.NoError(t, mediaDB.UpdateMediaTags(ctx, f.ids[index], nil, []database.MediaTagRef{ref}))
		} else {
			require.NoError(t, mediaDB.UpdateMediaTags(ctx, f.ids[index], []database.MediaTagRef{ref}, nil))
		}
	}
}

// A revision change landing entirely within one cursorless browse must not
// be handed back to the client as an error: nothing the client did is stale,
// there is no cursor to restart, and Core already knows how to run the
// browse again. This is the regression test for #1564.
func TestFreshBrowseRerunsOnceAfterMidRunVisibilityChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newVisibilityInjectorFixture(t)
	f.injector.onFiles[1] = f.toggleHidden(ctx, t, 0, true)

	result, err := HandleMediaBrowse(withParams(f.env, fmt.Sprintf(`{"path":%q}`, f.root)))
	require.NoError(t, err)
	page, ok := result.(models.BrowseResults)
	require.True(t, ok)
	assert.Equal(t, 2, page.TotalFiles, "the rerun must reflect the entry hidden mid-request")
	for _, entry := range page.Entries {
		assert.NotEqual(t, f.ids[0], entry.MediaID)
	}
	assert.Equal(t, 2, f.injector.filesCalls, "exactly one rerun: the injected call plus the clean rerun")

	// The rerun's own cursor must itself be usable, not carry a
	// pre-invalidated revision forward.
	if page.Pagination != nil && page.Pagination.NextCursor != nil {
		_, err = HandleMediaBrowse(withParams(f.env, fmt.Sprintf(
			`{"path":%q,"cursor":%q}`, f.root, *page.Pagination.NextCursor)))
		require.NoError(t, err)
	}
}

// If the revision keeps moving even across the one rerun, Core gives up and
// reports it rather than looping or serving a page that may mix visibility.
func TestFreshBrowseFailsWhenVisibilityKeepsChanging(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newVisibilityInjectorFixture(t)
	f.injector.onFiles[1] = f.toggleHidden(ctx, t, 0, true)
	f.injector.onFiles[2] = f.toggleHidden(ctx, t, 1, true)

	_, err := HandleMediaBrowse(withParams(f.env, fmt.Sprintf(`{"path":%q}`, f.root)))
	require.ErrorContains(t, err, "library visibility changed")
	require.ErrorContains(t, err, "during browse")
	assert.Equal(t, 2, f.injector.filesCalls, "no more than the one rerun")
}

// A cursor request is a continuation of a specific earlier page. A change
// mid-request still means "restart without a cursor" for it, with no rerun.
func TestCursorBrowseRejectsWithoutRerunOnVisibilityChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newVisibilityInjectorFixture(t)

	first, err := HandleMediaBrowse(withParams(f.env, fmt.Sprintf(`{"path":%q,"maxResults":1}`, f.root)))
	require.NoError(t, err)
	page, ok := first.(models.BrowseResults)
	require.True(t, ok)
	require.NotNil(t, page.Pagination)
	require.NotNil(t, page.Pagination.NextCursor)
	cursor := *page.Pagination.NextCursor

	// The next BrowseFiles call belongs to the cursor request below.
	f.injector.onFiles[f.injector.filesCalls+1] = f.toggleHidden(ctx, t, 0, true)
	callsBeforeCursorRequest := f.injector.filesCalls

	_, err = HandleMediaBrowse(withParams(f.env, fmt.Sprintf(`{"path":%q,"cursor":%q}`, f.root, cursor)))
	require.ErrorContains(t, err, "library visibility changed; restart browse without cursor")
	assert.Equal(t, callsBeforeCursorRequest+1, f.injector.filesCalls, "a cursor request never reruns")
}

// media.browse.index never takes a cursor, so it always gets the rerun
// treatment rather than a client-facing error.
func TestBrowseIndexRerunsOnceAfterMidRunVisibilityChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newVisibilityInjectorFixture(t)
	f.injector.onIndex[1] = f.toggleHidden(ctx, t, 0, true)

	result, err := HandleMediaBrowseIndex(withParams(f.env, fmt.Sprintf(`{"path":%q}`, f.root)))
	require.NoError(t, err)
	index, ok := result.(models.BrowseIndexResults)
	require.True(t, ok)
	assert.Equal(t, 2, index.TotalFiles, "the rerun must reflect the entry hidden mid-request")
	assert.Equal(t, 2, f.injector.indexCalls)

	for _, group := range index.Groups {
		if group.Cursor == "" {
			continue
		}
		page, pageErr := HandleMediaBrowse(withParams(f.env, fmt.Sprintf(
			`{"path":%q,"cursor":%q}`, f.root, group.Cursor)))
		require.NoError(t, pageErr)
		_, ok = page.(models.BrowseResults)
		require.True(t, ok)
	}
}

// A write that only advances UserDB's own media_preferences_revision
// DeviceState row - and never touches MediaDB's projection - must not
// invalidate an open browse cursor. Cursor validity depends solely on
// MediaDB's own revision (#1564): UserDB's counter still advances (see
// deckTx and mediaUserDataTx) for its own reasons, but a browse never reads
// it. This reproduces the reported failure directly: unhiding an
// already-visible entry, a deck edit that changes nothing, and a synced
// deck write-back that matches what is already stored are three real writes
// that bump only UserDB's counter without changing anything a browse or
// search would serve.
func TestUserDBOnlyRevisionDoesNotInvalidateBrowse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	root := t.TempDir()
	paths := []string{filepath.Join(root, "Alpha.nes"), filepath.Join(root, "Beta.nes")}
	addTestMediaPaths(t, mediaDB, paths...)
	require.NoError(t, mediaDB.PopulateBrowseCache(ctx))

	platform := mocks.NewMockPlatform()
	platform.On("RootDirs", mock.Anything).Return([]string{root})
	platform.On("SupportedReaders", mock.Anything).Return(nil)
	cache := &phelpers.LauncherCache{}
	cache.InitializeFromSlice([]platforms.Launcher{{ID: "NES", SystemID: "NES", Folders: []string{root}}})
	env := requests.RequestEnv{
		Context: ctx, Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
		Platform: platform, Config: &config.Instance{}, LauncherCache: cache,
	}

	page, err := HandleMediaBrowse(withParams(&env, fmt.Sprintf(`{"path":%q,"maxResults":1}`, root)))
	require.NoError(t, err)
	first, ok := page.(models.BrowseResults)
	require.True(t, ok)
	require.NotNil(t, first.Pagination)
	require.NotNil(t, first.Pagination.NextCursor)
	cursor := *first.Pagination.NextCursor

	before, _, err := userDB.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	// The exact write does not matter here: any UserDB-only bump reproduces
	// the bug, since the point under test is that a browse never reads it.
	require.NoError(t, userDB.SetDeviceState(database.DeviceStateKeyMediaPreferencesRevision, "not-a-real-revision"))
	after, _, err := userDB.GetDeviceState(database.DeviceStateKeyMediaPreferencesRevision)
	require.NoError(t, err)
	require.NotEqual(t, before, after, "the write under test must actually move UserDB's counter")

	next, err := HandleMediaBrowse(withParams(&env, fmt.Sprintf(`{"path":%q,"cursor":%q}`, root, cursor)))
	require.NoError(t, err, "a UserDB-only revision change must not invalidate an open browse cursor")
	_, ok = next.(models.BrowseResults)
	require.True(t, ok)
}
