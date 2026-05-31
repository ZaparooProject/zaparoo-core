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
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	phelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeResolveMediaEnv(
	mockDB *testhelpers.MockMediaDBI,
	platform *mocks.MockPlatform,
	launcherCache *phelpers.LauncherCache,
	cfg *config.Instance,
) requests.RequestEnv {
	return requests.RequestEnv{
		Context:       context.Background(),
		Database:      &database.Database{MediaDB: mockDB},
		Platform:      platform,
		Config:        cfg,
		LauncherCache: launcherCache,
	}
}

func makeResolveLauncherCache(launchers []platforms.Launcher) *phelpers.LauncherCache {
	launcherCache := &phelpers.LauncherCache{}
	launcherCache.InitializeFromSlice(launchers)
	return launcherCache
}

func TestResolveMediaBySystemAndPath_RelativeFallbackSuccess(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	system := database.System{DBID: 10, SystemID: "NES", Name: "Nintendo Entertainment System"}
	rootDir := filepath.Join(string(filepath.Separator), "roms")
	mediaPath := filepath.Join(rootDir, "NES", "mario.nes")
	media := database.Media{DBID: 20, Path: mediaPath}
	row := &database.MediaFullRow{
		Media:  media,
		Title:  database.MediaTitle{DBID: 30, Name: "Mario"},
		System: system,
	}
	launcherCache := makeResolveLauncherCache([]platforms.Launcher{
		{ID: "nes-skipped", SystemID: "NES", Folders: []string{"Ignored"}, SkipFilesystemScan: true},
		{ID: "nes", SystemID: "NES", Folders: []string{"NES"}},
	})

	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, filepath.Join("NES", "mario.nes")).
		Return((*database.Media)(nil), nil)
	pl.On("RootDirs", cfg).Return([]string{rootDir})
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, filepath.ToSlash(mediaPath)).Return(&media, nil)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, media.DBID).Return(row, nil)

	env := makeResolveMediaEnv(mockDB, pl, launcherCache, cfg)
	result, err := resolveMediaBySystemAndPath(&env, "NES", filepath.Join("NES", "mario.nes"))
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, mediaPath, result.Path)
	mockDB.AssertExpectations(t)
	pl.AssertExpectations(t)
}

func TestResolveMediaBySystemAndPath_RelativeFallbackAmbiguous(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	system := database.System{DBID: 10, SystemID: "NES", Name: "Nintendo Entertainment System"}
	rootDirA := filepath.Join(string(filepath.Separator), "roms-a")
	rootDirB := filepath.Join(string(filepath.Separator), "roms-b")
	pathA := filepath.Join(rootDirA, "NES", "mario.nes")
	pathB := filepath.Join(rootDirB, "NES", "mario.nes")
	launcherCache := makeResolveLauncherCache([]platforms.Launcher{
		{ID: "nes", SystemID: "NES", Folders: []string{"NES"}},
	})

	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, filepath.Join("NES", "mario.nes")).
		Return((*database.Media)(nil), nil)
	pl.On("RootDirs", cfg).Return([]string{rootDirA, rootDirB})
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, filepath.ToSlash(pathA)).
		Return(&database.Media{DBID: 20, Path: pathA}, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, filepath.ToSlash(pathB)).
		Return(&database.Media{DBID: 21, Path: pathB}, nil)

	env := makeResolveMediaEnv(mockDB, pl, launcherCache, cfg)
	_, err := resolveMediaBySystemAndPath(&env, "NES", filepath.Join("NES", "mario.nes"))
	require.Error(t, err)
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
	assert.Contains(t, err.Error(), "ambiguous relative path")
	mockDB.AssertExpectations(t)
	pl.AssertExpectations(t)
}

func TestResolveMediaBySystemAndPath_SingletonContainerFallbackSuccess(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	system := database.System{DBID: 10, SystemID: "NES", Name: "Nintendo Entertainment System"}
	containerPath := filepath.ToSlash(filepath.Join("roms", "NES", "Game.zip"))
	childPath := filepath.ToSlash(filepath.Join(containerPath, "Game.nes"))
	parentDir := filepath.ToSlash(containerPath) + "/"
	media := database.Media{DBID: 20, Path: childPath, ParentDir: parentDir}
	row := &database.MediaFullRow{
		Media:  media,
		Title:  database.MediaTitle{DBID: 30, Name: "Game"},
		System: system,
	}

	pl.On("Settings").Return(platforms.Settings{ZipsAsDirs: true})
	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, containerPath).
		Return((*database.Media)(nil), nil)
	mockDB.On("FindSingleDescendantMedia", mock.Anything, system.DBID, containerPath).Return(&media, nil)
	mockDB.On("GetMediaWithTitleAndSystem", mock.Anything, media.DBID).Return(row, nil)

	env := makeResolveMediaEnv(mockDB, pl, nil, cfg)
	result, err := resolveMediaBySystemAndPath(&env, "NES", containerPath)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, childPath, result.Path)
	mockDB.AssertExpectations(t)
}

func TestResolveMediaBySystemAndPath_SingletonContainerFallbackMiss(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	system := database.System{DBID: 10, SystemID: "NES", Name: "Nintendo Entertainment System"}
	containerPath := filepath.ToSlash(filepath.Join("roms", "NES", "Collection"))

	pl.On("Settings").Return(platforms.Settings{ZipsAsDirs: true})
	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, containerPath).
		Return((*database.Media)(nil), nil)
	mockDB.On("FindSingleDescendantMedia", mock.Anything, system.DBID, containerPath).
		Return((*database.Media)(nil), nil)

	env := makeResolveMediaEnv(mockDB, pl, nil, cfg)
	_, err := resolveMediaBySystemAndPath(&env, "NES", containerPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media not found")
	mockDB.AssertExpectations(t)
}

func TestResolveMediaBySystemAndPath_SingletonContainerFallbackDisabled(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	system := database.System{DBID: 10, SystemID: "NES", Name: "Nintendo Entertainment System"}
	containerPath := filepath.ToSlash(filepath.Join("roms", "NES", "Game.zip"))

	pl.On("Settings").Return(platforms.Settings{ZipsAsDirs: false})
	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, containerPath).
		Return((*database.Media)(nil), nil)

	env := makeResolveMediaEnv(mockDB, pl, nil, cfg)
	_, err := resolveMediaBySystemAndPath(&env, "NES", containerPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media not found")
	mockDB.AssertNotCalled(t, "FindSingleDescendantMedia", mock.Anything, system.DBID, containerPath)
	mockDB.AssertExpectations(t)
}

func TestResolveMediaBySystemAndPath_URIDoesNotUseRelativeFallback(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	system := database.System{DBID: 10, SystemID: "Steam", Name: "Steam"}
	launcherCache := makeResolveLauncherCache([]platforms.Launcher{
		{ID: "steam", SystemID: "Steam", Folders: []string{"Steam"}},
	})
	mediaPath := "steam://440/Team%20Fortress%202"

	pl.On("Settings").Return(platforms.Settings{})
	mockDB.On("FindSystemBySystemID", "Steam").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, mediaPath).
		Return((*database.Media)(nil), nil)

	env := makeResolveMediaEnv(mockDB, pl, launcherCache, cfg)
	_, err := resolveMediaBySystemAndPath(&env, "Steam", mediaPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media not found")
	mockDB.AssertExpectations(t)
	pl.AssertNotCalled(t, "RootDirs", mock.Anything)
}

func TestResolveMediaBySystemAndPath_MissingLauncherCacheSkipsRelativeFallback(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	system := database.System{DBID: 10, SystemID: "NES", Name: "Nintendo Entertainment System"}
	mediaPath := filepath.Join("NES", "mario.nes")

	pl.On("Settings").Return(platforms.Settings{})
	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, mediaPath).
		Return((*database.Media)(nil), nil)

	env := makeResolveMediaEnv(mockDB, pl, nil, cfg)
	_, err := resolveMediaBySystemAndPath(&env, "NES", mediaPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media not found")
	mockDB.AssertExpectations(t)
	pl.AssertNotCalled(t, "RootDirs", mock.Anything)
}

func TestResolveMediaBySystemAndPath_MalformedRelativePathSkipsFallback(t *testing.T) {
	t.Parallel()

	mockDB := testhelpers.NewMockMediaDBI()
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	system := database.System{DBID: 10, SystemID: "NES", Name: "Nintendo Entertainment System"}
	launcherCache := makeResolveLauncherCache([]platforms.Launcher{
		{ID: "nes", SystemID: "NES", Folders: []string{"NES"}},
	})
	mediaPath := "mario.nes"

	pl.On("Settings").Return(platforms.Settings{})
	mockDB.On("FindSystemBySystemID", "NES").Return(system, nil)
	mockDB.On("FindMediaBySystemAndPath", mock.Anything, system.DBID, mediaPath).
		Return((*database.Media)(nil), nil)

	env := makeResolveMediaEnv(mockDB, pl, launcherCache, cfg)
	_, err := resolveMediaBySystemAndPath(&env, "NES", mediaPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media not found")
	mockDB.AssertExpectations(t)
	pl.AssertNotCalled(t, "RootDirs", mock.Anything)
}
