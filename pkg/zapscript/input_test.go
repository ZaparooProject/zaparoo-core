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

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	} {
		assert.Equal(t, config.InputModeUnrestricted, defaultInputMode(id),
			"embedded platform %s should default to unrestricted", id)
	}
}

func TestCheckInputKey_CombosMode(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
mode = "combos"
block = []
`))

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

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
mode = "unrestricted"
`))

	// Everything passes on embedded (no default block list)
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "a"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{f1}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{ctrl+alt+t}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Mister, "{super}"))
}

func TestCheckInputKey_DefaultBlockList(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
mode = "unrestricted"
`))

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

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
mode = "unrestricted"
block = ["{f12}", "{ctrl+q}"]
`))

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

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
mode = "unrestricted"
block = []
`))

	// Default-blocked keys now allowed because block = [] clears defaults
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+alt+t}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{super}"))
}

func TestCheckInputKey_AllowStrictMode(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
mode = "combos"
allow = ["p", "{f1}", "{ctrl+q}"]
`))

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

	cfg := &config.Instance{}
	// Allow includes a default-blocked key
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
mode = "unrestricted"
allow = ["{ctrl+alt+delete}", "{f1}"]
`))

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

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
mode = "bogus-mode"
block = []
`))

	// Special keys allowed
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{f1}"))
	assert.NoError(t, checkInputKey(cfg, platformids.Linux, "{ctrl+q}"))

	// Single characters blocked (combos behavior)
	err := checkInputKey(cfg, platformids.Linux, "a")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInputNotAllowed)
}

func TestCmdKeyboard_CombosBlocksSingleChar(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return(platformids.Linux)

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
block = []
`))

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

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
block = []
`))

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

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
block = []
`))

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

	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML(`
[zapscript.input]
block = []
`))

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
