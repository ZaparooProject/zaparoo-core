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

package zapscript

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	pathhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestCmdLaunch_RelativePathRoundTrip feeds the relativePath the API reports
// for a media file back into launch and expects the same file to start.
func TestCmdLaunch_RelativePathRoundTrip(t *testing.T) {
	t.Parallel()

	pathRoot := launchTestAbsPath("path-root")
	external := launchTestAbsPath("external", "MegaDrive")

	tests := []struct {
		name     string
		systemID string
		// file is what must exist on disk; mediaPath is the indexed path,
		// which differs for a file inside a zip.
		file      string
		mediaPath string
		wantRel   string
	}{
		{
			name:      "plain file",
			systemID:  "SNES",
			file:      filepath.Join(pathRoot, "Console", "SNES", "USA", "Game.sfc"),
			mediaPath: filepath.Join(pathRoot, "Console", "SNES", "USA", "Game.sfc"),
			wantRel:   "SNES/USA/Game.sfc",
		},
		{
			name:      "file with no extension",
			systemID:  "SNES",
			file:      filepath.Join(pathRoot, "Console", "SNES", "Homebrew", "Game Without Extension"),
			mediaPath: filepath.Join(pathRoot, "Console", "SNES", "Homebrew", "Game Without Extension"),
			wantRel:   "SNES/Homebrew/Game Without Extension",
		},
		{
			name:      "file inside a zip",
			systemID:  "Arcade",
			file:      filepath.Join(pathRoot, "_Arcade", "NEOGEO.zip"),
			mediaPath: filepath.Join(pathRoot, "_Arcade", "NEOGEO.zip", "mslug"),
			wantRel:   "Arcade/NEOGEO.zip/mslug",
		},
		{
			name:      "launcher with an absolute folder",
			systemID:  "Genesis",
			file:      filepath.Join(external, "Sonic.md"),
			mediaPath: filepath.Join(external, "Sonic.md"),
			wantRel:   "Genesis/Sonic.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := helpers.NewMemoryFS()
			require.NoError(t, fs.WriteFile(tt.file, []byte("rom"), 0o600))

			launchers := []platforms.Launcher{
				{ID: "snes", SystemID: "SNES", Folders: []string{filepath.Join("Console", "SNES")}},
				{ID: "arcade", SystemID: "Arcade", Folders: []string{"_Arcade"}},
				{ID: "genesis", SystemID: "Genesis", Folders: []string{external}},
			}
			launcherCache := &pathhelpers.LauncherCache{}
			launcherCache.InitializeFromSlice(launchers)
			rel := launcherCache.ToRelativePath([]string{pathRoot}, tt.systemID, tt.mediaPath)
			require.Equal(t, tt.wantRel, rel)

			cfg := &config.Instance{}
			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("Launchers", cfg).Return(launchers)
			mockPlatform.On("RootDirs", cfg).Return([]string{})
			mockPlatform.On("Settings").Return(platforms.Settings{DataDir: launchTestAbsPath("data")}).Maybe()
			mockPlatform.On(
				"LaunchMedia", cfg, tt.mediaPath, mock.Anything, mock.Anything, mock.Anything,
			).Return(nil).Once()

			env := platforms.CmdEnv{
				Cmd:      zapscript.Command{Name: "launch", Args: []string{rel}},
				Cfg:      cfg,
				PathRoot: pathRoot,
			}

			result, err := cmdLaunchWithFS(fs.Fs, mockPlatform, env)

			require.NoError(t, err)
			assert.True(t, result.MediaChanged)
			mockPlatform.AssertExpectations(t)
		})
	}
}

// TestCmdLaunch_SystemFolderNamedLikeTitleStaysTitle keeps a directory under
// the launcher folder from being launched as a file: a game's own folder name
// still resolves through the title format.
func TestCmdLaunch_SystemFolderNamedLikeTitleStaysTitle(t *testing.T) {
	t.Parallel()

	pathRoot := launchTestAbsPath("path-root")
	gameDir := filepath.Join(pathRoot, "Console", "SNES", "Super Mario World")
	titlePath := filepath.Join(gameDir, "Super Mario World.sfc")
	fs := helpers.NewMemoryFS()
	require.NoError(t, fs.WriteFile(titlePath, []byte("rom"), 0o600))

	cfg := &config.Instance{}
	launchers := []platforms.Launcher{
		{ID: "snes", SystemID: "SNES", Folders: []string{filepath.Join("Console", "SNES")}},
	}

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Return(launchers)
	mockPlatform.On("RootDirs", cfg).Return([]string{})
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: launchTestAbsPath("data")}).Maybe()
	mockPlatform.On(
		"LaunchMedia", cfg, titlePath, mock.Anything, mock.Anything, mock.Anything,
	).Return(nil).Once()

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "SNES", "supermarioworld", mock.Anything).
		Return(database.SlugResolution{}, false)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "SNES", "supermarioworld", mock.Anything).
		Return([]database.SearchResultWithCursor{
			{Path: titlePath, SystemID: "SNES", Name: "Super Mario World"},
		}, nil)
	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Maybe()

	env := platforms.CmdEnv{
		Cmd:      zapscript.Command{Name: "launch", Args: []string{"SNES/Super Mario World"}},
		Cfg:      cfg,
		PathRoot: pathRoot,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdLaunchWithFS(fs.Fs, mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
	mockMediaDB.AssertExpectations(t)
}

// TestCmdLaunch_SystemPathReportsOwningSystem checks which system a
// <system>/<path> launch is attributed to when the file is found through a
// fallback system's launcher folder.
func TestCmdLaunch_SystemPathReportsOwningSystem(t *testing.T) {
	t.Parallel()

	pathRoot := launchTestAbsPath("path-root")

	tests := []struct {
		name       string
		file       string
		arg        string
		wantSystem string
		launchers  []platforms.Launcher
	}{
		{
			name: "file in the fallback system's folder",
			launchers: []platforms.Launcher{
				{ID: "amiga500", SystemID: systemdefs.SystemAmiga500, Folders: []string{"A500"}},
				{ID: "amiga", SystemID: systemdefs.SystemAmiga, Folders: []string{"Amiga"}},
			},
			file:       filepath.Join(pathRoot, "Amiga", "Game.adf"),
			arg:        systemdefs.SystemAmiga500 + "/Game.adf",
			wantSystem: systemdefs.SystemAmiga,
		},
		{
			name: "file with no extension in the fallback system's folder",
			launchers: []platforms.Launcher{
				{ID: "amiga500", SystemID: systemdefs.SystemAmiga500, Folders: []string{"A500"}},
				{ID: "amiga", SystemID: systemdefs.SystemAmiga, Folders: []string{"Amiga"}},
			},
			file:       filepath.Join(pathRoot, "Amiga", "Sub", "Game"),
			arg:        systemdefs.SystemAmiga500 + "/Sub/Game",
			wantSystem: systemdefs.SystemAmiga,
		},
		{
			name: "file in the requested system's folder",
			launchers: []platforms.Launcher{
				{ID: "amiga500", SystemID: systemdefs.SystemAmiga500, Folders: []string{"A500"}},
				{ID: "amiga", SystemID: systemdefs.SystemAmiga, Folders: []string{"Amiga"}},
			},
			file:       filepath.Join(pathRoot, "A500", "Game.adf"),
			arg:        systemdefs.SystemAmiga500 + "/Game.adf",
			wantSystem: systemdefs.SystemAmiga500,
		},
		{
			name: "folder shared with the fallback system",
			launchers: []platforms.Launcher{
				{ID: "amiga", SystemID: systemdefs.SystemAmiga, Folders: []string{"Amiga"}},
				{ID: "amiga500", SystemID: systemdefs.SystemAmiga500, Folders: []string{"Amiga"}},
			},
			file:       filepath.Join(pathRoot, "Amiga", "Game.adf"),
			arg:        systemdefs.SystemAmiga500 + "/Game.adf",
			wantSystem: systemdefs.SystemAmiga500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := helpers.NewMemoryFS()
			require.NoError(t, fs.WriteFile(tt.file, []byte("rom"), 0o600))

			cfg := &config.Instance{}
			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("Launchers", cfg).Return(tt.launchers)
			mockPlatform.On("RootDirs", cfg).Return([]string{})
			mockPlatform.On("Settings").Return(platforms.Settings{DataDir: launchTestAbsPath("data")}).Maybe()
			mockPlatform.On(
				"LaunchMedia", cfg, tt.file, mock.Anything, mock.Anything, mock.Anything,
			).Return(nil).Once()

			var resolved []platforms.ResolvedLaunch
			env := platforms.CmdEnv{
				Cmd:      zapscript.Command{Name: "launch", Args: []string{tt.arg}},
				Cfg:      cfg,
				PathRoot: pathRoot,
				PrepareMediaLaunch: func(launch platforms.ResolvedLaunch) (bool, error) {
					resolved = append(resolved, launch)
					return true, nil
				},
			}

			result, err := cmdLaunchWithFS(fs.Fs, mockPlatform, env)

			require.NoError(t, err)
			assert.True(t, result.MediaChanged)
			require.Len(t, resolved, 1)
			assert.Equal(t, tt.wantSystem, resolved[0].SystemID)
			assert.Equal(t, tt.file, resolved[0].Path)
			mockPlatform.AssertExpectations(t)
		})
	}
}

// A <system>/<path> launch names a file below one of that system's launcher
// folders. A path that climbs out of the folder names a file somewhere else,
// which the format does not reach.
func TestFindSystemFile_StaysInsideTheLauncherFolder(t *testing.T) {
	t.Parallel()

	pathRoot := launchTestAbsPath("path-root")
	inside := filepath.Join(pathRoot, "A500", "Sub", "Game.adf")
	otherSystem := filepath.Join(pathRoot, "Other", "Secret.adf")
	aboveRoot := filepath.Join(filepath.Dir(pathRoot), "Outside.adf")
	fs := helpers.NewMemoryFS()
	for _, file := range []string{inside, otherSystem, aboveRoot} {
		require.NoError(t, fs.WriteFile(file, []byte("rom"), 0o600))
	}

	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{
		{ID: "amiga500", SystemID: systemdefs.SystemAmiga500, Folders: []string{"A500"}},
	})
	mockPlatform.On("RootDirs", cfg).Return([]string{})
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: launchTestAbsPath("data")}).Maybe()
	env := &platforms.CmdEnv{Cfg: cfg, PathRoot: pathRoot}
	system, err := systemdefs.LookupSystem(systemdefs.SystemAmiga500)
	require.NoError(t, err)

	path, _, found := findSystemFile(fs.Fs, mockPlatform, env, system, filepath.Join("Sub", "Game.adf"))
	require.True(t, found)
	assert.Equal(t, inside, path)
	path, _, found = findSystemFile(fs.Fs, mockPlatform, env, system, filepath.Join("Sub", "..", "Sub", "Game.adf"))
	require.True(t, found, "a path that comes back inside the folder is still inside it")
	assert.Equal(t, inside, path)

	for _, escape := range []string{
		filepath.Join("..", "Other", "Secret.adf"),
		filepath.Join("Sub", "..", "..", "Other", "Secret.adf"),
		filepath.Join("..", "..", "Outside.adf"),
	} {
		_, _, found = findSystemFile(fs.Fs, mockPlatform, env, system, escape)
		assert.False(t, found, escape)
	}
}

// A relative path is looked up below each games folder. One that climbs out
// with ".." names a file outside every folder a relative path can reach.
func TestFindFile_RelativePathStaysInsideItsRoot(t *testing.T) {
	t.Parallel()

	root := launchTestAbsPath("games")
	inside := filepath.Join(root, "NES", "Game.nes")
	outside := filepath.Join(filepath.Dir(root), "zaparoo", "config.toml")
	fs := helpers.NewMemoryFS()
	for _, file := range []string{inside, outside} {
		require.NoError(t, fs.WriteFile(file, []byte("x"), 0o600))
	}
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("RootDirs", cfg).Return([]string{root})

	found, err := findFile(fs.Fs, mockPlatform, cfg, filepath.Join("PSX", "..", "NES", "Game.nes"))
	require.NoError(t, err, "a path that stays below the games folder is an ordinary relative path")
	assert.Equal(t, inside, found)

	for _, escape := range []string{
		filepath.Join("..", "zaparoo", "config.toml"),
		filepath.Join("NES", "..", "..", "zaparoo", "config.toml"),
	} {
		_, err = findFile(fs.Fs, mockPlatform, cfg, escape)
		require.ErrorIs(t, err, ErrFileNotFound, escape)
	}

	// An absolute path is the caller naming the file outright, and is unchanged.
	found, err = findFile(fs.Fs, mockPlatform, cfg, outside)
	require.NoError(t, err)
	assert.Equal(t, outside, found)
}

// A case-insensitive filesystem opens a file under any spelling of its path.
// The launch carries on with the spelling the index holds, so what is
// launched is the media the index knows.
func TestIndexedPathSpelling(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	indexed := "/games/PSX/Crash Bandicoot (USA)/Crash Bandicoot (USA).chd"
	scantest.IndexMediaPaths(t, mediaDB, systemdefs.SystemPSX, indexed)
	cfg := &config.Instance{}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{
		{ID: "psx", SystemID: systemdefs.SystemPSX, Folders: []string{"/games/PSX"}, Extensions: []string{".chd"}},
	})
	mockPlatform.On("RootDirs", cfg).Return([]string{}).Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{}).Maybe()
	env := &platforms.CmdEnv{Cfg: cfg, Database: &database.Database{MediaDB: mediaDB}}
	typed := filepath.FromSlash("/games/psx/crash bandicoot (usa)/crash bandicoot (usa).chd")

	assert.Equal(t, filepath.FromSlash(indexed), indexedPathSpelling(mockPlatform, env, systemdefs.SystemPSX, typed))
	assert.Equal(t, filepath.FromSlash(indexed),
		indexedPathSpelling(mockPlatform, env, "", filepath.FromSlash(indexed)),
		"a path that is already the indexed one is returned as given")
	unindexed := filepath.FromSlash("/games/psx/not indexed.chd")
	assert.Equal(t, unindexed, indexedPathSpelling(mockPlatform, env, systemdefs.SystemPSX, unindexed))
	assert.Equal(t, typed, indexedPathSpelling(mockPlatform, &platforms.CmdEnv{Cfg: cfg}, systemdefs.SystemPSX, typed),
		"no index, no other spelling to follow")
}

// A tag usually names a Steam game by ID alone, and the index holds it as
// steam://id/name. The launch carries on with the indexed path.
func TestIndexedVirtualPath(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	scantest.IndexMediaPaths(t, mediaDB, systemdefs.SystemPC, "steam://620/Portal%202", "steam://6200/Ghost%20Master")
	env := &platforms.CmdEnv{Cfg: &config.Instance{}, Database: &database.Database{MediaDB: mediaDB}}

	assert.Equal(t, "steam://620/Portal%202", indexedVirtualPath(env, "steam://620"))
	assert.Equal(t, "steam://620/Portal%202", indexedVirtualPath(env, "steam://620/"))
	for _, unchanged := range []string{
		"steam://620/Portal%202",
		"steam://999",
		"steam://rungameid/620",
		"https://620",
		"steam://62",
	} {
		assert.Equal(t, unchanged, indexedVirtualPath(env, unchanged))
	}
	assert.Equal(t, "steam://620", indexedVirtualPath(&platforms.CmdEnv{}, "steam://620"))
}
