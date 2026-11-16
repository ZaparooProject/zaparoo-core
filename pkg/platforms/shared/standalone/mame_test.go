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

package standalone

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMAMEArgsWithoutConfigUseDefaultROMPath(t *testing.T) {
	t.Parallel()

	gameDir := filepath.Join(t.TempDir(), "Arcade")
	args, err := MAMEArgs(&Context{Path: filepath.Join(gameDir, "1941.zip")})
	require.NoError(t, err)
	assert.Equal(t, []string{"-rompath", gameDir + ";roms", "1941"}, args)
}

func TestMAMEArgsKeepConfiguredROMPath(t *testing.T) {
	t.Parallel()

	gameDir := filepath.Join(t.TempDir(), "Arcade")
	showConfig := []byte(`
#
# CORE SEARCH PATH OPTIONS
#
homepath                  .
rompath                   "roms;D:\BIOS;` + gameDir + `"
hashpath                  hash
`)
	args, err := MAMEArgs(&Context{
		Path:       filepath.Join(gameDir, "neobombe.7z"),
		MAMEConfig: func() ([]byte, error) { return showConfig, nil },
	})
	require.NoError(t, err)
	// The game folder is not repeated.
	assert.Equal(t, []string{"-rompath", gameDir + `;roms;D:\BIOS`, "neobombe"}, args)
}

func TestMAMEArgsRejectBadInput(t *testing.T) {
	t.Parallel()

	_, err := MAMEArgs(&Context{Path: filepath.Join(t.TempDir(), "outrun.chd")})
	require.Error(t, err)
	_, err = MAMEArgs(&Context{Path: filepath.Join(t.TempDir(), "a;b", "outrun.zip")})
	require.ErrorContains(t, err, "cannot contain ';'")
	_, err = MAMEArgs(&Context{
		Path:       filepath.Join(t.TempDir(), "outrun.zip"),
		MAMEConfig: func() ([]byte, error) { return nil, errors.New("no MAME") },
	})
	require.ErrorContains(t, err, "no MAME")
}

func TestMAMEConfiguredROMPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "b;c", MAMEConfiguredROMPath([]byte("ROMPATH a\nrompath\nrompath\tb;c\n")),
		"the last value wins")
	assert.Equal(t, "roms", MAMEConfiguredROMPath([]byte("# rompath commented\nwindow 1\n")))
	assert.Equal(t, "roms", MAMEConfiguredROMPath([]byte("rompath \"\"\n")), "an empty value means the default")
	assert.Equal(t, "roms", MAMEConfiguredROMPath(nil))
}

func TestMAMEShowConfigCapsOutput(t *testing.T) {
	t.Parallel()

	buffer := &limitedBuffer{limit: 4}
	n, err := io.Copy(buffer, strings.NewReader("abcdefg"))
	require.NoError(t, err)
	assert.Equal(t, int64(7), n, "writes report success so the process is not killed by a pipe error")
	assert.Equal(t, "abcd", string(buffer.Bytes()), "io.Copy cannot bypass the limit")
}

// TestMAMEShowConfigHelperProcess stands in for MAME when run by
// TestMAMEShowConfigRunsCommand.
func TestMAMEShowConfigHelperProcess(t *testing.T) {
	t.Parallel()

	if os.Getenv("ZAPAROO_FAKE_MAME") == "" {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		os.Exit(2)
	}
	_, _ = fmt.Fprintf(os.Stdout, "rompath %s;%s\n", os.Getenv("ZAPAROO_FAKE_MAME"), wd)
	os.Exit(0)
}

func TestMAMEShowConfigRunsCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	self, err := os.Executable()
	require.NoError(t, err)
	prepared := false
	output, err := MAMEShowConfig(&platforms.LaunchCommand{
		Executable: self,
		Args:       []string{"-test.run=^TestMAMEShowConfigHelperProcess$"},
		Dir:        dir,
		Env:        []string{"ZAPAROO_FAKE_MAME=bios"},
	}, func(*exec.Cmd) { prepared = true })
	require.NoError(t, err)
	assert.True(t, prepared)
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	got := MAMEConfiguredROMPath(output)
	assert.Contains(t, []string{"bios;" + dir, "bios;" + resolved}, got)

	_, err = MAMEShowConfig(&platforms.LaunchCommand{Executable: filepath.Join(dir, "missing.exe")}, nil)
	require.ErrorContains(t, err, "read MAME configuration")
}
