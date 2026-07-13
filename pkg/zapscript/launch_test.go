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
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	platformshared "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func launchTestAbsPath(parts ...string) string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	root := filepath.VolumeName(wd) + string(filepath.Separator)
	return filepath.Join(append([]string{root}, parts...)...)
}

func TestMediaIDForHistoryEntry_ResolvesMedia(t *testing.T) {
	t.Parallel()

	mediaPath := filepath.Join("games", "mario.nes")
	entry := database.MediaHistoryEntry{SystemID: "nes", MediaPath: mediaPath}
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("FindSystemBySystemID", "nes").
		Return(database.System{DBID: 99, SystemID: "nes"}, nil).Once()
	mockMediaDB.On("FindMediaBySystemAndPath", mock.Anything, int64(99), mediaPath).
		Return(&database.Media{DBID: 123}, nil).Once()
	env := platforms.CmdEnv{Database: &database.Database{MediaDB: mockMediaDB}}

	assert.Equal(t, int64(123), mediaIDForHistoryEntry(&env, &entry))
	mockMediaDB.AssertExpectations(t)
}

func TestMediaIDForHistoryEntry_ReturnsZeroWhenUnavailable(t *testing.T) {
	t.Parallel()

	mediaPath := filepath.Join("games", "mario.nes")
	entry := database.MediaHistoryEntry{SystemID: "nes", MediaPath: mediaPath}
	dbErr := errors.New("database unavailable")

	tests := []struct {
		env  func(t *testing.T) platforms.CmdEnv
		name string
	}{
		{
			name: "nil database",
			env: func(t *testing.T) platforms.CmdEnv {
				t.Helper()
				return platforms.CmdEnv{}
			},
		},
		{
			name: "system missing",
			env: func(t *testing.T) platforms.CmdEnv {
				t.Helper()
				mockMediaDB := helpers.NewMockMediaDBI()
				mockMediaDB.On("FindSystemBySystemID", "nes").
					Return(database.System{}, sql.ErrNoRows).Once()
				t.Cleanup(func() { mockMediaDB.AssertExpectations(t) })
				return platforms.CmdEnv{Database: &database.Database{MediaDB: mockMediaDB}}
			},
		},
		{
			name: "system lookup error",
			env: func(t *testing.T) platforms.CmdEnv {
				t.Helper()
				mockMediaDB := helpers.NewMockMediaDBI()
				mockMediaDB.On("FindSystemBySystemID", "nes").
					Return(database.System{}, dbErr).Once()
				t.Cleanup(func() { mockMediaDB.AssertExpectations(t) })
				return platforms.CmdEnv{Database: &database.Database{MediaDB: mockMediaDB}}
			},
		},
		{
			name: "media missing",
			env: func(t *testing.T) platforms.CmdEnv {
				t.Helper()
				mockMediaDB := helpers.NewMockMediaDBI()
				mockMediaDB.On("FindSystemBySystemID", "nes").
					Return(database.System{DBID: 99, SystemID: "nes"}, nil).Once()
				mockMediaDB.On("FindMediaBySystemAndPath", mock.Anything, int64(99), mediaPath).
					Return(nil, nil).Once()
				t.Cleanup(func() { mockMediaDB.AssertExpectations(t) })
				return platforms.CmdEnv{Database: &database.Database{MediaDB: mockMediaDB}}
			},
		},
		{
			name: "media lookup error",
			env: func(t *testing.T) platforms.CmdEnv {
				t.Helper()
				mockMediaDB := helpers.NewMockMediaDBI()
				mockMediaDB.On("FindSystemBySystemID", "nes").
					Return(database.System{DBID: 99, SystemID: "nes"}, nil).Once()
				mockMediaDB.On("FindMediaBySystemAndPath", mock.Anything, int64(99), mediaPath).
					Return(nil, dbErr).Once()
				t.Cleanup(func() { mockMediaDB.AssertExpectations(t) })
				return platforms.CmdEnv{Database: &database.Database{MediaDB: mockMediaDB}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.env(t)
			assert.Zero(t, mediaIDForHistoryEntry(&env, &entry))
		})
	}
}

func TestVirtualStatPath_PreservesAbsoluteRoot(t *testing.T) {
	t.Parallel()

	lookupPath := filepath.Join(launchTestAbsPath("games"), "neogeo", "NEOGEO.zip", "game.neo")
	parts := strings.Split(lookupPath, string(filepath.Separator))

	statPath := virtualStatPath(lookupPath, parts, len(parts)-1)

	assert.True(t, filepath.IsAbs(statPath))
	assert.Equal(t, filepath.Join(launchTestAbsPath("games"), "neogeo", "NEOGEO.zip"), statPath)
}

// TestApplyMediaLauncherOverride_SetsLauncherArg verifies that a media override sets the launcher AdvArg.
func TestApplyMediaLauncherOverride_SetsLauncherArg(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockMediaDB := helpers.NewMockMediaDBI()
	cfg := &config.Instance{}
	launchers := []platforms.Launcher{
		{ID: "Default", SystemID: "NES"},
		{ID: "RetroArch", SystemID: "NES"},
	}
	mockPlatform.On("Launchers", cfg).Return(launchers)
	mockMediaDB.On("GetMediaPropertyMetadata", mock.Anything, int64(123)).
		Return([]database.MediaProperty{{
			TypeTag: launcherOverridePropertyTypeTag(),
			Text:    "retroarch",
		}}, nil).Once()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.title",
			AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	launcherID := applyMediaLauncherOverride(mockPlatform, &env, 123, "NES")

	assert.Equal(t, "RetroArch", launcherID)
	assert.Equal(t, "RetroArch", env.Cmd.AdvArgs.Get(zapscript.KeyLauncher))
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_AbsolutePathAppliesMediaLauncherOverride(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockMediaDB := helpers.NewMockMediaDBI()
	db := &database.Database{MediaDB: mockMediaDB}
	cfg := &config.Instance{}
	root := t.TempDir()
	absPath := filepath.Join(root, "game.nes")
	launchers := []platforms.Launcher{
		{ID: "Default", SystemID: "NES", Folders: []string{root}, Extensions: []string{".nes"}},
		{ID: "Override", SystemID: "NES"},
	}

	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()}).Maybe()
	mockPlatform.On("RootDirs", cfg).Return([]string{root}).Maybe()
	mockPlatform.On("Launchers", cfg).Return(launchers)
	mockMediaDB.On("FindSystemBySystemID", "NES").
		Return(database.System{DBID: 10, SystemID: "NES"}, nil).Once()
	mockMediaDB.On("FindMediaBySystemAndPath", mock.Anything, int64(10), absPath).
		Return(&database.Media{DBID: 123, Path: absPath}, nil).Once()
	mockMediaDB.On("GetMediaPropertyMetadata", mock.Anything, int64(123)).
		Return([]database.MediaProperty{{
			TypeTag: launcherOverridePropertyTypeTag(),
			Text:    "Override",
		}}, nil).Once()
	mockPlatform.On("LaunchMedia", cfg, absPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "Override"
		}),
		db,
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch",
			Args:    []string{absPath},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
		},
		Cfg:      cfg,
		Database: db,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_AbsolutePathExplicitLauncherOverridesMediaOverride(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockMediaDB := helpers.NewMockMediaDBI()
	db := &database.Database{MediaDB: mockMediaDB}
	cfg := &config.Instance{}
	root := t.TempDir()
	absPath := filepath.Join(root, "game.nes")
	explicit := platforms.Launcher{ID: "Explicit", SystemID: "NES", Folders: []string{root}}

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{explicit})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "Explicit"
		}),
		db,
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"launcher": "Explicit",
			}),
		},
		Cfg:      cfg,
		Database: db,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_SystemArgAppliesDefaults(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "genesis-retroarch"
`))

	genesisLauncher := platforms.Launcher{
		ID:       "genesis-retroarch",
		SystemID: "genesis",
		Launch: func(_ *config.Instance, _ string, _ *platforms.LaunchOptions) (*os.Process, error) {
			return &os.Process{}, nil
		},
	}

	// Use a platform-specific absolute path
	absPath := filepath.Join(t.TempDir(), "game.bin")

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{genesisLauncher})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-retroarch"
		}),
		(*database.Database)(nil),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"system": "genesis",
			}),
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err, "cmdLaunch should not return error with valid system arg")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")
	mockPlatform.AssertExpectations(t)
}

// TestCmdLaunch_LauncherArgOverridesSystemArg verifies launcher arg takes precedence over system arg
func TestCmdLaunch_LauncherArgOverridesSystemArg(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "genesis-default"
`))

	explicitLauncher := platforms.Launcher{
		ID:       "genesis-explicit",
		SystemID: "genesis",
		Launch: func(_ *config.Instance, _ string, _ *platforms.LaunchOptions) (*os.Process, error) {
			return &os.Process{}, nil
		},
	}

	// Use a platform-specific absolute path
	absPath := filepath.Join(t.TempDir(), "game.bin")

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{explicitLauncher})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-explicit"
		}),
		(*database.Database)(nil),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"system":   "genesis",
				"launcher": "genesis-explicit", // Explicit launcher should win
			}),
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err, "cmdLaunch should not return error")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_InheritsCurrentPlaylistBackgroundSlot(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	absPath := filepath.Join(t.TempDir(), "song.mp3")

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil),
		(*database.Database)(nil),
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			return opts != nil && opts.Slot == mediaslot.Background
		}),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{absPath},
		},
		Cfg: cfg,
		Playlist: playlists.PlaylistController{
			Current: &playlists.Playlist{Slot: mediaslot.Background},
		},
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_AbsolutePathAppliesInferredSystemDefault(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	rootDir := t.TempDir()
	romPath := filepath.Join(rootDir, "GENESIS", "game.bin")

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "genesis-alt"
`))

	baseLauncher := platforms.Launcher{
		ID:         "genesis-base",
		SystemID:   "genesis",
		Folders:    []string{"GENESIS"},
		Extensions: []string{".bin"},
	}
	altLauncher := platforms.Launcher{
		ID:         "genesis-alt",
		SystemID:   "genesis",
		Folders:    []string{"GENESIS"},
		Extensions: []string{".bin"},
	}
	launchers := []platforms.Launcher{baseLauncher, altLauncher}

	mockPlatform.On("Launchers", cfg).Return(launchers)
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: rootDir}).Maybe()
	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-alt"
		}),
		(*database.Database)(nil),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{romPath},
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_AbsolutePathExplicitLauncherOverridesInferredDefault(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	rootDir := t.TempDir()
	romPath := filepath.Join(rootDir, "GENESIS", "game.bin")

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "genesis-alt"
`))

	explicitLauncher := platforms.Launcher{
		ID:         "genesis-explicit",
		SystemID:   "genesis",
		Folders:    []string{"GENESIS"},
		Extensions: []string{".bin"},
	}
	altLauncher := platforms.Launcher{
		ID:         "genesis-alt",
		SystemID:   "genesis",
		Folders:    []string{"GENESIS"},
		Extensions: []string{".bin"},
	}
	launchers := []platforms.Launcher{explicitLauncher, altLauncher}

	mockPlatform.On("Launchers", cfg).Return(launchers)
	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-explicit"
		}),
		(*database.Database)(nil),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{romPath},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"launcher": "genesis-explicit",
			}),
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_SystemDefaultGroupResolvesWithinTargetSystem(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	rootDir := t.TempDir()
	romPath := filepath.Join(rootDir, "GENESIS", "game.bin")

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "RA"
`))

	baseLauncher := platforms.Launcher{
		ID:         "genesis-base",
		SystemID:   "genesis",
		Folders:    []string{"GENESIS"},
		Extensions: []string{".bin"},
	}
	nesRALauncher := platforms.Launcher{ID: "RANES", SystemID: "nes", Groups: []string{"RA"}}
	genesisRALauncher := platforms.Launcher{ID: "RAGenesis", SystemID: "genesis", Groups: []string{"RA"}}
	launchers := []platforms.Launcher{baseLauncher, nesRALauncher, genesisRALauncher}

	mockPlatform.On("Launchers", cfg).Return(launchers)
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: rootDir}).Maybe()
	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "RAGenesis"
		}),
		(*database.Database)(nil),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{romPath},
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestResolveLauncherRefForSystem_SkipsWrongSystemIDMatch(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	launchers := []platforms.Launcher{
		{ID: "shared-id", SystemID: "nes"},
		{ID: "other", SystemID: "genesis"},
	}
	mockPlatform.On("Launchers", cfg).Return(launchers)

	launcherID, found := resolveLauncherRefForSystem(mockPlatform, &platforms.CmdEnv{Cfg: cfg}, "shared-id", "genesis")

	assert.False(t, found)
	assert.Empty(t, launcherID)
	mockPlatform.AssertExpectations(t)
}

func TestApplySystemDefaultLauncher_UsesOrderedGlobalPreference(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[launchers]
preference = ["Native", "RetroDECK"]
`))
	launchers := []platforms.Launcher{
		{
			ID: "NativeSNES", SystemID: "SNES", Groups: []string{platformshared.LauncherGroupNative},
			Availability: func(*config.Instance) error { return errors.New("unavailable") },
		},
		{ID: "RetroDECKSNES", SystemID: "SNES", Groups: []string{platformshared.LauncherGroupRetroDECK}},
	}
	mockPlatform.On("Launchers", cfg).Once().Return(launchers)
	env := platforms.CmdEnv{Cfg: cfg, Cmd: zapscript.Command{AdvArgs: zapscript.NewAdvArgs(nil)}}

	launcherID := applySystemDefaultLauncher(mockPlatform, &env, "SNES")

	assert.Equal(t, "RetroDECKSNES", launcherID)
	assert.Equal(t, "RetroDECKSNES", env.Cmd.AdvArgs.Get(zapscript.KeyLauncher))
	mockPlatform.AssertExpectations(t)
}

func TestApplySystemDefaultLauncher_SystemDefaultBeatsGlobalPreference(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[launchers]
preference = ["Native"]

[[systems.default]]
system = "SNES"
launcher = "RetroDECK"
`))
	launchers := []platforms.Launcher{
		{ID: "NativeSNES", SystemID: "SNES", Groups: []string{platformshared.LauncherGroupNative}},
		{ID: "RetroDECKSNES", SystemID: "SNES", Groups: []string{platformshared.LauncherGroupRetroDECK}},
	}
	mockPlatform.On("Launchers", cfg).Once().Return(launchers)
	env := platforms.CmdEnv{Cfg: cfg, Cmd: zapscript.Command{AdvArgs: zapscript.NewAdvArgs(nil)}}

	launcherID := applySystemDefaultLauncher(mockPlatform, &env, "SNES")

	assert.Equal(t, "RetroDECKSNES", launcherID)
	mockPlatform.AssertExpectations(t)
}

func TestLaunchClosurePreservesExplicitLauncher(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	launcher := platforms.Launcher{ID: "ExplicitSNES", SystemID: "SNES"}
	path := filepath.Join("games", "game.sfc")
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Once().Return([]platforms.Launcher{launcher})
	mockPlatform.On("LaunchMedia", cfg, path,
		mock.MatchedBy(func(selected *platforms.Launcher) bool {
			return selected != nil && selected.ID == launcher.ID
		}), (*database.Database)(nil), (*platforms.LaunchOptions)(nil)).Return(nil)
	env := platforms.CmdEnv{
		Cfg: cfg,
		Cmd: zapscript.Command{AdvArgs: zapscript.NewAdvArgs(map[string]string{
			string(zapscript.KeyLauncher): launcher.ID,
		})},
	}

	launch := getLaunchClosure(mockPlatform, &env, true)
	require.NoError(t, launch(launchTarget{path: path, systemID: "SNES"}))
	mockPlatform.AssertExpectations(t)
}

func TestLaunchClosureAppliesSystemDefault(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "SNES"
launcher = "Native"
`))
	launcher := platforms.Launcher{
		ID: "NativeSNES", SystemID: "SNES", Groups: []string{platformshared.LauncherGroupNative},
	}
	path := filepath.Join("games", "game.sfc")
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Twice().Return([]platforms.Launcher{launcher})
	mockPlatform.On("LaunchMedia", cfg, path,
		mock.MatchedBy(func(selected *platforms.Launcher) bool {
			return selected != nil && selected.ID == launcher.ID
		}), (*database.Database)(nil), (*platforms.LaunchOptions)(nil)).Return(nil)
	env := platforms.CmdEnv{
		Cfg: cfg,
		Cmd: zapscript.Command{AdvArgs: zapscript.NewAdvArgs(nil)},
	}

	launch := getLaunchClosure(mockPlatform, &env, false)
	require.NoError(t, launch(launchTarget{path: path, systemID: "SNES"}))
	assert.Equal(t, launcher.ID, env.Cmd.AdvArgs.Get(zapscript.KeyLauncher))
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_SetNameArgsPassedThrough(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	absPath := filepath.Join(t.TempDir(), "game.nes")

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil), (*database.Database)(nil),
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			return opts != nil &&
				opts.SetName == "RA_NES" &&
				opts.SetNameSameDir == "notabool" &&
				opts.Action == ""
		})).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"set_name":          "RA_NES",
				"set_name_same_dir": "notabool",
			}),
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

// TestCmdLaunch_InvalidSystemArgReturnsError verifies invalid system returns validation error
func TestCmdLaunch_InvalidSystemArgReturnsError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// Mock Launchers for advargs validation context
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	// Use a platform-specific absolute path
	absPath := filepath.Join(t.TempDir(), "game.bin")

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"system": "invalidname", // Invalid system should return validation error
			}),
		},
		Cfg: cfg,
	}

	_, err := cmdLaunch(mockPlatform, env)

	require.Error(t, err, "cmdLaunch should return error for invalid system")
	assert.Contains(t, err.Error(), "invalidname", "error should mention invalid system name")
}

// TestCmdLaunch_SystemArgWithNoDefaults verifies system with no configured defaults works
func TestCmdLaunch_SystemArgWithNoDefaults(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// Mock Launchers for advargs validation context
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	// Use a platform-specific absolute path
	absPath := filepath.Join(t.TempDir(), "game.bin")
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil), (*database.Database)(nil),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"system": "genesis",
			}),
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err, "cmdLaunch should work with valid system but no defaults")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_DelegationToTitlePreservesLauncher(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "snes"
launcher = "snes-retroarch"
`))

	snesLauncher := platforms.Launcher{
		ID:       "snes-retroarch",
		SystemID: "SNES",
		Launch: func(_ *config.Instance, _ string, _ *platforms.LaunchOptions) (*os.Process, error) {
			return &os.Process{}, nil
		},
	}

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{snesLauncher})
	mockPlatform.On("RootDirs", cfg).Return([]string{})
	mockPlatform.On("LaunchMedia", cfg, mock.AnythingOfType("string"),
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "snes-retroarch"
		}),
		mock.Anything,
		mock.Anything,
	).Return(nil)

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetCachedSlugResolution",
		mock.Anything, "SNES", "supermarioworld", mock.Anything).
		Return(int64(0), "", false)
	mockMediaDB.On("SearchMediaBySlug",
		mock.Anything, "SNES", "supermarioworld", mock.Anything).
		Return([]database.SearchResultWithCursor{
			{Path: "/games/snes/Super Mario World.sfc", SystemID: "SNES", Name: "Super Mario World"},
		}, nil)
	mockMediaDB.On("SetCachedSlugResolution",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Maybe()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{"snes/Super Mario World"},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err, "cmdLaunch should not error for title format delegation")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_SystemPathFormatUsesDefaultLauncher(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()

	// Create temp dir with a game file
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "game.sfc"), []byte("test"), 0o600))

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "snes"
launcher = "snes-retroarch"
`))

	snesLauncher := platforms.Launcher{
		ID:       "snes-retroarch",
		SystemID: "SNES",
		Folders:  []string{tmpDir},
		Launch: func(_ *config.Instance, _ string, _ *platforms.LaunchOptions) (*os.Process, error) {
			return &os.Process{}, nil
		},
	}

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{snesLauncher})
	mockPlatform.On("RootDirs", cfg).Return([]string{})
	mockPlatform.On("LaunchMedia", cfg, mock.AnythingOfType("string"),
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "snes-retroarch"
		}),
		(*database.Database)(nil),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{"snes/game.sfc"},
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err, "cmdLaunch should not error for valid system/path format")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")
	mockPlatform.AssertExpectations(t)
}

func TestFindFile_ResolvesCaseInsensitiveVirtualZipPath(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	fs := afero.NewMemMapFs()
	rootDir := filepath.Join(t.TempDir(), "games")
	virtualGame := "Neo Turf Masters (turfmast).neo"
	zipPath := filepath.Join(rootDir, "NEOGEO", "NEOGEO.zip")
	relativePath := filepath.Join("NeoGeo", "NEOGEO.zip", virtualGame)
	expectedPath := filepath.Join(rootDir, "NEOGEO", "NEOGEO.zip", virtualGame)

	require.NoError(t, fs.MkdirAll(filepath.Dir(zipPath), 0o700))
	require.NoError(t, afero.WriteFile(fs, zipPath, []byte("test"), 0o600))
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})

	result, err := findFile(fs, mockPlatform, cfg, relativePath)

	require.NoError(t, err)
	assert.Equal(t, expectedPath, result)
	mockPlatform.AssertExpectations(t)
}

func TestFindFile_ResolvesCaseInsensitiveAbsoluteVirtualZipPath(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	fs := afero.NewMemMapFs()
	rootDir := launchTestAbsPath("games")
	virtualGame := "Neo Turf Masters (turfmast).neo"
	zipPath := filepath.Join(rootDir, "NEOGEO", "NEOGEO.zip")
	absolutePath := filepath.Join(rootDir, "neogeo", "NEOGEO.zip", virtualGame)
	expectedPath := filepath.Join(rootDir, "NEOGEO", "NEOGEO.zip", virtualGame)

	require.NoError(t, fs.MkdirAll(filepath.Dir(zipPath), 0o700))
	require.NoError(t, afero.WriteFile(fs, zipPath, []byte("test"), 0o600))

	result, err := findFile(fs, mockPlatform, cfg, absolutePath)

	require.NoError(t, err)
	assert.Equal(t, expectedPath, result)
	mockPlatform.AssertExpectations(t)
}

func TestFindFile_ResolvesCaseInsensitiveVirtualTxtPath(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	fs := afero.NewMemMapFs()
	rootDir := launchTestAbsPath("games")
	virtualGame := "Favorite Game.sfc"
	txtPath := filepath.Join(rootDir, "SNES", "Favorites.txt")
	relativePath := filepath.Join("snes", "Favorites.txt", virtualGame)
	expectedPath := filepath.Join(rootDir, "SNES", "Favorites.txt", virtualGame)

	require.NoError(t, fs.MkdirAll(filepath.Dir(txtPath), 0o700))
	require.NoError(t, afero.WriteFile(fs, txtPath, []byte("test"), 0o600))
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})

	result, err := findFile(fs, mockPlatform, cfg, relativePath)

	require.NoError(t, err)
	assert.Equal(t, expectedPath, result)
	mockPlatform.AssertExpectations(t)
}

func TestFindFile_ReturnsAmbiguousCaseInsensitivePathError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	fs := afero.NewMemMapFs()
	rootDir := launchTestAbsPath("games")
	relativePath := filepath.Join("neogeo", "game.zip")

	require.NoError(t, fs.MkdirAll(filepath.Join(rootDir, "NEOGEO"), 0o700))
	require.NoError(t, fs.MkdirAll(filepath.Join(rootDir, "NeoGeo"), 0o700))
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})

	result, err := findFile(fs, mockPlatform, cfg, relativePath)

	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "ambiguous case-insensitive path")
	mockPlatform.AssertExpectations(t)
}

// TestCmdLaunch_FileNotFound verifies that ErrFileNotFound is returned when file doesn't exist
func TestCmdLaunch_FileNotFound(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// Use empty folders so file lookup fails
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{
		{
			ID:       "snes-launcher",
			SystemID: "SNES",
			Folders:  []string{}, // No folders = can't find files
		},
	})
	mockPlatform.On("RootDirs", cfg).Return([]string{})

	mockMediaDB := helpers.NewMockMediaDBI()
	// Return empty results for the media search
	mockMediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.SearchResult{}, nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			// Use system/path.ext format with file extension to skip title search
			Args: []string{"snes/nonexistent_game.sfc"},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := cmdLaunch(mockPlatform, env)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrFileNotFound, "should return ErrFileNotFound for missing file")
	mockPlatform.AssertExpectations(t)
}

func TestMediaDBLookupContext_UsesTimeoutWithoutServiceContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := mediaDBLookupContext(&platforms.CmdEnv{})
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	assert.WithinDuration(t, time.Now().Add(mediaDBLookupTimeout), deadline, 200*time.Millisecond)
}

func TestMediaDBLookupContext_IgnoresCanceledLauncherContext(t *testing.T) {
	t.Parallel()

	launcherCtx, launcherCancel := context.WithCancel(context.Background())
	launcherCancel()
	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	defer serviceCancel()

	env := platforms.CmdEnv{
		LauncherCtx: launcherCtx,
		ServiceCtx:  serviceCtx,
	}
	ctx, cancel := mediaDBLookupContext(&env)
	defer cancel()

	select {
	case <-ctx.Done():
		t.Fatal("MediaDB lookup context should not use canceled launcher context")
	default:
	}

	serviceCancel()
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("MediaDB lookup context should follow service context")
	}
}

func TestCmdLaunch_ExactFallbackMediaDBLookupUsesServiceContext(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{
		{ID: "snes-launcher", SystemID: "snes", Folders: []string{}},
	})
	mockPlatform.On("RootDirs", cfg).Return([]string{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, "Sonic\\Game").
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			<-ctx.Done()
		}).
		Return([]database.SearchResult{}, context.DeadlineExceeded)

	serviceCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch",
			Args: []string{"snes/Sonic\\Game"},
		},
		Cfg:        cfg,
		ServiceCtx: serviceCtx,
		Database:   &database.Database{MediaDB: mockMediaDB},
	}

	started := time.Now()
	_, err := cmdLaunch(mockPlatform, env)

	require.Error(t, err)
	assert.Less(t, time.Since(started), 500*time.Millisecond)
	mockPlatform.AssertNotCalled(
		t, "LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	)
	mockMediaDB.AssertExpectations(t)
}

func TestCmdSearch_MediaDBLookupUsesServiceContext(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			<-ctx.Done()
		}).
		Return([]database.SearchResultWithCursor{}, context.DeadlineExceeded)

	serviceCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.search",
			Args: []string{"sonic"},
		},
		Cfg:        cfg,
		ServiceCtx: serviceCtx,
		Database:   &database.Database{MediaDB: mockMediaDB},
	}

	started := time.Now()
	_, err := cmdSearch(mockPlatform, env)

	require.Error(t, err)
	assert.Less(t, time.Since(started), 500*time.Millisecond)
	mockPlatform.AssertNotCalled(
		t, "LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	)
	mockMediaDB.AssertExpectations(t)
}

func TestCmdSearch_AppliesDefaultFromResultSystem(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "genesis-alt"
`))

	altLauncher := platforms.Launcher{ID: "genesis-alt", SystemID: "genesis"}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{altLauncher})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			return filters.Query == "sonic" && filters.Limit == 1
		}),
	).Return([]database.SearchResultWithCursor{
		{SystemID: "genesis", Path: filepath.Join(launchTestAbsPath("games"), "GENESIS", "Sonic.bin")},
	}, nil)

	romPath := filepath.Join(launchTestAbsPath("games"), "GENESIS", "Sonic.bin")
	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-alt"
		}),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.search",
			Args: []string{"sonic"},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdSearch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdSearch_AppliesMediaLauncherOverride(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	overrideLauncher := platforms.Launcher{ID: "genesis-override", SystemID: "genesis"}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{overrideLauncher})

	romPath := filepath.Join(launchTestAbsPath("games"), "GENESIS", "Sonic.bin")
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			return filters.Query == "sonic" && filters.Limit == 1
		}),
	).Return([]database.SearchResultWithCursor{
		{MediaID: 44, SystemID: "genesis", Path: romPath},
	}, nil).Once()
	mockMediaDB.On("GetMediaPropertyMetadata", mock.Anything, int64(44)).
		Return([]database.MediaProperty{{
			TypeTag: launcherOverridePropertyTypeTag(),
			Text:    "genesis-override",
		}}, nil).Once()

	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-override"
		}),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil).Once()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.search",
			Args: []string{"sonic"},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdSearch(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdRandom_MediaDBLookupUsesServiceContext(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			<-ctx.Done()
		}).
		Return(database.SearchResult{}, context.DeadlineExceeded)

	serviceCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{"all"},
		},
		Cfg:        cfg,
		ServiceCtx: serviceCtx,
		Database:   &database.Database{MediaDB: mockMediaDB},
	}

	started := time.Now()
	_, err := cmdRandom(mockPlatform, env)

	require.Error(t, err)
	assert.Less(t, time.Since(started), 500*time.Millisecond)
	mockPlatform.AssertNotCalled(
		t, "LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	)
	mockMediaDB.AssertExpectations(t)
}

func TestCmdRandom_AppliesDefaultFromResultSystem(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "genesis-alt"
`))

	altLauncher := platforms.Launcher{ID: "genesis-alt", SystemID: "genesis"}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{altLauncher})

	romPath := filepath.Join(launchTestAbsPath("games"), "GENESIS", "Sonic.bin")
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Return(database.SearchResult{SystemID: "genesis", Path: romPath}, nil)

	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-alt"
		}),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{"all"},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdRandom(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdRandom_AppliesMediaLauncherOverride(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	overrideLauncher := platforms.Launcher{ID: "genesis-override", SystemID: "genesis"}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{overrideLauncher})

	romPath := filepath.Join(launchTestAbsPath("games"), "GENESIS", "Sonic.bin")
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Return(database.SearchResult{MediaID: 55, SystemID: "genesis", Path: romPath}, nil).Once()
	mockMediaDB.On("GetMediaPropertyMetadata", mock.Anything, int64(55)).
		Return([]database.MediaProperty{{
			TypeTag: launcherOverridePropertyTypeTag(),
			Text:    "genesis-override",
		}}, nil).Once()

	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-override"
		}),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil).Once()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{"all"},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdRandom(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdRandom_AbsolutePathDBBackedGroupDefaultWithinResultSystem(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "RA"
`))

	launchers := []platforms.Launcher{
		{ID: "RA-NES", SystemID: "nes", Groups: []string{"RA"}},
		{ID: "genesis-explicit", SystemID: "genesis"},
		{ID: "RA-Genesis", SystemID: "genesis", Groups: []string{"RA"}},
	}
	mockPlatform.On("Launchers", cfg).Return(launchers)

	queryPath := filepath.Join(launchTestAbsPath("games"), "GENESIS")
	romPath := filepath.Join(queryPath, "Sonic.bin")
	wantPathPrefix := filepath.ToSlash(queryPath)
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery",
		mock.Anything,
		mock.MatchedBy(func(q *database.MediaQuery) bool {
			return q.PathPrefix == wantPathPrefix
		}),
	).Return(database.SearchResult{SystemID: "genesis", Path: romPath}, nil)

	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "RA-Genesis"
		}),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{queryPath},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdRandom(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdRandom_AbsolutePathFilesystemFallbackAppliesInferredGroupDefault(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	dir := filepath.Join(rootDir, "GENESIS")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	romPath := filepath.Join(dir, "Sonic.bin")
	require.NoError(t, os.WriteFile(romPath, []byte("x"), 0o600))
	wantPathPrefix := filepath.ToSlash(dir)

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[[systems.default]]
system = "genesis"
launcher = "RA"
`))

	launchers := []platforms.Launcher{
		{ID: "genesis-base", SystemID: "genesis", Folders: []string{"GENESIS"}, Extensions: []string{".bin"}},
		{ID: "RA-NES", SystemID: "nes", Groups: []string{"RA"}},
		{ID: "genesis-explicit", SystemID: "genesis"},
		{ID: "RA-Genesis", SystemID: "genesis", Groups: []string{"RA"}},
	}
	mockPlatform.On("Launchers", cfg).Return(launchers)
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: rootDir}).Maybe()

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery",
		mock.Anything,
		mock.MatchedBy(func(q *database.MediaQuery) bool {
			return q.PathPrefix == wantPathPrefix
		}),
	).Return(database.SearchResult{}, sql.ErrNoRows)

	mockPlatform.On("LaunchMedia", cfg, romPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "RA-Genesis"
		}),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{dir},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdRandom(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

// TestCmdRandom_DoubleSlashPathCleaned verifies that a double-slash prefix
// (e.g. from **launch.random://path) is normalized before querying the DB.
// On Windows, //media/fat is a UNC path and filepath.Clean preserves the double
// slash, so the collapse to single slash is POSIX-only behavior.
func TestCmdRandom_DoubleSlashPathCleaned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("//media/fat is a UNC path on Windows; double-slash collapse is POSIX-only filepath.Clean behavior")
	}
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	// Expect the cleaned path (single slash) in the PathPrefix
	mockMediaDB.On("RandomGameWithQuery",
		mock.Anything,
		mock.MatchedBy(func(q *database.MediaQuery) bool {
			return q.PathPrefix == "/media/fat/_#Insert-Coin/_#Essentials"
		}),
	).Return(database.SearchResult{
		Path:     "/media/fat/_#Insert-Coin/_#Essentials/game.zip",
		SystemID: "arcade",
	}, nil)

	mockPlatform.On("LaunchMedia", cfg,
		"/media/fat/_#Insert-Coin/_#Essentials/game.zip",
		(*platforms.Launcher)(nil),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			// Double slash — as parsed from **launch.random://media/fat/...
			Args: []string{"//media/fat/_#Insert-Coin/_#Essentials"},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdRandom(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
}

// TestCmdRandom_UNCPathPrefixPreserved is a regression test against PR #921's approach.
// On Windows, a UNC path such as \\server\share\games must be passed to the DB as
// //server/share/games (via filepath.ToSlash(filepath.Clean(...))), matching how the
// media scanner stores those paths. Collapsing the leading // to / would make the prefix
// never match any stored media and silently return no results.
func TestCmdRandom_UNCPathPrefixPreserved(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC path handling is Windows-only")
	}
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	// The scanner stores UNC paths as //server/share/... (via filepath.ToSlash).
	// The DB prefix must preserve the leading // to match.
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery",
		mock.Anything,
		mock.MatchedBy(func(q *database.MediaQuery) bool {
			return q.PathPrefix == "//server/share/games"
		}),
	).Return(database.SearchResult{
		Path:     "//server/share/games/game.zip",
		SystemID: "genesis",
	}, nil)

	mockPlatform.On("LaunchMedia", cfg,
		"//server/share/games/game.zip",
		(*platforms.Launcher)(nil),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{`\\server\share\games`},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdRandom(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

// TestCmdRandom_AbsolutePathFallbackToFilesystem is a regression test for #576.
// When an absolute path has no entries in the media database, launch.random
// should fall back to picking a random file directly from disk.
func TestCmdRandom_AbsolutePathFallbackToFilesystem(t *testing.T) {
	t.Parallel()

	// Create temp dir with some files and a subdirectory
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "game1.vhd"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "game2.vhd"), []byte("x"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o750))
	wantPathPrefix := filepath.ToSlash(dir)

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	// Database has no entries for this path
	mockMediaDB.On("RandomGameWithQuery",
		mock.Anything,
		mock.MatchedBy(func(q *database.MediaQuery) bool {
			return q.PathPrefix == wantPathPrefix
		}),
	).Return(database.SearchResult{}, sql.ErrNoRows)

	// Accept launch of either file (but not the subdirectory)
	mockPlatform.On("LaunchMedia", cfg,
		mock.MatchedBy(func(path string) bool {
			return path == filepath.Join(dir, "game1.vhd") ||
				path == filepath.Join(dir, "game2.vhd")
		}),
		(*platforms.Launcher)(nil),
		mock.Anything,
		(*platforms.LaunchOptions)(nil),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{dir},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	result, err := cmdRandom(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockMediaDB.AssertExpectations(t)
	mockPlatform.AssertExpectations(t)
}

func TestCmdRandom_AbsolutePathFallback_NonExistentPath(t *testing.T) {
	t.Parallel()

	// Use a subdirectory of TempDir so the path is absolute on all platforms
	nonexistent := filepath.Join(t.TempDir(), "nonexistent")

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Return(database.SearchResult{}, sql.ErrNoRows)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{nonexistent},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := cmdRandom(mockPlatform, env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read path")
}

func TestCmdRandom_AbsolutePathFallback_OnlySubdirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir1"), 0o750))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir2"), 0o750))

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Return(database.SearchResult{}, sql.ErrNoRows)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{dir},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := cmdRandom(mockPlatform, env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files found in")
}

func TestCmdRandom_AbsolutePathDBError_NoFallback(t *testing.T) {
	t.Parallel()

	// Use TempDir so the path is absolute on all platforms (including Windows)
	dir := t.TempDir()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("RandomGameWithQuery", mock.Anything, mock.Anything).
		Return(database.SearchResult{}, errors.New("connection lost"))

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.random",
			Args: []string{dir},
		},
		Cfg:      cfg,
		Database: &database.Database{MediaDB: mockMediaDB},
	}

	_, err := cmdRandom(mockPlatform, env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}
