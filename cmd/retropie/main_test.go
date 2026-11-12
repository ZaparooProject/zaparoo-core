//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/retropie"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testExe = "/home/pi/zaparoo/zaparoo"

// withArgs makes run() see args, on a fresh flag set so it can be called
// more than once per test binary.
func withArgs(t *testing.T, args ...string) {
	t.Helper()
	oldArgs, oldFlags := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = oldArgs, oldFlags })
	os.Args = append([]string{"zaparoo"}, args...)
	flag.CommandLine = flag.NewFlagSet("zaparoo", flag.ContinueOnError)
}

type fakeSystem struct {
	err   error
	calls []string
	euid  int
}

// useFakeSystem records installer calls instead of touching the system. Each
// call also runs its CommandRunner, to show the terminal-attached runner is
// the one passed through. Not parallel: it replaces package variables.
func useFakeSystem(t *testing.T) *fakeSystem {
	t.Helper()
	sys := &fakeSystem{euid: 1000}
	oldEuid, oldExe, oldRun := geteuid, findExecutable, runCommand
	oldInstall, oldUninstall := userInstall, userUninstall
	oldSysInstall, oldSysUninstall := installSystemSide, uninstallSystemSide
	t.Cleanup(func() {
		geteuid, findExecutable, runCommand = oldEuid, oldExe, oldRun
		userInstall, userUninstall = oldInstall, oldUninstall
		installSystemSide, uninstallSystemSide = oldSysInstall, oldSysUninstall
	})
	record := func(ctx context.Context, run retropie.CommandRunner, call string) error {
		sys.calls = append(sys.calls, call)
		if err := run(ctx, "runner"); err != nil {
			return err
		}
		return sys.err
	}
	geteuid = func() int { return sys.euid }
	findExecutable = func() (string, error) { return testExe, nil }
	runCommand = func(_ context.Context, name string, args ...string) error {
		sys.calls = append(sys.calls, "ran "+strings.Join(append([]string{name}, args...), " "))
		return nil
	}
	userInstall = func(_ *retropie.Platform, ctx context.Context, exe string, run retropie.CommandRunner) error {
		return record(ctx, run, "install "+exe)
	}
	userUninstall = func(_ *retropie.Platform, ctx context.Context, exe string, run retropie.CommandRunner) error {
		return record(ctx, run, "uninstall "+exe)
	}
	installSystemSide = func(ctx context.Context, userName, exe string, run retropie.CommandRunner) error {
		return record(ctx, run, "install-system "+userName+" "+exe)
	}
	uninstallSystemSide = func(ctx context.Context, run retropie.CommandRunner) error {
		return record(ctx, run, "uninstall-system")
	}
	return sys
}

func TestRun_Dispatch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
		euid int
	}{
		{name: "install", args: []string{"-install"}, euid: 1000, want: []string{"install " + testExe, "ran runner"}},
		{
			name: "uninstall",
			args: []string{"-uninstall"},
			euid: 1000,
			want: []string{"uninstall " + testExe, "ran runner"},
		},
		{
			name: "system install as root",
			args: []string{"-" + retropie.InstallSystemFlag + "=pi"},
			euid: 0,
			want: []string{"install-system pi " + testExe, "ran runner"},
		},
		{
			name: "system uninstall as root",
			args: []string{"-" + retropie.UninstallSystemFlag},
			euid: 0,
			want: []string{"uninstall-system", "ran runner"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys := useFakeSystem(t)
			sys.euid = tt.euid
			withArgs(t, tt.args...)
			require.NoError(t, run())
			assert.Equal(t, tt.want, sys.calls)
		})
	}
}

func TestRun_DispatchErrors(t *testing.T) {
	tests := []struct {
		name string
		want string
		args []string
	}{
		{name: "install", args: []string{"-install"}, want: "install: boom"},
		{name: "uninstall", args: []string{"-uninstall"}, want: "uninstall: boom"},
		{
			name: "system install",
			args: []string{"-" + retropie.InstallSystemFlag + "=pi"},
			want: "install service: boom",
		},
		{name: "system uninstall", args: []string{"-" + retropie.UninstallSystemFlag}, want: "uninstall service: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sys := useFakeSystem(t)
			sys.err = errors.New("boom")
			withArgs(t, tt.args...)
			require.EqualError(t, run(), tt.want)
		})
	}
}

func TestRun_ExecutableUnknown(t *testing.T) {
	for _, args := range [][]string{{"-install"}, {"-" + retropie.UninstallSystemFlag}} {
		sys := useFakeSystem(t)
		findExecutable = func() (string, error) { return "", errors.New("no exe") }
		withArgs(t, args...)
		require.EqualError(t, run(), "no exe", args)
		assert.Empty(t, sys.calls)
	}
}

func TestRun_RefusesRoot(t *testing.T) {
	sys := useFakeSystem(t)
	sys.euid = 0
	for _, args := range [][]string{nil, {"-install"}, {"-daemon"}} {
		withArgs(t, args...)
		require.EqualError(t, run(), "zaparoo must not be run as root", args)
	}
	assert.Empty(t, sys.calls)
}

func TestRunInteractive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	require.NoError(t, runInteractive(ctx, "true"))
	err := runInteractive(ctx, "false")
	require.ErrorContains(t, err, "false: exit status 1")
}

func TestExecutablePath_ResolvesTestBinary(t *testing.T) {
	t.Parallel()
	exe, err := executablePath()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(exe))
	resolved, err := filepath.EvalSymlinks(exe)
	require.NoError(t, err)
	assert.Equal(t, resolved, exe)
}
