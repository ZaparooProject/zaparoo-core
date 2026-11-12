//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package inputactivity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const waitFor = 5 * time.Second

// newPipe returns a non-blocking read end for a pollSet and a write end for
// the test. The test must not close the read end once it is handed over.
func newPipe(t *testing.T) (readFD int, write func(p []byte), closeWrite func()) {
	t.Helper()
	var fds [2]int
	require.NoError(t, unix.Pipe2(fds[:], unix.O_NONBLOCK|unix.O_CLOEXEC))
	closed := false
	closeWrite = func() {
		if !closed {
			closed = true
			_ = unix.Close(fds[1])
		}
	}
	t.Cleanup(closeWrite)
	return fds[0], func(p []byte) {
		_, err := unix.Write(fds[1], p)
		require.NoError(t, err)
	}, closeWrite
}

func runSet(t *testing.T, set *pollSet) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		ch <- set.run(ctx)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(waitFor):
			t.Error("pollSet.run did not stop")
		}
	})
	return cancel, ch
}

func TestPollSetDeliversData(t *testing.T) {
	t.Parallel()
	set, err := newPollSet()
	require.NoError(t, err)
	got := make(chan string, 4)
	fd, write, _ := newPipe(t)
	require.True(t, set.add("/dev/a", fd, func(p []byte) { got <- string(p) }))
	cancel, done := runSet(t, set)

	write([]byte("hello"))
	select {
	case s := <-got:
		assert.Equal(t, "hello", s)
	case <-time.After(waitFor):
		t.Fatal("no data delivered")
	}

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("run did not stop promptly after cancel")
	}
}

func TestPollSetCancelWithNoDevices(t *testing.T) {
	t.Parallel()
	set, err := newPollSet()
	require.NoError(t, err)
	cancel, done := runSet(t, set)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("blocked read survived cancel")
	}
}

func TestPollSetAddWhileRunning(t *testing.T) {
	t.Parallel()
	set, err := newPollSet()
	require.NoError(t, err)
	runSet(t, set)

	got := make(chan struct{}, 1)
	fd, write, _ := newPipe(t)
	require.True(t, set.add("/dev/late", fd, func([]byte) { got <- struct{}{} }))
	assert.True(t, set.has("/dev/late"))
	assert.Equal(t, []string{"/dev/late"}, set.paths())

	write([]byte{1})
	select {
	case <-got:
	case <-time.After(waitFor):
		t.Fatal("device added while running was not read")
	}

	dupFD, _, _ := newPipe(t)
	assert.False(t, set.add("/dev/late", dupFD, func([]byte) {}), "duplicate path is refused")
	_ = unix.Close(dupFD)
}

func TestPollSetRemoveClosesDevice(t *testing.T) {
	t.Parallel()
	set, err := newPollSet()
	require.NoError(t, err)
	var fds [2]int
	require.NoError(t, unix.Pipe2(fds[:], unix.O_NONBLOCK|unix.O_CLOEXEC))
	defer func() { _ = unix.Close(fds[1]) }()
	require.True(t, set.add("/dev/gone", fds[0], func([]byte) {}))
	runSet(t, set)

	set.remove("/dev/gone")
	assert.Eventually(t, func() bool {
		return !set.has("/dev/gone") && len(set.paths()) == 0
	}, waitFor, 5*time.Millisecond)
	// The loop closed the read end it owned, so writing now hits a broken pipe.
	assert.Eventually(t, func() bool {
		_, err := unix.Write(fds[1], []byte{1})
		return errors.Is(err, unix.EPIPE)
	}, waitFor, 5*time.Millisecond)
}

func TestPollSetDropsHungUpDevice(t *testing.T) {
	t.Parallel()
	set, err := newPollSet()
	require.NoError(t, err)
	fd, _, closeWrite := newPipe(t)
	require.True(t, set.add("/dev/unplugged", fd, func([]byte) {}))
	runSet(t, set)

	closeWrite()
	assert.Eventually(t, func() bool { return !set.has("/dev/unplugged") }, waitFor, 5*time.Millisecond)
}

func TestReadNonblocking(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "js0")
	require.NoError(t, os.WriteFile(path, []byte("12345678"), 0o600))

	data, err := readNonblocking(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("12345678"), data)

	_, err = readNonblocking(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}

func TestHotplug(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	events, stop := hotplug([]string{dir, filepath.Join(dir, "missing")}, isEventName)
	defer stop()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "other"), nil, 0o600))
	select {
	case <-events:
		t.Fatal("unrelated node triggered a resync")
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "event7"), nil, 0o600))
	select {
	case <-events:
	case <-time.After(waitFor):
		t.Fatal("new event node not noticed")
	}

	require.NoError(t, os.Remove(filepath.Join(dir, "event7")))
	select {
	case <-events:
	case <-time.After(waitFor):
		t.Fatal("removed event node not noticed")
	}
}

func TestHotplugWithNothingToWatch(t *testing.T) {
	t.Parallel()
	events, stop := hotplug([]string{filepath.Join(t.TempDir(), "missing")}, isEventName)
	stop()
	select {
	case <-events:
		t.Fatal("unexpected event")
	default:
	}
}

func TestCoverageFromProbes(t *testing.T) {
	t.Parallel()
	all := Keyboard | Mouse | Controller
	tests := []struct {
		name    string
		probes  []probeResult
		want    Class
		expect  Class
		canRead bool
	}{
		{name: "nothing readable", want: all},
		{name: "readable but no devices", want: all, canRead: true, expect: all},
		{name: "one pad opens", want: all, probes: []probeResult{{Controller, true}}, expect: all},
		{
			name: "denied keyboard is dropped", want: all, canRead: true, expect: Controller,
			probes: []probeResult{{Keyboard, false}, {Controller, true}},
		},
		{
			name: "one of two keyboards denied", want: all, canRead: true, expect: 0,
			probes: []probeResult{{Keyboard, true}, {Keyboard, false}},
		},
		{
			name: "all denied", want: all, canRead: true,
			probes: []probeResult{{Keyboard, false}, {Mouse, false}, {Controller, false}},
		},
		{
			name: "controllers only ignores others", want: Controller, expect: Controller,
			probes: []probeResult{{Controller, true}},
		},
		{name: "controllers only with no pad and no access", want: Controller},
		{
			name: "a refused device blocks claiming absent classes", want: all, canRead: true,
			probes: []probeResult{{Keyboard, false}}, expect: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expect, coverageFromProbes(tt.want, tt.probes, tt.canRead))
		})
	}
}

// counter counts callbacks and lets a test wait for the next one.
type counter struct {
	ch chan struct{}
	n  atomic.Int64
}

func newCounter() *counter { return &counter{ch: make(chan struct{}, 64)} }

func (c *counter) fire() {
	c.n.Add(1)
	select {
	case c.ch <- struct{}{}:
	default:
	}
}

func (c *counter) wait(t *testing.T) {
	t.Helper()
	select {
	case <-c.ch:
	case <-time.After(waitFor):
		t.Fatal("no activity reported")
	}
}

func TestPollSetAddReplacingRemovedEntryDoesNotLeak(t *testing.T) {
	t.Parallel()
	set, err := newPollSet()
	require.NoError(t, err)
	t.Cleanup(set.closeAll)

	var old, fresh [2]int
	require.NoError(t, unix.Pipe2(old[:], unix.O_NONBLOCK|unix.O_CLOEXEC))
	require.NoError(t, unix.Pipe2(fresh[:], unix.O_NONBLOCK|unix.O_CLOEXEC))
	defer func() { _ = unix.Close(old[1]); _ = unix.Close(fresh[1]) }()

	require.True(t, set.add("/dev/x", old[0], func([]byte) {}))
	set.remove("/dev/x")
	require.True(t, set.add("/dev/x", fresh[0], func([]byte) {}), "a removed entry can be replaced")

	// The loop has not run, so the old descriptor is parked, not leaked.
	runSet(t, set)
	assert.Eventually(t, func() bool {
		_, err := unix.Write(old[1], []byte{1})
		return errors.Is(err, unix.EPIPE)
	}, waitFor, 5*time.Millisecond, "old read end was closed")
	assert.True(t, set.has("/dev/x"))
}
