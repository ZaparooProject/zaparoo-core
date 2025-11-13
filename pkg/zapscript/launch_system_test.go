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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestCmdSystem_Menu is a regression test for the bug where `launch.system:menu`
// would stop the active launcher but never actually launch the menu core.
// This caused the success sound to play and media state to clear, but the menu
// never appeared.
func TestCmdSystem_Menu(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// Mock StopActiveLauncher to verify it's called
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()

	// Mock LaunchSystem with "menu" to verify it's called (the fix!)
	mockPlatform.On("LaunchSystem", cfg, "menu").Return(nil).Once()

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name:    "launch.system",
			Args:    []string{"menu"},
			AdvArgs: map[string]string{},
		},
		Cfg: cfg,
	}

	result, err := cmdSystem(mockPlatform, env)

	require.NoError(t, err, "cmdSystem should not return an error for menu")
	assert.True(t, result.MediaChanged, "MediaChanged should be true when launching menu")

	// Verify both StopActiveLauncher AND LaunchSystem were called
	mockPlatform.AssertExpectations(t)
}

// TestCmdSystem_MenuStopFails verifies that if StopActiveLauncher fails,
// we still attempt to launch the menu (just log the error)
func TestCmdSystem_MenuStopFails(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// Mock StopActiveLauncher to return an error
	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).
		Return(assert.AnError).Once()

	// LaunchSystem should still be called despite stop failure
	mockPlatform.On("LaunchSystem", cfg, "menu").Return(nil).Once()

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name:    "launch.system",
			Args:    []string{"menu"},
			AdvArgs: map[string]string{},
		},
		Cfg: cfg,
	}

	result, err := cmdSystem(mockPlatform, env)

	require.NoError(t, err, "cmdSystem should succeed even if stop fails")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")

	mockPlatform.AssertExpectations(t)
}

// TestCmdSystem_MenuLaunchFails verifies error handling when menu launch fails
func TestCmdSystem_MenuLaunchFails(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()
	mockPlatform.On("LaunchSystem", cfg, "menu").Return(assert.AnError).Once()

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name:    "launch.system",
			Args:    []string{"menu"},
			AdvArgs: map[string]string{},
		},
		Cfg: cfg,
	}

	result, err := cmdSystem(mockPlatform, env)

	require.Error(t, err, "cmdSystem should return error when LaunchSystem fails")
	assert.Contains(t, err.Error(), "failed to launch system 'menu'")
	assert.True(t, result.MediaChanged, "MediaChanged should still be true")

	mockPlatform.AssertExpectations(t)
}

// TestCmdSystem_RegularSystem verifies normal system launch (non-menu)
func TestCmdSystem_RegularSystem(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// For non-menu systems, StopActiveLauncher should NOT be called
	mockPlatform.On("LaunchSystem", cfg, "NES").Return(nil).Once()

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name:    "launch.system",
			Args:    []string{"NES"},
			AdvArgs: map[string]string{},
		},
		Cfg: cfg,
	}

	result, err := cmdSystem(mockPlatform, env)

	require.NoError(t, err, "cmdSystem should not return error for valid system")
	assert.True(t, result.MediaChanged, "MediaChanged should be true")

	mockPlatform.AssertExpectations(t)
	// Verify StopActiveLauncher was NOT called for regular systems
	mockPlatform.AssertNotCalled(t, "StopActiveLauncher", mock.Anything)
}

// TestCmdSystem_InvalidArgCount verifies error when no system ID provided
func TestCmdSystem_InvalidArgCount(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	env := platforms.CmdEnv{
		Cmd: parser.Command{
			Name:    "launch.system",
			Args:    []string{}, // No args
			AdvArgs: map[string]string{},
		},
		Cfg: cfg,
	}

	_, err := cmdSystem(mockPlatform, env)

	require.Error(t, err, "cmdSystem should return error with no args")
	assert.Equal(t, ErrArgCount, err)
}

// TestCmdSystem_MenuCaseInsensitive verifies menu works with different cases
func TestCmdSystem_MenuCaseInsensitive(t *testing.T) {
	t.Parallel()

	testCases := []string{"menu", "Menu", "MENU", "MeNu"}

	for _, menuVariant := range testCases {
		t.Run(menuVariant, func(t *testing.T) {
			mockPlatform := mocks.NewMockPlatform()
			cfg := &config.Instance{}

			mockPlatform.On("StopActiveLauncher", platforms.StopForPreemption).Return(nil).Once()
			mockPlatform.On("LaunchSystem", cfg, menuVariant).Return(nil).Once()

			env := platforms.CmdEnv{
				Cmd: parser.Command{
					Name:    "launch.system",
					Args:    []string{menuVariant},
					AdvArgs: map[string]string{},
				},
				Cfg: cfg,
			}

			result, err := cmdSystem(mockPlatform, env)

			require.NoError(t, err, "cmdSystem should work with %s", menuVariant)
			assert.True(t, result.MediaChanged, "MediaChanged should be true")

			mockPlatform.AssertExpectations(t)
		})
	}
}
