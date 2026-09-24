//go:build linux

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scriptStatFailureFS struct {
	afero.Fs
	err error
}

func (fs scriptStatFailureFS) Stat(name string) (os.FileInfo, error) {
	return nil, &os.PathError{Op: "stat", Path: name, Err: fs.err}
}

func TestCheckScriptFile(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	path := filepath.Join("scripts", "test.sh")
	err := checkScriptFile(fs, path)
	require.ErrorIs(t, err, zapscript.ErrFileNotFound)
	require.ErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, afero.WriteFile(fs, path, nil, 0o600))
	require.NoError(t, checkScriptFile(fs, path))

	for _, cause := range []error{os.ErrPermission, syscall.EIO} {
		err = checkScriptFile(scriptStatFailureFS{Fs: fs, err: cause}, path)
		require.ErrorIs(t, err, cause)
		require.NotErrorIs(t, err, zapscript.ErrFileNotFound)
	}
}

func restoreScriptTestHooks(t *testing.T) {
	t.Helper()

	oldCheckScriptActive := checkScriptActive
	oldGetConsoleManager := getScriptConsoleManager
	oldRunChvt := runScriptChvt
	oldWriteLauncher := writeScriptLauncher
	oldStartCommand := startScriptCommand
	oldRunHiddenCommand := runHiddenScriptCommand
	oldKillHiddenProcessGroup := killHiddenScriptProcessGroup
	t.Cleanup(func() {
		checkScriptActive = oldCheckScriptActive
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

func TestRunScriptContext_RejectsBusyRunnerBeforeSideEffects(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(fmt.Sprintf("hidden=%t", hidden), func(t *testing.T) {
			restoreScriptTestHooks(t)
			checkScriptActive = func(context.Context) bool { return true }
			getScriptConsoleManager = func(*Platform) platforms.ConsoleManager {
				t.Fatal("busy refusal must not open a console")
				return nil
			}
			startScriptCommand = func(*exec.Cmd) error {
				t.Fatal("busy refusal must not start a visible script")
				return nil
			}
			runHiddenScriptCommand = func(*exec.Cmd) error {
				t.Fatal("busy refusal must not start a hidden script")
				return nil
			}
			err := runScriptContext(t.Context(), nil, newTestScript(t, "busy.sh"), "", hidden)
			require.ErrorIs(t, err, platforms.ErrScriptAlreadyRunning)
			require.EqualError(t, err, "a script is already running")
		})
	}
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

func captureScriptLauncher(t *testing.T, bin, args string) string {
	t.Helper()
	restoreScriptTestHooks(t)

	var launcher string
	checkScriptActive = func(context.Context) bool { return false }
	getScriptConsoleManager = func(*Platform) platforms.ConsoleManager { return &testConsoleManager{} }
	runScriptChvt = func(context.Context, string) error { return nil }
	writeScriptLauncher = func(_ string, data []byte, _ os.FileMode) error {
		launcher = string(data)
		return nil
	}
	startScriptCommand = func(*exec.Cmd) error { return assert.AnError }

	err := runScript(newTestScriptPlatform(), bin, args, false)
	require.ErrorIs(t, err, assert.AnError)
	return launcher
}

func TestRunScript_VisibleUserScriptSetsLaunchOrigin(t *testing.T) {
	launcher := captureScriptLauncher(t, newTestScript(t, "update_all.sh"), "")
	assert.Contains(t, launcher, "\nexport LAUNCH_ORIGIN_ID=zaparoo\n")
}

func TestRunScript_WidgetDoesNotSetLaunchOrigin(t *testing.T) {
	launcher := captureScriptLauncher(t, newTestScript(t, "zaparoo.sh"), "'-show-picker' 'args.json'")
	require.NotEmpty(t, launcher)
	assert.NotContains(t, launcher, "LAUNCH_ORIGIN_ID")
}

func TestRunScript_HiddenLaunchOriginOverridesInherited(t *testing.T) {
	restoreScriptTestHooks(t)
	t.Setenv("LAUNCH_ORIGIN_ID", "degauss")

	var env []string
	checkScriptActive = func(context.Context) bool { return false }
	runHiddenScriptCommand = func(cmd *exec.Cmd) error {
		env = cmd.Env
		return nil
	}

	require.NoError(t, runScript(nil, newTestScript(t, "update_all.sh"), "", true))

	// exec keeps the last value of a duplicated key.
	var origin string
	for _, kv := range env {
		if value, ok := strings.CutPrefix(kv, "LAUNCH_ORIGIN_ID="); ok {
			origin = value
		}
	}
	assert.Equal(t, launchOriginID, origin)
}
