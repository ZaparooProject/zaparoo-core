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
	"errors"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newBrowseEnv(
	t *testing.T,
	mockMediaDB *helpers.MockMediaDBI,
	mockPlatform *mocks.MockPlatform,
	params any,
) requests.RequestEnv {
	t.Helper()

	var paramsJSON json.RawMessage
	if params != nil {
		var err error
		paramsJSON, err = json.Marshal(params)
		require.NoError(t, err)
	}

	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice(mockPlatform.Launchers(&config.Instance{}))

	return requests.RequestEnv{
		Context: context.Background(),
		Params:  paramsJSON,
		Database: &database.Database{
			MediaDB: mockMediaDB,
		},
		Platform:      mockPlatform,
		Config:        &config.Instance{},
		LauncherCache: launcherCache,
	}
}

func browseDirectoriesOpts(pathPrefix string) any {
	return mock.MatchedBy(func(opts database.BrowseDirectoriesOptions) bool {
		return opts.PathPrefix == pathPrefix && len(opts.Systems) == 0
	})
}

func browseFileCountOpts(pathPrefix string, letter *string) any {
	return mock.MatchedBy(func(opts database.BrowseFileCountOptions) bool {
		if opts.PathPrefix != pathPrefix || len(opts.Systems) != 0 {
			return false
		}
		if letter == nil {
			return opts.Letter == nil
		}
		return opts.Letter != nil && *opts.Letter == *letter
	})
}

func browseDirectoriesSystemOpts(pathPrefix, systemID string) any {
	return mock.MatchedBy(func(opts database.BrowseDirectoriesOptions) bool {
		return opts.PathPrefix == pathPrefix && len(opts.Systems) == 1 && opts.Systems[0].ID == systemID
	})
}

func browseFilesSystemOpts(pathPrefix, systemID string) any {
	return mock.MatchedBy(func(opts *database.BrowseFilesOptions) bool {
		return opts.PathPrefix == pathPrefix && len(opts.Systems) == 1 && opts.Systems[0].ID == systemID
	})
}

func browseFileCountSystemOpts(pathPrefix, systemID string) any {
	return mock.MatchedBy(func(opts database.BrowseFileCountOptions) bool {
		return opts.PathPrefix == pathPrefix && len(opts.Systems) == 1 && opts.Systems[0].ID == systemID
	})
}

func browseTestAbsPath(parts ...string) string {
	return filepath.Join(append([]string{string(filepath.Separator)}, parts...)...)
}

func TestHandleMediaBrowse_RootLevel(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{
			{ID: "Steam", SystemID: "pc", Schemes: []string{"steam"}},
		})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseRootCounts", mock.Anything, mock.Anything).
		Return(map[string]*int{"/roms": intPtr(500)}, nil)
	mockMediaDB.On("BrowseVirtualSchemes", mock.Anything, database.BrowseVirtualSchemesOptions{}).
		Return([]database.BrowseVirtualScheme{
			{Scheme: "steam://", FileCount: 42},
		}, nil)

	env := newBrowseEnv(t, mockMediaDB, mockPlatform, nil)
	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 2)

	// Filesystem root
	assert.Equal(t, "root", browseResults.Entries[0].Type)
	assert.Equal(t, "roms", browseResults.Entries[0].Name)
	assert.Equal(t, "/roms", browseResults.Entries[0].Path)
	require.NotNil(t, browseResults.Entries[0].FileCount)
	assert.Equal(t, 500, *browseResults.Entries[0].FileCount)

	// Virtual root with group
	assert.Equal(t, "root", browseResults.Entries[1].Type)
	assert.Equal(t, "Steam", browseResults.Entries[1].Name)
	assert.Equal(t, "steam://", browseResults.Entries[1].Path)
	require.NotNil(t, browseResults.Entries[1].Group)
	assert.Equal(t, "Steam", *browseResults.Entries[1].Group)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_SystemRootRoutes(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	snesPath := filepath.Join(romsRoot, "SNES")
	sharedPath := filepath.Join(romsRoot, "shared")
	snesAPIPath := filepath.ToSlash(snesPath)
	sharedAPIPath := filepath.ToSlash(sharedPath)
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{
			{ID: "SNES", SystemID: "SNES", Folders: []string{"SNES"}},
			{ID: "SharedSNES", SystemID: "SNES", Folders: []string{"shared"}},
			{ID: "OutsideSNES", SystemID: "SNES", Folders: []string{browseTestAbsPath("tmp", "outside")}},
			{ID: "Steam", SystemID: "pc", Schemes: []string{"steam"}},
		})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseRouteCounts", mock.Anything,
		mock.MatchedBy(func(opts database.BrowseRouteCountsOptions) bool {
			return len(opts.Systems) == 1 && opts.Systems[0].ID == "SNES" &&
				assert.ElementsMatch(t, []string{snesAPIPath, sharedAPIPath}, opts.Routes)
		}),
	).Return(map[string]database.BrowseRouteCount{
		snesAPIPath: {Path: snesAPIPath, FileCount: 12, SystemIDs: []string{"SNES"}},
	}, nil)

	systems := []string{"SNES"}
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{Systems: &systems})
	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 1)
	entry := browseResults.Entries[0]
	assert.Equal(t, "root", entry.Type)
	assert.Equal(t, "SNES", entry.Name)
	assert.Equal(t, snesAPIPath, entry.Path)
	assert.Equal(t, []string{"SNES"}, entry.SystemIDs)
	require.NotNil(t, entry.SystemID)
	assert.Equal(t, "SNES", *entry.SystemID)
	require.NotNil(t, entry.FileCount)
	assert.Equal(t, 12, *entry.FileCount)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_FilesystemDirectory(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/")).
		Return([]database.BrowseDirectoryResult{
			{Name: "NES", FileCount: 100},
			{Name: "SNES", FileCount: 200},
		}, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{}, nil)
	// BrowseFileCount is skipped when BrowseFiles returns empty and no cursor
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts("/roms/", nil)).
		Return(0, nil).Maybe()

	path := "/roms"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path: &path,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	assert.Equal(t, "/roms", browseResults.Path)
	require.Len(t, browseResults.Entries, 2)

	assert.Equal(t, "directory", browseResults.Entries[0].Type)
	assert.Equal(t, "NES", browseResults.Entries[0].Name)
	assert.Equal(t, "/roms/NES", browseResults.Entries[0].Path)
	require.NotNil(t, browseResults.Entries[0].FileCount)
	assert.Equal(t, 100, *browseResults.Entries[0].FileCount)

	assert.Equal(t, "SNES", browseResults.Entries[1].Name)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_FilesystemWithFiles(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/SNES/")).
		Return([]database.BrowseDirectoryResult{}, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{
			{
				SystemID: "snes", Name: "Super Mario World",
				Path: "/roms/SNES/Super Mario World.sfc", MediaID: 1,
			},
			{
				SystemID: "snes", Name: "Zelda",
				Path: "/roms/SNES/Zelda.sfc", MediaID: 2,
			},
		}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts("/roms/SNES/", nil)).
		Return(2, nil)

	path := "/roms/SNES"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path: &path,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 2)
	assert.Equal(t, 2, browseResults.TotalFiles)
	assert.Equal(t, "media", browseResults.Entries[0].Type)
	assert.Equal(t, "Super Mario World", browseResults.Entries[0].Name)
	require.NotNil(t, browseResults.Entries[0].SystemID)
	assert.Equal(t, "snes", *browseResults.Entries[0].SystemID)
	require.NotNil(t, browseResults.Entries[0].ZapScript)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_FilesystemFiltersBySystem(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	sharedPath := filepath.Join(romsRoot, "shared")
	sharedPrefix := filepath.ToSlash(sharedPath) + "/"
	chronoPath := filepath.ToSlash(filepath.Join(sharedPath, "Chrono Trigger.sfc"))
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesSystemOpts(sharedPrefix, "SNES")).
		Return([]database.BrowseDirectoryResult{
			{Name: "RPG", FileCount: 3, SystemIDs: []string{"SNES"}},
		}, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, browseFilesSystemOpts(sharedPrefix, "SNES")).
		Return([]database.SearchResultWithCursor{
			{
				SystemID: "snes", Name: "Chrono Trigger",
				Path: chronoPath, MediaID: 7,
			},
		}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountSystemOpts(sharedPrefix, "SNES")).
		Return(1, nil)

	path := filepath.ToSlash(sharedPath)
	systems := []string{"SNES"}
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:    &path,
		Systems: &systems,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	assert.Equal(t, 1, browseResults.TotalFiles)
	require.Len(t, browseResults.Entries, 2)
	assert.Equal(t, "directory", browseResults.Entries[0].Type)
	assert.Equal(t, []string{"SNES"}, browseResults.Entries[0].SystemIDs)
	assert.Equal(t, "media", browseResults.Entries[1].Type)
	require.NotNil(t, browseResults.Entries[1].SystemID)
	assert.Equal(t, "snes", *browseResults.Entries[1].SystemID)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_Pagination(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	// Return 3 results when limit is maxResults+1=3 (maxResults=2),
	// triggering hasNextPage=true.
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/SNES/")).
		Return([]database.BrowseDirectoryResult{}, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{
			{SystemID: "snes", Name: "Alpha", Path: "/roms/SNES/Alpha.sfc", MediaID: 1},
			{SystemID: "snes", Name: "Beta", Path: "/roms/SNES/Beta.sfc", MediaID: 2},
			{SystemID: "snes", Name: "Gamma", Path: "/roms/SNES/Gamma.sfc", MediaID: 3},
		}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts("/roms/SNES/", nil)).
		Return(5, nil)

	path := "/roms/SNES"
	maxResults := 2
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:       &path,
		MaxResults: &maxResults,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)

	// Only maxResults entries returned (3rd truncated)
	require.Len(t, browseResults.Entries, 2)
	assert.Equal(t, "Alpha", browseResults.Entries[0].Name)
	assert.Equal(t, "Beta", browseResults.Entries[1].Name)
	assert.Equal(t, 5, browseResults.TotalFiles)

	// Pagination present with next cursor
	require.NotNil(t, browseResults.Pagination)
	assert.True(t, browseResults.Pagination.HasNextPage)
	assert.Equal(t, 2, browseResults.Pagination.PageSize)
	require.NotNil(t, browseResults.Pagination.NextCursor)

	// Decode cursor and verify it encodes last result's sort value + DBID
	cursor, decErr := decodeBrowseCursor(*browseResults.Pagination.NextCursor)
	require.NoError(t, decErr)
	require.NotNil(t, cursor)
	assert.Equal(t, int64(2), cursor.LastID)
	assert.Equal(t, "Beta", cursor.SortValue)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_CursorRoundTrip(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	// Encode a cursor as if page 1 ended at MediaID=5, SortValue="Mario"
	cursorStr, err := encodeBrowseCursor(5, "Mario")
	require.NoError(t, err)

	mockMediaDB := helpers.NewMockMediaDBI()
	// BrowseDirectories is skipped when a cursor is present (page 2+).
	// Verify the decoded cursor is passed through to BrowseFiles.
	mockMediaDB.On("BrowseFiles", mock.Anything,
		mock.MatchedBy(func(opts *database.BrowseFilesOptions) bool {
			return opts.Cursor != nil &&
				opts.Cursor.LastID == 5 &&
				opts.Cursor.SortValue == "Mario" &&
				opts.PathPrefix == "/roms/SNES/"
		}),
	).Return([]database.SearchResultWithCursor{
		{SystemID: "snes", Name: "Zelda", Path: "/roms/SNES/Zelda.sfc", MediaID: 8},
	}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts("/roms/SNES/", nil)).
		Return(10, nil)

	path := "/roms/SNES"
	maxResults := 5
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:       &path,
		MaxResults: &maxResults,
		Cursor:     &cursorStr,
	})

	result, browseErr := HandleMediaBrowse(env)
	require.NoError(t, browseErr)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)

	require.Len(t, browseResults.Entries, 1)
	assert.Equal(t, "Zelda", browseResults.Entries[0].Name)
	// Only 1 result returned (< maxResults), so no next page
	require.NotNil(t, browseResults.Pagination)
	assert.False(t, browseResults.Pagination.HasNextPage)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_FilenameSortCursor(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/SNES/")).
		Return([]database.BrowseDirectoryResult{}, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{
			{SystemID: "snes", Name: "Alpha", Path: "/roms/SNES/alpha.sfc", MediaID: 1},
			{SystemID: "snes", Name: "Beta", Path: "/roms/SNES/beta.sfc", MediaID: 2},
			{SystemID: "snes", Name: "Gamma", Path: "/roms/SNES/gamma.sfc", MediaID: 3},
		}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts("/roms/SNES/", nil)).
		Return(5, nil)

	path := "/roms/SNES"
	maxResults := 2
	sort := "filename-asc"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:       &path,
		MaxResults: &maxResults,
		Sort:       &sort,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)

	require.Len(t, browseResults.Entries, 2)
	require.NotNil(t, browseResults.Pagination)
	require.NotNil(t, browseResults.Pagination.NextCursor)

	// Filename sort cursor should encode Path, not Name
	cursor, decErr := decodeBrowseCursor(*browseResults.Pagination.NextCursor)
	require.NoError(t, decErr)
	require.NotNil(t, cursor)
	assert.Equal(t, int64(2), cursor.LastID)
	assert.Equal(t, "/roms/SNES/beta.sfc", cursor.SortValue)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_NameDescSort(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/SNES/")).
		Return([]database.BrowseDirectoryResult{}, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{
			{SystemID: "snes", Name: "Zelda", Path: "/roms/SNES/Zelda.sfc", MediaID: 3},
			{SystemID: "snes", Name: "Mario", Path: "/roms/SNES/Mario.sfc", MediaID: 2},
			{SystemID: "snes", Name: "Alpha", Path: "/roms/SNES/Alpha.sfc", MediaID: 1},
		}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts("/roms/SNES/", nil)).
		Return(5, nil)

	path := "/roms/SNES"
	maxResults := 2
	sort := "name-desc"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:       &path,
		MaxResults: &maxResults,
		Sort:       &sort,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)

	require.Len(t, browseResults.Entries, 2)
	require.NotNil(t, browseResults.Pagination)
	require.NotNil(t, browseResults.Pagination.NextCursor)

	// Name-desc sort cursor should encode Name (not Path)
	cursor, decErr := decodeBrowseCursor(*browseResults.Pagination.NextCursor)
	require.NoError(t, decErr)
	require.NotNil(t, cursor)
	assert.Equal(t, int64(2), cursor.LastID)
	assert.Equal(t, "Mario", cursor.SortValue)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_VirtualScheme(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{
			{ID: "Steam", SystemID: "pc", Schemes: []string{"steam"}},
		})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{
			{
				SystemID: "pc", Name: "Team Fortress 2",
				Path: "steam://440/Team%20Fortress%202", MediaID: 10,
			},
		}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts("steam://", nil)).
		Return(1, nil)

	path := "steam://"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path: &path,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	assert.Equal(t, "steam://", browseResults.Path)
	require.Len(t, browseResults.Entries, 1)
	assert.Equal(t, 1, browseResults.TotalFiles)
	assert.Equal(t, "media", browseResults.Entries[0].Type)
	assert.Equal(t, "Team Fortress 2", browseResults.Entries[0].Name)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_PathTraversalRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		errMatch string
	}{
		{
			name:     "outside root",
			path:     "/etc/passwd",
			errMatch: "not within an allowed root directory",
		},
		{
			name:     "dotdot traversal",
			path:     "/roms/../etc/passwd",
			errMatch: "contains disallowed components",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
			mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
				Return([]string{"/roms"})
			mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
				Return([]platforms.Launcher{})

			mockMediaDB := helpers.NewMockMediaDBI()

			path := tt.path
			env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
				Path: &path,
			})

			_, err := HandleMediaBrowse(env)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMatch)

			var clientErr *models.ClientError
			require.ErrorAs(t, err, &clientErr)
		})
	}
}

func TestHandleMediaBrowse_UnknownVirtualScheme(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()

	path := "evil://"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path: &path,
	})

	_, err := HandleMediaBrowse(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown virtual scheme")

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestHandleMediaBrowse_VirtualGrouping(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{
			{
				ID: "KodiMovie", SystemID: "movie",
				Schemes: []string{"kodi-movie"}, Groups: []string{"Kodi"},
			},
			{
				ID: "KodiEpisode", SystemID: "tv.episode",
				Schemes: []string{"kodi-episode"},
				Groups:  []string{"Kodi", "KodiTV"},
			},
			{ID: "Steam", SystemID: "pc", Schemes: []string{"steam"}},
		})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseRootCounts", mock.Anything, []string{}).
		Return(map[string]*int{}, nil)
	mockMediaDB.On("BrowseVirtualSchemes", mock.Anything, database.BrowseVirtualSchemesOptions{}).
		Return([]database.BrowseVirtualScheme{
			{Scheme: "kodi-episode://", FileCount: 200},
			{Scheme: "kodi-movie://", FileCount: 80},
			{Scheme: "steam://", FileCount: 150},
		}, nil)

	env := newBrowseEnv(t, mockMediaDB, mockPlatform, nil)
	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 3)

	for _, entry := range browseResults.Entries {
		if entry.Path == "kodi-movie://" || entry.Path == "kodi-episode://" {
			require.NotNil(t, entry.Group)
			assert.Equal(t, "Kodi", *entry.Group)
		}
		if entry.Path == "steam://" {
			require.NotNil(t, entry.Group)
			assert.Equal(t, "Steam", *entry.Group)
		}
	}

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_WithLetterFilter(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	letterM := "M"
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/SNES/")).
		Return([]database.BrowseDirectoryResult{}, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{
			{
				SystemID: "snes", Name: "Mega Man X",
				Path: "/roms/SNES/Mega Man X.sfc", MediaID: 5,
			},
		}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts("/roms/SNES/", &letterM)).
		Return(15, nil)

	path := "/roms/SNES"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:   &path,
		Letter: &letterM,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	assert.Equal(t, 15, browseResults.TotalFiles)
	require.Len(t, browseResults.Entries, 1)
	assert.Equal(t, "Mega Man X", browseResults.Entries[0].Name)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_RootError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseRootCounts", mock.Anything, mock.Anything).
		Return(map[string]*int(nil), errors.New("db connection lost"))

	env := newBrowseEnv(t, mockMediaDB, mockPlatform, nil)
	_, err := HandleMediaBrowse(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "root counts")
}

func TestHandleMediaBrowse_FilesystemError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/SNES/")).
		Return([]database.BrowseDirectoryResult(nil), errors.New("disk io error"))

	path := "/roms/SNES"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path: &path,
	})

	_, err := HandleMediaBrowse(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browsing directories")
}

func TestHandleMediaBrowse_VirtualError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{
			{ID: "Steam", SystemID: "pc", Schemes: []string{"steam"}},
		})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor(nil), errors.New("query timeout"))

	path := "steam://"
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path: &path,
	})

	_, err := HandleMediaBrowse(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browsing virtual media")
}

func TestSchemeDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scheme   string
		expected string
	}{
		{"steam://", "Steam"},
		{"kodi-movie://", "Kodi Movie"},
		{"kodi-episode://", "Kodi Episode"},
		{"gog://", "Gog"},
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, schemeDisplayName(tt.scheme))
		})
	}
}
