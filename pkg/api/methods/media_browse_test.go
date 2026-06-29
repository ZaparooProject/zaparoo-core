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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
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

func browseDirectoriesAfterOpts(pathPrefix, afterName string, limit int) any {
	return mock.MatchedBy(func(opts database.BrowseDirectoriesOptions) bool {
		return opts.PathPrefix == pathPrefix && opts.AfterName == afterName &&
			opts.Limit == limit && len(opts.Systems) == 0
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

func browseDirCountOpts(pathPrefix string) any {
	return mock.MatchedBy(func(opts database.BrowseDirCountOptions) bool {
		return opts.PathPrefix == pathPrefix && len(opts.Systems) == 0
	})
}

func browseDirCountSystemOpts(pathPrefix, systemID string) any {
	return mock.MatchedBy(func(opts database.BrowseDirCountOptions) bool {
		return opts.PathPrefix == pathPrefix && len(opts.Systems) == 1 && opts.Systems[0].ID == systemID
	})
}

func browseVirtualSchemesSystemOpts(t *testing.T, systemID string) any {
	t.Helper()
	return mock.MatchedBy(func(opts database.BrowseVirtualSchemesOptions) bool {
		return len(opts.Systems) == 1 && opts.Systems[0].ID == systemID
	})
}

// mockSystemRootCandidatesNotReady wires BrowseSystemRootCandidates to
// report cacheReady=false so callers fall back to the per-root
// BrowseFileCount / BrowseDirectories fan-out that these tests assert on.
func mockSystemRootCandidatesNotReady(mockMediaDB *helpers.MockMediaDBI) {
	mockMediaDB.On("BrowseSystemRootCandidates", mock.Anything, mock.Anything).
		Return(database.BrowseSystemRootCandidates{}, false, nil)
}

func browseTestAbsPath(parts ...string) string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	root := filepath.VolumeName(wd) + string(filepath.Separator)
	return filepath.Join(append([]string{root}, parts...)...)
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
		Return(map[string]*int{romsRoot: intPtr(500)}, nil)
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
	assert.Equal(t, romsRoot, browseResults.Entries[0].Path)
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
	mockSystemRootCandidatesNotReady(mockMediaDB)
	romsPrefix := filepath.ToSlash(romsRoot) + "/"
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountSystemOpts(romsPrefix, "SNES")).
		Return(0, nil)
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesSystemOpts(romsPrefix, "SNES")).
		Return([]database.BrowseDirectoryResult{}, nil)
	mockMediaDB.On("BrowseVirtualSchemes", mock.Anything, browseVirtualSchemesSystemOpts(t, "SNES")).
		Return([]database.BrowseVirtualScheme{}, nil)
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

func TestHandleMediaBrowse_SystemRootRoutesIncludesIndexedDirectories(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	customPath := filepath.Join(romsRoot, "custom")
	customAPIPath := filepath.ToSlash(customPath)
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockSystemRootCandidatesNotReady(mockMediaDB)
	romsPrefix := filepath.ToSlash(romsRoot) + "/"
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountSystemOpts(romsPrefix, "SNES")).
		Return(0, nil)
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesSystemOpts(romsPrefix, "SNES")).
		Return([]database.BrowseDirectoryResult{{Name: "custom", FileCount: 3, SystemIDs: []string{"SNES"}}}, nil)
	mockMediaDB.On("BrowseVirtualSchemes", mock.Anything, browseVirtualSchemesSystemOpts(t, "SNES")).
		Return([]database.BrowseVirtualScheme{{Scheme: "steam://", FileCount: 2, SystemIDs: []string{"SNES"}}}, nil)
	mockMediaDB.On("BrowseRouteCounts", mock.Anything,
		mock.MatchedBy(func(opts database.BrowseRouteCountsOptions) bool {
			return len(opts.Systems) == 1 && opts.Systems[0].ID == "SNES" &&
				assert.ElementsMatch(t, []string{customAPIPath, "steam://"}, opts.Routes)
		}),
	).Return(map[string]database.BrowseRouteCount{
		customAPIPath: {Path: customAPIPath, FileCount: 3, SystemIDs: []string{"SNES"}},
		"steam://":    {Path: "steam://", FileCount: 2, SystemIDs: []string{"SNES"}},
	}, nil)

	systems := []string{"SNES"}
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{Systems: &systems})
	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 2)
	assert.Equal(t, customAPIPath, browseResults.Entries[0].Path)
	assert.Equal(t, "steam://", browseResults.Entries[1].Path)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_SystemRootRoutesDedupesCoveredParent(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	nesPath := filepath.Join(romsRoot, "NES")
	romsAPIPath := filepath.ToSlash(romsRoot)
	nesAPIPath := filepath.ToSlash(nesPath)
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{
			{ID: "NES", SystemID: "NES", Folders: []string{"NES"}},
		})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockSystemRootCandidatesNotReady(mockMediaDB)
	romsPrefix := filepath.ToSlash(romsRoot) + "/"
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountSystemOpts(romsPrefix, "NES")).
		Return(10, nil)
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesSystemOpts(romsPrefix, "NES")).
		Return([]database.BrowseDirectoryResult{{Name: "NES", FileCount: 10, SystemIDs: []string{"NES"}}}, nil)
	mockMediaDB.On("BrowseVirtualSchemes", mock.Anything, browseVirtualSchemesSystemOpts(t, "NES")).
		Return([]database.BrowseVirtualScheme{}, nil)
	mockMediaDB.On("BrowseRouteCounts", mock.Anything,
		mock.MatchedBy(func(opts database.BrowseRouteCountsOptions) bool {
			return len(opts.Systems) == 1 && opts.Systems[0].ID == "NES" &&
				assert.ElementsMatch(t, []string{nesAPIPath, romsAPIPath}, opts.Routes)
		}),
	).Return(map[string]database.BrowseRouteCount{
		romsAPIPath: {Path: romsAPIPath, FileCount: 10, SystemIDs: []string{"NES"}},
		nesAPIPath:  {Path: nesAPIPath, FileCount: 10, SystemIDs: []string{"NES"}},
	}, nil)

	systems := []string{"NES"}
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{Systems: &systems})
	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 1)
	assert.Equal(t, nesAPIPath, browseResults.Entries[0].Path)
	require.NotNil(t, browseResults.Entries[0].FileCount)
	assert.Equal(t, 10, *browseResults.Entries[0].FileCount)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_SystemRootRoutesIncludesRootMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	romsAPIPath := filepath.ToSlash(romsRoot)
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockSystemRootCandidatesNotReady(mockMediaDB)
	romsPrefix := filepath.ToSlash(romsRoot) + "/"
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountSystemOpts(romsPrefix, "SNES")).
		Return(1, nil)
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesSystemOpts(romsPrefix, "SNES")).
		Return([]database.BrowseDirectoryResult{}, nil)
	mockMediaDB.On("BrowseVirtualSchemes", mock.Anything, browseVirtualSchemesSystemOpts(t, "SNES")).
		Return([]database.BrowseVirtualScheme{}, nil)
	mockMediaDB.On("BrowseRouteCounts", mock.Anything,
		mock.MatchedBy(func(opts database.BrowseRouteCountsOptions) bool {
			return len(opts.Systems) == 1 && opts.Systems[0].ID == "SNES" &&
				assert.ElementsMatch(t, []string{romsAPIPath}, opts.Routes)
		}),
	).Return(map[string]database.BrowseRouteCount{
		romsAPIPath: {Path: romsAPIPath, FileCount: 1, SystemIDs: []string{"SNES"}},
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
	assert.Equal(t, "roms", entry.Name)
	assert.Equal(t, romsAPIPath, entry.Path)
	require.NotNil(t, entry.FileCount)
	assert.Equal(t, 1, *entry.FileCount)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_SystemRootRoutesUsesCachedCandidates(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	romsAPIPath := filepath.ToSlash(romsRoot)
	snesPath := filepath.Join(romsRoot, "SNES")
	snesAPIPath := filepath.ToSlash(snesPath)
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseSystemRootCandidates", mock.Anything,
		mock.MatchedBy(func(opts database.BrowseSystemRootCandidatesOptions) bool {
			return len(opts.Systems) == 1 && opts.Systems[0].ID == "SNES" &&
				assert.ElementsMatch(t, []string{romsRoot}, opts.Roots)
		}),
	).Return(database.BrowseSystemRootCandidates{
		HasMedia: map[string]bool{romsRoot: true},
		Children: map[string][]string{romsRoot: {"SNES"}},
	}, true, nil)
	mockMediaDB.On("BrowseVirtualSchemes", mock.Anything, browseVirtualSchemesSystemOpts(t, "SNES")).
		Return([]database.BrowseVirtualScheme{}, nil)
	mockMediaDB.On("BrowseRouteCounts", mock.Anything,
		mock.MatchedBy(func(opts database.BrowseRouteCountsOptions) bool {
			return len(opts.Systems) == 1 && opts.Systems[0].ID == "SNES" &&
				assert.ElementsMatch(t, []string{romsAPIPath, snesAPIPath}, opts.Routes)
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
	assert.Equal(t, snesAPIPath, browseResults.Entries[0].Path)

	// BrowseFileCount and BrowseDirectories must NOT be called when the
	// candidates cache is ready — that's the whole point of the shortcut.
	mockMediaDB.AssertNotCalled(t, "BrowseFileCount", mock.Anything, mock.Anything)
	mockMediaDB.AssertNotCalled(t, "BrowseDirectories", mock.Anything, mock.Anything)
	mockMediaDB.AssertExpectations(t)
}

func TestBuildBrowseResponse_SingletonAnnotation_WhenZipsAsDirsEnabled(t *testing.T) {
	t.Parallel()

	nesSystem := database.System{DBID: 1, SystemID: "NES"}
	systems := []systemdefs.System{{ID: "NES"}}
	path := filepath.ToSlash(filepath.Join("roms", "NES"))
	dirName := "Game.zip"
	dirPath := filepath.ToSlash(filepath.Join(path, dirName))
	row := database.MediaFullRow{
		Media: database.Media{
			DBID:      20,
			Path:      filepath.ToSlash(filepath.Join(dirPath, "Game.nes")),
			ParentDir: dirPath + "/",
		},
		Title:  database.MediaTitle{DBID: 30, Name: "Game"},
		System: nesSystem,
	}
	tags := []database.TagInfo{{Type: "favorite", Tag: "true", Label: "Favorite"}}
	alias := []database.SingletonContainerAlias{{
		ChildDir:      dirPath + "/",
		Row:           row,
		Tags:          tags,
		ZapScriptTags: []database.TagInfo{},
		HasCover:      true,
	}}

	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()
	mockMediaDB.On("FindSystemBySystemID", "NES").Return(nesSystem, nil).Once()
	mockMediaDB.On("ResolveSingletonContainerAliases", mock.Anything, nesSystem.DBID,
		[]database.SingletonAliasCandidate{{ChildDir: dirPath + "/", FileCount: 1}}).
		Return(alias, nil).Once()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
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
	assert.Equal(t, row.DBID, entry.MediaID)
	require.NotNil(t, entry.SystemID)
	assert.Equal(t, "NES", *entry.SystemID)
	require.NotNil(t, entry.ZapScript)
	assert.NotEmpty(t, *entry.ZapScript)
	assert.Equal(t, tags, entry.Tags)
	assert.True(t, entry.HasCover)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestBuildBrowseResponse_SingletonAnnotation_UsesMediaDisplayNameFallbacks(t *testing.T) {
	t.Parallel()

	psxSystem := database.System{DBID: 1, SystemID: "PSX"}
	systems := []systemdefs.System{{ID: "PSX"}}
	path := filepath.ToSlash(filepath.Join("roms", "PSX"))

	tests := []struct {
		name      string
		dirName   string
		mediaPath string
		sortName  string
		titleName string
		wantName  string
	}{
		{
			name:      "sort name",
			dirName:   "D (USA)",
			mediaPath: filepath.ToSlash(filepath.Join("roms", "PSX", "D (USA)", "D (USA) (Disc 1).chd")),
			sortName:  "D (Disc 1)",
			titleName: "D",
			wantName:  "D (Disc 1)",
		},
		{
			name:      "path basename",
			dirName:   "D (USA)",
			mediaPath: filepath.ToSlash(filepath.Join("roms", "PSX", "D (USA)", "D (USA) (Disc 2).chd")),
			titleName: "D",
			wantName:  "D (USA) (Disc 2)",
		},
		{
			name:      "title name",
			dirName:   "No Path",
			titleName: "Title Fallback",
			wantName:  "Title Fallback",
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dirPath := filepath.ToSlash(filepath.Join(path, tt.dirName))
			row := database.MediaFullRow{
				Media: database.Media{
					DBID:      int64(20 + i),
					Path:      tt.mediaPath,
					ParentDir: dirPath + "/",
					SortName:  tt.sortName,
				},
				Title:  database.MediaTitle{DBID: int64(30 + i), Name: tt.titleName},
				System: psxSystem,
			}
			alias := []database.SingletonContainerAlias{{
				ChildDir:      dirPath + "/",
				Row:           row,
				Tags:          []database.TagInfo{},
				ZapScriptTags: []database.TagInfo{},
			}}

			mockMediaDB := helpers.NewMockMediaDBI()
			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()
			mockMediaDB.On("FindSystemBySystemID", "PSX").Return(psxSystem, nil).Once()
			mockMediaDB.On("ResolveSingletonContainerAliases", mock.Anything, psxSystem.DBID,
				[]database.SingletonAliasCandidate{{ChildDir: dirPath + "/", FileCount: 1}}).
				Return(alias, nil).Once()

			env := &requests.RequestEnv{
				Context:  context.Background(),
				Database: &database.Database{MediaDB: mockMediaDB},
				Platform: mockPlatform,
			}
			result, err := buildBrowseResponse(env, path,
				[]database.BrowseDirectoryResult{{Name: tt.dirName, FileCount: 1, SystemIDs: []string{"PSX"}}},
				nil, defaultMaxResults, 0, 0, nil, false, systems)
			require.NoError(t, err)
			browseResults, ok := result.(models.BrowseResults)
			require.True(t, ok)
			require.Len(t, browseResults.Entries, 1)
			entry := browseResults.Entries[0]
			assert.Equal(t, "directory", entry.Type)
			assert.Equal(t, tt.wantName, entry.Name)
			assert.Equal(t, dirPath, entry.Path)
			assert.Equal(t, row.DBID, entry.MediaID)
			require.NotNil(t, entry.SystemID)
			assert.Equal(t, "PSX", *entry.SystemID)
			mockMediaDB.AssertExpectations(t)
			mockPlatform.AssertExpectations(t)
		})
	}
}

func TestBuildBrowseResponse_SingletonAnnotation_InferredFromDirSystemIDs(t *testing.T) {
	t.Parallel()

	// systems filter is empty — system must be inferred from dir.SystemIDs.
	nesSystem := database.System{DBID: 1, SystemID: "NES"}
	path := filepath.ToSlash(filepath.Join("roms", "NES"))
	dirName := "Game.zip"
	dirPath := filepath.ToSlash(filepath.Join(path, dirName))
	row := database.MediaFullRow{
		Media: database.Media{
			DBID:      20,
			Path:      filepath.ToSlash(filepath.Join(dirPath, "Game.nes")),
			ParentDir: dirPath + "/",
		},
		Title:  database.MediaTitle{DBID: 30, Name: "Game"},
		System: nesSystem,
	}
	alias := []database.SingletonContainerAlias{{
		ChildDir:      dirPath + "/",
		Row:           row,
		Tags:          []database.TagInfo{},
		ZapScriptTags: []database.TagInfo{},
	}}

	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()
	mockMediaDB.On("FindSystemBySystemID", "NES").Return(nesSystem, nil).Once()
	mockMediaDB.On("ResolveSingletonContainerAliases", mock.Anything, nesSystem.DBID,
		[]database.SingletonAliasCandidate{{ChildDir: dirPath + "/", FileCount: 1}}).
		Return(alias, nil).Once()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
	}
	// No systems filter passed — inferred from dir.SystemIDs.
	result, err := buildBrowseResponse(env, path,
		[]database.BrowseDirectoryResult{{Name: dirName, FileCount: 1, SystemIDs: []string{"NES"}}},
		nil, defaultMaxResults, 0, 0, nil, false, nil)
	require.NoError(t, err)
	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 1)
	entry := browseResults.Entries[0]
	assert.Equal(t, "directory", entry.Type)
	assert.Equal(t, row.DBID, entry.MediaID)
	require.NotNil(t, entry.SystemID)
	assert.Equal(t, "NES", *entry.SystemID)
	require.NotNil(t, entry.ZapScript)
	assert.NotEmpty(t, *entry.ZapScript)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestBuildBrowseResponse_SingletonAnnotation_SkipsLookupForMixedDirSystems(t *testing.T) {
	t.Parallel()

	path := filepath.ToSlash(filepath.Join("roms", "shared"))
	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
	}
	result, err := buildBrowseResponse(env, path,
		[]database.BrowseDirectoryResult{
			{Name: "NES", FileCount: 1, SystemIDs: []string{"NES"}},
			{Name: "SNES", FileCount: 1, SystemIDs: []string{"SNES"}},
		},
		nil, defaultMaxResults, 0, 0, nil, false, nil)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 2)
	for _, entry := range browseResults.Entries {
		assert.Zero(t, entry.MediaID)
		assert.Nil(t, entry.ZapScript)
	}
	mockPlatform.AssertNotCalled(t, "Settings")
	mockMediaDB.AssertNotCalled(t, "FindSystemBySystemID", mock.Anything)
	mockMediaDB.AssertNotCalled(t, "ResolveSingletonContainerAliases", mock.Anything, mock.Anything, mock.Anything)
}

func TestBuildBrowseResponse_SingletonAnnotation_WhenZipsAsDirsDisabledSkipsLookup(t *testing.T) {
	t.Parallel()

	systems := []systemdefs.System{{ID: "NES"}}
	path := filepath.ToSlash(filepath.Join("roms", "NES"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: false}).Once()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
	}
	result, err := buildBrowseResponse(env, path,
		[]database.BrowseDirectoryResult{{Name: "Game.zip", FileCount: 1, SystemIDs: []string{"NES"}}},
		nil, defaultMaxResults, 0, 0, nil, false, systems)
	require.NoError(t, err)
	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 1)
	entry := browseResults.Entries[0]
	assert.Zero(t, entry.MediaID)
	assert.Nil(t, entry.ZapScript)
	mockMediaDB.AssertNotCalled(t, "FindSystemBySystemID", mock.Anything)
	mockMediaDB.AssertNotCalled(t, "ResolveSingletonContainerAliases", mock.Anything)
	mockPlatform.AssertExpectations(t)
}

func TestBuildBrowseResponse_AnnotatesLogicalBinCueDirectory(t *testing.T) {
	t.Parallel()

	psxSystem := database.System{DBID: 1, SystemID: "PSX"}
	systems := []systemdefs.System{{ID: "PSX"}}
	path := filepath.ToSlash(filepath.Join("roms", "PSX"))
	dirPath := filepath.ToSlash(filepath.Join(path, "Game"))
	row := database.MediaFullRow{
		Media: database.Media{
			DBID:      20,
			Path:      filepath.ToSlash(filepath.Join(dirPath, "Game.cue")),
			ParentDir: dirPath + "/",
		},
		Title:  database.MediaTitle{DBID: 30, Name: "Game"},
		System: psxSystem,
	}
	alias := []database.SingletonContainerAlias{{
		ChildDir:      dirPath + "/",
		Row:           row,
		Tags:          []database.TagInfo{},
		ZapScriptTags: []database.TagInfo{},
		HasCover:      true,
	}}

	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()
	mockMediaDB.On("FindSystemBySystemID", "PSX").Return(psxSystem, nil).Once()
	mockMediaDB.On("ResolveSingletonContainerAliases", mock.Anything, psxSystem.DBID,
		[]database.SingletonAliasCandidate{{ChildDir: dirPath + "/", FileCount: 2}}).
		Return(alias, nil).Once()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
	}
	result, err := buildBrowseResponse(env, path,
		[]database.BrowseDirectoryResult{{Name: "Game", FileCount: 2, SystemIDs: []string{"PSX"}}},
		nil, defaultMaxResults, 0, 0, nil, false, systems)
	require.NoError(t, err)
	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 1)
	entry := browseResults.Entries[0]
	assert.Equal(t, "directory", entry.Type)
	assert.Equal(t, row.DBID, entry.MediaID)
	require.NotNil(t, entry.ZapScript)
	assert.NotEmpty(t, *entry.ZapScript)
	assert.True(t, entry.HasCover)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestBuildBrowseResponse_NestedOnlyDirectoryRemainsPlain(t *testing.T) {
	t.Parallel()

	nesSystem := database.System{DBID: 1, SystemID: "NES"}
	systems := []systemdefs.System{{ID: "NES"}}
	path := filepath.ToSlash(filepath.Join("roms", "NES"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()
	// ResolveSingletonContainerAliases returns nil — no alias for the nested dir.
	mockMediaDB.On("FindSystemBySystemID", "NES").Return(nesSystem, nil).Once()
	mockMediaDB.On("ResolveSingletonContainerAliases", mock.Anything, nesSystem.DBID,
		[]database.SingletonAliasCandidate{{ChildDir: path + "/Collection/", FileCount: 1}}).
		Return([]database.SingletonContainerAlias(nil), nil).Once()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
	}
	result, err := buildBrowseResponse(env, path,
		[]database.BrowseDirectoryResult{{Name: "Collection", FileCount: 1, SystemIDs: []string{"NES"}}},
		nil, defaultMaxResults, 0, 0, nil, false, systems)
	require.NoError(t, err)
	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 1)
	entry := browseResults.Entries[0]
	assert.Equal(t, "directory", entry.Type)
	assert.Zero(t, entry.MediaID)
	assert.Nil(t, entry.ZapScript)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestBuildBrowseResponse_SingletonAnnotation_OversizedDirSkipsLookup(t *testing.T) {
	t.Parallel()

	// A directory above the candidate file cap (e.g. MiSTer's
	// _Arcade/_alternatives tree) must not trigger any alias resolution —
	// no Settings, system lookup, or resolver calls at all.
	systems := []systemdefs.System{{ID: "Arcade"}}
	path := filepath.ToSlash(filepath.Join("media", "fat", "_Arcade"))

	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
	}
	result, err := buildBrowseResponse(env, path,
		[]database.BrowseDirectoryResult{{Name: "_alternatives", FileCount: 5000, SystemIDs: []string{"Arcade"}}},
		nil, defaultMaxResults, 0, 0, nil, false, systems)
	require.NoError(t, err)
	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 1)
	entry := browseResults.Entries[0]
	assert.Zero(t, entry.MediaID)
	assert.Nil(t, entry.ZapScript)
	mockMediaDB.AssertNotCalled(t, "FindSystemBySystemID", mock.Anything)
	mockMediaDB.AssertNotCalled(t, "ResolveSingletonContainerAliases", mock.Anything)
	mockPlatform.AssertNotCalled(t, "Settings")
}

func TestBuildBrowseResponse_SingletonAnnotation_OversizedDirExcludedFromCandidates(t *testing.T) {
	t.Parallel()

	// When the page mixes small candidate dirs with an oversized one, only
	// the small dirs are passed to the resolver.
	nesSystem := database.System{DBID: 1, SystemID: "NES"}
	systems := []systemdefs.System{{ID: "NES"}}
	path := filepath.ToSlash(filepath.Join("roms", "NES"))
	dirPath := filepath.ToSlash(filepath.Join(path, "Game"))
	row := database.MediaFullRow{
		Media: database.Media{
			DBID:      20,
			Path:      filepath.ToSlash(filepath.Join(dirPath, "Game.nes")),
			ParentDir: dirPath + "/",
		},
		Title:  database.MediaTitle{DBID: 30, Name: "Game"},
		System: nesSystem,
	}
	alias := []database.SingletonContainerAlias{{
		ChildDir:      dirPath + "/",
		Row:           row,
		Tags:          []database.TagInfo{},
		ZapScriptTags: []database.TagInfo{},
	}}

	mockMediaDB := helpers.NewMockMediaDBI()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()
	mockMediaDB.On("FindSystemBySystemID", "NES").Return(nesSystem, nil).Once()
	mockMediaDB.On("ResolveSingletonContainerAliases", mock.Anything, nesSystem.DBID,
		[]database.SingletonAliasCandidate{{ChildDir: dirPath + "/", FileCount: 1}}).
		Return(alias, nil).Once()

	env := &requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: mockMediaDB},
		Platform: mockPlatform,
	}
	result, err := buildBrowseResponse(env, path,
		[]database.BrowseDirectoryResult{
			{Name: "Game", FileCount: 1, SystemIDs: []string{"NES"}},
			{Name: "Collection", FileCount: 500, SystemIDs: []string{"NES"}},
		},
		nil, defaultMaxResults, 0, 0, nil, false, systems)
	require.NoError(t, err)
	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	require.Len(t, browseResults.Entries, 2)
	assert.Equal(t, row.DBID, browseResults.Entries[0].MediaID)
	assert.Zero(t, browseResults.Entries[1].MediaID)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestBuildBrowseResponse_SingletonAnnotation_HasCoverPropagated(t *testing.T) {
	t.Parallel()

	nesSystem := database.System{DBID: 1, SystemID: "NES"}
	systems := []systemdefs.System{{ID: "NES"}}
	path := filepath.ToSlash(filepath.Join("roms", "NES"))
	dirName := "Game.zip"
	dirPath := filepath.ToSlash(filepath.Join(path, dirName))
	row := database.MediaFullRow{
		Media: database.Media{
			DBID:      20,
			Path:      filepath.ToSlash(filepath.Join(dirPath, "Game.nes")),
			ParentDir: dirPath + "/",
		},
		Title:  database.MediaTitle{DBID: 30, Name: "Game"},
		System: nesSystem,
	}

	tests := []struct {
		name          string
		aliasHasCover bool
	}{
		{name: "HasCover true propagates", aliasHasCover: true},
		{name: "HasCover false propagates", aliasHasCover: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			alias := []database.SingletonContainerAlias{{
				ChildDir:      dirPath + "/",
				Row:           row,
				Tags:          []database.TagInfo{},
				ZapScriptTags: []database.TagInfo{},
				HasCover:      tt.aliasHasCover,
			}}

			mockMediaDB := helpers.NewMockMediaDBI()
			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: true}).Once()
			mockMediaDB.On("FindSystemBySystemID", "NES").Return(nesSystem, nil).Once()
			mockMediaDB.On("ResolveSingletonContainerAliases", mock.Anything, nesSystem.DBID,
				[]database.SingletonAliasCandidate{{ChildDir: dirPath + "/", FileCount: 1}}).
				Return(alias, nil).Once()

			env := &requests.RequestEnv{
				Context:  context.Background(),
				Database: &database.Database{MediaDB: mockMediaDB},
				Platform: mockPlatform,
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
			assert.Equal(t, row.DBID, entry.MediaID)
			assert.Equal(t, tt.aliasHasCover, entry.HasCover)
			mockMediaDB.AssertExpectations(t)
			mockPlatform.AssertExpectations(t)
		})
	}
}

func TestDedupeSystemRootEntries(t *testing.T) {
	t.Parallel()

	count := func(v int) *int { return &v }
	path := func(parts ...string) string {
		return filepath.Join(append([]string{string(filepath.Separator)}, parts...)...)
	}

	tests := []struct {
		name    string
		entries []models.BrowseEntry
		want    []string
	}{
		{
			name: "single child absorbs parent",
			entries: []models.BrowseEntry{
				{Path: path("media", "fat", "games"), FileCount: count(10)},
				{Path: path("media", "fat", "games", "NES"), FileCount: count(10)},
			},
			want: []string{path("media", "fat", "games", "NES")},
		},
		{
			name: "children absorb parent with aggregate count",
			entries: []models.BrowseEntry{
				{Path: path("media", "fat", "games"), FileCount: count(15)},
				{Path: path("media", "fat", "games", "NES"), FileCount: count(10)},
				{Path: path("media", "fat", "games", "NES Hacks"), FileCount: count(5)},
			},
			want: []string{path("media", "fat", "games", "NES"), path("media", "fat", "games", "NES Hacks")},
		},
		{
			name: "parent retained when descendants do not cover count",
			entries: []models.BrowseEntry{
				{Path: path("media", "fat", "games"), FileCount: count(20)},
				{Path: path("media", "fat", "games", "NES"), FileCount: count(10)},
				{Path: path("media", "fat", "games", "NES Hacks"), FileCount: count(5)},
			},
			want: []string{
				path("media", "fat", "games"),
				path("media", "fat", "games", "NES"),
				path("media", "fat", "games", "NES Hacks"),
			},
		},
		{
			name: "unknown descendant count retains parent",
			entries: []models.BrowseEntry{
				{Path: path("media", "fat", "games"), FileCount: count(20)},
				{Path: path("media", "fat", "games", "NES"), FileCount: nil},
			},
			want: []string{
				path("media", "fat", "games"),
				path("media", "fat", "games", "NES"),
			},
		},
		{
			name: "sibling equal counts are unrelated",
			entries: []models.BrowseEntry{
				{Path: path("media", "fat", "games", "NES"), FileCount: count(10)},
				{Path: path("media", "fat", "alt", "NES"), FileCount: count(10)},
			},
			want: []string{path("media", "fat", "games", "NES"), path("media", "fat", "alt", "NES")},
		},
		{
			name: "nil counts are not deduped",
			entries: []models.BrowseEntry{
				{Path: path("media", "fat", "games"), FileCount: nil},
				{Path: path("media", "fat", "games", "NES"), FileCount: count(10)},
				{Path: path("media", "fat", "games", "SNES"), FileCount: nil},
			},
			want: []string{
				path("media", "fat", "games"),
				path("media", "fat", "games", "NES"),
				path("media", "fat", "games", "SNES"),
			},
		},
		{
			name: "string prefix is not ancestry",
			entries: []models.BrowseEntry{
				{Path: path("media", "fat", "games"), FileCount: count(10)},
				{Path: path("media", "fat", "games-extra"), FileCount: count(10)},
			},
			want: []string{path("media", "fat", "games"), path("media", "fat", "games-extra")},
		},
		{
			name: "virtual schemes are ignored",
			entries: []models.BrowseEntry{
				{Path: "box://", FileCount: count(10)},
				{Path: "box://NES", FileCount: count(10)},
			},
			want: []string{"box://", "box://NES"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := dedupeSystemRootEntries(tt.entries)
			gotPaths := make([]string, 0, len(got))
			for _, entry := range got {
				gotPaths = append(gotPaths, entry.Path)
			}

			assert.Equal(t, tt.want, gotPaths)
		})
	}
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
	romsAPIPath := filepath.ToSlash(romsRoot)
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts(romsAPIPath+"/")).
		Return([]database.BrowseDirectoryResult{
			{Name: "NES", FileCount: 100},
			{Name: "SNES", FileCount: 200},
		}, nil)
	mockMediaDB.On("BrowseDirCount", mock.Anything, browseDirCountOpts(romsAPIPath+"/")).
		Return(2, nil)
	mockMediaDB.On("BrowseFiles", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{}, nil)
	// The directory page also reports the total file count as the denominator.
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountOpts(romsAPIPath+"/", nil)).
		Return(0, nil)

	path := romsAPIPath
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path: &path,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	assert.Equal(t, romsAPIPath, browseResults.Path)
	require.Len(t, browseResults.Entries, 2)

	assert.Equal(t, "directory", browseResults.Entries[0].Type)
	assert.Equal(t, "NES", browseResults.Entries[0].Name)
	assert.Equal(t, romsAPIPath+"/NES", browseResults.Entries[0].Path)
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
	mockMediaDB.On("BrowseDirCount", mock.Anything, browseDirCountOpts("/roms/SNES/")).
		Return(0, nil)
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
	mockPlatform.On("Settings").Return(platforms.Settings{ZipsAsDirs: false})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesSystemOpts(sharedPrefix, "SNES")).
		Return([]database.BrowseDirectoryResult{
			{Name: "RPG", FileCount: 3, SystemIDs: []string{"SNES"}},
		}, nil)
	mockMediaDB.On("BrowseDirCount", mock.Anything, browseDirCountSystemOpts(sharedPrefix, "SNES")).
		Return(1, nil)
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
	mockMediaDB.On("BrowseDirCount", mock.Anything, browseDirCountOpts("/roms/SNES/")).
		Return(0, nil)
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

func TestHandleMediaBrowse_DirPaginationPureDirPage(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	// maxResults=2 but 3 dirs are returned (limit maxResults+1=3), so more
	// directories remain and the page is directory-only.
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/SNES/")).
		Return([]database.BrowseDirectoryResult{
			{Name: "Alpha", FileCount: 1},
			{Name: "Beta", FileCount: 1},
			{Name: "Gamma", FileCount: 1},
		}, nil)
	mockMediaDB.On("BrowseDirCount", mock.Anything, browseDirCountOpts("/roms/SNES/")).
		Return(10, nil)
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

	// Only the directory page (2 dirs) is returned; files are not fetched yet.
	require.Len(t, browseResults.Entries, 2)
	assert.Equal(t, "directory", browseResults.Entries[0].Type)
	assert.Equal(t, "Alpha", browseResults.Entries[0].Name)
	assert.Equal(t, "directory", browseResults.Entries[1].Type)
	assert.Equal(t, "Beta", browseResults.Entries[1].Name)
	assert.Equal(t, 10, browseResults.TotalDirs)
	assert.Equal(t, 5, browseResults.TotalFiles)

	require.NotNil(t, browseResults.Pagination)
	assert.True(t, browseResults.Pagination.HasNextPage)
	require.NotNil(t, browseResults.Pagination.NextCursor)

	// The cursor resumes the directory stream after the last returned dir.
	cursor, decErr := decodeBrowseCursor(*browseResults.Pagination.NextCursor)
	require.NoError(t, decErr)
	require.NotNil(t, cursor)
	assert.Equal(t, browsePhaseDirs, cursor.Phase)
	assert.Equal(t, "Beta", cursor.DirName)
	assert.Equal(t, 10, cursor.TotalDirs)
	assert.Equal(t, 5, cursor.TotalFiles)

	// Files are not queried while directories remain.
	mockMediaDB.AssertNotCalled(t, "BrowseFiles", mock.Anything, mock.Anything)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_DirPaginationCursorAdvance(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	// A dirs-phase cursor positioned after "Beta" carrying the first-page counts.
	cursorStr, err := encodeDirCursor("Beta", 5, 10)
	require.NoError(t, err)

	path := "/roms/SNES"
	maxResults := 2

	mockMediaDB := helpers.NewMockMediaDBI()
	// The next page is fetched with AfterName="Beta" and the overfetch limit
	// (maxResults+1) to detect whether more dirs remain.
	mockMediaDB.On("BrowseDirectories", mock.Anything,
		browseDirectoriesAfterOpts("/roms/SNES/", "Beta", maxResults+1)).
		Return([]database.BrowseDirectoryResult{
			{Name: "Delta", FileCount: 1},
			{Name: "Epsilon", FileCount: 1},
			{Name: "Zeta", FileCount: 1},
		}, nil)
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:       &path,
		MaxResults: &maxResults,
		Cursor:     &cursorStr,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)

	require.Len(t, browseResults.Entries, 2)
	assert.Equal(t, "Delta", browseResults.Entries[0].Name)
	assert.Equal(t, "Epsilon", browseResults.Entries[1].Name)
	// Counts are carried forward from the cursor, not recomputed.
	assert.Equal(t, 10, browseResults.TotalDirs)
	assert.Equal(t, 5, browseResults.TotalFiles)

	require.NotNil(t, browseResults.Pagination)
	assert.True(t, browseResults.Pagination.HasNextPage)
	cursor, decErr := decodeBrowseCursor(*browseResults.Pagination.NextCursor)
	require.NoError(t, decErr)
	require.NotNil(t, cursor)
	assert.Equal(t, browsePhaseDirs, cursor.Phase)
	assert.Equal(t, "Epsilon", cursor.DirName)

	// Cursor pages do not rerun the count queries.
	mockMediaDB.AssertNotCalled(t, "BrowseDirCount", mock.Anything, mock.Anything)
	mockMediaDB.AssertNotCalled(t, "BrowseFileCount", mock.Anything, mock.Anything)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_DirToFileTransition(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{"/roms"})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	// Exactly maxResults dirs are returned (no extra row), so directories are
	// exhausted but fill the whole page, leaving no room for files this page.
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseDirectories", mock.Anything, browseDirectoriesOpts("/roms/SNES/")).
		Return([]database.BrowseDirectoryResult{
			{Name: "Alpha", FileCount: 1},
			{Name: "Beta", FileCount: 1},
		}, nil)
	mockMediaDB.On("BrowseDirCount", mock.Anything, browseDirCountOpts("/roms/SNES/")).
		Return(2, nil)
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

	require.Len(t, browseResults.Entries, 2)
	assert.Equal(t, "directory", browseResults.Entries[0].Type)
	assert.Equal(t, "directory", browseResults.Entries[1].Type)
	assert.Equal(t, 2, browseResults.TotalDirs)
	assert.Equal(t, 5, browseResults.TotalFiles)

	// More files remain, so the next cursor switches to the files phase from
	// the beginning (no keyset).
	require.NotNil(t, browseResults.Pagination)
	assert.True(t, browseResults.Pagination.HasNextPage)
	cursor, decErr := decodeBrowseCursor(*browseResults.Pagination.NextCursor)
	require.NoError(t, decErr)
	require.NotNil(t, cursor)
	assert.Equal(t, browsePhaseFiles, cursor.Phase)
	assert.Equal(t, int64(0), cursor.LastID)
	assert.Equal(t, 5, cursor.TotalFiles)
	assert.Equal(t, 2, cursor.TotalDirs)

	// Files are not fetched on the boundary page (the page is full of dirs).
	mockMediaDB.AssertNotCalled(t, "BrowseFiles", mock.Anything, mock.Anything)
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

func TestDecodeBrowseCursor_RejectsInvalidPhase(t *testing.T) {
	t.Parallel()

	bogus, err := encodeCursorData(&browseCursorData{Phase: "bogus"})
	require.NoError(t, err)

	cursor, decErr := decodeBrowseCursor(bogus)
	require.Error(t, decErr)
	assert.Nil(t, cursor)
	var clientErr *models.ClientError
	assert.ErrorAs(t, decErr, &clientErr, "expected a client error for an invalid phase")
}

func TestDecodeBrowseCursor_AcceptsValidPhases(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"", browsePhaseDirs, browsePhaseFiles} {
		encoded, err := encodeCursorData(&browseCursorData{Phase: phase})
		require.NoError(t, err)

		cursor, decErr := decodeBrowseCursor(encoded)
		require.NoError(t, decErr)
		require.NotNil(t, cursor)
		assert.Equal(t, phase, cursor.Phase)
	}
}

func TestHandleMediaBrowse_CursorTotalFilesSkipsCountQuery(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	snesPath := filepath.Join(romsRoot, "SNES")
	snesAPIPath := filepath.ToSlash(snesPath)
	snesPrefix := snesAPIPath + "/"
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	cursorStr, err := encodeBrowseCursor(5, "Mario", 10)
	require.NoError(t, err)

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseFiles", mock.Anything,
		mock.MatchedBy(func(opts *database.BrowseFilesOptions) bool {
			return opts.Cursor != nil && opts.Cursor.TotalFiles == 10 && opts.PathPrefix == snesPrefix
		}),
	).Return([]database.SearchResultWithCursor{
		{SystemID: "snes", Name: "Zelda", Path: filepath.ToSlash(filepath.Join(snesPath, "Zelda.sfc")), MediaID: 8},
	}, nil)

	maxResults := 5
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:       &snesAPIPath,
		MaxResults: &maxResults,
		Cursor:     &cursorStr,
	})

	result, browseErr := HandleMediaBrowse(env)
	require.NoError(t, browseErr)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	assert.Equal(t, 10, browseResults.TotalFiles)
	mockMediaDB.AssertNotCalled(t, "BrowseFileCount", mock.Anything, mock.Anything)
	mockMediaDB.AssertExpectations(t)
}

func TestHandleMediaBrowse_VirtualCursorTotalFilesSkipsCountQuery(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{browseTestAbsPath("roms")})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{{ID: "Steam", SystemID: "pc", Schemes: []string{"steam"}}})

	cursorStr, err := encodeBrowseCursor(5, "Mario", 10)
	require.NoError(t, err)

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseFiles", mock.Anything,
		mock.MatchedBy(func(opts *database.BrowseFilesOptions) bool {
			return opts.Cursor != nil && opts.Cursor.TotalFiles == 10 && opts.PathPrefix == "steam://"
		}),
	).Return([]database.SearchResultWithCursor{
		{SystemID: "pc", Name: "Portal", Path: "steam://620", MediaID: 8},
	}, nil)

	path := "steam://"
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
	assert.Equal(t, 10, browseResults.TotalFiles)
	mockMediaDB.AssertNotCalled(t, "BrowseFileCount", mock.Anything, mock.Anything)
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
	mockMediaDB.On("BrowseDirCount", mock.Anything, browseDirCountOpts("/roms/SNES/")).
		Return(0, nil)
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
	mockMediaDB.On("BrowseDirCount", mock.Anything, browseDirCountOpts("/roms/SNES/")).
		Return(0, nil)
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

func TestHandleMediaBrowse_VirtualFiltersBySystem(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{browseTestAbsPath("roms")})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{
			{ID: "Steam", SystemID: "Windows", Schemes: []string{"steam"}},
		})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseFiles", mock.Anything, browseFilesSystemOpts("steam://", "Windows")).
		Return([]database.SearchResultWithCursor{
			{
				SystemID: "Windows", Name: "Team Fortress 2",
				Path: "steam://440/Team%20Fortress%202", MediaID: 10,
			},
		}, nil)
	mockMediaDB.On("BrowseFileCount", mock.Anything, browseFileCountSystemOpts("steam://", "Windows")).
		Return(1, nil)

	path := "steam://"
	systems := []string{"Windows"}
	env := newBrowseEnv(t, mockMediaDB, mockPlatform, models.BrowseParams{
		Path:    &path,
		Systems: &systems,
	})

	result, err := HandleMediaBrowse(env)
	require.NoError(t, err)

	browseResults, ok := result.(models.BrowseResults)
	require.True(t, ok)
	assert.Equal(t, "steam://", browseResults.Path)
	assert.Equal(t, 1, browseResults.TotalFiles)
	require.Len(t, browseResults.Entries, 1)
	assert.Equal(t, "media", browseResults.Entries[0].Type)
	require.NotNil(t, browseResults.Entries[0].SystemID)
	assert.Equal(t, "Windows", *browseResults.Entries[0].SystemID)

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
	// A letter filter browses files only — the directory phase is skipped,
	// so BrowseDirectories/BrowseDirCount are never called.
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

// TestHandleMediaBrowseCancelledIsQuiet verifies that a browse request cancelled
// by the client is returned as a QuietClientError (logged at Debug, kept out of
// Sentry) rather than a plain error (logged at Error).
func TestHandleMediaBrowseCancelledIsQuiet(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	romsRoot := browseTestAbsPath("roms")
	mockPlatform.On("SupportedReaders", mock.Anything).Return(nil)
	mockPlatform.On("RootDirs", mock.AnythingOfType("*config.Instance")).
		Return([]string{romsRoot})
	mockPlatform.On("Launchers", mock.AnythingOfType("*config.Instance")).
		Return([]platforms.Launcher{})

	// A DB query that fails because the request context was cancelled mid-browse
	// (client navigated away) is the real-world source of these events.
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("BrowseRootCounts", mock.Anything, mock.Anything).
		Return(map[string]*int(nil), context.Canceled)

	env := newBrowseEnv(t, mockMediaDB, mockPlatform, nil)

	result, err := HandleMediaBrowse(env)
	assert.Nil(t, result)
	require.Error(t, err)

	// Cancellation is returned as a QuietClientError (logged at Debug, out of
	// Sentry) and still unwraps to context.Canceled for the JSON-RPC response.
	var quietErr *models.QuietClientError
	require.ErrorAs(t, err, &quietErr)
	assert.ErrorIs(t, err, context.Canceled)
}
