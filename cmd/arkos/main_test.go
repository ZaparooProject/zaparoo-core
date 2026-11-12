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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type execCall struct {
	path string
	argv []string
}

func recordExec(calls *[]execCall, err error) func(string, []string, []string) error {
	return func(path string, argv, _ []string) error {
		*calls = append(*calls, execCall{path: path, argv: argv})
		return err
	}
}

func findSudo(string) (string, error) { return "/usr/bin/sudo", nil }

func TestRunAsRoot_AlreadyRoot(t *testing.T) {
	t.Parallel()
	var calls []execCall
	require.NoError(t, runAsRoot(0, "/home/ark/zaparoo", nil, findSudo, recordExec(&calls, nil)))
	assert.Empty(t, calls)
}

func TestRunAsRoot_ReexecsThroughSudo(t *testing.T) {
	t.Parallel()
	var calls []execCall
	require.NoError(t, runAsRoot(1002, "/home/ark/zaparoo", []string{"-run", "**echo:hi"},
		findSudo, recordExec(&calls, nil)))
	require.Len(t, calls, 1)
	assert.Equal(t, "/usr/bin/sudo", calls[0].path)
	assert.Equal(t, []string{"sudo", "-n", "--", "/home/ark/zaparoo", "-run", "**echo:hi"}, calls[0].argv)
}

func TestRunAsRoot_Failures(t *testing.T) {
	t.Parallel()
	var calls []execCall
	noSudo := func(string) (string, error) { return "", errors.New("not found") }
	err := runAsRoot(1002, "/home/ark/zaparoo", nil, noSudo, recordExec(&calls, nil))
	require.ErrorContains(t, err, "sudo was not found")
	assert.Empty(t, calls)

	err = runAsRoot(1002, "/home/ark/zaparoo", nil, findSudo, recordExec(&calls, errors.New("denied")))
	require.ErrorContains(t, err, "running it with sudo failed")
}

func TestPrepareInstallDir_TraversableNotWritable(t *testing.T) {
	t.Parallel()
	base := t.TempDir()

	fresh := filepath.Join(base, "fresh", "zaparoo")
	require.NoError(t, prepareInstallDir(fresh))
	info, err := os.Stat(fresh)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	// A directory from an earlier install, too closed or too open.
	for _, mode := range []os.FileMode{0o750, 0o777} {
		dir := filepath.Join(base, mode.String())
		require.NoError(t, os.Mkdir(dir, 0o700))
		require.NoError(t, os.Chmod(dir, mode))
		require.NoError(t, prepareInstallDir(dir))
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), mode.String())
	}
}

// withArgs makes run() see args, on a fresh flag set so it can be called
// more than once per test binary.
func withArgs(t *testing.T, args ...string) {
	t.Helper()
	oldArgs, oldFlags := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = oldArgs, oldFlags })
	os.Args = append([]string{"zaparoo"}, args...)
	flag.CommandLine = flag.NewFlagSet("zaparoo", flag.ContinueOnError)
}

type installEnv struct {
	dir       string
	exe       string
	unit      string
	failOn    string
	systemctl [][]string
}

// fakeInstall points the installer at a temp tree and records systemctl
// instead of running it. Not parallel: it swaps package variables.
func fakeInstall(t *testing.T) *installEnv {
	t.Helper()
	base := t.TempDir()
	env := &installEnv{
		dir:  filepath.Join(base, "home", "ark", ".config", "zaparoo"),
		exe:  filepath.Join(base, "download", "zaparoo"),
		unit: filepath.Join(base, "zaparoo.service"),
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(env.exe), 0o750))
	require.NoError(t, os.WriteFile(env.exe, []byte("binary"), 0o600))

	oldUnit, oldDir, oldSystemctl := serviceFilePath, installDir, runSystemctl
	oldExe, oldEuid := executable, geteuid
	t.Cleanup(func() {
		serviceFilePath, installDir, runSystemctl = oldUnit, oldDir, oldSystemctl
		executable, geteuid = oldExe, oldEuid
	})
	serviceFilePath = env.unit
	installDir = env.dir
	executable = func() (string, error) { return env.exe, nil }
	geteuid = func() int { return 0 }
	runSystemctl = func(args ...string) error {
		env.systemctl = append(env.systemctl, args)
		if env.failOn != "" && args[0] == env.failOn {
			return errors.New("systemctl failed")
		}
		return nil
	}
	return env
}

func TestRun_InstallWritesServiceAndBinary(t *testing.T) {
	env := fakeInstall(t)
	withArgs(t, "-install")

	require.NoError(t, run())

	unit, err := os.ReadFile(env.unit)
	require.NoError(t, err)
	assert.Equal(t, serviceFile, string(unit))
	info, err := os.Stat(env.unit)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())

	bin, err := os.ReadFile(filepath.Join(env.dir, binaryName))
	require.NoError(t, err)
	assert.Equal(t, "binary", string(bin))
	info, err = os.Stat(filepath.Join(env.dir, binaryName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	assert.NoFileExists(t, filepath.Join(env.dir, binaryName+".new"))

	assert.Equal(t, [][]string{
		{"daemon-reload"},
		{"enable", "zaparoo.service"},
		{"restart", "zaparoo.service"},
	}, env.systemctl)
}

func TestInstallService_FromInstalledBinaryKeepsIt(t *testing.T) {
	env := fakeInstall(t)
	require.NoError(t, os.MkdirAll(env.dir, 0o750))
	installed := filepath.Join(env.dir, binaryName)
	// Write-only: copying it over itself would fail on the read.
	require.NoError(t, os.WriteFile(installed, []byte("installed"), 0o200))
	executable = func() (string, error) { return installed, nil }

	require.NoError(t, installService(), "running from the install dir copies nothing")
	assert.NoFileExists(t, installed+".new")
}

func TestInstallService_SystemctlFailures(t *testing.T) {
	for _, step := range []string{"daemon-reload", "enable", "restart"} {
		t.Run(step, func(t *testing.T) {
			env := fakeInstall(t)
			env.failOn = step
			require.ErrorContains(t, installService(), "systemctl failed")
			assert.Equal(t, step, env.systemctl[len(env.systemctl)-1][0], "stops at the failing step")
		})
	}
}

func TestInstallService_Failures(t *testing.T) {
	t.Run("executable unknown", func(t *testing.T) {
		fakeInstall(t)
		executable = func() (string, error) { return "", errors.New("no exe") }
		require.ErrorContains(t, installService(), "error finding executable")
	})
	t.Run("executable unreadable", func(t *testing.T) {
		env := fakeInstall(t)
		require.NoError(t, os.Remove(env.exe))
		require.ErrorContains(t, installService(), "error reading executable")
	})
	t.Run("install dir is a file", func(t *testing.T) {
		env := fakeInstall(t)
		require.NoError(t, os.MkdirAll(filepath.Dir(env.dir), 0o750))
		require.NoError(t, os.WriteFile(env.dir, nil, 0o600))
		require.ErrorContains(t, installService(), "error creating install directory")
	})
	t.Run("unit dir missing", func(t *testing.T) {
		env := fakeInstall(t)
		serviceFilePath = filepath.Join(env.unit, "missing", "zaparoo.service")
		require.ErrorContains(t, installService(), "error writing service file")
		assert.Empty(t, env.systemctl)
	})
}

func TestRun_UninstallRemovesService(t *testing.T) {
	env := fakeInstall(t)
	require.NoError(t, os.WriteFile(env.unit, []byte(serviceFile), 0o600))
	withArgs(t, "-uninstall")

	require.NoError(t, run())

	assert.NoFileExists(t, env.unit)
	assert.Equal(t, [][]string{
		{"stop", "zaparoo.service"},
		{"disable", "zaparoo.service"},
		{"daemon-reload"},
	}, env.systemctl)

	// Again, with nothing installed: still succeeds.
	env.systemctl = nil
	require.NoError(t, uninstallService())
	assert.Len(t, env.systemctl, 3)

	env.failOn = "daemon-reload"
	require.ErrorContains(t, uninstallService(), "error reloading systemd")
}

func TestUninstallService_UnitNotRemovable(t *testing.T) {
	env := fakeInstall(t)
	require.NoError(t, os.MkdirAll(filepath.Join(env.unit, "child"), 0o750))
	require.ErrorContains(t, uninstallService(), "error removing service file")
}

func TestRun_ReexecsAsRootBeforeInstalling(t *testing.T) {
	env := fakeInstall(t)
	geteuid = func() int { return 1002 }
	oldLook, oldExec := lookPath, execProcess
	t.Cleanup(func() { lookPath, execProcess = oldLook, oldExec })
	lookPath = findSudo
	var calls []execCall
	execProcess = recordExec(&calls, errors.New("sudo: a password is required"))
	withArgs(t, "-install")

	require.ErrorContains(t, run(), "running it with sudo failed")
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"sudo", "-n", "--", env.exe, "-install"}, calls[0].argv)
	assert.NoFileExists(t, env.unit, "nothing installed without root")
	assert.Empty(t, env.systemctl)
}

func TestRun_ExecutableUnknown(t *testing.T) {
	fakeInstall(t)
	executable = func() (string, error) { return "", errors.New("no exe") }
	withArgs(t, "-install")
	require.ErrorContains(t, run(), "error finding executable")
}
