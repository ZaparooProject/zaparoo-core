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
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestCmdSystem_Menu verifies that launch.system:menu calls ReturnToMenu
func TestCmdSystem_Menu(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// Mock ReturnToMenu - cmdSystem now directly calls this for "menu"
	mockPlatform.On("ReturnToMenu").Return(nil).Once()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.system",
			Args:    []string{"menu"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
		},
		Cfg: cfg,
	}

	result, err := cmdSystem(mockPlatform, env)

	require.NoError(t, err, "cmdSystem should not return an error for menu")
	assert.True(t, result.MediaChanged, "MediaChanged should be true when launching menu")

	// Verify ReturnToMenu was called
	mockPlatform.AssertExpectations(t)
}

// TestCmdSystem_MenuReturnFails verifies error handling when ReturnToMenu fails
func TestCmdSystem_MenuReturnFails(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}

	// Mock ReturnToMenu to return an error
	mockPlatform.On("ReturnToMenu").Return(assert.AnError).Once()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch.system",
			Args:    []string{"menu"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
		},
		Cfg: cfg,
	}

	result, err := cmdSystem(mockPlatform, env)

	require.Error(t, err, "cmdSystem should return error when ReturnToMenu fails")
	assert.Contains(t, err.Error(), "failed to return to menu")
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
		Cmd: zapscript.Command{
			Name:    "launch.system",
			Args:    []string{"NES"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
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
		Cmd: zapscript.Command{
			Name:    "launch.system",
			Args:    []string{}, // No args
			AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
		},
		Cfg: cfg,
	}

	_, err := cmdSystem(mockPlatform, env)

	require.Error(t, err, "cmdSystem should return error with no args")
	assert.Equal(t, ErrArgCount, err)
}

// launcherSelectorMockPlatform adds platforms.SystemLauncherSelector to
// MockPlatform, mirroring the runtimeMockPlatform pattern used for
// LauncherRuntimeProvider in pkg/api/methods/launchers_test.go.
type launcherSelectorMockPlatform struct {
	*mocks.MockPlatform
	err         error
	gotSystem   string
	gotLauncher string
}

func (p *launcherSelectorMockPlatform) LaunchSystemLauncher(
	_ *config.Instance, systemID string, launcher *platforms.Launcher,
) error {
	p.gotSystem = systemID
	p.gotLauncher = launcher.ID
	return p.err
}

// TestCmdSystem_LauncherArg verifies an explicit launcher adv arg routes
// through SystemLauncherSelector instead of the default LaunchSystem path.
func TestCmdSystem_LauncherArg(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	launcher := platforms.Launcher{ID: "SuperNT", SystemID: "SNES"}

	base := mocks.NewMockPlatform()
	base.On("Launchers", cfg).Return([]platforms.Launcher{launcher})
	selector := &launcherSelectorMockPlatform{MockPlatform: base}

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.system",
			Args: []string{"SNES"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"launcher": "SuperNT",
			}),
		},
		Cfg: cfg,
	}

	result, err := cmdSystem(selector, env)

	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	assert.Equal(t, "SNES", selector.gotSystem)
	assert.Equal(t, "SuperNT", selector.gotLauncher)
	selector.AssertNotCalled(t, "LaunchSystem", mock.Anything, mock.Anything)
}

// TestCmdSystem_LauncherArgNotFound verifies an unknown launcher ID errors
// before any platform launch call is attempted.
func TestCmdSystem_LauncherArgNotFound(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	base := mocks.NewMockPlatform()
	base.On("Launchers", cfg).Return([]platforms.Launcher{})
	selector := &launcherSelectorMockPlatform{MockPlatform: base}

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.system",
			Args: []string{"SNES"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"launcher": "missing",
			}),
		},
		Cfg: cfg,
	}

	_, err := cmdSystem(selector, env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "launcher not found")
}

// TestCmdSystem_LauncherArgWrongSystem verifies a launcher belonging to a
// different system is rejected rather than silently launched.
func TestCmdSystem_LauncherArgWrongSystem(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	launcher := platforms.Launcher{ID: "SuperNT", SystemID: "SNES"}
	base := mocks.NewMockPlatform()
	base.On("Launchers", cfg).Return([]platforms.Launcher{launcher})
	selector := &launcherSelectorMockPlatform{MockPlatform: base}

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.system",
			Args: []string{"Genesis"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"launcher": "SuperNT",
			}),
		},
		Cfg: cfg,
	}

	_, err := cmdSystem(selector, env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to system")
}

// TestCmdSystem_LauncherArgUnsupportedPlatform verifies a platform that
// doesn't implement SystemLauncherSelector errors instead of silently
// ignoring the requested launcher.
func TestCmdSystem_LauncherArgUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	launcher := platforms.Launcher{ID: "SuperNT", SystemID: "SNES"}
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", cfg).Return([]platforms.Launcher{launcher})
	mockPlatform.On("ID").Return("windows")

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.system",
			Args: []string{"SNES"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"launcher": "SuperNT",
			}),
		},
		Cfg: cfg,
	}

	_, err := cmdSystem(mockPlatform, env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "launcher selection not supported")
}

// TestCmdSystem_LauncherArgPropagatesFailure verifies a launch failure from
// the selector is wrapped and MediaChanged still reflects the attempt.
func TestCmdSystem_LauncherArgPropagatesFailure(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	launcher := platforms.Launcher{ID: "SuperNT", SystemID: "SNES"}
	base := mocks.NewMockPlatform()
	base.On("Launchers", cfg).Return([]platforms.Launcher{launcher})
	selector := &launcherSelectorMockPlatform{MockPlatform: base, err: assert.AnError}

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.system",
			Args: []string{"SNES"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{
				"launcher": "SuperNT",
			}),
		},
		Cfg: cfg,
	}

	result, err := cmdSystem(selector, env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to launch system")
	assert.True(t, result.MediaChanged)
}

// TestCmdSystem_MenuCaseInsensitive verifies menu works with different cases
func TestCmdSystem_MenuCaseInsensitive(t *testing.T) {
	t.Parallel()

	testCases := []string{"menu", "Menu", "MENU", "MeNu"}

	for _, menuVariant := range testCases {
		t.Run(menuVariant, func(t *testing.T) {
			mockPlatform := mocks.NewMockPlatform()
			cfg := &config.Instance{}

			// Mock ReturnToMenu - should work for all case variants
			mockPlatform.On("ReturnToMenu").Return(nil).Once()

			env := platforms.CmdEnv{
				Cmd: zapscript.Command{
					Name:    "launch.system",
					Args:    []string{menuVariant},
					AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
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
