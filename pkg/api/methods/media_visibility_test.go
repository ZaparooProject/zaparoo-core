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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
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
	_, err = mediaDB.SystemMediaCounts(ctx, nil)
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
	_, err := mediaDB.SystemMediaCounts(ctx, nil)
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
