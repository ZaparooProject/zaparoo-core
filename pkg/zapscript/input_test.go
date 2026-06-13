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
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCfgFromTOML(t testing.TB, toml string) *config.Instance {
	t.Helper()
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(toml))
	return cfg
}

type sequenceRecorder struct {
	sequenceErr   error
	sequenceArgs  []string
	sequenceDelay time.Duration
}

type sequenceMockPlatform struct {
	*mocks.MockPlatform
	recorder *sequenceRecorder
}

func newSequenceMockPlatform() *sequenceMockPlatform {
	return &sequenceMockPlatform{
		MockPlatform: mocks.NewMockPlatform(),
		recorder:     &sequenceRecorder{},
	}
}

func (p *sequenceMockPlatform) KeyboardPressSequence(args []string, interKeyDelay time.Duration) error {
	p.recorder.sequenceArgs = append([]string(nil), args...)
	p.recorder.sequenceDelay = interKeyDelay
	return p.recorder.sequenceErr
}

func TestIsSpecialKey(t *testing.T) {
	t.Parallel()

	assert.True(t, isSpecialKey("{f1}"))
	assert.True(t, isSpecialKey("{enter}"))
	assert.True(t, isSpecialKey("{ctrl+q}"))
	assert.True(t, isSpecialKey("{ctrl+alt+delete}"))
	assert.False(t, isSpecialKey("a"))
	assert.False(t, isSpecialKey("p"))
	assert.False(t, isSpecialKey("5"))
	assert.False(t, isSpecialKey("+"))
	assert.False(t, isSpecialKey(""))
	assert.False(t, isSpecialKey("{a}"), "braced single char is not a special key")
	assert.False(t, isSpecialKey("{5}"), "braced single digit is not a special key")
}

func TestDefaultInputMode(t *testing.T) {
	t.Parallel()

	for _, id := range []string{
		platformids.Linux, platformids.Windows, platformids.Mac,
		platformids.SteamOS, platformids.ChimeraOS, platformids.Bazzite,
	} {
		assert.Equal(t, config.InputModeCombos, defaultInputMode(id),
			"desktop platform %s should default to combos", id)
	}

	for _, id := range []string{
		platformids.Mister, platformids.Mistex, platformids.Batocera,
		platformids.Recalbox, platformids.LibreELEC, platformids.RetroPie,
		platformids.ReplayOS,
	} {
		assert.Equal(t, config.InputModeUnrestricted, defaultInputMode(id),
			"embedded platform %s should default to unrestricted", id)
	}
}

func TestCheckInputKey_CombosMode(t *testing.T) {
	t.Parallel()

	cfg := newCfgFromTOML(t, `
[zapscript.input]
mode = "combos"
block = []
`)

	// Special keys and combos allowed
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{f1}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{enter}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+q}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{alt+f4}"))

	// Single characters blocked
	err := checkInputKey(cfg, platformids.Linux, "a")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)

	err = checkInputKey(cfg, platformids.Linux, "5")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)
}

func TestCheckInputKey_UnrestrictedMode(t *testing.T) {
	t.Parallel()

	cfg := newCfgFromTOML(t, `
[zapscript.input]
mode = "unrestricted"
`)

	// Everything passes on embedded (no default block list)
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "a"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{f1}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{ctrl+alt+t}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{super}"))
}

func TestCheckInputKey_DefaultBlockList(t *testing.T) {
	t.Parallel()

	cfg := newCfgFromTOML(t, `
[zapscript.input]
mode = "unrestricted"
`)

	// Default block list applied on desktop
	err := checkInputKey(cfg, platformids.Linux, "{ctrl+alt+t}")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputBlocked)

	err = checkInputKey(cfg, platformids.Linux, "{super}")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputBlocked)

	err = checkInputKey(cfg, platformids.Linux, "{ctrl+alt+f1}")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputBlocked)

	// Not applied on embedded
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{ctrl+alt+t}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{super}"))
}

func TestCheckInputKey_CustomBlockList(t *testing.T) {
	t.Parallel()

	cfg := newCfgFromTOML(t, `
[zapscript.input]
mode = "unrestricted"
block = ["{f12}", "{ctrl+q}"]
`)

	// Custom blocked key
	err := checkInputKey(cfg, platformids.Linux, "{f12}")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputBlocked)

	// Default block list items NOT blocked when custom list replaces it
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+alt+t}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{super}"))
}

func TestCheckInputKey_EmptyBlockListClearsDefaults(t *testing.T) {
	t.Parallel()

	cfg := newCfgFromTOML(t, `
[zapscript.input]
mode = "unrestricted"
block = []
`)

	// Default-blocked keys now allowed because block = [] clears defaults
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+alt+t}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{super}"))
}

func TestCheckInputKey_AllowStrictMode(t *testing.T) {
	t.Parallel()

	cfg := newCfgFromTOML(t, `
[zapscript.input]
mode = "combos"
allow = ["p", "{f1}", "{ctrl+q}"]
`)

	// Only allowed keys pass
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "p"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "P"), "case insensitive")
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{f1}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+q}"))

	// Everything else blocked — mode and block list irrelevant
	err := checkInputKey(cfg, platformids.Linux, "a")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)

	err = checkInputKey(cfg, platformids.Linux, "{f2}")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)
}

func TestCheckInputKey_AllowOverridesBlockList(t *testing.T) {
	t.Parallel()

	// Allow includes a default-blocked key
	cfg := newCfgFromTOML(t, `
[zapscript.input]
mode = "unrestricted"
allow = ["{ctrl+alt+delete}", "{f1}"]
`)

	// Allowed even though it's in default block list — allow is strict mode,
	// block list doesn't apply when allow is set
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+alt+delete}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{f1}"))

	// Not in allow list — blocked
	err := checkInputKey(cfg, platformids.Linux, "{super}")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)
}

func TestCheckInputKey_CombosDefaultOnDesktop(t *testing.T) {
	t.Parallel()

	// No explicit mode — desktop defaults to combos
	cfg := &config.Instance{}

	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{f1}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+q}"))

	err := checkInputKey(cfg, platformids.Linux, "a")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)
}

func TestCheckInputKey_UnrestrictedDefaultOnEmbedded(t *testing.T) {
	t.Parallel()

	// No explicit mode — embedded defaults to unrestricted
	cfg := &config.Instance{}

	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "a"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{ctrl+q}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{ctrl+alt+t}"))
}

func TestCheckInputKey_UnknownModeDefaultsToCombos(t *testing.T) {
	t.Parallel()

	cfg := newCfgFromTOML(t, `
[zapscript.input]
mode = "bogus-mode"
block = []
`)

	// Special keys allowed
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{f1}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+q}"))

	// Single characters blocked (combos behavior)
	err := checkInputKey(cfg, platformids.Linux, "a")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)
}

func TestCmdCoinMultiplayerKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmdName string
		cmdFunc func(platforms.Platform, platforms.CmdEnv) (platforms.CmdResult, error)
		wantKey string
	}{
		{
			name:    "player 3 coin",
			cmdName: gozapscript.ZapScriptCmdInputCoinP3,
			cmdFunc: cmdCoinP3,
			wantKey: "7",
		},
		{
			name:    "player 4 coin",
			cmdName: gozapscript.ZapScriptCmdInputCoinP4,
			cmdFunc: cmdCoinP4,
			wantKey: "8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.On("KeyboardPress", tt.wantKey).Return(nil).Once()

			env := platforms.CmdEnv{
				Cmd: gozapscript.Command{Name: tt.cmdName},
			}

			_, err := tt.cmdFunc(mockPlatform, env)
			require.NoError(t, err)
			assert.Equal(t, []string{tt.wantKey}, mockPlatform.GetKeyboardPresses())
			mockPlatform.AssertExpectations(t)
		})
	}
}

func TestCmdKeyboard_CombosBlocksSingleChar(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return(platformids.Linux)

	cfg := newCfgFromTOML(t, `
[zapscript.input]
block = []
`)

	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdInputKeyboard,
			Args: []string{"a"},
		},
		Cfg: cfg,
	}

	_, err := cmdKeyboard(mockPlatform, env)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)
}

func TestCmdKeyboard_CombosAllowsSpecialKey(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return(platformids.Linux)
	mockPlatform.On("KeyboardPress", "{f1}").Return(nil)

	cfg := newCfgFromTOML(t, `
[zapscript.input]
block = []
`)

	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdInputKeyboard,
			Args: []string{"{f1}"},
		},
		Cfg: cfg,
	}

	_, err := cmdKeyboard(mockPlatform, env)
	require.NoError(t, err)
	mockPlatform.AssertCalled(t, "KeyboardPress", "{f1}")
}

func TestCmdKeyboard_BracedSingleCharRejected(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return(platformids.Linux)

	cfg := newCfgFromTOML(t, `
[zapscript.input]
block = []
`)

	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdInputKeyboard,
			Args: []string{"{a}"},
		},
		Cfg: cfg,
	}

	_, err := cmdKeyboard(mockPlatform, env)
	require.ErrorIs(t, err, ErrInputNotAllowed)
	mockPlatform.AssertNotCalled(t, "KeyboardPress", "{a}")
}

func TestCmdGamepad_CombosBlocksSingleChar(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return(platformids.Linux)

	cfg := newCfgFromTOML(t, `
[zapscript.input]
block = []
`)

	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdInputGamepad,
			Args: []string{"A"},
		},
		Cfg: cfg,
	}

	_, err := cmdGamepad(mockPlatform, env)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)
}

func TestPressKeyboardSequence_UsesPlatformSequencer(t *testing.T) {
	t.Parallel()

	mockPlatform := newSequenceMockPlatform()
	args := []string{"A", "{delay:0}"}

	err := PressKeyboardSequence(mockPlatform, args, 7*time.Millisecond)

	require.NoError(t, err)
	assert.Equal(t, args, mockPlatform.recorder.sequenceArgs)
	assert.Equal(t, 7*time.Millisecond, mockPlatform.recorder.sequenceDelay)
}

func TestPressKeyboardSequence_SequencerErrorIsWrapped(t *testing.T) {
	t.Parallel()

	mockPlatform := newSequenceMockPlatform()
	mockPlatform.recorder.sequenceErr = errors.New("device failed")

	err := PressKeyboardSequence(mockPlatform, []string{"a"}, time.Nanosecond)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keyboard sequence")
	assert.Contains(t, err.Error(), "device failed")
}

func TestPressGamepadSequence_PressesButtons(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("GamepadPress", "a").Return(nil).Once()
	mockPlatform.On("GamepadPress", "start").Return(nil).Once()

	err := PressGamepadSequence(mockPlatform, []string{"a", "start"}, time.Nanosecond)

	require.NoError(t, err)
	assert.Equal(t, []string{"a", "start"}, mockPlatform.GetGamepadPresses())
	mockPlatform.AssertExpectations(t)
}

func TestParseSpeedArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		speed string
		want  time.Duration
	}{
		{name: "unset", want: 0},
		{name: "milliseconds integer", speed: "25", want: 25 * time.Millisecond},
		{name: "go duration", speed: "250ms", want: 250 * time.Millisecond},
		{name: "invalid", speed: "bogus", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			advArgs := gozapscript.NewAdvArgs(nil)
			if tt.speed != "" {
				advArgs = gozapscript.NewAdvArgs(map[string]string{"speed": tt.speed})
			}

			got := parseSpeedArg(platforms.CmdEnv{Cmd: gozapscript.Command{AdvArgs: advArgs}})

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCmdKeyboard_SpeedArgPassedToSequencer(t *testing.T) {
	t.Parallel()

	mockPlatform := newSequenceMockPlatform()
	mockPlatform.On("ID").Return(platformids.Linux)
	cfg := newCfgFromTOML(t, `
[zapscript.input]
block = []
`)
	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name:    gozapscript.ZapScriptCmdInputKeyboard,
			Args:    []string{"{f1}"},
			AdvArgs: gozapscript.NewAdvArgs(map[string]string{"speed": "250ms"}),
		},
		Cfg: cfg,
	}

	_, err := cmdKeyboard(mockPlatform, env)

	require.NoError(t, err)
	assert.Equal(t, []string{"{f1}"}, mockPlatform.recorder.sequenceArgs)
	assert.Equal(t, 250*time.Millisecond, mockPlatform.recorder.sequenceDelay)
}

func TestCmdGamepad_SpeedArgUsedForSequence(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return(platformids.Mister)
	mockPlatform.On("GamepadPress", "start").Return(nil).Once()
	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name:    gozapscript.ZapScriptCmdInputGamepad,
			Args:    []string{"start"},
			AdvArgs: gozapscript.NewAdvArgs(map[string]string{"speed": "1ns"}),
		},
		Cfg: &config.Instance{},
	}

	_, err := cmdGamepad(mockPlatform, env)

	require.NoError(t, err)
	assert.Equal(t, []string{"start"}, mockPlatform.GetGamepadPresses())
	mockPlatform.AssertExpectations(t)
}

func TestCmdInputText_UsesKeyboardSequence(t *testing.T) {
	t.Parallel()

	mockPlatform := newSequenceMockPlatform()
	mockPlatform.On("ID").Return(platformids.Mister)
	env := platforms.CmdEnv{
		Cmd: gozapscript.Command{
			Name: gozapscript.ZapScriptCmdInputText,
			Args: []string{"h", "i"},
		},
		Cfg: &config.Instance{},
	}

	_, err := cmdInputText(mockPlatform, env)

	require.NoError(t, err)
	assert.Equal(t, []string{"h", "i"}, mockPlatform.recorder.sequenceArgs)
	assert.Equal(t, defaultInterKeyDelay, mockPlatform.recorder.sequenceDelay)
}
