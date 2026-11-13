//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/lakka"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/rivo/tview"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingExec records every command and fails the one named in failOn.
type recordingExec struct {
	failOn string
	calls  []string
}

func (e *recordingExec) Run(_ context.Context, name string, args ...string) error {
	call := strings.Join(append([]string{name}, args...), " ")
	e.calls = append(e.calls, call)
	if e.failOn != "" && call == e.failOn {
		return errors.New("command failed")
	}
	return nil
}

func (*recordingExec) Output(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("unexpected Output")
}

func (*recordingExec) Start(context.Context, string, ...string) error {
	return errors.New("unexpected Start")
}

func (*recordingExec) StartWithOptions(context.Context, command.StartOptions, string, ...string) error {
	return errors.New("unexpected StartWithOptions")
}

type installEnv struct {
	fs   afero.Fs
	exec *recordingExec
	exe  string
}

// fakeInstall points the installer at an in-memory filesystem and a
// recording executor. Not parallel: it swaps package variables.
func fakeInstall(t *testing.T) *installEnv {
	t.Helper()
	dir := t.TempDir()
	env := &installEnv{
		fs:   afero.NewMemMapFs(),
		exec: &recordingExec{},
		exe:  filepath.Join(dir, "zaparoo"),
	}
	require.NoError(t, os.WriteFile(env.exe, []byte("binary"), 0o600))
	// Resolve the temp dir itself, which may sit behind a symlink.
	resolved, err := filepath.EvalSymlinks(env.exe)
	require.NoError(t, err)
	env.exe = resolved

	oldExe, oldFS, oldExec := executable, installFS, installExec
	t.Cleanup(func() { executable, installFS, installExec = oldExe, oldFS, oldExec })
	executable = func() (string, error) { return env.exe, nil }
	installFS = env.fs
	installExec = env.exec
	return env
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

func TestServiceFileHasPlaceholders(t *testing.T) {
	t.Parallel()
	assert.Contains(t, serviceFile, "ExecStart=@ZAPAROO_BIN@ -daemon")
	assert.Contains(t, serviceFile, "WorkingDirectory=@ZAPAROO_DIR@")
	assert.Contains(t, serviceFile, "RestartPreventExitStatus=78")
	assert.Contains(t, serviceFile, "\n@ZAPAROO_PORT_LOCK@\n")
}

func TestRun_InstallWritesUnitAndEnablesNetworkCommands(t *testing.T) {
	env := fakeInstall(t)
	require.NoError(t, afero.WriteFile(env.fs, lakka.RetroArchConfigPath,
		[]byte("video_fullscreen = \"true\"\nnetwork_cmd_enable = \"false\"\n"), 0o644))
	withArgs(t, "-install")

	require.NoError(t, run())

	unit, err := afero.ReadFile(env.fs, lakka.UnitPath)
	require.NoError(t, err)
	assert.Contains(t, string(unit), "ExecStart="+env.exe+" -daemon")
	assert.Contains(t, string(unit), "WorkingDirectory="+filepath.Dir(env.exe))
	assert.NotContains(t, string(unit), "@ZAPAROO_")

	cfg, err := afero.ReadFile(env.fs, lakka.RetroArchConfigPath)
	require.NoError(t, err)
	assert.True(t, lakka.NetworkCommandsEnabled(string(cfg)))
	assert.Contains(t, string(cfg), "video_fullscreen = \"true\"", "other settings kept")

	assert.Contains(t, string(unit), "ExecStartPre=-/bin/sh -c 'iptables -w -C INPUT -p udp --dport 55355 ! -i lo")

	assert.Equal(t, []string{
		"iptables -w -C INPUT -p udp --dport 55355 ! -i lo -j DROP",
		"systemctl daemon-reload",
		"systemctl enable zaparoo.service",
		"systemctl stop retroarch.service",
		"systemctl start retroarch.service",
		"systemctl restart zaparoo.service",
	}, env.exec.calls)
}

func TestInstallService_Failures(t *testing.T) {
	t.Run("executable unknown", func(t *testing.T) {
		fakeInstall(t)
		executable = func() (string, error) { return "", errors.New("no exe") }
		require.ErrorContains(t, installService(), "find zaparoo executable")
	})
	t.Run("executable missing", func(t *testing.T) {
		env := fakeInstall(t)
		require.NoError(t, os.Remove(env.exe))
		require.ErrorContains(t, installService(), "resolve zaparoo executable")
	})
	t.Run("systemctl fails", func(t *testing.T) {
		env := fakeInstall(t)
		require.NoError(t, afero.WriteFile(env.fs, lakka.RetroArchConfigPath,
			[]byte("network_cmd_enable = \"true\"\n"), 0o644))
		env.exec.failOn = "systemctl daemon-reload"
		err := installService()
		require.ErrorContains(t, err, "error installing service")
		require.ErrorContains(t, err, "command failed")
		assert.Equal(t, []string{"systemctl daemon-reload"}, env.exec.calls)
	})
}

func TestRun_UninstallRemovesUnit(t *testing.T) {
	env := fakeInstall(t)
	require.NoError(t, afero.WriteFile(env.fs, lakka.UnitPath, []byte("unit"), 0o644))
	withArgs(t, "-uninstall")

	require.NoError(t, run())

	exists, err := afero.Exists(env.fs, lakka.UnitPath)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, []string{
		"systemctl stop zaparoo.service",
		"systemctl disable zaparoo.service",
		"systemctl daemon-reload",
	}, env.exec.calls)
}

func TestUninstallService_Failures(t *testing.T) {
	t.Run("executable unknown", func(t *testing.T) {
		fakeInstall(t)
		executable = func() (string, error) { return "", errors.New("no exe") }
		require.ErrorContains(t, uninstallService(), "find zaparoo executable")
	})
	t.Run("reload fails", func(t *testing.T) {
		env := fakeInstall(t)
		env.exec.failOn = "systemctl daemon-reload"
		require.ErrorContains(t, uninstallService(), "error uninstalling service")
	})
}

type runEnv struct {
	stopErr     error
	setupDaemon *bool
	signal      os.Signal
	done        chan struct{}
	events      []string
	stops       int
}

// fakeRun replaces config setup, the service and the TUI so run() can take
// its normal paths without touching /storage or the terminal. Not parallel:
// it swaps package variables.
func fakeRun(t *testing.T) *runEnv {
	t.Helper()
	env := &runEnv{done: make(chan struct{})}
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)

	oldSetup, oldNotify, oldStart := setupCLI, notifySignals, startService
	oldSpawn, oldShow := spawnDaemon, showTUI
	oldAfter, oldIfAsked := reexecAfter, reexecIfAsked
	t.Cleanup(func() {
		setupCLI, notifySignals, startService = oldSetup, oldNotify, oldStart
		spawnDaemon, showTUI = oldSpawn, oldShow
		reexecAfter, reexecIfAsked = oldAfter, oldIfAsked
	})
	reexecAfter = func(cause error) error {
		env.events = append(env.events, "reexec after rollback")
		return cause
	}
	reexecIfAsked = func(requested func() bool) error {
		if requested != nil && requested() {
			env.events = append(env.events, "reexec")
			return errors.New("exec failed")
		}
		return nil
	}

	setupCLI = func(_ platforms.Platform, defaults config.Values, writers []io.Writer) *config.Instance {
		require.Len(t, writers, 1)
		_, plain := writers[0].(*os.File)
		env.setupDaemon = &plain
		assert.Equal(t, config.BaseDefaults, defaults)
		return cfg
	}
	notifySignals = func(c chan<- os.Signal, sigs ...os.Signal) {
		assert.ElementsMatch(t, []os.Signal{syscall.SIGINT, syscall.SIGTERM}, sigs)
		if env.signal != nil {
			c <- env.signal
		}
	}
	startService = func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
		env.events = append(env.events, "start")
		return &service.StartResult{
			Done: env.done,
			Stop: func() error {
				env.stops++
				return env.stopErr
			},
		}, nil
	}
	spawnDaemon = func(*config.Instance) (func(), error) {
		env.events = append(env.events, "spawn")
		return func() { env.events = append(env.events, "stop daemon") }, nil
	}
	showTUI = func(*config.Instance, platforms.Platform, func() (*tview.Application, error)) error {
		env.events = append(env.events, "tui")
		return nil
	}
	return env
}

func TestRun_DaemonStopsOnSignal(t *testing.T) {
	env := fakeRun(t)
	env.signal = syscall.SIGTERM
	withArgs(t, "-daemon")

	require.NoError(t, run())

	require.NotNil(t, env.setupDaemon)
	assert.True(t, *env.setupDaemon, "daemon logs go to plain stderr")
	assert.Equal(t, []string{"start"}, env.events)
	assert.Equal(t, 1, env.stops)
}

func TestRunDaemon_StopFails(t *testing.T) {
	env := fakeRun(t)
	env.signal = syscall.SIGINT
	env.stopErr = errors.New("stuck")
	require.ErrorContains(t, runDaemon(lakka.NewPlatform(), nil), "error stopping service: stuck")
}

func TestRunDaemon_ServiceEndsItself(t *testing.T) {
	env := fakeRun(t)
	close(env.done)
	require.NoError(t, runDaemon(lakka.NewPlatform(), nil))
	assert.Zero(t, env.stops, "an internal shutdown is not stopped again")
}

func TestRunDaemon_RestartRequested(t *testing.T) {
	env := fakeRun(t)
	close(env.done)
	startService = func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
		return &service.StartResult{Done: env.done, RestartRequested: func() bool { return true }}, nil
	}
	require.ErrorContains(t, runDaemon(lakka.NewPlatform(), nil), "failed to re-exec for restart: exec failed")
	assert.Equal(t, []string{"reexec"}, env.events)
}

func TestRunDaemon_RolledBackRestarts(t *testing.T) {
	env := fakeRun(t)
	startService = func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
		return nil, fmt.Errorf("watchdog: %w", updater.ErrRolledBack)
	}
	err := runDaemon(lakka.NewPlatform(), nil)
	require.ErrorIs(t, err, updater.ErrRolledBack)
	require.ErrorContains(t, err, "restarting after update rollback")
	assert.Equal(t, []string{"reexec after rollback"}, env.events)
}

func TestRunDaemon_StartFails(t *testing.T) {
	for _, cause := range []error{errors.New("boom"), database.ErrSchemaAhead} {
		t.Run(cause.Error(), func(t *testing.T) {
			fakeRun(t)
			startService = func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
				return nil, cause
			}
			err := runDaemon(lakka.NewPlatform(), nil)
			require.ErrorIs(t, err, cause)
			require.ErrorContains(t, err, "error starting service")
		})
	}
}

func TestRun_TUISpawnsDaemonAndStopsIt(t *testing.T) {
	env := fakeRun(t)
	withArgs(t)

	require.NoError(t, run())

	require.NotNil(t, env.setupDaemon)
	assert.False(t, *env.setupDaemon, "interactive logs use the console writer")
	assert.Equal(t, []string{"spawn", "tui", "stop daemon"}, env.events)
}

func TestRunTUI_Failures(t *testing.T) {
	t.Run("spawn fails", func(t *testing.T) {
		env := fakeRun(t)
		spawnDaemon = func(*config.Instance) (func(), error) { return nil, errors.New("no daemon") }
		require.ErrorContains(t, runTUI(lakka.NewPlatform(), nil), "error spawning daemon: no daemon")
		assert.Empty(t, env.events)
	})
	t.Run("tui fails", func(t *testing.T) {
		env := fakeRun(t)
		showTUI = func(*config.Instance, platforms.Platform, func() (*tview.Application, error)) error {
			return errors.New("no tty")
		}
		require.ErrorContains(t, runTUI(lakka.NewPlatform(), nil), "error displaying TUI: no tty")
		assert.Equal(t, []string{"spawn", "stop daemon"}, env.events, "daemon still stopped")
	})
}
