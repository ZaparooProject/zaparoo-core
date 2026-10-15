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
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var errDenied = errors.New("permission denied")

// fakeNodes hands out pipe read ends in place of device nodes.
type fakeNodes struct {
	writers map[string]func([]byte)
	denied  map[string]bool
	opened  map[string]int
	mu      syncutil.Mutex
}

func newFakeNodes() *fakeNodes {
	return &fakeNodes{
		writers: map[string]func([]byte){},
		denied:  map[string]bool{},
		opened:  map[string]int{},
	}
}

// add makes path openable and returns a function that feeds it data.
func (f *fakeNodes) add(t *testing.T, path string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writers[path] = nil
}

func (f *fakeNodes) open(t *testing.T) func(path string) (int, error) {
	return func(path string) (int, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.denied[path] {
			return -1, errDenied
		}
		if _, ok := f.writers[path]; !ok {
			return -1, os.ErrNotExist
		}
		var fds [2]int
		if err := unix.Pipe2(fds[:], unix.O_NONBLOCK|unix.O_CLOEXEC); err != nil {
			return -1, fmt.Errorf("pipe: %w", err)
		}
		f.opened[path]++
		f.writers[path] = func(p []byte) { _, _ = unix.Write(fds[1], p) }
		t.Cleanup(func() { _ = unix.Close(fds[1]) })
		return fds[0], nil
	}
}

func (f *fakeNodes) send(t *testing.T, path string, data []byte) {
	t.Helper()
	require.Eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.writers[path] != nil
	}, waitFor, time.Millisecond, "device %s never opened", path)
	f.mu.Lock()
	w := f.writers[path]
	f.mu.Unlock()
	w(data)
}

func (f *fakeNodes) openedCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opened[path]
}

func memProc(t *testing.T, text string) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, ProcDevicesPath, []byte(text), 0o644))
	return fs
}

func stopWatch(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("watch did not return promptly after cancel")
	}
}

func evdevFixture(t *testing.T) (*Evdev, *fakeNodes, *clockwork.FakeClock) {
	t.Helper()
	text := procText(
		keyboardEntry("Keyboard", "usb-1-2/input0", "sysrq kbd event0"),
		padEntry("Pad", "usb-1-3/input0", "event1 js0"),
		mouseEntry("Mouse", "usb-1-4/input0", "mouse0 event2"),
		procEntry{
			name: "Zaparoo Pad", sysfs: "/devices/virtual/input/input9", handlers: "event3 js1",
			ev: []int{evKey, evAbs}, key: padButtons, abs: []int{0, 1},
		},
	)
	nodes := newFakeNodes()
	for _, n := range []string{"event0", "event1", "event2", "event3"} {
		nodes.add(t, "/dev/input/"+n)
	}
	clock := clockwork.NewFakeClock()
	e := NewEvdevAll()
	e.fs = memProc(t, text)
	e.clock = clock
	e.devInput = "/dev/input"
	e.open = nodes.open(t)
	e.absInfo = func(int, uint16) (axisRange, bool) { return axisRange{min: -32768, max: 32767}, true }
	e.readable = func(string) bool { return true }
	return e, nodes, clock
}

func TestEvdevWatchReportsEachClass(t *testing.T) {
	t.Parallel()
	e, nodes, clock := evdevFixture(t)
	c := newCounter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Watch(ctx, false, c.fire) }()
	t.Cleanup(cancel)

	tests := []struct {
		name string
		path string
		data []byte
	}{
		{"key press", "/dev/input/event0", eventBytes(inputEvent{typ: evKey, code: 30, value: 1})},
		{"stick past deadzone", "/dev/input/event1", eventBytes(inputEvent{typ: evAbs, code: 0, value: 20000})},
		{"mouse motion", "/dev/input/event2", eventBytes(inputEvent{typ: evRel, code: 0, value: 4})},
	}
	for _, tt := range tests {
		clock.Advance(time.Second)
		nodes.send(t, tt.path, tt.data)
		c.wait(t)
	}

	assert.Zero(t, nodes.openedCount("/dev/input/event3"), "Zaparoo's own device must never be opened")
	stopWatch(t, cancel, done)
}

func TestEvdevWatchIgnoresQuietInput(t *testing.T) {
	t.Parallel()
	e, nodes, clock := evdevFixture(t)
	c := newCounter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Watch(ctx, false, c.fire) }()
	t.Cleanup(cancel)

	// Same device, in order: a stick wobble and a key release, then a press.
	clock.Advance(time.Second)
	nodes.send(t, "/dev/input/event1", eventBytes(
		inputEvent{typ: evAbs, code: 0, value: 100},
		inputEvent{typ: evKey, code: 0x130, value: 0},
	))
	nodes.send(t, "/dev/input/event1", eventBytes(inputEvent{typ: evKey, code: 0x130, value: 1}))
	c.wait(t)
	assert.EqualValues(t, 1, c.n.Load(), "only the press counts")
	stopWatch(t, cancel, done)
}

func TestEvdevHandlerSuppressesChatter(t *testing.T) {
	t.Parallel()
	e, _, clock := evdevFixture(t)
	calls := 0
	throttle := NewThrottle(clock, 0, func() { calls++ })
	handle := e.handler(-1, &candidate{class: Keyboard}, throttle)
	key := eventBytes(inputEvent{typ: evKey, code: 30, value: 1})

	// A key reported every 10ms for ten seconds with no pause.
	for range 1000 {
		clock.Advance(10 * time.Millisecond)
		handle(key)
	}
	counted := calls
	assert.Less(t, counted, 600, "events after the first five seconds do not count")

	clock.Advance(10 * time.Millisecond)
	handle(key)
	assert.Equal(t, counted, calls, "still chattering")

	clock.Advance(2 * time.Second)
	handle(key)
	assert.Equal(t, counted+1, calls, "a pause clears the filter")
}

func TestEvdevCoverage(t *testing.T) {
	t.Parallel()
	e, nodes, _ := evdevFixture(t)
	assert.Equal(t, Keyboard|Mouse|Controller, e.Coverage())

	nodes.mu.Lock()
	nodes.denied["/dev/input/event0"] = true
	nodes.mu.Unlock()
	assert.Equal(t, Mouse|Controller, e.Coverage(), "a keyboard that will not open removes the keyboard class")

	e.classes = Controller
	assert.Equal(t, Controller, e.Coverage())
}

func TestEvdevCoverageNoAccess(t *testing.T) {
	t.Parallel()
	e, _, _ := evdevFixture(t)
	e.fs = memProc(t, "")
	e.readable = func(string) bool { return false }
	assert.Equal(t, Class(0), e.Coverage())
}

func TestEvdevHotplugAddsDevice(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	devInput := filepath.Join(dir, "input")
	require.NoError(t, os.MkdirAll(devInput, 0o750))
	procPath := filepath.Join(dir, "devices")
	write := func(text string) {
		require.NoError(t, os.WriteFile(procPath, []byte(text), 0o600))
	}
	write("")

	nodes := newFakeNodes()
	nodes.add(t, filepath.Join(devInput, "event5"))
	clock := clockwork.NewFakeClock()
	e := NewEvdev(Keyboard)
	e.fs = afero.NewOsFs()
	e.procDevices = procPath
	e.devInput = devInput
	e.clock = clock
	e.open = nodes.open(t)

	c := newCounter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Watch(ctx, false, c.fire) }()
	t.Cleanup(cancel)

	// Give the first resync time to finish, then plug a keyboard in.
	time.Sleep(100 * time.Millisecond)
	write(procText(keyboardEntry("New Keyboard", "usb-1-2/input0", "sysrq kbd event5")))
	require.NoError(t, os.WriteFile(filepath.Join(devInput, "event5"), nil, 0o600))

	clock.Advance(time.Second)
	nodes.send(t, filepath.Join(devInput, "event5"), eventBytes(inputEvent{typ: evKey, code: 30, value: 1}))
	c.wait(t)
	stopWatch(t, cancel, done)
}

func TestEvdevSyncDropsUnplugged(t *testing.T) {
	t.Parallel()
	e, _, clock := evdevFixture(t)
	set := mustPollSet(t)
	throttle := NewThrottle(clock, NotifyInterval, func() {})

	e.sync(set, throttle)
	assert.ElementsMatch(t,
		[]string{"/dev/input/event0", "/dev/input/event1", "/dev/input/event2"}, set.paths())

	text := procText(padEntry("Pad", "usb-1-3/input0", "event1 js0"))
	require.NoError(t, afero.WriteFile(e.fs, ProcDevicesPath, []byte(text), 0o644))
	e.sync(set, throttle)
	assert.Equal(t, []string{"/dev/input/event1"}, set.paths())
}

func mustPollSet(t *testing.T) *pollSet {
	t.Helper()
	set, err := newPollSet()
	require.NoError(t, err)
	t.Cleanup(set.closeAll)
	return set
}

func TestEvdevCandidatesIncludeBluetooth(t *testing.T) {
	t.Parallel()
	e := NewEvdevAll()
	e.fs = memProc(t, procText(bluetoothEntries()...))
	e.devInput = "/dev/input"
	cands := e.candidates()
	require.Len(t, cands, 3)
	assert.Equal(t, "/dev/input/event20", cands[0].path)
	assert.Equal(t, Controller, cands[2].class)
}
