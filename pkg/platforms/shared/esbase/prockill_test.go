//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package esbase

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testProcRoot = "/proc"

type signalled struct {
	pid int
	sig syscall.Signal
}

type fakeProcs struct {
	fs       afero.Fs
	stubborn map[int]bool
	// zombies exit on SIGTERM but are left unreaped.
	zombies map[int]bool
	sent    []signalled
	mu      syncutil.Mutex
}

func newFakeProcs(t *testing.T) *fakeProcs {
	t.Helper()
	return &fakeProcs{fs: afero.NewMemMapFs(), stubborn: map[int]bool{}, zombies: map[int]bool{}}
}

func (f *fakeProcs) add(t *testing.T, pid int, comm, argv0 string) {
	t.Helper()
	dir := filepath.Join(testProcRoot, strconv.Itoa(pid))
	require.NoError(t, afero.WriteFile(f.fs, filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644))
	require.NoError(t, afero.WriteFile(f.fs, filepath.Join(dir, "cmdline"), []byte(argv0+"\x00--flag\x00"), 0o644))
}

func (f *fakeProcs) signal(pid int, sig syscall.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, signalled{pid: pid, sig: sig})
	if f.zombies[pid] {
		stat := strconv.Itoa(pid) + " (my game) (x) Z 1 0 0"
		//nolint:wrapcheck // fake process table
		return afero.WriteFile(f.fs, filepath.Join(testProcRoot, strconv.Itoa(pid), "stat"), []byte(stat), 0o644)
	}
	if sig == syscall.SIGKILL || !f.stubborn[pid] {
		return f.fs.RemoveAll(filepath.Join(testProcRoot, strconv.Itoa(pid))) //nolint:wrapcheck // fake process table
	}
	return nil
}

func (f *fakeProcs) signals() []signalled {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]signalled(nil), f.sent...)
}

func (f *fakeProcs) killer(clock clockwork.Clock) *ProcessKiller {
	return &ProcessKiller{
		FS:       f.fs,
		Clock:    clock,
		Signal:   f.signal,
		ProcPath: testProcRoot,
		Grace:    5 * time.Second,
		Poll:     time.Second,
		SelfPID:  99,
	}
}

func TestValidProcessName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"retroarch", "mupen64plus", "PPSSPPSDL", "flycast.elf", "duckstation-nogui"} {
		assert.True(t, ValidProcessName(name), name)
	}
	for _, name := range []string{
		"", ".", "..", "/usr/bin/retroarch", "retro arch", "-9", "a\tb", "a\nb", "a;b\x00",
		string(make([]byte, 256)),
	} {
		assert.False(t, ValidProcessName(name), "%q", name)
	}
}

func TestProcessKiller_TermsMatchingProcessesOnly(t *testing.T) {
	t.Parallel()

	procs := newFakeProcs(t)
	procs.add(t, 1, "retroarch", "/usr/bin/retroarch")
	procs.add(t, 99, "retroarch", "/usr/bin/retroarch")
	procs.add(t, 200, "retroarch", "/usr/bin/retroarch")
	procs.add(t, 201, "emulationstatio", "/usr/bin/emulationstation")
	procs.add(t, 202, "sh", "/bin/sh")

	n, err := procs.killer(clockwork.NewFakeClock()).Kill(context.Background(), []string{"retroarch"})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []signalled{{pid: 200, sig: syscall.SIGTERM}}, procs.signals(),
		"init and Core itself are never signalled")
}

func TestProcessKiller_LongNamesNeedTheCommandLine(t *testing.T) {
	t.Parallel()

	procs := newFakeProcs(t)
	procs.add(t, 300, "duckstation-nog", "/usr/bin/duckstation-nogui")
	procs.add(t, 301, "duckstation-nog", "/usr/bin/duckstation-nogame")

	n, err := procs.killer(clockwork.NewFakeClock()).Kill(context.Background(), []string{"duckstation-nogui"})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []signalled{{pid: 300, sig: syscall.SIGTERM}}, procs.signals())
}

func TestProcessKiller_KillsSurvivorsAfterGrace(t *testing.T) {
	t.Parallel()

	procs := newFakeProcs(t)
	procs.add(t, 400, "retroarch", "/usr/bin/retroarch")
	procs.stubborn[400] = true
	clock := clockwork.NewFakeClock()
	killer := procs.killer(clock)

	done := make(chan error, 1)
	go func() {
		_, err := killer.Kill(context.Background(), []string{"retroarch"})
		done <- err
	}()
	for range 5 {
		require.NoError(t, clock.BlockUntilContext(context.Background(), 1))
		clock.Advance(time.Second)
	}
	require.NoError(t, <-done)
	assert.Equal(t, []signalled{
		{pid: 400, sig: syscall.SIGTERM},
		{pid: 400, sig: syscall.SIGKILL},
	}, procs.signals())
}

func TestProcessKiller_KillsSurvivorsWhenTheCallerGivesUp(t *testing.T) {
	t.Parallel()

	procs := newFakeProcs(t)
	procs.add(t, 500, "retroarch", "/usr/bin/retroarch")
	procs.stubborn[500] = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := procs.killer(clockwork.NewFakeClock()).Kill(ctx, []string{"retroarch"})
	require.NoError(t, err)
	assert.Equal(t, syscall.SIGKILL, procs.signals()[1].sig)
}

func TestProcessKiller_NothingToKill(t *testing.T) {
	t.Parallel()

	procs := newFakeProcs(t)
	killer := procs.killer(clockwork.NewFakeClock())

	_, err := killer.Kill(context.Background(), []string{"../x", ""})
	require.ErrorIs(t, err, ErrNoProcessNames)

	n, err := killer.Kill(context.Background(), []string{"retroarch"})
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, procs.signals())
}

// A game that exits on SIGTERM but whose parent has not reaped it yet is
// done. The stop must not wait out the grace period and SIGKILL a zombie.
func TestProcessKiller_UnreapedExitIsNotAlive(t *testing.T) {
	t.Parallel()

	procs := newFakeProcs(t)
	procs.add(t, 400, "retroarch", "/usr/bin/retroarch")
	procs.zombies[400] = true

	done := make(chan error, 1)
	go func() {
		_, err := procs.killer(clockwork.NewFakeClock()).Kill(context.Background(), []string{"retroarch"})
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Kill waited on a process that had already exited")
	}
	assert.Equal(t, []signalled{{pid: 400, sig: syscall.SIGTERM}}, procs.signals())
}

func TestProcessKiller_ZombieState(t *testing.T) {
	t.Parallel()

	procs := newFakeProcs(t)
	k := procs.killer(clockwork.NewFakeClock())
	for pid, stat := range map[int]string{
		500: "500 (retroarch) S 1 0 0",
		501: "501 (a) Z (b) R 1 0 0",
		502: "502 retroarch Z",
		504: "504 (my game) (x) Z 1 0 0",
	} {
		require.NoError(t, afero.WriteFile(procs.fs,
			filepath.Join(testProcRoot, strconv.Itoa(pid), "stat"), []byte(stat), 0o644))
	}
	assert.False(t, k.zombie(500), "running")
	assert.False(t, k.zombie(501), "the state follows the last parenthesis")
	assert.False(t, k.zombie(502), "no name field")
	assert.False(t, k.zombie(503), "no stat file")
	assert.True(t, k.zombie(504), "zombie with parentheses in its name")
}

func TestProcessKiller_KillNowSendsSIGKILL(t *testing.T) {
	t.Parallel()

	procs := newFakeProcs(t)
	procs.add(t, 200, "retroarch", "/usr/bin/retroarch")
	procs.add(t, 201, "sh", "/bin/sh")

	n, err := procs.killer(clockwork.NewFakeClock()).KillNow([]string{"retroarch", "bad name"})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, []signalled{{pid: 200, sig: syscall.SIGKILL}}, procs.signals())

	_, err = procs.killer(clockwork.NewFakeClock()).KillNow([]string{"bad name"})
	require.ErrorIs(t, err, ErrNoProcessNames)
}

func TestNewProcessKiller_UsesThisSystem(t *testing.T) {
	t.Parallel()

	k := NewProcessKiller()
	assert.Equal(t, "/proc", k.ProcPath)
	assert.Equal(t, os.Getpid(), k.SelfPID)
	assert.Equal(t, 5*time.Second, k.Grace)
	assert.NotNil(t, k.Signal)
	assert.NotNil(t, k.FS)
	assert.NotNil(t, k.Clock)
}
