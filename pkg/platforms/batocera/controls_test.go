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

package batocera

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const retroarchContext = `{
  "name": "retroarch",
  "keys": {
    "exit": ["KEY_LEFTSHIFT", "KEY_ESC"],
    "menu": ["KEY_LEFTSHIFT", "KEY_F1"],
    "pause": ["KEY_LEFTSHIFT", "KEY_P"],
    "save_state": ["KEY_LEFTSHIFT", "KEY_F3"],
    "restore_state": ["KEY_LEFTSHIFT", "KEY_F4"],
    "reset": ["KEY_LEFTSHIFT", "KEY_F10"],
    "screenshot": "batocera-screenshot"
  }
}`

const limitedContext = `{"name": "dolphin", "keys": {"exit": "KEY_ESC", "pause": "KEY_F10"}}`

func contextFs(t *testing.T, content string) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	if content != "" {
		require.NoError(t, afero.WriteFile(fs, hotkeygenContextFile, []byte(content), 0o644))
	}
	return fs
}

type sentActions struct{ actions []string }

func (s *sentActions) send(_ context.Context, action string) error {
	s.actions = append(s.actions, action)
	return nil
}

func TestHotkeyControls_SendMappedAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		control string
		action  string
	}{
		{platforms.ControlSaveState, "save_state"},
		{platforms.ControlLoadState, "restore_state"},
		{platforms.ControlToggleMenu, "menu"},
		{platforms.ControlTogglePause, "pause"},
		{platforms.ControlReset, "reset"},
	}
	for _, tt := range tests {
		t.Run(tt.control, func(t *testing.T) {
			t.Parallel()
			sent := &sentActions{}
			controls := hotkeyControls(contextFs(t, retroarchContext), sent.send, func() error { return nil })
			require.NoError(t, controls[tt.control].Func(context.Background(), nil, platforms.ControlParams{}))
			assert.Equal(t, []string{tt.action}, sent.actions)
		})
	}
}

func TestHotkeyControls_RefusedWhenEmulatorLacksHotkey(t *testing.T) {
	t.Parallel()

	sent := &sentActions{}
	controls := hotkeyControls(contextFs(t, limitedContext), sent.send, func() error { return nil })

	err := controls[platforms.ControlSaveState].Func(context.Background(), nil, platforms.ControlParams{})
	require.ErrorIs(t, err, platforms.ErrControlUnavailable)
	assert.Contains(t, err.Error(), "dolphin", "the error names the emulator")
	assert.Empty(t, sent.actions, "hotkeygen accepts unbound actions and does nothing, so none may be sent")

	pause := controls[platforms.ControlTogglePause]
	require.NoError(t, pause.Func(context.Background(), nil, platforms.ControlParams{}))
	assert.Equal(t, []string{"pause"}, sent.actions)
}

func TestHotkeyControls_NoEmulatorRunning(t *testing.T) {
	t.Parallel()

	sent := &sentActions{}
	controls := hotkeyControls(contextFs(t, ""), sent.send, func() error { return nil })
	err := controls[platforms.ControlReset].Func(context.Background(), nil, platforms.ControlParams{})
	require.ErrorIs(t, err, errNoEmulatorContext)
	assert.Empty(t, sent.actions)
}

func TestHotkeyControls_MalformedContext(t *testing.T) {
	t.Parallel()

	sent := &sentActions{}
	controls := hotkeyControls(contextFs(t, "{not json"), sent.send, func() error { return nil })
	require.Error(t, controls[platforms.ControlReset].Func(context.Background(), nil, platforms.ControlParams{}))
	assert.Empty(t, sent.actions)
}

func TestHotkeyControls_SlotArgumentRejected(t *testing.T) {
	t.Parallel()

	sent := &sentActions{}
	controls := hotkeyControls(contextFs(t, retroarchContext), sent.send, func() error { return nil })
	params := platforms.ControlParams{Args: map[string]string{"slot": "2"}}
	err := controls[platforms.ControlSaveState].Func(context.Background(), nil, params)
	require.ErrorIs(t, err, errSlotUnsupported)
	assert.Empty(t, sent.actions)
}

func TestHotkeyControls_SendErrorPropagates(t *testing.T) {
	t.Parallel()

	sendErr := errors.New("hotkeygen missing")
	controls := hotkeyControls(contextFs(t, retroarchContext),
		func(context.Context, string) error { return sendErr }, func() error { return nil })
	err := controls[platforms.ControlReset].Func(context.Background(), nil, platforms.ControlParams{})
	require.ErrorIs(t, err, sendErr)
}

func TestHotkeyControls_Stop(t *testing.T) {
	t.Parallel()

	stopped := 0
	controls := hotkeyControls(contextFs(t, ""), (&sentActions{}).send, func() error { stopped++; return nil })
	require.NoError(t, controls[platforms.ControlStop].Func(context.Background(), nil, platforms.ControlParams{}))
	assert.Equal(t, 1, stopped, "stop works without an emulator context")

	stopErr := errors.New("stop failed")
	controls = hotkeyControls(contextFs(t, ""), (&sentActions{}).send, func() error { return stopErr })
	err := controls[platforms.ControlStop].Func(context.Background(), nil, platforms.ControlParams{})
	require.ErrorIs(t, err, stopErr)
}

func TestHotkeyControlNames(t *testing.T) {
	t.Parallel()

	controls := hotkeyControls(contextFs(t, ""), (&sentActions{}).send, func() error { return nil })
	assert.Equal(t, platforms.ControlNames(&platforms.Launcher{Controls: controls}), hotkeyControlNames())
}
