//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArmadaDefaults(t *testing.T) {
	t.Parallel()

	defaults := armadaDefaults()
	require.NotNil(t, defaults.Service.Encryption)
	assert.True(t, *defaults.Service.Encryption)
	assert.Nil(t, config.BaseDefaults.Service.Encryption, "must not mutate shared platform defaults")
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

type runEnv struct {
	installErr error
	startErr   error
	appErr     error
	cfg        *config.Instance
	writers    []io.Writer
	calls      []string
	euid       int
	daemon     bool
	setup      bool
}

// fakeRun records what run() does instead of installing, starting or running
// anything. Not parallel: it swaps package variables.
func fakeRun(t *testing.T) *runEnv {
	t.Helper()
	cfg, err := config.NewConfig(filepath.Join(t.TempDir(), "config"), config.BaseDefaults)
	require.NoError(t, err)
	env := &runEnv{cfg: cfg, euid: 1000}

	oldInstall, oldUninstall, oldEuid := handleInstall, handleUninstall, geteuid
	oldSetup, oldStart, oldRun := setupCLI, startAndOpen, runApp
	t.Cleanup(func() {
		handleInstall, handleUninstall, geteuid = oldInstall, oldUninstall, oldEuid
		setupCLI, startAndOpen, runApp = oldSetup, oldStart, oldRun
	})

	handleInstall = func(c string) error {
		env.calls = append(env.calls, "install "+c)
		return env.installErr
	}
	handleUninstall = func(c string) error {
		env.calls = append(env.calls, "uninstall "+c)
		return env.installErr
	}
	geteuid = func() int { return env.euid }
	setupCLI = func(_ platforms.Platform, defaults config.Values, writers []io.Writer) *config.Instance {
		env.setup = true
		env.writers = writers
		assert.Equal(t, armadaDefaults(), defaults)
		return env.cfg
	}
	startAndOpen = func(c *config.Instance) error {
		assert.Same(t, env.cfg, c)
		env.calls = append(env.calls, "start")
		return env.startErr
	}
	runApp = func(_ platforms.Platform, c *config.Instance, daemon bool) error {
		assert.Same(t, env.cfg, c)
		env.calls = append(env.calls, "app")
		env.daemon = daemon
		return env.appErr
	}
	return env
}

func TestRun_InstallAndUninstall(t *testing.T) {
	for _, op := range []string{"install", "uninstall"} {
		t.Run(op, func(t *testing.T) {
			env := fakeRun(t)
			env.euid = 0 // installing is allowed as root
			withArgs(t, "-"+op, "service")
			require.NoError(t, run())
			assert.Equal(t, []string{op + " service"}, env.calls)
			assert.False(t, env.setup, "nothing else is set up")

			env.calls = nil
			env.installErr = errors.New("denied")
			withArgs(t, "-"+op, "desktop")
			err := run()
			require.ErrorIs(t, err, env.installErr)
			require.ErrorContains(t, err, op+" failed")
		})
	}
}

func TestRun_RefusesRoot(t *testing.T) {
	env := fakeRun(t)
	env.euid = 0
	withArgs(t)
	require.ErrorContains(t, run(), "cannot be run as root")
	assert.False(t, env.setup)
	assert.Empty(t, env.calls)
}

func TestRun_App(t *testing.T) {
	env := fakeRun(t)
	withArgs(t)
	require.NoError(t, run())
	assert.Equal(t, []string{"app"}, env.calls)
	assert.False(t, env.daemon)
	assert.Empty(t, env.writers, "interactive mode keeps logs off the terminal")

	env.appErr = errors.New("crashed")
	withArgs(t)
	err := run()
	require.ErrorIs(t, err, env.appErr)
	require.ErrorContains(t, err, "run failed")
}

func TestRun_Daemon(t *testing.T) {
	env := fakeRun(t)
	withArgs(t, "-daemon")
	require.NoError(t, run())
	assert.True(t, env.daemon)
	assert.Equal(t, []io.Writer{os.Stderr}, env.writers, "daemon logs go to stderr")
}

func TestRun_Start(t *testing.T) {
	env := fakeRun(t)
	withArgs(t, "-start")
	require.NoError(t, run())
	assert.Equal(t, []string{"start"}, env.calls, "the app is not run as well")

	env.startErr = errors.New("no browser")
	withArgs(t, "-start")
	err := run()
	require.ErrorIs(t, err, env.startErr)
	require.ErrorContains(t, err, "start failed")
}
