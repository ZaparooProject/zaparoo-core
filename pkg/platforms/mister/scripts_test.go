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

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
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
			err := runScriptContext(t.Context(), nil, newTestScript(t, "busy.sh"), "", hidden, "")
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
		result <- runScriptContext(ctx, nil, script, "", true, "")
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

func captureScriptLauncher(t *testing.T, bin, args, launchOrigin string) string {
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

	err := runScriptContext(t.Context(), newTestScriptPlatform(), bin, args, false, launchOrigin)
	require.ErrorIs(t, err, assert.AnError)
	require.NotEmpty(t, launcher)
	return launcher
}

func captureHiddenScriptEnv(t *testing.T, launchOrigin string) []string {
	t.Helper()
	restoreScriptTestHooks(t)

	var env []string
	checkScriptActive = func(context.Context) bool { return false }
	runHiddenScriptCommand = func(cmd *exec.Cmd) error {
		env = cmd.Env
		return nil
	}

	require.NoError(t, runScriptContext(
		t.Context(), nil, newTestScript(t, "update_all.sh"), "", true, launchOrigin))
	return env
}

func launchOriginValues(env []string) []string {
	var values []string
	for _, kv := range env {
		if value, ok := strings.CutPrefix(kv, launchOriginEnv+"="); ok {
			values = append(values, value)
		}
	}
	return values
}

func TestRunScript_VisibleExportsLaunchOrigin(t *testing.T) {
	launcher := captureScriptLauncher(t, newTestScript(t, "update_all.sh"), "", "zaparoo_frontend")
	assert.Contains(t, launcher, "\nexport LAUNCH_ORIGIN_ID='zaparoo_frontend'\n")
}

func TestRunScript_VisibleOmitsUnsetLaunchOrigin(t *testing.T) {
	launcher := captureScriptLauncher(t, newTestScript(t, "update_all.sh"), "", "")
	assert.NotContains(t, launcher, launchOriginEnv)
}

func TestRunScript_VisibleLaunchOriginIsLiteral(t *testing.T) {
	origin := `x'; touch pwned; '$(touch pwned2)` + "\n`touch pwned3`"
	launcher := captureScriptLauncher(t, newTestScript(t, "update_all.sh"), "", origin)

	// Everything from the export up to the cd line is the launch origin.
	start := strings.Index(launcher, "export "+launchOriginEnv+"=")
	end := strings.Index(launcher, "\ncd $(dirname")
	require.NotEqual(t, -1, start)
	require.Greater(t, end, start)
	export := launcher[start:end]
	require.NotEmpty(t, export)

	dir := t.TempDir()
	script := export + "\nprintf '%s' \"$" + launchOriginEnv + "\""
	cmd := exec.CommandContext(t.Context(), "bash", "-c", script) //nolint:gosec // runs the launcher line under test
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, origin, string(out))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "launch origin must not execute")
}

func TestRunScript_HiddenLaunchOriginOverridesInherited(t *testing.T) {
	t.Setenv(launchOriginEnv, "degauss")

	values := launchOriginValues(captureHiddenScriptEnv(t, "zaparoo_frontend"))

	// exec keeps the last value of a duplicated key.
	require.NotEmpty(t, values)
	assert.Equal(t, "zaparoo_frontend", values[len(values)-1])
}

func TestRunScript_HiddenOmitsUnsetLaunchOrigin(t *testing.T) {
	t.Setenv(launchOriginEnv, "degauss")

	values := launchOriginValues(captureHiddenScriptEnv(t, ""))

	assert.Equal(t, []string{"degauss"}, values)
}

func TestCmdMisterScript_AcceptsLaunchOriginAdvarg(t *testing.T) {
	t.Parallel()

	run := func(advArgs map[string]string) error {
		env := &platforms.CmdEnv{
			Cmd: gozapscript.Command{
				Name:    gozapscript.ZapScriptCmdMisterScript,
				Args:    []string{"zaparoo-core-missing-test-script.sh"},
				AdvArgs: gozapscript.NewAdvArgs(advArgs),
			},
			Cfg: &config.Instance{},
		}
		pl := mocks.NewMockPlatform()
		pl.SetupBasicMock()
		_, err := cmdMisterScript(nil)(pl, env)
		return err
	}

	// Parsing succeeds, so the command gets as far as looking for the script.
	err := run(map[string]string{"launch_origin_id": "zaparoo_frontend"})
	require.ErrorIs(t, err, zapscript.ErrFileNotFound)

	err = run(map[string]string{"launch_origin": "zaparoo_frontend"})
	require.ErrorContains(t, err, "invalid advanced arguments")
}
