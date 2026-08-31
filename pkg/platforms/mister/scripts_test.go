//go:build linux

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func restoreScriptTestHooks(t *testing.T) {
	t.Helper()

	oldGetConsoleManager := getScriptConsoleManager
	oldRunChvt := runScriptChvt
	oldWriteLauncher := writeScriptLauncher
	oldStartCommand := startScriptCommand
	oldRunHiddenCommand := runHiddenScriptCommand
	oldKillHiddenProcessGroup := killHiddenScriptProcessGroup
	t.Cleanup(func() {
		getScriptConsoleManager = oldGetConsoleManager
		runScriptChvt = oldRunChvt
		writeScriptLauncher = oldWriteLauncher
		startScriptCommand = oldStartCommand
		runHiddenScriptCommand = oldRunHiddenCommand
		killHiddenScriptProcessGroup = oldKillHiddenProcessGroup
	})
}

func newTestScript(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // Test executable.
	return path
}

func newTestScriptPlatform() *Platform {
	return &Platform{activeMedia: func() *models.ActiveMedia { return nil }}
}

func TestRunScriptContext_CancelsHiddenScriptWithExecutionContext(t *testing.T) {
	restoreScriptTestHooks(t)

	started := make(chan struct{})
	killed := make(chan int, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runHiddenScriptCommand = func(cmd *exec.Cmd) error {
		cmd.Process = &os.Process{Pid: 1364}
		close(started)
		<-ctx.Done()
		if err := cmd.Cancel(); err != nil {
			return fmt.Errorf("cancel hidden command: %w", err)
		}
		return ctx.Err()
	}
	killHiddenScriptProcessGroup = func(pid int) error {
		killed <- pid
		return nil
	}
	script := newTestScript(t, "slow.sh")
	result := make(chan error, 1)
	go func() {
		result <- runScriptContext(ctx, nil, script, "", true)
	}()

	select {
	case <-started:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("hidden command runner did not start")
	}
	err := <-result

	require.ErrorIs(t, err, context.Canceled)
	select {
	case pid := <-killed:
		assert.Equal(t, 1364, pid)
	case <-time.After(time.Second):
		t.Fatal("hidden script process group was not canceled")
	}
}

func TestRunScript_WidgetUsesFrontendTTYAndCleansUpSetupFailure(t *testing.T) {
	restoreScriptTestHooks(t)

	cm := &testConsoleManager{}
	getScriptConsoleManager = func(*Platform) platforms.ConsoleManager { return cm }
	runScriptChvt = func(context.Context, string) error { return assert.AnError }

	err := runScript(newTestScriptPlatform(), newTestScript(t, "zaparoo.sh"), "'-show-picker' 'args.json'", false)
	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, frontendConsoleVT, cm.openVT)
	assert.True(t, cm.closeCalled)
}

func TestRunScript_CleansUpLauncherWriteFailure(t *testing.T) {
	restoreScriptTestHooks(t)

	cm := &testConsoleManager{}
	getScriptConsoleManager = func(*Platform) platforms.ConsoleManager { return cm }
	runScriptChvt = func(context.Context, string) error { return nil }
	writeScriptLauncher = func(string, []byte, os.FileMode) error { return assert.AnError }

	err := runScript(newTestScriptPlatform(), newTestScript(t, "test.sh"), "", false)
	require.ErrorIs(t, err, assert.AnError)
	assert.True(t, cm.closeCalled)
}

func TestRunScript_CleansUpCommandStartFailure(t *testing.T) {
	restoreScriptTestHooks(t)

	cm := &testConsoleManager{}
	getScriptConsoleManager = func(*Platform) platforms.ConsoleManager { return cm }
	runScriptChvt = func(context.Context, string) error { return nil }
	writeScriptLauncher = func(string, []byte, os.FileMode) error { return nil }
	startScriptCommand = func(*exec.Cmd) error { return assert.AnError }

	err := runScript(newTestScriptPlatform(), newTestScript(t, "test.sh"), "", false)
	require.ErrorIs(t, err, assert.AnError)
	assert.True(t, cm.closeCalled)
}

func TestRunScript_DoesNotCloseWhenOpenFails(t *testing.T) {
	restoreScriptTestHooks(t)

	cm := &testConsoleManager{openErr: errors.New("open failed")}
	getScriptConsoleManager = func(*Platform) platforms.ConsoleManager { return cm }

	err := runScript(newTestScriptPlatform(), newTestScript(t, "test.sh"), "", false)
	require.Error(t, err)
	assert.False(t, cm.closeCalled)
}
