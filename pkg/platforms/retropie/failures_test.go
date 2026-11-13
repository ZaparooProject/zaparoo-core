//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFault = errors.New("injected fault")

// faultFs fails the operations fail picks: "mkdir", "open", "remove",
// "rename", "chmod", and "write" or "close" on a file opened for writing.
type faultFs struct {
	afero.Fs
	fail func(op, name string) bool
}

func (f faultFs) check(op, name string) error {
	if f.fail(op, name) {
		return &os.PathError{Op: op, Path: name, Err: errFault}
	}
	return nil
}

func (f faultFs) MkdirAll(name string, mode os.FileMode) error {
	if err := f.check("mkdir", name); err != nil {
		return err
	}
	return f.Fs.MkdirAll(name, mode) //nolint:wrapcheck // passthrough
}

func (f faultFs) Open(name string) (afero.File, error) {
	if err := f.check("open", name); err != nil {
		return nil, err
	}
	return f.Fs.Open(name) //nolint:wrapcheck // passthrough
}

func (f faultFs) OpenFile(name string, flag int, mode os.FileMode) (afero.File, error) {
	if err := f.check("open", name); err != nil {
		return nil, err
	}
	file, err := f.Fs.OpenFile(name, flag, mode)
	if err != nil {
		return nil, err //nolint:wrapcheck // passthrough
	}
	return faultFile{File: file, fs: f}, nil
}

func (f faultFs) Remove(name string) error {
	if err := f.check("remove", name); err != nil {
		return err
	}
	return f.Fs.Remove(name) //nolint:wrapcheck // passthrough
}

func (f faultFs) Rename(oldName, newName string) error {
	if err := f.check("rename", newName); err != nil {
		return err
	}
	return f.Fs.Rename(oldName, newName) //nolint:wrapcheck // passthrough
}

func (f faultFs) Chmod(name string, mode os.FileMode) error {
	if err := f.check("chmod", name); err != nil {
		return err
	}
	return f.Fs.Chmod(name, mode) //nolint:wrapcheck // passthrough
}

type faultFile struct {
	afero.File
	fs faultFs
}

func (f faultFile) Write(p []byte) (int, error) {
	if err := f.fs.check("write", f.Name()); err != nil {
		return 0, err
	}
	return f.File.Write(p) //nolint:wrapcheck // passthrough
}

func (f faultFile) Close() error {
	err := f.File.Close()
	if checkErr := f.fs.check("close", f.Name()); checkErr != nil {
		return checkErr
	}
	return err //nolint:wrapcheck // passthrough
}

func failOn(op, path string) func(string, string) bool {
	return func(gotOp, name string) bool { return gotOp == op && filepath.Clean(name) == path }
}

// signalErrProcs records signals and reports every one as failed.
type signalErrProcs struct {
	*fakeProcs
}

func (s signalErrProcs) signal(pid int, sig syscall.Signal) error {
	_ = s.fakeProcs.signal(pid, sig)
	return errFault
}

func newTestConfig(t *testing.T) *config.Instance {
	t.Helper()
	cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	return cfg
}

func findLauncher(t *testing.T, launchers []platforms.Launcher, id string) platforms.Launcher {
	t.Helper()
	for i := range launchers {
		if launchers[i].ID == id {
			return launchers[i]
		}
	}
	require.Failf(t, "launcher not found", "%s", id)
	return platforms.Launcher{}
}

func TestPlatformStubs(t *testing.T) {
	t.Parallel()
	p := &Platform{}
	assert.Equal(t, platformids.RetroPie, p.ID())
	require.NoError(t, p.ScanHook(nil))
	require.NoError(t, p.KeyboardPress("a"))
	require.NoError(t, p.GamepadPress("a"))
	_, err := p.Screenshot()
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	_, err = p.ForwardCmd(nil)
	require.ErrorIs(t, err, platforms.ErrNotSupported)
	require.ErrorIs(t, p.LaunchSystem(nil, "nes"), platforms.ErrNotSupported)
	_, ok := p.LookupMapping(nil)
	assert.False(t, ok)
	assert.Equal(t, platforms.NoOpConsoleManager{}, p.ConsoleManager())
	assert.False(t, p.ManagedByPackageManager())
	assert.NotPanics(t, func() { p.SupportedReaders(newTestConfig(t)) })
}

func TestStartPreCreatesStateDir(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	require.NoError(t, tp.StartPre(nil))
	isDir, err := afero.IsDir(tp.fs, testStateDir)
	require.NoError(t, err)
	assert.True(t, isDir)

	tp.Platform.fs = afero.NewReadOnlyFs(afero.NewMemMapFs())
	require.ErrorContains(t, tp.StartPre(nil), testStateDir)
}

func TestStartPostWatchesStateAndStopEndsIt(t *testing.T) {
	t.Parallel()

	for name, loopRunning := range map[string]bool{"loop running": true, "loop not running": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tp := newTestPlatform(t)
			tp.Platform.fs = afero.NewOsFs()
			tp.stateDir = t.TempDir()
			if loopRunning {
				tp.procs.add(loopPID, 1, "bash", "bash autostart.sh")
				writeHostFile(t, filepath.Join(tp.stateDir, loopPIDFile), "500\n", 0o600)
			}
			tp.procs.add(runcommandPID, loopPID, "runcommand.sh", "/bin/bash runcommand.sh")
			media := &mediaRecorder{}

			require.NoError(t, tp.StartPost(context.Background(), nil, nil, media.get, media.set, nil, nil))
			require.NotNil(t, tp.stopWatcher)

			writeHostFile(t, filepath.Join(tp.stateDir, stateFile),
				"start\x00700\x00nes\x00lr-fceumm\x00"+testROM+"\x00", 0o600)
			require.Eventually(t, func() bool { return media.get() != nil }, 5*time.Second, 10*time.Millisecond,
				"StartPost wires the watcher to the media setter it was given")

			require.NoError(t, tp.Stop())
			assert.Nil(t, tp.stopWatcher)
			require.NoError(t, tp.Stop(), "stopping twice is harmless")
		})
	}
}

func TestStopLogsWatcherCloseFailure(t *testing.T) {
	t.Parallel()
	p := &Platform{stopWatcher: func() error { return errFault }}
	require.NoError(t, p.Stop(), "a watcher that fails to close does not fail shutdown")
	assert.Nil(t, p.stopWatcher)
}

func TestStartPostWithoutWatcherStillStarts(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.Platform.fs = afero.NewReadOnlyFs(afero.NewMemMapFs())
	require.NoError(t, tp.StartPost(context.Background(), nil, nil, nil, nil, nil, nil),
		"game tracking failing does not stop Core starting")
	assert.Nil(t, tp.stopWatcher)
}

func TestWaitTrackedProcessSecondWaiterWaitsForFirst(t *testing.T) {
	t.Parallel()
	proc, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	done := make(chan struct{})
	p := &Platform{trackedProcess: proc, processDone: done, waitClaimed: true}

	result := make(chan error, 1)
	go func() { result <- p.WaitTrackedProcess(proc) }()
	select {
	case <-result:
		require.Fail(t, "a second waiter returned before the first finished")
	default:
	}
	close(done)
	require.NoError(t, <-result)
}

func TestRootDirsAddsRomsDirOnce(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	cfg := newTestConfig(t)
	require.NoError(t, cfg.LoadTOML(`
[launchers]
index_root = ["/media/usb/", "/home/pi/RetroPie/roms/", "/media/usb"]
`))
	assert.Equal(t, []string{"/media/usb", testRomsDir}, tp.RootDirs(cfg))
}

func TestReturnToMenuClearsMedia(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	require.NoError(t, tp.ReturnToMenu())
	sets := tp.media.sets()
	require.Len(t, sets, 1)
	assert.Nil(t, sets[0])
}

func TestLaunchMedia(t *testing.T) {
	t.Parallel()

	holdPath := filepath.Join(testStateDir, holdFile)
	held := func(t *testing.T, tp *testPlatform) bool {
		t.Helper()
		ok, err := afero.Exists(tp.fs, holdPath)
		require.NoError(t, err)
		return ok
	}

	t.Run("no launcher for path", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		writeTestFile(t, tp.fs, holdPath, "", 0o600)
		err := tp.LaunchMedia(newTestConfig(t), "/nowhere/game.zzzunknown", nil, nil, nil)
		require.ErrorContains(t, err, "error finding launcher")
		assert.False(t, held(t, tp), "a failed launch still releases the loop")
	})

	t.Run("launcher fails", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		writeTestFile(t, tp.fs, holdPath, "", 0o600)
		tp.Platform.fs = faultFs{Fs: tp.fs, fail: failOn("remove", holdPath)}
		launcher := &platforms.Launcher{
			ID: "Broken",
			Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
				return nil, errFault
			},
		}
		err := tp.LaunchMedia(newTestConfig(t), testROM, launcher, nil, nil)
		require.ErrorIs(t, err, errFault)
		require.ErrorContains(t, err, "error launching")
		assert.True(t, held(t, tp), "the hold removal failed and is only logged")
	})

	t.Run("launcher runs", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		writeTestFile(t, tp.fs, holdPath, "", 0o600)
		var launched atomic.Bool
		launcher := &platforms.Launcher{
			ID:       "Works",
			SystemID: "NES",
			Launch: func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
				launched.Store(true)
				return nil, nil //nolint:nilnil // matches launchers that track no process
			},
		}
		require.NoError(t, tp.LaunchMedia(newTestConfig(t), testROM, launcher, nil, nil))
		assert.True(t, launched.Load())
		assert.False(t, held(t, tp))
	})
}

func TestGenericLauncherRunsScript(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	generic := findLauncher(t, tp.Launchers(newTestConfig(t)), "Generic")

	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	marker := filepath.Join(dir, "ran")
	writeHostFile(t, script, "#!/bin/sh\n: > \""+marker+"\"\n", 0o700)

	proc, err := generic.Launch(nil, script, nil)
	require.NoError(t, err)
	assert.Nil(t, proc)
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(marker)
		return statErr == nil
	}, 5*time.Second, 10*time.Millisecond)

	_, err = generic.Launch(nil, filepath.Join(dir, "missing.sh"), nil)
	require.ErrorContains(t, err, "failed to start command")
}

func TestSystemLauncherHandsGameToLoop(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.prepareLaunch(t)
	tp.procs.onSignal = func(f *fakeProcs, pid int, sig syscall.Signal) {
		if pid == esPID && sig == syscall.SIGTERM {
			f.exit(esPID)
			tp.takeRequest(t)
		}
	}
	autoAdvance(t, tp.clock, pollInterval)
	nes := findLauncher(t, tp.Launchers(newTestConfig(t)), "NES")

	proc, err := nes.Launch(nil, testROM, nil)
	require.NoError(t, err)
	assert.Nil(t, proc, "the game runs under the autostart loop")

	_, err = nes.Launch(nil, "relative/game.nes", nil)
	require.ErrorContains(t, err, "must be absolute")
}

func TestSendRetroArchQuit(t *testing.T) {
	t.Parallel()
	require.ErrorContains(t, sendRetroArchQuit(context.Background(), ""), "stop control unavailable")
	require.ErrorContains(t, sendRetroArchQuit(context.Background(), "no-port"), "send retroarch quit")
}

func TestLaunchFailures(t *testing.T) {
	t.Parallel()
	requestPath := filepath.Join(testStateDir, requestFile)

	t.Run("running game will not stop", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.prepareLaunch(t)
		tp.startGame("mupen64plus")
		autoAdvance(t, tp.clock, pollInterval)
		err := tp.launch(context.Background(), "nes", requestSystem, testROM)
		require.ErrorIs(t, err, platforms.ErrStopFailed)
		assert.False(t, tp.requestExists(t))
	})

	t.Run("system name the loop cannot take", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.prepareLaunch(t)
		writeTestFile(t, tp.fs, filepath.Join(testConfigsDir, "nes!", "emulators.cfg"), "", 0o644)
		err := tp.launch(context.Background(), "nes!", requestSystem, testROM)
		require.ErrorContains(t, err, "invalid RetroPie system name")
		assert.False(t, tp.requestExists(t))
	})

	t.Run("request cannot be written", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.prepareLaunch(t)
		tp.Platform.fs = afero.NewReadOnlyFs(tp.fs)
		require.Error(t, tp.launch(context.Background(), "nes", requestSystem, testROM))
		assert.False(t, tp.awaitingStart)
		assert.Empty(t, tp.procs.sent(), "EmulationStation is left alone")
	})

	t.Run("canceled while waiting and withdrawal fails", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.prepareLaunch(t)
		tp.Platform.fs = faultFs{Fs: tp.fs, fail: failOn("remove", requestPath)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := tp.launch(ctx, "nes", requestSystem, testROM)
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "launch canceled")
		assert.False(t, tp.awaitingStart)
		assert.True(t, tp.requestExists(t), "the failed withdrawal is only logged")
	})
}

func TestLaunchHoldRemovalFailureIsLogged(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.prepareLaunch(t)
	holdPath := filepath.Join(testStateDir, holdFile)
	writeTestFile(t, tp.fs, holdPath, "", 0o600)
	tp.Platform.fs = faultFs{Fs: tp.fs, fail: failOn("remove", holdPath)}
	tp.procs.onSignal = func(f *fakeProcs, pid int, _ syscall.Signal) {
		f.exit(pid)
		tp.takeRequest(t)
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.launch(context.Background(), "nes", requestSystem, testROM))
}

func TestCheckLaunchableRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.prepareLaunch(t)
	require.ErrorContains(t, tp.checkLaunchable("nes", requestKind("bogus"), testROM), "invalid request kind")
}

func TestSignalFailureIsLogged(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.Platform.procs = signalErrProcs{tp.procs}
	tp.signalAll([]int{esPID, emulatorPID}, syscall.SIGTERM)
	assert.Equal(t, []sentSignal{
		{pid: esPID, sig: syscall.SIGTERM},
		{pid: emulatorPID, sig: syscall.SIGTERM},
	}, tp.procs.sent(), "one failure does not stop the rest")
}

func TestStopWhenHoldCannotBeWritten(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.startGame("mupen64plus")
	tp.Platform.fs = afero.NewReadOnlyFs(tp.fs)
	tp.procs.onSignal = func(f *fakeProcs, pid int, _ syscall.Signal) {
		f.exit(pid)
		f.exit(runcommandPID)
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.StopActiveLauncher(platforms.StopForPreemption))
	assert.Zero(t, tp.run.pid, "the game is still stopped")
}

func TestStopFallsBackToSignalsWhenQuitFails(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.startGame("lr-fceumm")
	enableNetworkCommands(t, tp)
	tp.sendQuit = func(context.Context, string) error { return errFault }
	tp.procs.onSignal = func(f *fakeProcs, pid int, _ syscall.Signal) {
		f.exit(pid)
		f.exit(runcommandPID)
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.StopActiveLauncher(platforms.StopForMenu))
	assert.Equal(t, []sentSignal{{pid: emulatorPID, sig: syscall.SIGTERM}}, tp.procs.sent())
}

func TestReadRetroArchConfigSkipsOtherLines(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	path := filepath.Join(testConfigsDir, "all", "retroarch.cfg")
	writeTestFile(t, fs, path,
		"not a setting\nvideo_driver = \"gl\"\nnetwork_cmd_enable = \"true\"\nnetwork_cmd_port = 55400\n", 0o644)
	assert.Equal(t, map[string]string{"network_cmd_enable": "true", "network_cmd_port": "55400"},
		readRetroArchConfig(fs, path))
}

func TestWriteFileReplacingFailures(t *testing.T) {
	t.Parallel()
	dir := "/etc/systemd/system"
	path := filepath.Join(dir, "zaparoo.service")
	isTemp := func(name string) bool { return strings.HasPrefix(filepath.Base(name), ".zaparoo.service-") }

	for _, tc := range []struct {
		fail    func(op, name string) bool
		name    string
		wantErr string
	}{
		{
			name: "create", wantErr: "create temporary file",
			fail: func(op, name string) bool { return op == "open" && isTemp(name) },
		},
		{name: "write", wantErr: "write ", fail: func(op, name string) bool { return op == "write" && isTemp(name) }},
		{name: "close", wantErr: "close ", fail: func(op, name string) bool { return op == "close" && isTemp(name) }},
		{name: "chmod", wantErr: "chmod ", fail: func(op, name string) bool { return op == "chmod" && isTemp(name) }},
		{name: "rename", wantErr: "rename ", fail: failOn("rename", path)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := afero.NewMemMapFs()
			require.NoError(t, base.MkdirAll(dir, 0o755))
			writeTestFile(t, base, path, "old", 0o644)
			fs := faultFs{Fs: base, fail: tc.fail}

			err := writeFileReplacing(fs, path, []byte("new"), 0o644)
			require.ErrorIs(t, err, errFault)
			require.ErrorContains(t, err, tc.wantErr)
			assert.Equal(t, "old", readTestFile(t, base, path))
			entries, err := afero.ReadDir(base, dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "the temporary file is removed")
		})
	}
}

func TestHookFileFailures(t *testing.T) {
	t.Parallel()

	t.Run("read for add", func(t *testing.T) {
		t.Parallel()
		fs := faultFs{Fs: afero.NewMemMapFs(), fail: failOn("open", allPath(onStartFile))}
		err := addBlockToFile(fs, allPath(onStartFile), hookBlock(eventStart, testStateDir), false)
		require.ErrorIs(t, err, errFault)
	})

	t.Run("read for remove", func(t *testing.T) {
		t.Parallel()
		base := afero.NewMemMapFs()
		writeTestFile(t, base, allPath(onStartFile), hookBlock(eventStart, testStateDir), 0o755)
		fs := faultFs{Fs: base, fail: failOn("open", allPath(onStartFile))}
		require.ErrorIs(t, removeBlockFromFile(fs, allPath(onStartFile), true), errFault)
	})

	t.Run("remove emptied file", func(t *testing.T) {
		t.Parallel()
		base := afero.NewMemMapFs()
		require.NoError(t, addBlockToFile(base, allPath(onStartFile), hookBlock(eventStart, testStateDir), false))
		fs := faultFs{Fs: base, fail: failOn("remove", allPath(onStartFile))}
		require.ErrorIs(t, removeBlockFromFile(fs, allPath(onStartFile), true), errFault)
		assert.Contains(t, readTestFile(t, base, allPath(onStartFile)), blockBegin)
	})

	t.Run("install stops at onstart hook", func(t *testing.T) {
		t.Parallel()
		base := afero.NewMemMapFs()
		writeTestFile(t, base, allPath(autostartFile), retroPieAuto, 0o644)
		fs := faultFs{Fs: base, fail: failOn("open", allPath(onStartFile))}
		require.ErrorIs(t, installFrontendHooks(fs, testConfigsDir, testStateDir), errFault)
		exists, err := afero.Exists(base, allPath(onEndFile))
		require.NoError(t, err)
		assert.False(t, exists)
	})
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("covers the non-root path")
	}
}

func TestInstallAsUser(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	tp := newTestPlatform(t)
	writeTestFile(t, tp.fs, allPath(autostartFile), retroPieAuto, 0o644)
	cmds := &commandLog{}

	err := tp.Install(context.Background(), testExe, cmds.run)
	if err != nil {
		// The test user's name may not be one a unit file accepts.
		require.ErrorContains(t, err, "invalid user name")
		return
	}
	require.Len(t, cmds.calls, 1)
	assert.True(t, strings.HasPrefix(cmds.calls[0], "sudo -- "+testExe+" -install-system="))
	assert.Contains(t, readTestFile(t, tp.fs, allPath(onStartFile)), blockBegin)

	cmds.calls = nil
	require.NoError(t, tp.Uninstall(context.Background(), testExe, cmds.run))
	assert.Equal(t, []string{"sudo -- " + testExe + " -uninstall-system"}, cmds.calls)
	assert.Equal(t, retroPieAuto, readTestFile(t, tp.fs, allPath(autostartFile)))
}

func TestSystemHalfNeedsRoot(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	cmds := &commandLog{}
	require.ErrorContains(t, InstallSystem(context.Background(), "pi", testExe, cmds.run), "must be run as root")
	require.ErrorContains(t, UninstallSystem(context.Background(), cmds.run), "must be run as root")
	assert.Empty(t, cmds.calls)
}

func TestRealSystemSide(t *testing.T) {
	t.Parallel()
	cmds := &commandLog{}
	side := realSystemSide(cmds.run)
	assert.Equal(t, ServiceFilePath, side.unitPath)
	assert.IsType(t, &afero.OsFs{}, side.fs)
	assert.NotNil(t, side.lookupUser)
	assert.NotNil(t, side.installHardware)
	assert.NotNil(t, side.uninstallHardware)
	require.NoError(t, side.run(context.Background(), "systemctl", "status"))
	assert.Equal(t, []string{"systemctl status"}, cmds.calls)
}

func TestUserInstallFailures(t *testing.T) {
	t.Parallel()

	t.Run("state dir", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.Platform.fs = afero.NewReadOnlyFs(tp.fs)
		cmds := &commandLog{}
		require.ErrorContains(t, tp.installUser(context.Background(), "pi", testExe, cmds.run), testStateDir)
		assert.Empty(t, cmds.calls)
	})

	t.Run("rollback fails", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		writeTestFile(t, tp.fs, allPath(autostartFile), retroPieAuto, 0o644)
		var broken atomic.Bool
		tp.Platform.fs = faultFs{Fs: tp.fs, fail: func(op, _ string) bool { return broken.Load() && op == "open" }}
		run := func(context.Context, string, ...string) error {
			broken.Store(true)
			return errFault
		}
		err := tp.installUser(context.Background(), "pi", testExe, run)
		require.ErrorContains(t, err, "install the Zaparoo service")
		assert.Contains(t, readTestFile(t, tp.fs, allPath(autostartFile)), blockBegin,
			"the hooks that could not be rolled back are left")
	})

	t.Run("uninstall sudo fails", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		writeTestFile(t, tp.fs, allPath(autostartFile), retroPieAuto, 0o644)
		require.NoError(t, installFrontendHooks(tp.fs, testConfigsDir, testStateDir))
		cmds := &commandLog{failOn: "sudo"}
		err := tp.uninstallUser(context.Background(), testExe, cmds.run)
		require.ErrorContains(t, err, "uninstall the Zaparoo service")
		assert.Contains(t, readTestFile(t, tp.fs, allPath(autostartFile)), blockBegin,
			"hooks stay while the service is still installed")
	})

	t.Run("uninstall hooks fail", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.Platform.fs = faultFs{Fs: tp.fs, fail: failOn("open", allPath(autostartFile))}
		cmds := &commandLog{}
		require.ErrorContains(t, tp.uninstallUser(context.Background(), testExe, cmds.run), "remove RetroPie hooks")
	})
}

func TestSystemSideFailures(t *testing.T) {
	t.Parallel()

	t.Run("reader support", func(t *testing.T) {
		t.Parallel()
		cmds := &commandLog{}
		side, _, _ := newSystemSide(cmds)
		side.installHardware = func() error { return errFault }
		require.ErrorIs(t, side.install(context.Background(), "pi", testExe), errFault)
		assert.Empty(t, cmds.calls)
	})

	t.Run("unit write", func(t *testing.T) {
		t.Parallel()
		cmds := &commandLog{}
		side, _, _ := newSystemSide(cmds)
		side.fs = afero.NewReadOnlyFs(side.fs)
		require.ErrorContains(t, side.install(context.Background(), "pi", testExe), "write service file")
		assert.Empty(t, cmds.calls)
	})

	t.Run("systemctl", func(t *testing.T) {
		t.Parallel()
		cmds := &commandLog{failOn: "enable"}
		side, _, _ := newSystemSide(cmds)
		require.ErrorContains(t, side.install(context.Background(), "pi", testExe), "systemctl enable")
		assert.Equal(t, []string{"systemctl daemon-reload", "systemctl enable zaparoo.service"}, cmds.calls)
	})

	t.Run("unit removal", func(t *testing.T) {
		t.Parallel()
		cmds := &commandLog{}
		side, _, uninstalled := newSystemSide(cmds)
		writeTestFile(t, side.fs, ServiceFilePath, "unit", 0o644)
		side.fs = faultFs{Fs: side.fs, fail: failOn("remove", ServiceFilePath)}
		require.ErrorIs(t, side.uninstall(context.Background()), errFault)
		assert.Zero(t, *uninstalled)
	})

	t.Run("daemon-reload", func(t *testing.T) {
		t.Parallel()
		cmds := &commandLog{failOn: "daemon-reload"}
		side, _, uninstalled := newSystemSide(cmds)
		require.ErrorContains(t, side.uninstall(context.Background()), "systemctl daemon-reload")
		assert.Zero(t, *uninstalled)
	})

	t.Run("reader support removal", func(t *testing.T) {
		t.Parallel()
		cmds := &commandLog{}
		side, _, _ := newSystemSide(cmds)
		side.uninstallHardware = func() error { return errFault }
		require.ErrorIs(t, side.uninstall(context.Background()), errFault)
	})
}

func TestProcFSMalformed(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "/proc", procFS{}.dir())

	root := t.TempDir()
	write := func(pid, stat string) {
		dir := filepath.Join(root, pid)
		require.NoError(t, os.MkdirAll(dir, 0o750))
		writeHostFile(t, filepath.Join(dir, "stat"), stat, 0o600)
	}
	write("10", "10 no parens S 1")
	write("11", "11 (short)")
	write("12", "12 (badppid) S x")
	write("13", "13 (ends)")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "self"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "0"), 0o750))

	p := procFS{root: root}
	for _, pid := range []int{10, 11, 12, 13, 99} {
		_, ok := p.stat(pid)
		assert.False(t, ok, "pid %d", pid)
		assert.False(t, p.zombie(pid), "pid %d", pid)
	}
	write("14", "14 S 1")
	assert.False(t, p.zombie(14), "no closing paren")
	assert.Empty(t, p.cmdline(99))
	assert.Empty(t, p.all(), "malformed and non-numeric entries are skipped")
	assert.Nil(t, procFS{root: filepath.Join(root, "missing")}.all())
}

func TestDescendantsOfSurvivesCycle(t *testing.T) {
	t.Parallel()
	procs := map[int]procInfo{1: {ppid: 2}, 2: {ppid: 1}}
	assert.Equal(t, []int{2}, descendantsOf(procs, 1))
}

func TestProcFSSignalReportsPermissionError(t *testing.T) {
	t.Parallel()
	skipIfRoot(t)
	// Signal 0 only checks the target exists and may be signalled.
	require.ErrorIs(t, procFS{}.signal(1, 0), syscall.EPERM)
	require.NoError(t, procFS{}.signal(os.Getpid(), 0))
}

func TestRequestFileFailures(t *testing.T) {
	t.Parallel()
	ro := afero.NewReadOnlyFs(afero.NewMemMapFs())
	require.ErrorContains(t, writeRequest(ro, testStateDir, []byte("x")), testStateDir)

	base := afero.NewMemMapFs()
	writeTestFile(t, base, filepath.Join(testStateDir, holdFile), "", 0o600)
	err := removeIfExists(afero.NewReadOnlyFs(base), filepath.Join(testStateDir, holdFile))
	require.ErrorContains(t, err, "remove ")
	require.NoError(t, removeIfExists(base, filepath.Join(testStateDir, "missing")))
}

func TestStateFileFailures(t *testing.T) {
	t.Parallel()

	t.Run("missing state", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.loadState()
		assert.Empty(t, tp.media.sets())
		_, err := tp.readStateFile()
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("unreadable state", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.Platform.fs = afero.NewOsFs()
		tp.stateDir = t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tp.stateDir, stateFile), 0o750))
		_, err := tp.readStateFile()
		require.ErrorContains(t, err, "read state file")
	})

	t.Run("state dir cannot be created", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.Platform.fs = afero.NewReadOnlyFs(tp.fs)
		_, err := tp.startStateWatcher()
		require.ErrorContains(t, err, "create ")
	})

	t.Run("state dir cannot be watched", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.stateDir = filepath.Join(t.TempDir(), "only-in-memory")
		_, err := tp.startStateWatcher()
		require.ErrorContains(t, err, "watch ")
	})
}
