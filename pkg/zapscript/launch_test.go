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
		Launch: func(_ *config.Instance, _ string) (*os.Process, error) {
			return &os.Process{}, nil
		},
	}

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{genesisLauncher})
	mockPlatform.On("LaunchMedia", cfg, "/absolute/path/game.bin",
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-retroarch"
		}),
		(*database.Database)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name: "launch",
			Args: []string{"/absolute/path/game.bin"},
			AdvArgs: map[string]string{
				"system": "genesis",
			},
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
		Launch: func(_ *config.Instance, _ string) (*os.Process, error) {
			return &os.Process{}, nil
		},
	}

	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{explicitLauncher})
	mockPlatform.On("LaunchMedia", cfg, "/absolute/path/game.bin",
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			return l != nil && l.ID == "genesis-explicit"
		}),
		(*database.Database)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name: "launch",
			Args: []string{"/absolute/path/game.bin"},
			AdvArgs: map[string]string{
				"system":   "genesis",
				"launcher": "genesis-explicit", // Explicit launcher should win
			},
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err, "cmdLaunch should not return error")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")
	mockPlatform.AssertExpectations(t)
}

// TestCmdLaunch_InvalidSystemArgFallsBackToAutoDetect verifies invalid system doesn't crash
func TestCmdLaunch_InvalidSystemArgFallsBackToAutoDetect(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// Use a platform-specific absolute path
	absPath := filepath.Join(t.TempDir(), "game.bin")
	mockPlatform.On("LaunchMedia", cfg, absPath,
		(*platforms.Launcher)(nil), (*database.Database)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name: "launch",
			Args: []string{absPath},
			AdvArgs: map[string]string{
				"system": "invalidname", // Invalid system should log warning and fall back
			},
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err, "cmdLaunch should not crash with invalid system")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")
	mockPlatform.AssertExpectations(t)
}

// TestCmdLaunch_SystemArgWithNoDefaults verifies system with no configured defaults works
func TestCmdLaunch_SystemArgWithNoDefaults(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	mockPlatform.On("LaunchMedia", cfg, "/absolute/path/game.bin",
		(*platforms.Launcher)(nil), (*database.Database)(nil)).Return(nil)

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name: "launch",
			Args: []string{"/absolute/path/game.bin"},
			AdvArgs: map[string]string{
				"system": "genesis",
			},
		},
		Cfg: cfg,
	}

	result, err := cmdLaunch(mockPlatform, env)

	require.NoError(t, err, "cmdLaunch should work with valid system but no defaults")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")
	mockPlatform.AssertExpectations(t)
}
