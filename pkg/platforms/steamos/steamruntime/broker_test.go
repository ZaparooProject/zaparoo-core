//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package steamruntime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
	paths := InstallPaths{
		Binary:   filepath.Join(dir, "zaparoo"),
		Runtime:  filepath.Join(dir, runtimeExecutableName),
		Desktop:  filepath.Join(dir, runtimeExecutableName+".desktop"),
		SteamDir: filepath.Join(dir, "steam"),
	}
	require.NoError(t, os.WriteFile(paths.Binary, []byte("binary"), 0o700)) //nolint:gosec // Test-controlled path.
	require.NoError(t, os.Symlink(paths.Binary, paths.Runtime))
	require.NoError(t, os.WriteFile(paths.Desktop, desktopEntry(paths.Runtime), 0o600))
	shortcutPath := filepath.Join(paths.SteamDir, "userdata", "123", "config", "shortcuts.vdf")
	require.NoError(t, os.MkdirAll(filepath.Dir(shortcutPath), 0o750))
	require.NoError(t, os.WriteFile(shortcutPath, shortcutFixture(42, paths.Runtime), 0o600))

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

	require.NoError(t, os.Remove(broker.paths.Runtime))
	assert.False(t, broker.Available())
}

func TestBrokerStartAndWait(t *testing.T) {
	broker := testBroker(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	process, err := broker.Start(ctx, &Command{Executable: "/bin/true"})
	require.NoError(t, err)
	require.NoError(t, broker.Wait(ctx, process.Pid))
	assert.True(t, broker.Clear(process.Pid))
	assert.False(t, broker.HasActive())
}

func TestBrokerPreemptsActiveSession(t *testing.T) {
	broker := testBroker(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	first, err := broker.Start(ctx, &Command{Executable: "/bin/sleep", Args: []string{"60"}})
	require.NoError(t, err)
	assert.True(t, broker.HasActive())

	second, err := broker.Start(ctx, &Command{Executable: "/bin/true"})
	require.NoError(t, err)
	require.NoError(t, broker.Wait(ctx, first.Pid))
	require.NoError(t, broker.Wait(ctx, second.Pid))
	assert.True(t, broker.Clear(second.Pid))
	assert.False(t, broker.HasActive())
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
