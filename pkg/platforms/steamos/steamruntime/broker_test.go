//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeHelperProcess(_ *testing.T) {
	if os.Getenv("ZAPAROO_RUNTIME_HELPER") != "1" {
		return
	}
	if err := Run(context.Background()); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func testBroker(t *testing.T) *Broker {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "runtime"))
	fs := afero.NewOsFs()
	paths := &InstallPaths{
		FS:       fs,
		Binary:   filepath.Join(dir, "zaparoo"),
		Runtime:  filepath.Join(dir, runtimeExecutableName),
		Desktop:  filepath.Join(dir, runtimeExecutableName+".desktop"),
		SteamDir: filepath.Join(dir, "steam"),
	}
	require.NoError(t, afero.WriteFile(fs, paths.Binary, []byte("binary"), 0o700))
	require.NoError(t, symlinkFS(fs, paths.Binary, paths.Runtime))
	require.NoError(t, afero.WriteFile(fs, paths.Desktop, desktopEntry(paths.Runtime), 0o600))
	shortcutPath := filepath.Join(paths.SteamDir, "userdata", "123", "config", "shortcuts.vdf")
	require.NoError(t, fs.MkdirAll(filepath.Dir(shortcutPath), 0o750))
	require.NoError(t, afero.WriteFile(fs, shortcutPath, runtimeShortcutFixture(42, paths.Runtime), 0o600))

	return brokerWithLauncher(paths, func(ctx context.Context, _ string) error {
		//nolint:gosec // Current test binary.
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestRuntimeHelperProcess")
		cmd.Env = append(os.Environ(), "ZAPAROO_RUNTIME_HELPER=1")
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start runtime helper: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	})
}

func TestBrokerAvailabilityRequiresReadyIntegration(t *testing.T) {
	broker := testBroker(t)
	assert.True(t, broker.Available())

	require.NoError(t, broker.paths.fileSystem().Remove(broker.paths.Runtime))
	assert.False(t, broker.Available())
}

func TestBrokerStartAndWait(t *testing.T) {
	broker := testBroker(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	process, err := broker.Start(ctx, &Command{Executable: "true"})
	require.NoError(t, err)
	require.NoError(t, broker.Wait(ctx, process.Pid))
	broker.mu.Lock()
	assert.Zero(t, broker.lastRuntimePID)
	broker.mu.Unlock()
	assert.True(t, broker.Clear(process.Pid))
	assert.False(t, broker.HasActive())
}

func TestBrokerStartReportsSteamLaunchFailure(t *testing.T) {
	broker := testBroker(t)
	launchErr := errors.New("Steam unavailable")
	broker.launch = func(context.Context, string) error { return launchErr }

	_, err := broker.Start(t.Context(), &Command{Executable: "true"})

	require.ErrorIs(t, err, launchErr)
	assert.False(t, broker.HasActive())
}

func TestBrokerPreemptsActiveSession(t *testing.T) {
	broker := testBroker(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	first, err := broker.Start(ctx, &Command{Executable: "sleep", Args: []string{"60"}})
	require.NoError(t, err)
	assert.True(t, broker.HasActive())

	second, err := broker.Start(ctx, &Command{Executable: "true"})
	require.NoError(t, err)
	firstWaitErr := broker.Wait(ctx, first.Pid)
	require.NotErrorIs(t, firstWaitErr, context.DeadlineExceeded)
	require.NotErrorIs(t, firstWaitErr, context.Canceled)
	require.NoError(t, broker.Wait(ctx, second.Pid))
	assert.True(t, broker.Clear(second.Pid))
	assert.False(t, broker.HasActive())
}

func TestCompleteSessionPreservesReplacementRuntimePID(t *testing.T) {
	t.Parallel()

	session := &brokerSession{done: make(chan struct{}), runtimePID: 41}
	broker := &Broker{active: session, lastRuntimePID: 42}

	broker.completeSession(session, nil)

	assert.Nil(t, broker.active)
	assert.Equal(t, 42, broker.lastRuntimePID)
	select {
	case <-session.done:
	default:
		require.Fail(t, "session completion was not published")
	}
}

func TestWaitSessionHonorsCancellation(t *testing.T) {
	t.Parallel()

	session := &brokerSession{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, waitSession(ctx, session), context.Canceled)
	require.ErrorIs(t, waitSessionDone(ctx, session), context.Canceled)
}

func TestBrokerStopWithoutActiveSession(t *testing.T) {
	t.Parallel()

	broker := brokerWithLauncher(&InstallPaths{}, func(context.Context, string) error { return nil })
	require.NoError(t, broker.Stop(t.Context()))
}

func TestBrokerClearRemovesStaleSessions(t *testing.T) {
	t.Parallel()

	broker := &Broker{
		sessions: map[int]*brokerSession{
			41: {},
			42: {},
		},
		latestPID: 42,
	}

	assert.False(t, broker.Clear(41))
	assert.False(t, broker.Owns(41))
	assert.True(t, broker.Clear(42))
	assert.False(t, broker.Owns(42))
	assert.False(t, broker.Clear(42))
}
