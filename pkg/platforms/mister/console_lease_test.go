//go:build linux

package mister

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConsoleLeaseController(t *testing.T) (*mainConsoleLeaseController, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	statePath := filepath.Join(string(filepath.Separator), "tmp", "console-state")
	commandPath := filepath.Join(string(filepath.Separator), "dev", "MiSTer_cmd")
	require.NoError(t, fs.MkdirAll(filepath.Dir(statePath), 0o755))
	require.NoError(t, fs.MkdirAll(filepath.Dir(commandPath), 0o755))
	require.NoError(t, fs.MkdirAll(
		filepath.Join(string(filepath.Separator), "proc", strconv.Itoa(os.Getpid())), 0o755,
	))
	require.NoError(t, afero.WriteFile(fs, commandPath, nil, 0o600))

	return &mainConsoleLeaseController{
		fs:             fs,
		statePath:      statePath,
		commandPath:    commandPath,
		pollInterval:   time.Millisecond,
		cleanupTimeout: 20 * time.Millisecond,
	}, fs
}

func writeConsoleLeaseState(fs afero.Fs, path, state, nonce string) error {
	if err := afero.WriteFile(
		fs, path, []byte(fmt.Sprintf("1 %d %s %s\n", os.Getpid(), state, nonce)), 0o600,
	); err != nil {
		return fmt.Errorf("write console lease state: %w", err)
	}
	return nil
}

func waitForLeaseCommand(fs afero.Fs, path, action string) []string {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		contents, err := afero.ReadFile(fs, path)
		if err == nil {
			fields := strings.Fields(string(contents))
			if len(fields) >= 3 && fields[0] == "zaparoo_console" && fields[1] == action {
				return fields
			}
		}
		time.Sleep(time.Millisecond)
	}
	return nil
}

func TestMainConsoleLeaseController_Available(t *testing.T) {
	t.Parallel()

	controller, fs := newTestConsoleLeaseController(t)
	require.NoError(t, writeConsoleLeaseState(fs, controller.statePath, "ready", "-"))
	assert.True(t, controller.Available())
}

func TestMainConsoleLeaseController_AvailableRejectsStalePID(t *testing.T) {
	t.Parallel()

	controller, fs := newTestConsoleLeaseController(t)
	require.NoError(t, afero.WriteFile(
		fs, controller.statePath, []byte("1 2147483647 ready -\n"), 0o600,
	))
	assert.False(t, controller.Available())
}

func TestMainConsoleLeaseController_Acquire(t *testing.T) {
	t.Parallel()

	controller, fs := newTestConsoleLeaseController(t)
	go func() {
		fields := waitForLeaseCommand(fs, controller.commandPath, "acquire")
		if len(fields) >= 3 {
			_ = writeConsoleLeaseState(fs, controller.statePath, "acquired", fields[2])
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	nonce, err := controller.Acquire(ctx, "3")
	require.NoError(t, err)
	assert.NotEmpty(t, nonce)
}

func TestMainConsoleLeaseController_AcquireCleansDelayedLeaseAfterContextFailure(t *testing.T) {
	t.Parallel()

	controller, fs := newTestConsoleLeaseController(t)
	controller.cleanupTimeout = time.Second
	go func() {
		fields := waitForLeaseCommand(fs, controller.commandPath, "acquire")
		if len(fields) < 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
		_ = writeConsoleLeaseState(fs, controller.statePath, "acquired", fields[2])
		if len(waitForLeaseCommand(fs, controller.commandPath, "release")) >= 3 {
			_ = writeConsoleLeaseState(fs, controller.statePath, "released", fields[2])
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := controller.Acquire(ctx, "3")
	require.ErrorIs(t, err, context.DeadlineExceeded)

	state, stateErr := controller.readState()
	require.NoError(t, stateErr)
	assert.Equal(t, "released", state.state)
}

func TestMainConsoleLeaseController_AcquireReportsCleanupFailure(t *testing.T) {
	t.Parallel()

	controller, _ := newTestConsoleLeaseController(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, err := controller.Acquire(ctx, "3")
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "clean up uncertain Main console lease")
}

func TestMainConsoleLeaseController_Release(t *testing.T) {
	t.Parallel()

	controller, fs := newTestConsoleLeaseController(t)
	go func() {
		fields := waitForLeaseCommand(fs, controller.commandPath, "release")
		if len(fields) >= 3 {
			_ = writeConsoleLeaseState(fs, controller.statePath, "released", fields[2])
		}
	}()

	require.NoError(t, controller.Release(context.Background(), "test-nonce"))
}

func TestMainConsoleLeaseController_AcquireCommandFailure(t *testing.T) {
	t.Parallel()

	controller, _ := newTestConsoleLeaseController(t)
	controller.commandPath = filepath.Join(string(filepath.Separator), "missing", "MiSTer_cmd")

	_, err := controller.Acquire(context.Background(), "3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open Main command interface")
}

func TestMainConsoleLeaseController_ReleaseCommandFailure(t *testing.T) {
	t.Parallel()

	controller, _ := newTestConsoleLeaseController(t)
	controller.commandPath = filepath.Join(string(filepath.Separator), "missing", "MiSTer_cmd")

	err := controller.Release(context.Background(), "test-nonce")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open Main command interface")
}

func TestMainConsoleLeaseController_WriteCommandFailure(t *testing.T) {
	t.Parallel()

	controller, _ := newTestConsoleLeaseController(t)
	controller.commandPath = filepath.Join(string(filepath.Separator), "missing", "MiSTer_cmd")

	err := controller.writeCommand("zaparoo_console release test-nonce\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open Main command interface")
}

func TestMainConsoleLeaseController_ReleaseStateFailure(t *testing.T) {
	t.Parallel()

	controller, fs := newTestConsoleLeaseController(t)
	go func() {
		fields := waitForLeaseCommand(fs, controller.commandPath, "release")
		if len(fields) >= 3 {
			_ = writeConsoleLeaseState(fs, controller.statePath, "failed", fields[2])
		}
	}()

	err := controller.Release(context.Background(), "test-nonce")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed")
}

func TestMainConsoleLeaseController_ReadStateValidation(t *testing.T) {
	t.Parallel()

	controller, fs := newTestConsoleLeaseController(t)
	require.NoError(t, afero.WriteFile(fs, controller.statePath, []byte("invalid\n"), 0o600))
	_, err := controller.readState()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Main console state")

	require.NoError(t, afero.WriteFile(fs, controller.statePath, []byte("1 bad ready -\n"), 0o600))
	_, err = controller.readState()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Main console PID")
}

func TestMainConsoleLeaseController_WaitForFailure(t *testing.T) {
	t.Parallel()

	controller, fs := newTestConsoleLeaseController(t)
	require.NoError(t, writeConsoleLeaseState(fs, controller.statePath, "busy", "test-nonce"))

	err := controller.waitForState(context.Background(), "acquired", "test-nonce")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "busy")
}
