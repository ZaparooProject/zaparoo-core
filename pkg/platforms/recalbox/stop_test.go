//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type signalLog struct {
	got []signalRecord
	mu  syncutil.Mutex
}

func (l *signalLog) add(pid int, sig syscall.Signal) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, signalRecord{pid: pid, sig: sig})
}

func (l *signalLog) all() []signalRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]signalRecord(nil), l.got...)
}

// runWithClock runs fn while advancing the fake clock whenever fn sleeps.
func runWithClock(t *testing.T, clock *clockwork.FakeClock, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			t.Fatal("stop did not finish")
			return nil
		default:
		}
		waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Millisecond)
		if clock.BlockUntilContext(waitCtx, 1) == nil {
			clock.Advance(stopPollEvery)
		}
		waitCancel()
	}
}

func TestFindLaunchers(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	addRunningGame(t, fs)
	addProc(t, fs, 50, 1, "python", "python", "/usr/bin/other.py")
	addProc(t, fs, 51, 1, "vi", "vi", "/usr/lib/python3.11/site-packages/configgen/emulatorlauncher.py")
	addProc(t, fs, 52, 1, "grep", "grep", "-r", "emulatorlauncher")
	addProc(t, fs, 53, 1, "python3", "python3", "-m", "emulatorlauncher")
	addProc(t, fs, 54, 1, "emulatorlaunch", "/usr/bin/emulatorlauncher", "-system", "nes")
	addProc(t, fs, 1, 0, "init", "/usr/bin/emulatorlauncher")
	writeFile(t, fs, "/proc/self/stat", "garbage")
	writeFile(t, fs, "/proc/60/stat", "60 (broken")
	p, _, _ := newTestPlatform(t, fs)
	assert.ElementsMatch(t, []int{30, 54}, p.findLaunchers())
}

func TestParentPIDHandlesParensInComm(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, "/proc/70/stat", "70 (a) b (c)) S 12 1 1")
	p, _, _ := newTestPlatform(t, fs)
	ppid, ok := p.parentPID(70)
	require.True(t, ok)
	assert.Equal(t, 12, ppid)
}

func TestDescendantsOf(t *testing.T) {
	t.Parallel()
	procs := []procEntry{
		{pid: 30, ppid: 20}, {pid: 40, ppid: 30}, {pid: 41, ppid: 40}, {pid: 42, ppid: 30}, {pid: 99, ppid: 1},
	}
	assert.Equal(t, []int{41, 40, 42}, descendantsOf(procs, []int{30}))
}

func TestStopGame_NothingRunning(t *testing.T) {
	t.Parallel()
	p, _, _ := newTestPlatform(t, newTestFs(t))
	require.NoError(t, p.stopGame())
}

func TestStopGame_TerminatesEmulatorFirst(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	addRunningGame(t, fs)
	p, _, _ := newTestPlatform(t, fs)
	log := &signalLog{}
	p.signal = func(pid int, sig syscall.Signal) error {
		log.add(pid, sig)
		if pid == 40 && sig == syscall.SIGTERM {
			removeProc(fs, 40)
			removeProc(fs, 30)
			removeProc(fs, 20)
		}
		return nil
	}

	require.NoError(t, p.stopGame())
	assert.Equal(t, []signalRecord{{pid: 40, sig: syscall.SIGTERM}}, log.all(),
		"only the emulator is signalled; configgen and ES clean up after it")
}

func TestStopGame_TerminatesLauncherWithoutChildren(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	addRunningGame(t, fs)
	removeProc(fs, 40)
	p, _, _ := newTestPlatform(t, fs)
	log := &signalLog{}
	p.signal = func(pid int, sig syscall.Signal) error {
		log.add(pid, sig)
		removeProc(fs, pid)
		return nil
	}
	require.NoError(t, p.stopGame())
	assert.Equal(t, []signalRecord{{pid: 30, sig: syscall.SIGTERM}}, log.all())
}

func TestStopGame_KillsAfterTimeout(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	addRunningGame(t, fs)
	p, _, clock := newTestPlatform(t, fs)
	log := &signalLog{}
	p.signal = func(pid int, sig syscall.Signal) error {
		log.add(pid, sig)
		if sig == syscall.SIGKILL {
			removeProc(fs, pid)
		}
		return nil
	}

	require.NoError(t, runWithClock(t, clock, p.stopGame))
	assert.Equal(t, []signalRecord{
		{pid: 40, sig: syscall.SIGTERM},
		{pid: 40, sig: syscall.SIGKILL},
		{pid: 30, sig: syscall.SIGKILL},
	}, log.all())
}

func TestStopGame_ReportsFailure(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	addRunningGame(t, fs)
	p, rec, clock := newTestPlatform(t, fs)
	p.signal = func(int, syscall.Signal) error { return nil }
	current := snesMedia()
	rec.current = current

	err := runWithClock(t, clock, func() error { return p.StopActiveLauncher(platforms.StopForMenu) })
	require.ErrorIs(t, err, platforms.ErrStopFailed)
	assert.Same(t, current, rec.get(), "media stays while the game still runs")
}

func TestStopActiveLauncher_ClearsMedia(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	addRunningGame(t, fs)
	p, rec, _ := newTestPlatform(t, fs)
	p.signal = func(pid int, _ syscall.Signal) error {
		removeProc(fs, pid)
		removeProc(fs, 30)
		return nil
	}
	rec.current = snesMedia()
	p.gamePath = snesGame

	require.NoError(t, p.ReturnToMenu())
	assert.Nil(t, rec.get())
	assert.Empty(t, p.gamePath)
}
