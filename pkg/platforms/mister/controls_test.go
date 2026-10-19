//go:build linux && !android

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

package mister

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCoreControls(pl platforms.Platform, rbf string, rbfErr error) *coreControls {
	return &coreControls{
		pl:          pl,
		readRBFName: func() (string, error) { return rbf, rbfErr },
	}
}

func TestCoreControls_Hotkeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action string
		key    string
	}{
		{"toggle menu", platforms.ControlToggleMenu, "{f12}"},
		{"reset presses the user button", platforms.ControlReset, "{lctrl+lalt+ralt}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pl := mocks.NewMockPlatform()
			pl.On("KeyboardPress", tt.key).Return(nil)

			// These work on every core, including ones without savestates.
			cc := newTestCoreControls(pl, "MegaDrive", nil)
			ctrl := cc.controls()[tt.action]
			require.NotNil(t, ctrl.Func)
			require.NoError(t, ctrl.Func(context.Background(), nil, platforms.ControlParams{}))
			pl.AssertExpectations(t)
		})
	}
}

func TestCoreControls_Savestate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action string
		args   map[string]string
		key    string
	}{
		{"save defaults to slot 1", platforms.ControlSaveState, nil, "{alt+f1}"},
		{"save slot 3", platforms.ControlSaveState, map[string]string{"slot": "3"}, "{alt+f3}"},
		{"load defaults to slot 1", platforms.ControlLoadState, nil, "{f1}"},
		{"load slot 4", platforms.ControlLoadState, map[string]string{"slot": "4"}, "{f4}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pl := mocks.NewMockPlatform()
			pl.On("KeyboardPress", tt.key).Return(nil)

			cc := newTestCoreControls(pl, "NES", nil)
			ctrl := cc.controls()[tt.action]
			require.NoError(t, ctrl.Func(context.Background(), nil, platforms.ControlParams{Args: tt.args}))
			pl.AssertExpectations(t)
		})
	}
}

func TestCoreControls_SavestateRejectsBadSlot(t *testing.T) {
	t.Parallel()

	for _, slot := range []string{"0", "5", "-1", "two", "1.5"} {
		t.Run(slot, func(t *testing.T) {
			t.Parallel()
			pl := mocks.NewMockPlatform()
			cc := newTestCoreControls(pl, "NES", nil)
			ctrl := cc.controls()[platforms.ControlSaveState]
			err := ctrl.Func(context.Background(), nil, platforms.ControlParams{Args: map[string]string{"slot": slot}})
			require.Error(t, err)
			assert.Empty(t, pl.GetKeyboardPresses(), "no key may be sent for an invalid slot")
		})
	}
}

func TestCoreControls_SavestateRefusedOnUnsupportedCore(t *testing.T) {
	t.Parallel()

	// Genesis has no savestates and C64's F1-F4 are real keys: nothing may be sent.
	for _, core := range []string{"MegaDrive", "C64", "Apple-II", "MENU"} {
		for _, action := range []string{platforms.ControlSaveState, platforms.ControlLoadState} {
			t.Run(core+"/"+action, func(t *testing.T) {
				t.Parallel()
				pl := mocks.NewMockPlatform()
				cc := newTestCoreControls(pl, core, nil)
				err := cc.controls()[action].Func(context.Background(), nil, platforms.ControlParams{})
				require.ErrorIs(t, err, errNoSavestates)
				require.ErrorIs(t, err, platforms.ErrControlUnavailable)
				assert.Contains(t, err.Error(), core)
				assert.Empty(t, pl.GetKeyboardPresses())
			})
		}
	}
}

func TestCoreControls_SavestateCoreNameUnreadable(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	cc := newTestCoreControls(pl, "", errors.New("no such file"))
	err := cc.controls()[platforms.ControlSaveState].Func(context.Background(), nil, platforms.ControlParams{})
	require.Error(t, err)
	assert.Empty(t, pl.GetKeyboardPresses())
}

func TestCoreControls_KeyboardErrorPropagates(t *testing.T) {
	t.Parallel()

	keyErr := errors.New("no virtual keyboard")
	pl := mocks.NewMockPlatform()
	pl.On("KeyboardPress", "{f12}").Return(keyErr)
	cc := newTestCoreControls(pl, "NES", nil)
	err := cc.controls()[platforms.ControlToggleMenu].Func(context.Background(), nil, platforms.ControlParams{})
	require.ErrorIs(t, err, keyErr)
}

func TestCoreControls_StopReturnsToMenu(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.On("StopActiveLauncher", platforms.StopForMenu).Return(nil)
	cc := newTestCoreControls(pl, "NES", nil)
	require.NoError(t, cc.controls()[platforms.ControlStop].Func(context.Background(), nil, platforms.ControlParams{}))
	pl.AssertExpectations(t)

	stopErr := errors.New("stop failed")
	pl = mocks.NewMockPlatform()
	pl.On("StopActiveLauncher", platforms.StopForMenu).Return(stopErr)
	cc = newTestCoreControls(pl, "NES", nil)
	err := cc.controls()[platforms.ControlStop].Func(context.Background(), nil, platforms.ControlParams{})
	require.ErrorIs(t, err, stopErr)
}

func TestWithCoreControls_OnlyCoreLaunchers(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	custom := map[string]platforms.Control{platforms.ControlStop: {Script: "**stop"}}
	ls := withCoreControls(pl, []platforms.Launcher{
		{ID: "Core", Launch: launch(pl, systemdefs.SystemNES)},
		{ID: "ScannerOnly"},
		{ID: "Own", Launch: launch(pl, systemdefs.SystemNES), Controls: custom},
	})

	assert.Equal(t,
		[]string{
			platforms.ControlLoadState, platforms.ControlReset, platforms.ControlSaveState,
			platforms.ControlStop, platforms.ControlToggleMenu,
		},
		platforms.ControlNames(&ls[0]),
	)
	assert.Empty(t, ls[1].Controls, "launchers that start no core get no controls")
	assert.Equal(t, custom, ls[2].Controls, "existing controls are kept")
}

func TestCreateLaunchers_FPGACoreControls(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	var withControls int
	for _, l := range CreateLaunchers(pl) {
		if l.Launch != nil {
			require.NotEmpty(t, l.Controls, "core launcher %s has no controls", l.ID)
			assert.Contains(t, l.Controls, platforms.ControlToggleMenu)
			withControls++
		}
	}
	assert.Positive(t, withControls)
}
