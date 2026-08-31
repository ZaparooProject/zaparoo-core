//go:build linux

package mister

import (
	"context"
	"errors"
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
	t.Cleanup(func() {
		getScriptConsoleManager = oldGetConsoleManager
		runScriptChvt = oldRunChvt
		writeScriptLauncher = oldWriteLauncher
		startScriptCommand = oldStartCommand
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

func TestRunScriptContext_CancelsHiddenScriptAtDeadline(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "release")
	marker := filepath.Join(dir, "marker")
	script := filepath.Join(dir, "slow.sh")
	contents := "#!/bin/sh\n(while [ ! -e '" + release + "' ]; do :; done; " +
		"printf done > '" + marker + "') &\nwait\n"
	require.NoError(t, os.WriteFile(script, []byte(contents), 0o700)) //nolint:gosec // Test executable.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := runScriptContext(ctx, nil, script, "", true)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, os.WriteFile(release, nil, 0o600))
	assert.NoFileExists(t, marker)
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
