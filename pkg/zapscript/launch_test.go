// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestCmdLaunch_SystemArgAppliesDefaults verifies that system arg applies system defaults when no explicit launcher
func TestCmdLaunch_SystemArgAppliesDefaults(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()

	cfg := &config.Instance{}
	cfg.SetSystemDefaultsForTesting([]config.SystemsDefault{
		{
			System:   "genesis",
			Launcher: "genesis-retroarch",
		},
	})

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
		Cmd: parser.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: parser.NewAdvArgs(map[string]string{
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
	cfg.SetSystemDefaultsForTesting([]config.SystemsDefault{
		{
			System:   "genesis",
			Launcher: "genesis-default",
		},
	})

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
		Cmd: parser.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: parser.NewAdvArgs(map[string]string{
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
		Cmd: parser.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: parser.NewAdvArgs(map[string]string{
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
		Cmd: parser.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: parser.NewAdvArgs(map[string]string{
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
	cfg.SetSystemDefaultsForTesting([]config.SystemsDefault{
		{
			System:   "snes",
			Launcher: "snes-retroarch",
		},
	})

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
		Cmd: parser.Command{
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
	cfg.SetSystemDefaultsForTesting([]config.SystemsDefault{
		{
			System:   "snes",
			Launcher: "snes-retroarch",
		},
	})

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
		Cmd: parser.Command{
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
	mockMediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything).
		Return([]database.SearchResult{}, nil)

	env := platforms.CmdEnv{
		Cmd: parser.Command{
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
