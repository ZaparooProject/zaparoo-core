//go:build windows

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

package windows

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/standalone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestCustomIDTakenIgnoresCase(t *testing.T) {
	t.Parallel()

	taken := customIDTaken([]platforms.Launcher{{ID: "MAME"}, {ID: "dolphinwii"}})
	assert.True(t, taken("mame"))
	assert.True(t, taken("DolphinWii"))
	assert.False(t, taken("DolphinGameCube"))
}

func TestRetroArchLaunchersSkipTakenIDs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	install := standalone.WindowsInstall{Exe: filepath.Join(dir, "retroarch.exe"), Dir: dir}
	all := retroArchLaunchers(dir, install, nil)
	require.NotEmpty(t, all)
	skipped := all[0].ID
	some := retroArchLaunchers(dir, install, func(id string) bool { return id == skipped })
	assert.Len(t, some, len(all)-1)
	for i := range some {
		assert.NotEqual(t, skipped, some[i].ID)
	}
}

func TestHideConsoleWindowKeepsOtherFlags(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "emulator.exe")
	hideConsoleWindow(cmd)
	assert.Equal(t, uint32(windows.CREATE_NO_WINDOW), cmd.SysProcAttr.CreationFlags)
	cmd.SysProcAttr.CreationFlags = windows.CREATE_NEW_PROCESS_GROUP
	hideConsoleWindow(cmd)
	assert.Equal(t, uint32(windows.CREATE_NEW_PROCESS_GROUP|windows.CREATE_NO_WINDOW), cmd.SysProcAttr.CreationFlags)
}
