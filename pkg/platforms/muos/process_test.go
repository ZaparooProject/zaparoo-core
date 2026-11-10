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

package muos

import (
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPidsOf(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	addProc(t, fs, testSelf, "muxplore", "muxplore")
	addProc(t, fs, 10, "muxplore", "/opt/muos/frontend/muxplore")
	addProc(t, fs, 11, "a_very_long_emu", "/usr/bin/a_very_long_emulator_name")
	addProc(t, fs, 12, "sh", "/usr/bin/flycast", "-config")
	addProc(t, fs, 13, "muxplore", "muxplore")
	writeFile(t, fs, "/proc/13/stat", "13 (muxplore) Z 1 1")
	writeFile(t, fs, "/proc/self/comm", "muxplore")

	assert.Equal(t, []int{10}, p.pidsOf("muxplore"), "self, zombies and non-pid entries are skipped")
	assert.Equal(t, []int{11}, p.pidsOf("a_very_long_emulator_name"))
	assert.Equal(t, []int{12}, p.pidsOf("flycast"))
	assert.Empty(t, p.pidsOf(""))
	assert.Empty(t, p.pidsOf("../10"))
}

func TestCloseProcessTerminates(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	addProc(t, fs, 20, "retroarch", "retroarch")
	sigs.onSignal = func(pid int, sig syscall.Signal) {
		if sig == syscall.SIGTERM {
			removeProc(fs, pid)
		}
	}

	require.NoError(t, p.closeProcess("retroarch"))
	assert.Equal(t, []sentSignal{{20, syscall.SIGCONT}, {20, syscall.SIGTERM}}, sigs.signals())
}

func TestCloseProcessKillsWhenTermIgnored(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	addProc(t, fs, 20, "retroarch", "retroarch")
	sigs.onSignal = func(pid int, sig syscall.Signal) {
		if sig == syscall.SIGKILL {
			removeProc(fs, pid)
		}
	}

	require.NoError(t, p.closeProcess("retroarch"))
	assert.Equal(t, []sentSignal{
		{20, syscall.SIGCONT}, {20, syscall.SIGTERM}, {20, syscall.SIGKILL},
	}, sigs.signals())
}

func TestCloseProcessReportsSurvivor(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	addProc(t, fs, 20, "retroarch", "retroarch")

	err := p.closeProcess("retroarch")
	require.ErrorIs(t, err, platforms.ErrStopFailed)
}

func TestCloseProcessNothingRunning(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	require.NoError(t, p.closeProcess("retroarch"))
	assert.Empty(t, sigs.signals())
}

func TestIsMenuModule(t *testing.T) {
	t.Parallel()
	assert.True(t, isMenuModule("muxfrontend"))
	assert.True(t, isMenuModule("muxcharge"))
	assert.True(t, isMenuModule("muxbackup"))
	assert.False(t, isMenuModule("muxretro"))
	assert.False(t, isMenuModule("muxmedia"), "video content runs as muxmedia")
	assert.False(t, isMenuModule("muxterm"), "frotz content runs as muxterm")
	assert.False(t, isMenuModule(externalForeground))
	assert.False(t, isMenuModule("retroarch"))
	assert.False(t, isMenuModule(""))
}

func addChildProc(t *testing.T, fs afero.Fs, pid, ppid int, comm string, args ...string) {
	t.Helper()
	addProc(t, fs, pid, comm, args...)
	writeFile(t, fs, filepath.Join(testProc, strconv.Itoa(pid), "stat"),
		strconv.Itoa(pid)+" ("+comm+") S "+strconv.Itoa(ppid)+" 1")
}

// addPortTree mirrors a running port: launch.sh runs ext-general.sh, which
// runs the port script without exec, which runs the game binary.
func addPortTree(t *testing.T, fs afero.Fs) {
	t.Helper()
	addChildProc(t, fs, 30, 1, "launch.sh", "/bin/sh", "/opt/muos/script/mux/launch.sh")
	addChildProc(t, fs, 31, 30, "ext-general.sh",
		"/bin/sh", externalLauncher, "Game", "external", "/mnt/mmc/ROMS/PORTS/Game.sh")
	addChildProc(t, fs, 32, 31, "Game.sh", "bash", "/mnt/mmc/ROMS/PORTS/Game.sh")
	addChildProc(t, fs, 33, 32, "game.aarch64", "./game.aarch64")
	addChildProc(t, fs, 40, 1, "retroarch", "retroarch")
}

func TestForegroundPIDsExternalPort(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	addPortTree(t, fs)

	assert.Equal(t, []int{32, 33}, p.foregroundPIDs(externalForeground),
		"the port's process tree, not the launcher scripts or unrelated processes")
	assert.Empty(t, p.pidsOf(externalForeground), "no process is named external")
	assert.Equal(t, []int{40}, p.foregroundPIDs("retroarch"))
}

func TestCloseProcessEndsExternalPort(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	addPortTree(t, fs)
	sigs.onSignal = func(pid int, sig syscall.Signal) {
		if sig == syscall.SIGTERM {
			removeProc(fs, pid)
		}
	}

	require.NoError(t, p.closeProcess(externalForeground))
	assert.Equal(t, []sentSignal{
		{32, syscall.SIGCONT}, {33, syscall.SIGCONT}, {32, syscall.SIGTERM}, {33, syscall.SIGTERM},
	}, sigs.signals())
	assert.Empty(t, p.foregroundPIDs(externalForeground))
}
