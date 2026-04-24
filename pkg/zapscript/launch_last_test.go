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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeHistoryEntry(path, name, systemID string) database.MediaHistoryEntry {
	return database.MediaHistoryEntry{
		MediaPath:  path,
		MediaName:  name,
		SystemID:   systemID,
		SystemName: systemID,
		StartTime:  time.Now(),
	}
}

func createTestFile(t *testing.T, dir, relPath string) string {
	t.Helper()
	absPath := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o750))
	require.NoError(t, os.WriteFile(absPath, []byte("test"), 0o600))
	return absPath
}

func TestCmdLaunchLast_DefaultOffset(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}
	rootDir := t.TempDir()
	absPath := createTestFile(t, rootDir, "SNES/Mario.sfc")

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 10).
		Return([]database.MediaHistoryEntry{
			makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
			makeHistoryEntry("Genesis/Sonic.bin", "Sonic", "genesis"),
		}, nil)

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil),
		mock.AnythingOfType("*database.Database"),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.last",
			AdvArgs: zapscript.NewAdvArgs(nil),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := cmdLaunchLast(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
	mockUserDB.AssertExpectations(t)
}

func TestCmdLaunchLast_Offset3(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}
	rootDir := t.TempDir()
	absPath := createTestFile(t, rootDir, "NES/Zelda.nes")

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 30).
		Return([]database.MediaHistoryEntry{
			makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
			makeHistoryEntry("Genesis/Sonic.bin", "Sonic", "genesis"),
			makeHistoryEntry("NES/Zelda.nes", "Zelda", "nes"),
		}, nil)

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil),
		mock.AnythingOfType("*database.Database"),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.last",
			Args:    []string{"3"},
			AdvArgs: zapscript.NewAdvArgs(nil),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := cmdLaunchLast(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunchLast_Deduplication(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}
	rootDir := t.TempDir()
	absPath := createTestFile(t, rootDir, "Genesis/Sonic.bin")

	// Mario appears twice in history, should be deduplicated
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 20).
		Return([]database.MediaHistoryEntry{
			makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
			makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
			makeHistoryEntry("Genesis/Sonic.bin", "Sonic", "genesis"),
		}, nil)

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil),
		mock.AnythingOfType("*database.Database"),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.last",
			Args:    []string{"2"},
			AdvArgs: zapscript.NewAdvArgs(nil),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := cmdLaunchLast(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunchLast_EmptyHistory(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 10).
		Return([]database.MediaHistoryEntry{}, nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.last",
			AdvArgs: zapscript.NewAdvArgs(nil),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	_, err := cmdLaunchLast(mockPlatform, env)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoHistory)
}

func TestCmdLaunchLast_OffsetExceedsHistory(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 50).
		Return([]database.MediaHistoryEntry{
			makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
			makeHistoryEntry("Genesis/Sonic.bin", "Sonic", "genesis"),
		}, nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.last",
			Args:    []string{"5"},
			AdvArgs: zapscript.NewAdvArgs(nil),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	_, err := cmdLaunchLast(mockPlatform, env)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoHistory)
}

func TestCmdLaunchLast_InvalidOffset(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()

	tests := []struct {
		name string
		arg  string
	}{
		{name: "zero", arg: "0"},
		{name: "negative", arg: "-1"},
		{name: "non-numeric", arg: "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := platforms.CmdEnv{
				Cmd: zapscript.Command{
					Name:    "launch.last",
					Args:    []string{tt.arg},
					AdvArgs: zapscript.NewAdvArgs(nil),
				},
				Cfg:      &config.Instance{},
				Database: &database.Database{UserDB: helpers.NewMockUserDBI()},
			}

			_, err := cmdLaunchLast(mockPlatform, env)
			require.Error(t, err)
		})
	}
}

func TestCmdLaunchLast_DBError(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}
	dbErr := errors.New("database unavailable")

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 10).
		Return([]database.MediaHistoryEntry{}, dbErr)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.last",
			AdvArgs: zapscript.NewAdvArgs(nil),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	_, err := cmdLaunchLast(mockPlatform, env)

	require.Error(t, err)
	assert.ErrorIs(t, err, dbErr)
}

func TestCmdLaunchLast_LauncherAdvarg(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}
	rootDir := t.TempDir()
	absPath := createTestFile(t, rootDir, "SNES/Mario.sfc")

	snesLauncher := platforms.Launcher{
		ID:       "snes-retroarch",
		SystemID: "snes",
	}

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 10).
		Return([]database.MediaHistoryEntry{
			makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
		}, nil)

	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{snesLauncher})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "snes-retroarch"
		}),
		mock.AnythingOfType("*database.Database"),
		(*platforms.LaunchOptions)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.last",
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"launcher": "snes-retroarch",
			}),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := cmdLaunchLast(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunchLast_LaunchErrorReturnsMediaChanged(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}
	rootDir := t.TempDir()
	absPath := createTestFile(t, rootDir, "SNES/Mario.sfc")
	launchErr := errors.New("launch failed")

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 10).
		Return([]database.MediaHistoryEntry{
			makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
		}, nil)

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil),
		mock.AnythingOfType("*database.Database"),
		(*platforms.LaunchOptions)(nil)).Return(launchErr)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.last",
			AdvArgs: zapscript.NewAdvArgs(nil),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := cmdLaunchLast(mockPlatform, env)

	require.ErrorIs(t, err, launchErr)
	assert.True(t, result.MediaChanged, "MediaChanged should be true even on launch failure")
}

func TestCmdLaunchLast_ActionAdvarg(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockUserDB := helpers.NewMockUserDBI()
	cfg := &config.Instance{}
	rootDir := t.TempDir()
	absPath := createTestFile(t, rootDir, "SNES/Mario.sfc")

	mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 10).
		Return([]database.MediaHistoryEntry{
			makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
		}, nil)

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{})
	mockPlatform.On("RootDirs", cfg).Return([]string{rootDir})
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil),
		mock.AnythingOfType("*database.Database"),
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			return opts != nil && opts.Action == "details"
		})).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.last",
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"action": "details",
			}),
		},
		Cfg:      cfg,
		Database: &database.Database{UserDB: mockUserDB},
	}

	result, err := cmdLaunchLast(mockPlatform, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestGetUniqueRecentMedia(t *testing.T) {
	t.Parallel()

	t.Run("returns Nth unique entry", func(t *testing.T) {
		t.Parallel()
		mockUserDB := helpers.NewMockUserDBI()

		mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 20).
			Return([]database.MediaHistoryEntry{
				makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
				makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
				makeHistoryEntry("Genesis/Sonic.bin", "Sonic", "genesis"),
				makeHistoryEntry("NES/Zelda.nes", "Zelda", "nes"),
			}, nil)

		entry, err := getUniqueRecentMedia(mockUserDB, 2)
		require.NoError(t, err)
		assert.Equal(t, "Genesis/Sonic.bin", entry.MediaPath)
	})

	t.Run("not enough unique entries", func(t *testing.T) {
		t.Parallel()
		mockUserDB := helpers.NewMockUserDBI()

		mockUserDB.On("GetMediaHistory", []string(nil), int64(0), 30).
			Return([]database.MediaHistoryEntry{
				makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
				makeHistoryEntry("SNES/Mario.sfc", "Mario", "snes"),
			}, nil)

		_, err := getUniqueRecentMedia(mockUserDB, 3)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoHistory)
	})
}
