//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/recalbox"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type supervisorRun struct {
	dir     string
	pidFile string
	count   string
	script  string
	env     []string
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
	cmd.Env = append(cmd.Env, r.env...)
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

	// Stands in for a running supervisor: a shell with "zaparoo" on its
	// command line that stays alive, reading a pipe nothing writes to. It
	// is not a renamed copy of another program, because a multi-call
	// coreutils build refuses to run under a name it does not know.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	running := exec.CommandContext(ctx, "/bin/sh", "-c", "read -r _", "zaparoo-supervisor")
	stdin, err := running.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, running.Start())
	defer func() {
		cancel()
		_ = stdin.Close()
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

func TestSupervisorReplacesUntrustedRunDir(t *testing.T) {
	tests := []struct {
		setup func(t *testing.T, runDir string)
		name  string
	}{
		{
			name: "writable by others",
			setup: func(t *testing.T, runDir string) {
				t.Helper()
				require.NoError(t, os.Mkdir(runDir, 0o700))
				require.NoError(t, os.Chmod(runDir, 0o777)) //nolint:gosec // the untrusted state under test
			},
		},
		{
			name: "symlink to another directory",
			setup: func(t *testing.T, runDir string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(runDir), "elsewhere")
				require.NoError(t, os.Mkdir(target, 0o700))
				require.NoError(t, os.Symlink(target, runDir))
			},
		},
		{
			name: "regular file",
			setup: func(t *testing.T, runDir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(runDir, nil, 0o600))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSupervisorRun(t, 0)
			runDir := filepath.Join(r.dir, "run")
			tt.setup(t, runDir)

			assert.Equal(t, 0, r.run(t))
			assert.Equal(t, 1, r.runs(t))
			info, err := os.Lstat(runDir)
			require.NoError(t, err)
			assert.True(t, info.IsDir(), "run dir recreated as a real directory")
			assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
			assert.NoFileExists(t, filepath.Join(r.dir, "elsewhere", "zaparoo-start"))
		})
	}
}

func TestSupervisorKeepsPrivateRunDir(t *testing.T) {
	r := newSupervisorRun(t, 0)
	runDir := filepath.Join(r.dir, "run")
	require.NoError(t, os.Mkdir(runDir, 0o700))
	marker := filepath.Join(runDir, "marker")
	require.NoError(t, os.WriteFile(marker, nil, 0o600))

	assert.Equal(t, 0, r.run(t))
	assert.Equal(t, 1, r.runs(t))
	assert.FileExists(t, marker, "a trusted run dir is reused, not recreated")
}

func TestSupervisorReplacesRunDirOwnedByAnotherUser(t *testing.T) {
	r := newSupervisorRun(t, 0)
	runDir := filepath.Join(r.dir, "run")
	require.NoError(t, os.Mkdir(runDir, 0o700))
	marker := filepath.Join(runDir, "marker")
	require.NoError(t, os.WriteFile(marker, nil, 0o600))
	// Report a different uid so the existing directory looks foreign.
	binDir := filepath.Join(r.dir, "bin")
	require.NoError(t, os.Mkdir(binDir, 0o700))
	//nolint:gosec // test helper must be executable
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "id"), []byte("#!/bin/sh\necho 54321\n"), 0o700))
	r.env = []string{"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")}

	assert.Equal(t, 0, r.run(t))
	assert.Equal(t, 1, r.runs(t))
	assert.NoFileExists(t, marker, "a run dir owned by someone else is recreated")
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

type fakeShare struct {
	fs       afero.Fs
	startErr error
	exe      string
	started  []string
}

// useFakeShare swaps the installer onto an in-memory share. Not parallel: it
// replaces a package variable.
func useFakeShare(t *testing.T) *fakeShare {
	t.Helper()
	share := &fakeShare{fs: afero.NewMemMapFs()}
	old := newInstaller
	t.Cleanup(func() { newInstaller = old })
	newInstaller = func(script, exe string) *recalbox.Installer {
		share.exe = exe
		inst := recalbox.NewInstaller(script, exe)
		inst.FS = share.fs
		inst.StartScript = func(path string) error {
			share.started = append(share.started, path)
			return share.startErr
		}
		inst.Signal = func(pid int, _ syscall.Signal) error {
			t.Errorf("unexpected signal to %d", pid)
			return nil
		}
		inst.Sleep = func(time.Duration) {}
		return inst
	}
	return share
}

func TestRun_InstallWritesStartupScript(t *testing.T) {
	share := useFakeShare(t)
	t.Setenv(config.AppEnv, "")
	withArgs(t, "-install")

	require.NoError(t, run())

	exe, err := os.Executable()
	require.NoError(t, err)
	exe, err = filepath.EvalSymlinks(exe)
	require.NoError(t, err)
	assert.Equal(t, exe, share.exe)
	script, err := afero.ReadFile(share.fs, recalbox.ScriptPath)
	require.NoError(t, err)
	assert.Equal(t, strings.ReplaceAll(supervisorScript, "@ZAPAROO_BIN@", exe), string(script))
	assert.Equal(t, []string{recalbox.ScriptPath}, share.started)
}

func TestInstaller_UsesZaparooApp(t *testing.T) {
	share := useFakeShare(t)
	app := filepath.Join(t.TempDir(), "zaparoo")
	require.NoError(t, os.WriteFile(app, nil, 0o600))
	t.Setenv(config.AppEnv, app)

	require.NoError(t, installService())
	assert.Equal(t, app, share.exe, "the binary on the share, not the tmpfs copy running")
}

func TestInstallService_Failures(t *testing.T) {
	t.Run("start fails", func(t *testing.T) {
		share := useFakeShare(t)
		share.startErr = errors.New("no sh")
		require.ErrorContains(t, installService(), "error installing service")
	})
	t.Run("binary missing", func(t *testing.T) {
		share := useFakeShare(t)
		t.Setenv(config.AppEnv, filepath.Join(t.TempDir(), "missing"))
		require.ErrorContains(t, installService(), "resolve zaparoo executable")
		require.ErrorContains(t, uninstallService(), "resolve zaparoo executable")
		assert.Empty(t, share.started)
	})
}

func TestRun_UninstallRemovesStartupScript(t *testing.T) {
	share := useFakeShare(t)
	t.Setenv(config.AppEnv, "")
	require.NoError(t, afero.WriteFile(share.fs, recalbox.ScriptPath, []byte("#!/bin/sh\n"), 0o755))
	withArgs(t, "-uninstall")

	require.NoError(t, run())

	exists, err := afero.Exists(share.fs, recalbox.ScriptPath)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, share.started)
}

func TestUninstallService_ShareReadOnly(t *testing.T) {
	share := useFakeShare(t)
	t.Setenv(config.AppEnv, "")
	require.NoError(t, afero.WriteFile(share.fs, recalbox.ScriptPath, nil, 0o755))
	share.fs = afero.NewReadOnlyFs(share.fs)
	require.ErrorContains(t, uninstallService(), "error uninstalling service")
}
