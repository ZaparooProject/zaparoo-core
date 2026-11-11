//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/muos"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userInitDir is where muOS runs user init scripts from.
const userInitDir = "/run/muos/storage/init"

// withArgs makes run() see args, on a fresh flag set so it can be called
// more than once per test binary.
func withArgs(t *testing.T, args ...string) {
	t.Helper()
	oldArgs, oldFlags := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = oldArgs, oldFlags })
	os.Args = append([]string{"zaparoo"}, args...)
	flag.CommandLine = flag.NewFlagSet("zaparoo", flag.ContinueOnError)
}

type fakeDevice struct {
	fs        afero.Fs
	stopErr   error
	startErr  error
	removeErr error
	calls     []string
}

const testExe = "/mnt/mmc/MUOS/application/Zaparoo/zaparoo"

// useFakeDevice swaps every device side effect for a recording fake. Not
// parallel: it replaces package variables.
func useFakeDevice(t *testing.T) *fakeDevice {
	t.Helper()
	dev := &fakeDevice{fs: afero.NewMemMapFs()}
	require.NoError(t, dev.fs.MkdirAll(userInitDir, 0o755))
	oldExe, oldFS, oldStop, oldUninstall, oldStart :=
		findExecutable, initFS, stopService, uninstallScript, startService
	t.Cleanup(func() {
		findExecutable, initFS, stopService, uninstallScript, startService =
			oldExe, oldFS, oldStop, oldUninstall, oldStart
	})
	findExecutable = func() (string, error) { return testExe, nil }
	initFS = dev.fs
	stopService = func(_ *muos.Platform, exe string) error {
		dev.calls = append(dev.calls, "stop "+exe)
		return dev.stopErr
	}
	uninstallScript = func(_ *muos.Platform, exe string) error {
		dev.calls = append(dev.calls, "uninstall "+exe)
		return dev.removeErr
	}
	startService = func(path string) error {
		dev.calls = append(dev.calls, "start "+path)
		return dev.startErr
	}
	return dev
}

func TestRun_InstallWritesAndStartsInitScript(t *testing.T) {
	dev := useFakeDevice(t)
	withArgs(t, "-install")

	require.NoError(t, run())

	path := filepath.Join(userInitDir, "zaparoo.sh")
	script, err := afero.ReadFile(dev.fs, path)
	require.NoError(t, err)
	want, err := muos.InitScript(testExe)
	require.NoError(t, err)
	assert.Equal(t, want, string(script))
	assert.Equal(t, []string{"stop " + testExe, "start " + path}, dev.calls,
		"the old service stops before the new script starts")
}

func TestInstall_Failures(t *testing.T) {
	t.Run("executable unknown", func(t *testing.T) {
		dev := useFakeDevice(t)
		findExecutable = func() (string, error) { return "", errors.New("no exe") }
		require.ErrorContains(t, install(&muos.Platform{}), "no exe")
		assert.Empty(t, dev.calls)
	})
	t.Run("stop fails", func(t *testing.T) {
		dev := useFakeDevice(t)
		dev.stopErr = errors.New("busy")
		require.ErrorContains(t, install(&muos.Platform{}), "error stopping running zaparoo service")
		assert.Equal(t, []string{"stop " + testExe}, dev.calls)
	})
	t.Run("no init folder", func(t *testing.T) {
		dev := useFakeDevice(t)
		initFS = afero.NewMemMapFs()
		require.ErrorContains(t, install(&muos.Platform{}), "error installing init script")
		assert.Equal(t, []string{"stop " + testExe}, dev.calls, "nothing started")
	})
	t.Run("start fails", func(t *testing.T) {
		dev := useFakeDevice(t)
		dev.startErr = errors.New("no sh")
		require.ErrorContains(t, install(&muos.Platform{}), "no sh")
	})
}

func TestRun_UninstallStopsService(t *testing.T) {
	dev := useFakeDevice(t)
	withArgs(t, "-uninstall")

	require.NoError(t, run())
	assert.Equal(t, []string{"uninstall " + testExe}, dev.calls)

	dev.removeErr = errors.New("read-only")
	require.ErrorContains(t, uninstall(&muos.Platform{}), "error uninstalling init script")

	findExecutable = func() (string, error) { return "", errors.New("no exe") }
	require.ErrorContains(t, uninstall(&muos.Platform{}), "no exe")
}

func TestStartInitScript_RunsDetached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "zaparoo.sh")
	// The session check proves Setsid: the script leads its own session.
	body := "#!/bin/sh\n[ \"$(cut -d' ' -f6 /proc/$$/stat)\" = \"$$\" ] && echo setsid > '" +
		marker + "' || echo shared > '" + marker + "'\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))

	require.NoError(t, startInitScript(script))
	assert.Eventually(t, func() bool {
		data, err := os.ReadFile(marker) //nolint:gosec // test temp file
		return err == nil && string(data) == "setsid\n"
	}, 5*time.Second, 10*time.Millisecond)
}

func TestExecutable_ResolvesTestBinary(t *testing.T) {
	t.Parallel()
	exe, err := executable()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(exe))
	resolved, err := filepath.EvalSymlinks(exe)
	require.NoError(t, err)
	assert.Equal(t, resolved, exe)
}
