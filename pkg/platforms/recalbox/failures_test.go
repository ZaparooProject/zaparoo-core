//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFault = errors.New("injected fault")

// renameFailFs fails every rename, which is the last step of an atomic write.
type renameFailFs struct {
	afero.Fs
}

func (renameFailFs) Rename(string, string) error { return errFault }

func newConfig(t *testing.T) *config.Instance {
	t.Helper()
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	return cfg
}

func writeSupervisor(t *testing.T, fs afero.Fs) {
	t.Helper()
	writeFile(t, fs, SupervisorPIDFile, "321\n")
	writeFile(t, fs, "/proc/321/cmdline", "/bin/sh\x00"+ScriptPath+"\x00")
}

func TestNewInstallerUsesRealSystem(t *testing.T) {
	t.Parallel()
	inst := NewInstaller(testScript, testExe)
	assert.IsType(t, &afero.OsFs{}, inst.FS)
	assert.Equal(t, testScript, inst.Script)
	assert.Equal(t, testExe, inst.Executable)
	assert.NotNil(t, inst.StartScript)
	assert.NotNil(t, inst.Signal)
	assert.NotNil(t, inst.Sleep)
}

func TestInstall_ScriptWriteFails(t *testing.T) {
	t.Parallel()
	inst, started, _ := newTestInstaller(t, renameFailFs{afero.NewMemMapFs()})
	err := inst.Install()
	require.ErrorIs(t, err, errFault)
	require.ErrorContains(t, err, "write user script")
	assert.Empty(t, *started)
}

func TestUninstall_ScriptRemovalFails(t *testing.T) {
	t.Parallel()
	base := afero.NewMemMapFs()
	writeFile(t, base, ScriptPath, testScript)
	inst, _, _ := newTestInstaller(t, afero.NewReadOnlyFs(base))
	require.ErrorContains(t, inst.Uninstall(), "remove user script")
}

func TestStopSupervisor_DefaultSleep(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst, _, signals := newTestInstaller(t, fs)
	inst.Sleep = nil
	writeSupervisor(t, fs)

	require.NoError(t, inst.stopSupervisor())
	assert.Equal(t, []signalRecord{{pid: 321, sig: syscall.SIGTERM}}, *signals)
}

func TestStopSupervisor_ExitsOnLastWait(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	inst, _, _ := newTestInstaller(t, fs)
	inst.Signal = func(int, syscall.Signal) error { return nil }
	waits := int(supervisorStopTimeout / supervisorStopPoll)
	slept := 0
	inst.Sleep = func(time.Duration) {
		slept++
		if slept == waits {
			_ = fs.Remove(SupervisorPIDFile)
		}
	}
	writeSupervisor(t, fs)

	require.NoError(t, inst.stopSupervisor(), "a supervisor gone by the final check has stopped")
	assert.Equal(t, waits, slept)
}

func TestStartDetachedRunsScript(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "custom.sh")
	require.NoError(t, os.WriteFile(script, []byte(": > \""+marker+"\"\n"), 0o600))

	require.NoError(t, startDetached(script))
	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
}

func TestSendCommand_BadAddress(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, newTestFs(t))
	p.commandAddr = "no-port"
	require.ErrorContains(t, p.sendCommand(context.Background(), []byte("START")), "connect to EmulationStation")

	writeFile(t, p.fs, snesGame, "rom")
	_, err := p.launchGame(newConfig(t), snesGame, nil)
	require.ErrorContains(t, err, "connect to EmulationStation")
	assert.True(t, p.lastStart.IsZero(), "a START that was not sent does not hold back the next")
}

func TestSendStart_CanceledWhileSpacing(t *testing.T) {
	t.Parallel()
	p, _, clock := newTestPlatform(t, newTestFs(t))
	addr, frames := fakeES(t)
	p.commandAddr = addr
	p.lastStart = clock.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.sendStart(ctx, []byte("START|snes|/a"))
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "wait to launch")
	select {
	case frame := <-frames:
		t.Fatalf("START sent after the wait was canceled: %q", frame)
	default:
	}
}

func TestPlatformLaunchersAndRoots(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, newTestFs(t))
	cfg := newConfig(t)
	assert.NotPanics(t, func() { p.SupportedReaders(cfg) })

	require.NoError(t, cfg.LoadTOML(`
[launchers]
index_root = ["/media/usb/", "/recalbox/share/roms/", "/media/usb"]
`))
	assert.Equal(t, []string{"/media/usb", romsRoot}, p.RootDirs(cfg))

	var generic, snes bool
	for _, l := range p.Launchers(cfg) {
		switch {
		case l.ID == "Generic":
			generic = true
		case l.SystemID == systemdefs.SystemSNES:
			snes = true
		}
	}
	assert.True(t, generic)
	assert.True(t, snes)

	require.Error(t, p.LaunchMedia(cfg, "/nowhere/game.zzzunknown", nil, nil, nil))
}

func TestServiceContextEndsWithStop(t *testing.T) {
	t.Parallel()
	p, rec, _ := newTestPlatform(t, newTestFs(t))
	p.subscribe = nil
	assert.Equal(t, context.Background(), p.ctx())

	require.NoError(t, p.StartPost(context.Background(), newConfig(t), nil, rec.get, rec.set, nil, nil))
	ctx := p.ctx()
	require.NoError(t, ctx.Err())
	require.NoError(t, p.Stop())
	require.ErrorIs(t, ctx.Err(), context.Canceled, "launches waiting on ES end when Core stops")
}

func TestParseStateSkipsLinesWithoutValue(t *testing.T) {
	t.Parallel()
	st, ok := parseState([]byte("garbage\n" + stateText("rungame", "snes", "Game", snesGame)))
	require.True(t, ok)
	assert.Equal(t, "rungame", st.Action)
	assert.Equal(t, snesGame, st.GamePath)
}

func TestPokeDoesNotBlock(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, newTestFs(t))
	p.poke()
	p.poke()
	assert.Len(t, p.wake, 1)
}

func TestTracker_KeepsMatchingCoreMedia(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("gamelistbrowsing", "snes", "", ""))
	p.refresh()

	core := snesMedia()
	rec.set(core)
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Other Name", snesGame))
	p.refresh()
	assert.Same(t, core, rec.get(), "media Core already set for the game is kept")
	assert.Len(t, rec.sets, 1)
}

func TestMediaForState_UnresolvedSystem(t *testing.T) {
	t.Parallel()
	st := &esState{Action: "rungame", SystemID: "unknown"}

	p, _, _ := newTestPlatform(t, afero.NewMemMapFs())
	assert.Nil(t, p.mediaForState(st, "/recalbox/share/roms/unknown/x.bin"), "no system list")

	p, _, _ = newTestPlatform(t, newTestFs(t))
	assert.Nil(t, p.mediaForState(st, "/elsewhere/x.bin"), "path outside every system folder")
}

func TestWaitForGame(t *testing.T) {
	t.Parallel()

	t.Run("canceled", func(t *testing.T) {
		t.Parallel()
		p, _, _ := newTestPlatform(t, newTestFs(t))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		require.ErrorIs(t, p.waitForGame(ctx, snesGame, p.stateGeneration(), time.Minute), context.Canceled)
	})

	t.Run("wakes on another reader's refresh", func(t *testing.T) {
		t.Parallel()
		fs := newTestFs(t)
		p, _, clock := newTestPlatform(t, fs)
		writeFile(t, fs, stateFile, stateText("gamelistbrowsing", "snes", "", ""))
		p.refresh()
		since := p.stateGeneration()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- p.waitForGame(ctx, snesGame, since, time.Minute) }()
		// The deadline and the poll timer.
		require.NoError(t, clock.BlockUntilContext(ctx, 2))
		writeFile(t, fs, stateFile, stateText("rungame", "snes", "Game", snesGame))
		p.refresh()
		require.NoError(t, <-done)
	})
}

func TestProcParsingRejectsMalformedEntries(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	p, _, _ := newTestPlatform(t, fs)
	writeFile(t, fs, filepath.Join(procRoot, "50", "stat"), "50 7 no-parens")
	writeFile(t, fs, filepath.Join(procRoot, "51", "stat"), "51 (short) S")
	writeFile(t, fs, filepath.Join(procRoot, "52", "stat"), "52 (bad) S x")
	writeFile(t, fs, filepath.Join(procRoot, "53", "stat"), "53 (neg) S -4")

	for _, pid := range []int{50, 51, 52, 53, 99} {
		_, ok := p.parentPID(pid)
		assert.False(t, ok, "pid %d", pid)
	}
	assert.Nil(t, p.cmdline(99))
	assert.Empty(t, p.scanProcs())
	assert.False(t, isLauncher(nil))
}

func TestDescendantsOfSurvivesCycle(t *testing.T) {
	t.Parallel()
	procs := []procEntry{{pid: 2, ppid: 3}, {pid: 3, ppid: 2}}
	assert.Equal(t, []int{3}, descendantsOf(procs, []int{2}))
}

func TestSignalAllContinuesPastFailures(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, newTestFs(t))
	var sent []int
	p.signal = func(pid int, _ syscall.Signal) error {
		sent = append(sent, pid)
		if pid == 40 {
			return syscall.EPERM
		}
		return syscall.ESRCH
	}
	p.signalAll([]int{40, 30}, syscall.SIGTERM)
	assert.Equal(t, []int{40, 30}, sent)
}

func TestKillProcess(t *testing.T) {
	t.Parallel()
	// Signal 0 only checks the target exists and may be signalled.
	require.NoError(t, killProcess(os.Getpid(), 0))
	if os.Geteuid() != 0 {
		require.ErrorIs(t, killProcess(1, 0), syscall.EPERM)
	}
}

func TestReadSystemList_Rejects(t *testing.T) {
	t.Parallel()

	t.Run("too large", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		writeFile(t, fs, templateSystemList, strings.Repeat(" ", maxSystemListSize+1))
		_, err := readSystemList(fs, templateSystemList)
		require.ErrorContains(t, err, "is too large")
	})

	t.Run("unreadable", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewBasePathFs(afero.NewOsFs(), t.TempDir())
		require.NoError(t, fs.MkdirAll(templateSystemList, 0o750))
		_, err := readSystemList(fs, templateSystemList)
		require.ErrorContains(t, err, "read "+templateSystemList)
	})
}
