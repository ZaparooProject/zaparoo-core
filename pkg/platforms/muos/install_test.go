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
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitScript(t *testing.T) {
	t.Parallel()
	script, err := InitScript("/mnt/mmc/MUOS/zaparoo/it's zaparoo")
	require.NoError(t, err)

	assert.Contains(t, script, "#!/bin/sh\n")
	assert.Contains(t, script, "PIDFILE='/run/zaparoo-supervisor.pid'\n")
	assert.Contains(t, script, "*zaparoo.sh*) exit 0 ;;")
	assert.NotContains(t, script, "pidof", "pidof also matches the -install process")
	assert.Contains(t, script, "cd '/mnt/mmc/MUOS/zaparoo' || exit 1")
	assert.Contains(t, script, `	'/mnt/mmc/MUOS/zaparoo/it'\''s zaparoo' -daemon`)
	assert.Contains(t, script, `{ [ "$RC" -eq 0 ] || [ "$RC" -eq 78 ]; } && break`)

	check := exec.CommandContext(context.Background(), "/bin/sh", "-n")
	check.Stdin = strings.NewReader(script)
	out, err := check.CombinedOutput()
	require.NoError(t, err, "script syntax: %s", out)

	_, err = InitScript("relative/zaparoo")
	require.Error(t, err)
	_, err = InitScript("/mnt/mmc/zap\naroo")
	require.Error(t, err)
}

func TestInstallPrefersActiveStorage(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	require.NoError(t, fs.MkdirAll(userInitDir, 0o755))
	require.NoError(t, fs.MkdirAll("/mnt/mmc/MUOS/init", 0o755))

	path, err := Install(fs, "/mnt/mmc/MUOS/zaparoo/zaparoo")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(userInitDir, initScriptName), path)
	assert.Contains(t, readFile(t, fs, path), "'/mnt/mmc/MUOS/zaparoo/zaparoo' -daemon")
	info, err := fs.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, 0o755, int(info.Mode().Perm()))
}

func TestInstallFallsBackToSDCard(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, romMountFile, "/media/primary\n")
	require.NoError(t, fs.MkdirAll("/media/primary/MUOS/init", 0o755))

	path, err := Install(fs, "/media/primary/MUOS/zaparoo/zaparoo")
	require.NoError(t, err)
	assert.Equal(t, "/media/primary/MUOS/init/zaparoo.sh", path)

	_, err = Install(afero.NewMemMapFs(), "/zaparoo")
	require.Error(t, err, "no init folder")
}

func TestUninstall(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	script := filepath.Join(userInitDir, initScriptName)
	writeFile(t, fs, script, "#!/bin/sh")
	addProc(t, fs, testSelf, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "-uninstall")
	addProc(t, fs, 50, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "-daemon")
	addProc(t, fs, 51, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo")
	sigs.onSignal = func(pid int, _ syscall.Signal) { removeProc(fs, pid) }

	require.NoError(t, p.Uninstall("/mnt/mmc/MUOS/zaparoo/zaparoo"))

	exists, err := afero.Exists(fs, script)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, []sentSignal{{50, syscall.SIGTERM}}, sigs.signals())

	require.NoError(t, p.Uninstall("/mnt/mmc/MUOS/zaparoo/zaparoo"), "already removed")
}

func TestStopServiceStopsSupervisorAndDaemonAndWaits(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	writeFile(t, fs, supervisorPIDFile, "40\n")
	addProc(t, fs, 40, "sh", "sh", filepath.Join(userInitDir, initScriptName))
	addProc(t, fs, 50, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "-daemon")
	sigs.onSignal = func(pid int, _ syscall.Signal) { removeProc(fs, pid) }

	require.NoError(t, p.StopService("/mnt/mmc/MUOS/zaparoo/zaparoo"))
	assert.ElementsMatch(t, []sentSignal{{50, syscall.SIGTERM}, {40, syscall.SIGTERM}}, sigs.signals())
}

func TestStopServiceIgnoresStaleSupervisorPID(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, sigs, _ := newTestPlatform(t, fs)
	writeFile(t, fs, supervisorPIDFile, "40\n")
	addProc(t, fs, 40, "other", "/usr/bin/other")

	require.NoError(t, p.StopService("/mnt/mmc/MUOS/zaparoo/zaparoo"))
	assert.Empty(t, sigs.signals())
}

func TestStopServiceFailsWhenDaemonSurvives(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	addProc(t, fs, 50, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "-daemon")

	require.ErrorContains(t, p.StopService("/mnt/mmc/MUOS/zaparoo/zaparoo"), "did not stop")
}

func TestInstallFallsBackWhenWriteFails(t *testing.T) {
	t.Parallel()
	base := newTestFs(t)
	require.NoError(t, base.MkdirAll(userInitDir, 0o755))
	require.NoError(t, base.MkdirAll("/mnt/mmc/MUOS/init", 0o755))
	fs := &failFs{Fs: base, op: "open", path: filepath.Join(userInitDir, initScriptName)}

	path, err := Install(fs, "/mnt/mmc/MUOS/zaparoo/zaparoo")
	require.NoError(t, err)
	assert.Equal(t, "/mnt/mmc/MUOS/init/zaparoo.sh", path)

	require.NoError(t, base.RemoveAll("/mnt/mmc/MUOS/init"))
	_, err = Install(fs, "/mnt/mmc/MUOS/zaparoo/zaparoo")
	require.ErrorContains(t, err, "user init folder not found")

	_, err = Install(base, "relative/zaparoo")
	require.Error(t, err, "bad executable path")
}

func TestUninstallReportsRemoveFailure(t *testing.T) {
	t.Parallel()
	base := newTestFs(t)
	script := filepath.Join(userInitDir, initScriptName)
	writeFile(t, base, script, "#!/bin/sh")
	p, sigs, _ := newTestPlatform(t, &failFs{Fs: base, op: "remove", path: script})

	require.ErrorIs(t, p.Uninstall("/mnt/mmc/MUOS/zaparoo/zaparoo"), errInjected)
	assert.Empty(t, sigs.signals(), "the service is left running")
}

func TestStopServiceSignalErrors(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	addProc(t, fs, 50, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "-daemon")

	p.signal = func(pid int, _ syscall.Signal) error {
		removeProc(fs, pid)
		return syscall.ESRCH
	}
	require.NoError(t, p.StopService("/mnt/mmc/MUOS/zaparoo/zaparoo"), "already gone is fine")

	addProc(t, fs, 50, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "-daemon")
	p.signal = func(int, syscall.Signal) error { return syscall.EPERM }
	require.ErrorIs(t, p.StopService("/mnt/mmc/MUOS/zaparoo/zaparoo"), syscall.EPERM)
}

func TestStopServiceDaemonExitsOnLastWait(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	addProc(t, fs, 50, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "-daemon")
	polls := int(serviceStopTimeout / closePollStep)
	pauses := 0
	p.sleep = func(time.Duration) {
		pauses++
		if pauses == polls {
			removeProc(fs, 50)
		}
	}

	require.NoError(t, p.StopService("/mnt/mmc/MUOS/zaparoo/zaparoo"))
	assert.Equal(t, polls, pauses)
}

func TestSupervisorPIDRejectsBadPIDFiles(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	p.pid = 77
	// pid 1 and Core itself look like the init script, so only the pid
	// checks can turn them away. pid 60 is not running at all.
	script := filepath.Join(userInitDir, initScriptName)
	addProc(t, fs, 1, "sh", "sh", script)
	addProc(t, fs, p.pid, "sh", "sh", script)
	for _, content := range []string{"abc", "1", strconv.Itoa(p.pid), "-5", "60"} {
		writeFile(t, fs, supervisorPIDFile, content+"\n")
		_, ok := p.supervisorPID()
		assert.False(t, ok, content)
	}
}

func TestDaemonPIDsSkipsUnreadableCmdline(t *testing.T) {
	t.Parallel()
	base := newTestFs(t)
	addProc(t, base, 50, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "-daemon")
	addProc(t, base, 51, "zaparoo", "/mnt/mmc/MUOS/zaparoo/zaparoo", "--daemon")
	p, _, _ := newTestPlatform(t, &failFs{Fs: base, op: "open", path: filepath.Join(testProc, "50", "cmdline")})

	assert.Equal(t, []int{51}, p.daemonPIDs("zaparoo"))
}
