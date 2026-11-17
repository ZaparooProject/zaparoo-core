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
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errNotAdmitted = errors.New("content policy refused the launch")

// admitPlatform is a platform with no launchers of its own. Resolving a
// launch looks them up; nothing else is mocked, so a download or a launch
// that got past a refusal fails the test on an unexpected call.
func admitPlatform() *mocks.MockPlatform {
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("Launchers", mock.Anything).Return([]platforms.Launcher{}).Maybe()
	return mockPlatform
}

func TestCmdSystem_AsksTheContentPolicy(t *testing.T) {
	t.Parallel()
	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	var asked []platforms.ResolvedLaunch

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.system", Args: []string{"NES"}, AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
		},
		Cfg: cfg,
		AdmitLaunch: func(target platforms.ResolvedLaunch, _ bool) error {
			asked = append(asked, target)
			return errNotAdmitted
		},
	}

	_, err := cmdSystem(mockPlatform, env)
	require.ErrorIs(t, err, errNotAdmitted)
	require.Len(t, asked, 1)
	assert.Equal(t, "NES", asked[0].SystemID)
	assert.Empty(t, asked[0].Path)
	mockPlatform.AssertNotCalled(t, "LaunchSystem", mock.Anything, mock.Anything)
}

func TestCmdSystem_AdmittedSystemLaunches(t *testing.T) {
	t.Parallel()
	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("LaunchSystem", cfg, "NES").Return(nil).Once()

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name: "launch.system", Args: []string{"NES"}, AdvArgs: zapscript.NewAdvArgs(map[string]string{}),
		},
		Cfg:         cfg,
		AdmitLaunch: func(platforms.ResolvedLaunch, bool) error { return nil },
	}
	_, err := cmdSystem(mockPlatform, env)
	require.NoError(t, err)
	mockPlatform.AssertExpectations(t)
}

func TestLaunchClosure_AsksTheContentPolicyBeforeAndAtLaunch(t *testing.T) {
	t.Parallel()
	mockPlatform := admitPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("LaunchMedia", cfg, "/games/NES/Mario.nes",
		mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	var order []string
	env := &platforms.CmdEnv{
		Cmd: zapscript.Command{Name: "launch", AdvArgs: zapscript.NewAdvArgs(map[string]string{})},
		Cfg: cfg,
		AdmitLaunch: func(target platforms.ResolvedLaunch, final bool) error {
			assert.Equal(t, "/games/NES/Mario.nes", target.Path)
			assert.Equal(t, "NES", target.SystemID)
			if final {
				order = append(order, "admit-final")
			} else {
				order = append(order, "admit-first")
			}
			return nil
		},
		PrepareMediaLaunch: func(platforms.ResolvedLaunch) (bool, error) {
			order = append(order, "prepare")
			return true, nil
		},
	}

	launch := getLaunchClosure(mockPlatform, env, false)
	require.NoError(t, launch(launchTarget{path: "/games/NES/Mario.nes", systemID: "NES"}))

	// The first ask comes before a confirmation is shown, the last after
	// it, so a device that locks while the prompt is open still refuses.
	assert.Equal(t, []string{"admit-first", "prepare", "admit-final"}, order)
	mockPlatform.AssertExpectations(t)
}

func TestLaunchClosure_RefusedLaunchNeverPreparesOrStarts(t *testing.T) {
	t.Parallel()
	mockPlatform := admitPlatform()
	prepared := false
	env := &platforms.CmdEnv{
		Cmd:         zapscript.Command{Name: "launch", AdvArgs: zapscript.NewAdvArgs(map[string]string{})},
		Cfg:         &config.Instance{},
		AdmitLaunch: func(platforms.ResolvedLaunch, bool) error { return errNotAdmitted },
		PrepareMediaLaunch: func(platforms.ResolvedLaunch) (bool, error) {
			prepared = true
			return true, nil
		},
	}

	launch := getLaunchClosure(mockPlatform, env, false)
	err := launch(launchTarget{path: "/games/NES/Mario.nes", systemID: "NES"})
	require.ErrorIs(t, err, errNotAdmitted)
	// A refusal is the policy working, so it is reported as one and logged
	// as an expected outcome instead of a failure.
	require.ErrorIs(t, err, ErrLaunchNotAdmitted)
	assert.True(t, IsExpectedRunError(err))
	assert.False(t, prepared, "no confirmation or hook for a launch that will be refused")
	mockPlatform.AssertNotCalled(t, "LaunchMedia",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestLaunchClosure_RefusedAfterConfirmationNeverStarts(t *testing.T) {
	t.Parallel()
	mockPlatform := admitPlatform()
	env := &platforms.CmdEnv{
		Cmd: zapscript.Command{Name: "launch", AdvArgs: zapscript.NewAdvArgs(map[string]string{})},
		Cfg: &config.Instance{},
		AdmitLaunch: func(_ platforms.ResolvedLaunch, final bool) error {
			if final {
				// The device locked while the confirmation was waiting.
				return errNotAdmitted
			}
			return nil
		},
		PrepareMediaLaunch: func(platforms.ResolvedLaunch) (bool, error) { return true, nil },
	}

	launch := getLaunchClosure(mockPlatform, env, false)
	require.ErrorIs(t, launch(launchTarget{path: "/games/NES/Mario.nes", systemID: "NES"}), errNotAdmitted)
	mockPlatform.AssertNotCalled(t, "LaunchMedia",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestCmdLaunch_RefusedMediaInstallDownloadsNothing(t *testing.T) {
	t.Parallel()
	mockPlatform := admitPlatform()
	asked := 0
	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch",
			Args:    []string{"https://example.com/game.nes"},
			AdvArgs: zapscript.NewAdvArgs(map[string]string{"system": "NES"}),
		},
		Cfg: &config.Instance{},
		AdmitMediaInstall: func() error {
			asked++
			return errNotAdmitted
		},
	}

	// A download or a launch attempted past the refusal would fail the
	// test on an unexpected platform call.
	_, err := cmdLaunchWithFS(afero.NewMemMapFs(), mockPlatform, env)
	require.ErrorIs(t, err, errNotAdmitted)
	assert.Equal(t, 1, asked)
}

func TestPermittedSystems(t *testing.T) {
	t.Parallel()
	systems := []systemdefs.System{{ID: "NES"}, {ID: "SNES"}, {ID: "Genesis"}}

	assert.Equal(t, systems, permittedSystems(nil, systems))
	assert.Equal(t, systems, permittedSystems(&platforms.CmdEnv{}, systems), "no policy permits everything")

	env := &platforms.CmdEnv{SystemPermitted: func(id string) bool { return id != "Genesis" }}
	assert.Equal(t, []systemdefs.System{{ID: "NES"}, {ID: "SNES"}}, permittedSystems(env, systems))

	closed := &platforms.CmdEnv{SystemPermitted: func(string) bool { return false }}
	assert.Empty(t, permittedSystems(closed, systems))
}

func TestOpenSystems(t *testing.T) {
	t.Parallel()

	all, err := openSystems(&platforms.CmdEnv{})
	require.NoError(t, err)
	assert.Equal(t, systemdefs.AllImplicitSystems(), all, "no policy leaves every system open")

	onlyNES, err := openSystems(&platforms.CmdEnv{SystemPermitted: func(id string) bool { return id == "NES" }})
	require.NoError(t, err)
	require.Len(t, onlyNES, 1)
	assert.Equal(t, "NES", onlyNES[0].ID)

	// An empty list would read as every system further down, so a policy
	// that closes them all has to come back as a refusal.
	_, err = openSystems(&platforms.CmdEnv{SystemPermitted: func(string) bool { return false }})
	require.ErrorIs(t, err, ErrLaunchNotAdmitted)
	assert.True(t, IsExpectedRunError(err))
}
