//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type supervisorRun struct {
	dir     string
	pidFile string
	count   string
	script  string
}

// newSupervisorRun renders the supervisor around a fake Core that exits with
// the given statuses in turn.
func newSupervisorRun(t *testing.T, statuses ...int) *supervisorRun {
	t.Helper()
	dir := t.TempDir()
	r := &supervisorRun{
		dir:     dir,
		pidFile: filepath.Join(dir, "supervisor.pid"),
		count:   filepath.Join(dir, "count"),
		script:  filepath.Join(dir, "zaparoo(permanent).sh"),
	}
	bin := filepath.Join(dir, "zaparoo")
	var fake strings.Builder
	fake.WriteString("#!/bin/sh\n" +
		"[ \"$1\" = \"-daemon\" ] || exit 9\n" +
		"[ \"$ZAPAROO_APP\" = '" + bin + "' ] || exit 10\n" +
		"[ \"$0\" = '" + filepath.Join(dir, "run", "zaparoo-start") + "' ] || exit 11\n" +
		"n=$(cat '" + r.count + "' 2>/dev/null || echo 0)\n" +
		"n=$((n + 1))\n" +
		"echo \"$n\" > '" + r.count + "'\n" +
		"case $n in\n")
	for i, status := range statuses {
		fake.WriteString(strconv.Itoa(i+1) + ") exit " + strconv.Itoa(status) + " ;;\n")
	}
	fake.WriteString("esac\nexit 1\n")
	// Not executable, like the binary on Recalbox's FAT share.
	require.NoError(t, os.WriteFile(bin, []byte(fake.String()), 0o600))
	script := strings.ReplaceAll(supervisorScript, "@ZAPAROO_BIN@", bin)
	require.NoError(t, os.WriteFile(r.script, []byte(script), 0o600))
	return r
}

func (r *supervisorRun) run(t *testing.T) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", r.script) //nolint:gosec // script rendered by the test
	cmd.Env = append(os.Environ(), "ZAPAROO_SUPERVISOR_PIDFILE="+r.pidFile, "ZAPAROO_RETRY_DELAY=0",
		"ZAPAROO_RUN_DIR="+filepath.Join(r.dir, "run"))
	err := cmd.Run()
	require.NoError(t, ctx.Err(), "supervisor did not exit")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	require.NoError(t, err)
	return 0
}

func (r *supervisorRun) runs(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(r.count)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	require.NoError(t, err)
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	return n
}

func TestSupervisorDoesNotRestartOnUnrecoverableStatus(t *testing.T) {
	assert.Contains(t, supervisorScript, "EXIT_UNRECOVERABLE="+strconv.Itoa(cli.ExitUnrecoverable))

	r := newSupervisorRun(t, 1, cli.ExitUnrecoverable, 0)
	assert.Equal(t, cli.ExitUnrecoverable, r.run(t))
	assert.Equal(t, 2, r.runs(t), "restarted after a failure, then stopped")
	assert.NoFileExists(t, r.pidFile)
}

func TestSupervisorStopsOnCleanExit(t *testing.T) {
	r := newSupervisorRun(t, 0)
	assert.Equal(t, 0, r.run(t))
	assert.Equal(t, 1, r.runs(t))
}

func TestSupervisorRunsOnce(t *testing.T) {
	r := newSupervisorRun(t, 0)
	sleeper := filepath.Join(r.dir, "zaparoo-sleep")
	sleepBin, err := exec.LookPath("sleep")
	require.NoError(t, err)
	require.NoError(t, os.Symlink(sleepBin, sleeper))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running := exec.CommandContext(ctx, sleeper, "60") //nolint:gosec // test-created symlink to sleep
	require.NoError(t, running.Start())
	defer func() {
		cancel()
		_ = running.Wait()
	}()
	require.NoError(t, os.WriteFile(r.pidFile, []byte(strconv.Itoa(running.Process.Pid)), 0o600))

	assert.Equal(t, 0, r.run(t))
	assert.Equal(t, 0, r.runs(t), "a live supervisor already owns Core")
}

func TestSupervisorReplacesStalePIDFile(t *testing.T) {
	r := newSupervisorRun(t, 0)
	sleepBin, err := exec.LookPath("sleep")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	other := exec.CommandContext(ctx, sleepBin, "60") //nolint:gosec // sleep from PATH
	require.NoError(t, other.Start())
	defer func() {
		cancel()
		_ = other.Wait()
	}()
	require.NoError(t, os.WriteFile(r.pidFile, []byte(strconv.Itoa(other.Process.Pid)), 0o600))

	assert.Equal(t, 0, r.run(t), "a reused PID that is not the supervisor is ignored")
	assert.Equal(t, 1, r.runs(t))
}
