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
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testMainPID = 4242

func newTestMainCommands(t *testing.T, capabilities string) (*mainCommands, afero.Fs) {
	t.Helper()
	fs := afero.NewMemMapFs()
	m := &mainCommands{
		fs:               fs,
		capabilitiesPath: filepath.Join(string(filepath.Separator), "tmp", "zaparoo_main"),
		commandPath:      filepath.Join(string(filepath.Separator), "dev", "MiSTer_cmd"),
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(string(filepath.Separator), "proc", strconv.Itoa(testMainPID)), 0o755))
	require.NoError(t, afero.WriteFile(fs, m.commandPath, nil, 0o600))
	if capabilities != "" {
		require.NoError(t, afero.WriteFile(fs, m.capabilitiesPath, []byte(capabilities), 0o600))
	}
	return m, fs
}

func forkCapabilities(pid int) string {
	return fmt.Sprintf("1 %d cheat save mount osd kiosk frontend\n", pid)
}

func sentMainCommand(t *testing.T, m *mainCommands, fs afero.Fs) string {
	t.Helper()
	contents, err := afero.ReadFile(fs, m.commandPath)
	require.NoError(t, err)
	return string(contents)
}

func TestMainCommands_Supports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		capabilities string
		command      string
		want         bool
	}{
		{"fork lists cheat", forkCapabilities(testMainPID), mainCommandCheat, true},
		{"fork lists save", forkCapabilities(testMainPID), mainCommandSave, true},
		{"command not listed", "1 4242 save\n", mainCommandCheat, false},
		{"stock Main writes no file", "", mainCommandCheat, false},
		{"Main exited", forkCapabilities(9999), mainCommandCheat, false},
		{"unknown version", "2 4242 cheat\n", mainCommandCheat, false},
		{"bad pid", "1 abc cheat\n", mainCommandCheat, false},
		{"zero pid", "1 0 cheat\n", mainCommandCheat, false},
		{"pid only", "1 4242\n", mainCommandCheat, false},
		{"partial word is not a match", "1 4242 cheats\n", mainCommandCheat, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := newTestMainCommands(t, tt.capabilities)
			assert.Equal(t, tt.want, m.supports(tt.command))
		})
	}
}

func TestMainCommands_SendRequiresCapability(t *testing.T) {
	t.Parallel()

	m, fs := newTestMainCommands(t, "")
	err := m.send(mainCommandSave)
	require.ErrorIs(t, err, errMainCommandUnsupported)
	assert.Empty(t, sentMainCommand(t, m, fs))
}

func TestMainCommandArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		arg     string
		want    string
		wantErr bool
	}{
		{"plain", "Infinite HP", "Infinite HP", false},
		{"trimmed", "  Infinite HP.gg \t", "Infinite HP.gg", false},
		{"index", "3", "3", false},
		{"unicode", "Ínfinito ★", "Ínfinito ★", false},
		{"empty", "   ", "", true},
		{"newline injects a command", "x\nload_core /media/fat/menu.rbf", "", true},
		{"carriage return", "x\rload_core", "", true},
		{"nul", "x\x00y", "", true},
		{"tab inside", "a\tb", "", true},
		{"too long", strings.Repeat("a", maxMainCommandArg+1), "", true},
		{"at limit", strings.Repeat("a", maxMainCommandArg), strings.Repeat("a", maxMainCommandArg), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := mainCommandArg(tt.arg)
			if tt.wantErr {
				require.ErrorIs(t, err, errMainCommandArg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func newForkCoreControls(t *testing.T, capabilities string) (*coreControls, *mainCommands, afero.Fs) {
	t.Helper()
	m, fs := newTestMainCommands(t, capabilities)
	cc := newTestCoreControls(mocks.NewMockPlatform(), "SNES", nil)
	cc.main = m
	return cc, m, fs
}

func TestCoreControls_ForkControlsListedOnlyWithCapability(t *testing.T) {
	t.Parallel()

	cc, _, _ := newForkCoreControls(t, forkCapabilities(testMainPID))
	controls := cc.controls()
	assert.Contains(t, controls, platforms.ControlCheat)
	assert.Contains(t, controls, platforms.ControlSaveRAM)

	cc, _, _ = newForkCoreControls(t, "1 4242 save\n")
	controls = cc.controls()
	assert.NotContains(t, controls, platforms.ControlCheat)
	assert.Contains(t, controls, platforms.ControlSaveRAM)

	cc, _, _ = newForkCoreControls(t, "")
	controls = cc.controls()
	assert.NotContains(t, controls, platforms.ControlCheat)
	assert.NotContains(t, controls, platforms.ControlSaveRAM)
	assert.Contains(t, controls, platforms.ControlToggleMenu)
}

func TestCoreControls_Cheat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]string
		want string
	}{
		{"defaults to on", map[string]string{"name": "Infinite HP"}, "zaparoo_cheat on Infinite HP\n"},
		{"off", map[string]string{"name": "Infinite HP", "mode": "off"}, "zaparoo_cheat off Infinite HP\n"},
		{"toggle", map[string]string{"name": "Moon Jump.gg", "mode": "toggle"}, "zaparoo_cheat toggle Moon Jump.gg\n"},
		{"mode is case-insensitive", map[string]string{"name": "Lives", "mode": " ON "}, "zaparoo_cheat on Lives\n"},
		{"clear", map[string]string{"mode": "clear"}, "zaparoo_cheat clear\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cc, m, fs := newForkCoreControls(t, forkCapabilities(testMainPID))
			ctrl := cc.controls()[platforms.ControlCheat]
			require.NotNil(t, ctrl.Func)
			require.NoError(t, ctrl.Func(context.Background(), nil, platforms.ControlParams{Args: tt.args}))
			assert.Equal(t, tt.want, sentMainCommand(t, m, fs))
		})
	}
}

func TestCoreControls_CheatRejectsBadArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args map[string]string
		name string
	}{
		{name: "no name", args: map[string]string{}},
		{name: "blank name", args: map[string]string{"name": "  "}},
		{name: "unknown mode", args: map[string]string{"name": "Lives", "mode": "list"}},
		{name: "clear with a name", args: map[string]string{"name": "Lives", "mode": "clear"}},
		{name: "newline in name", args: map[string]string{"name": "Lives\nzaparoo_kiosk off"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cc, m, fs := newForkCoreControls(t, forkCapabilities(testMainPID))
			err := cc.controls()[platforms.ControlCheat].Func(
				context.Background(), nil, platforms.ControlParams{Args: tt.args},
			)
			require.ErrorIs(t, err, platforms.ErrInvalidControlArg)
			assert.Empty(t, sentMainCommand(t, m, fs))
		})
	}
}

func TestCoreControls_SaveRAM(t *testing.T) {
	t.Parallel()

	cc, m, fs := newForkCoreControls(t, forkCapabilities(testMainPID))
	ctrl := cc.controls()[platforms.ControlSaveRAM]
	require.NotNil(t, ctrl.Func)
	require.NoError(t, ctrl.Func(context.Background(), nil, platforms.ControlParams{}))
	assert.Equal(t, "zaparoo_save\n", sentMainCommand(t, m, fs))
}

func TestCoreControls_ForkControlUnavailableAfterMainExits(t *testing.T) {
	t.Parallel()

	cc, m, fs := newForkCoreControls(t, forkCapabilities(testMainPID))
	controls := cc.controls()
	require.NoError(t, fs.RemoveAll(filepath.Join(string(filepath.Separator), "proc", strconv.Itoa(testMainPID))))

	err := controls[platforms.ControlSaveRAM].Func(context.Background(), nil, platforms.ControlParams{})
	require.ErrorIs(t, err, platforms.ErrControlUnavailable)
	err = controls[platforms.ControlCheat].Func(
		context.Background(), nil, platforms.ControlParams{Args: map[string]string{"name": "Lives"}},
	)
	require.ErrorIs(t, err, platforms.ErrControlUnavailable)
	assert.Empty(t, sentMainCommand(t, m, fs))
}
