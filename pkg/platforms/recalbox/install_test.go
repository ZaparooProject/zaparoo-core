//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testScript = "#!/bin/sh\nZAPAROO_BIN=\"@ZAPAROO_BIN@\"\n\"$ZAPAROO_BIN\" -daemon\n"

const testExe = "/recalbox/share/system/zaparoo/zaparoo"

func newTestInstaller(t *testing.T, fs afero.Fs) (inst *Installer, started *[]string, signals *[]signalRecord) {
	t.Helper()
	started = &[]string{}
	signals = &[]signalRecord{}
	inst = &Installer{
		FS: fs,
		StartScript: func(path string) error {
			*started = append(*started, path)
			return nil
		},
		Signal: func(pid int, sig syscall.Signal) error {
			*signals = append(*signals, signalRecord{pid: pid, sig: sig})
			// The real supervisor removes its PID file as it exits.
			_ = fs.Remove(SupervisorPIDFile)
			return nil
		},
		Sleep:      func(time.Duration) {},
		Script:     testScript,
		Executable: testExe,
	}
	return inst, started, signals
}

func TestRenderScript(t *testing.T) {
	t.Parallel()

	out, err := renderScript(testScript, testExe)
	require.NoError(t, err)
	assert.Contains(t, out, `ZAPAROO_BIN="`+testExe+`"`)
	assert.NotContains(t, out, scriptExecPlaceholder)

	for _, bad := range []string{
		"zaparoo",
		"/path with space/zaparoo",
		"/a/$(reboot)/zaparoo",
		"/a/`id`/zaparoo",
		"/a/\"x/zaparoo",
		"/a/b;c/zaparoo",
		"/a/b\nc/zaparoo",
	} {
		_, err = renderScript(testScript, bad)
		require.Error(t, err, "%q", bad)
	}

	_, err = renderScript("#!/bin/sh\n", testExe)
	require.Error(t, err)
}

func TestInstall_WritesAndStartsScript(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst, started, _ := newTestInstaller(t, fs)

	require.NoError(t, inst.Install())
	data, err := afero.ReadFile(fs, ScriptPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `ZAPAROO_BIN="`+testExe+`"`)
	info, err := fs.Stat(ScriptPath)
	require.NoError(t, err)
	assert.Equal(t, "zaparoo(permanent).sh", info.Name())
	assert.Equal(t, []string{ScriptPath}, *started)

	require.NoError(t, inst.Install(), "installing again replaces the script")
	assert.Len(t, *started, 2)
}

func TestInstall_RestartsRunningSupervisor(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst, started, signals := newTestInstaller(t, fs)
	writeFile(t, fs, SupervisorPIDFile, "321\n")
	writeFile(t, fs, "/proc/321/cmdline", "/bin/sh\x00"+ScriptPath+"\x00")

	require.NoError(t, inst.Install())
	assert.Equal(t, []signalRecord{{pid: 321, sig: syscall.SIGTERM}}, *signals)
	assert.Equal(t, []string{ScriptPath}, *started)
}

func TestInstall_FailsWhenSupervisorWillNotExit(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst, started, _ := newTestInstaller(t, fs)
	inst.Signal = func(int, syscall.Signal) error { return nil }
	writeFile(t, fs, SupervisorPIDFile, "321\n")
	writeFile(t, fs, "/proc/321/cmdline", "/bin/sh\x00"+ScriptPath+"\x00")

	require.ErrorContains(t, inst.Install(), "did not exit")
	assert.Empty(t, *started)
}

func TestInstall_Errors(t *testing.T) {
	t.Parallel()

	inst, started, _ := newTestInstaller(t, afero.NewMemMapFs())
	inst.Executable = "relative/zaparoo"
	require.Error(t, inst.Install())
	assert.Empty(t, *started)

	inst, _, _ = newTestInstaller(t, afero.NewMemMapFs())
	inst.StartScript = func(string) error { return errors.New("boom") }
	require.Error(t, inst.Install())

	inst, started, _ = newTestInstaller(t, afero.NewReadOnlyFs(afero.NewMemMapFs()))
	require.Error(t, inst.Install())
	assert.Empty(t, *started)
}

func TestUninstall_StopsSupervisorAndRemovesScript(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst, _, signals := newTestInstaller(t, fs)
	require.NoError(t, inst.Install())
	writeFile(t, fs, SupervisorPIDFile, "321\n")
	writeFile(t, fs, "/proc/321/cmdline", "/bin/sh\x00"+ScriptPath+"\x00")

	require.NoError(t, inst.Uninstall())
	assert.Equal(t, []signalRecord{{pid: 321, sig: syscall.SIGTERM}}, *signals)
	_, err := fs.Stat(ScriptPath)
	require.Error(t, err)

	require.NoError(t, inst.Uninstall(), "uninstalling twice is fine")
}

func TestUninstall_IgnoresStaleOrForeignPID(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst, _, signals := newTestInstaller(t, fs)

	writeFile(t, fs, SupervisorPIDFile, "321")
	writeFile(t, fs, "/proc/321/cmdline", "/usr/bin/other\x00")
	require.NoError(t, inst.Uninstall())

	writeFile(t, fs, SupervisorPIDFile, "1")
	require.NoError(t, inst.Uninstall())

	writeFile(t, fs, SupervisorPIDFile, "not a pid")
	require.NoError(t, inst.Uninstall())

	assert.Empty(t, *signals)
}

func TestUninstall_SignalError(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst, _, _ := newTestInstaller(t, fs)
	writeFile(t, fs, SupervisorPIDFile, "321")
	writeFile(t, fs, "/proc/321/cmdline", "sh\x00"+ScriptPath+"\x00")

	inst.Signal = func(int, syscall.Signal) error { return syscall.ESRCH }
	require.NoError(t, inst.Uninstall(), "an exited supervisor is not an error")

	inst.Signal = func(int, syscall.Signal) error { return syscall.EPERM }
	require.Error(t, inst.Uninstall())
}
