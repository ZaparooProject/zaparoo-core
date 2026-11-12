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
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func samFixture(t *testing.T) (*SAM, *fakeNodes, *clockwork.FakeClock) {
	t.Helper()
	text := procText(
		keyboardEntry("Real Keyboard", "usb-1-2/input0", "sysrq kbd event3"),
		padEntry("Pad", "usb-1-3/input0", "event4 js0"),
		keyboardEntry("Pad Keyboard", "usb-1-3/input1", "sysrq kbd event6"),
		// Main's own uinput keyboard has no hidraw twin and must not stop the
		// keyboard class counting as covered.
		procEntry{
			name: "MiSTer virtual input", sysfs: "/devices/virtual/input/input70", handlers: "sysrq kbd event0",
			ev: []int{evKey}, key: letterKeys,
		},
		procEntry{
			name: "Zaparoo Pad", sysfs: "/devices/virtual/input/input9", handlers: "event9 js1",
			ev: []int{evKey, evAbs}, key: padButtons, abs: []int{0, 1},
		},
	)
	fs := memProc(t, text)
	uevent := func(node, phys string) {
		path := "/sys/class/hidraw/" + node + "/device/uevent"
		require.NoError(t, afero.WriteFile(fs, path, []byte("HID_PHYS="+phys+"\n"), 0o644))
	}
	uevent("hidraw0", "usb-1-2/input0")
	uevent("hidraw1", "usb-1-3/input1")

	nodes := newFakeNodes()
	nodes.add(t, "/dev/hidraw0")
	nodes.add(t, "/dev/hidraw1")
	nodes.add(t, "/dev/input/mice")

	clock := clockwork.NewFakeClock()
	s := NewSAM()
	s.fs = fs
	s.clock = clock
	s.open = nodes.open(t)
	s.readJS = func(string) ([]byte, error) { return nil, nil }
	return s, nodes, clock
}

func TestSAMCoverage(t *testing.T) {
	t.Parallel()

	t.Run("everything present", func(t *testing.T) {
		t.Parallel()
		s, _, _ := samFixture(t)
		assert.Equal(t, Keyboard|Mouse|Controller, s.Coverage())
	})

	t.Run("keyboard without a hidraw twin is not covered", func(t *testing.T) {
		t.Parallel()
		s, _, _ := samFixture(t)
		require.NoError(t, s.fs.Remove("/sys/class/hidraw/hidraw0/device/uevent"))
		assert.Equal(t, Mouse|Controller, s.Coverage())
	})

	t.Run("hidraw that will not open is not covered", func(t *testing.T) {
		t.Parallel()
		s, nodes, _ := samFixture(t)
		nodes.mu.Lock()
		nodes.denied["/dev/hidraw0"] = true
		nodes.mu.Unlock()
		assert.Equal(t, Mouse|Controller, s.Coverage())
	})

	t.Run("no mice node", func(t *testing.T) {
		t.Parallel()
		s, nodes, _ := samFixture(t)
		nodes.mu.Lock()
		delete(nodes.writers, "/dev/input/mice")
		nodes.mu.Unlock()
		assert.Equal(t, Keyboard|Controller, s.Coverage())
	})

	t.Run("no hidraw support at all", func(t *testing.T) {
		t.Parallel()
		s, _, _ := samFixture(t)
		require.NoError(t, s.fs.RemoveAll("/sys/class/hidraw"))
		assert.Equal(t, Mouse|Controller, s.Coverage())
	})

	t.Run("joydev loaded but no pad connected", func(t *testing.T) {
		t.Parallel()
		s, _, _ := samFixture(t)
		text := procText(keyboardEntry("Real Keyboard", "usb-1-2/input0", "sysrq kbd event3"))
		require.NoError(t, afero.WriteFile(s.fs, ProcDevicesPath, []byte(text), 0o644))
		assert.Equal(t, Keyboard|Mouse, s.Coverage())
		require.NoError(t, afero.WriteFile(s.fs, procHandlers, []byte("N: Number=1 Name=joydev Minor=0\n"), 0o644))
		assert.Equal(t, Keyboard|Mouse|Controller, s.Coverage())
	})

	t.Run("unreadable device list", func(t *testing.T) {
		t.Parallel()
		s, _, _ := samFixture(t)
		s.fs = afero.NewMemMapFs()
		assert.Equal(t, Class(0), s.Coverage())
	})
}

func TestSAMWatch(t *testing.T) {
	t.Parallel()
	s, nodes, clock := samFixture(t)

	var js struct {
		data  []byte
		reads int
		mu    syncutil.Mutex
	}
	s.readJS = func(path string) ([]byte, error) {
		if path != "/dev/input/js0" {
			t.Errorf("unexpected joystick node %s", path)
		}
		js.mu.Lock()
		defer js.mu.Unlock()
		js.reads++
		return js.data, nil
	}
	setJS := func(data []byte) {
		js.mu.Lock()
		js.data = data
		js.mu.Unlock()
	}
	button := func(v int16) []byte {
		b := make([]byte, jsEventSize)
		b[4] = byte(v) //nolint:gosec // test values are small
		b[6], b[7] = jsButton|jsInit, 0
		return b
	}
	setJS(button(0))

	c := newCounter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Watch(ctx, true, c.fire) }()
	t.Cleanup(cancel)

	clock.Advance(time.Second)
	nodes.send(t, "/dev/hidraw0", []byte{0, 0, 4, 0, 0, 0, 0, 0})
	c.wait(t)

	clock.Advance(time.Second)
	nodes.send(t, "/dev/input/mice", []byte{8, 1, 0})
	c.wait(t)

	// A press shows up on the next poll as a change against a baseline that an
	// earlier poll already took.
	require.Eventually(t, func() bool {
		js.mu.Lock()
		defer js.mu.Unlock()
		return js.reads >= 2
	}, waitFor, time.Millisecond)
	clock.Advance(time.Second)
	setJS(button(1))
	c.wait(t)

	assert.Zero(t, nodes.openedCount("/dev/hidraw1"), "a pad's own keyboard interface must not be read")
	stopWatch(t, cancel, done)
}

func TestSAMWatchStaysQuietWhileJoystickIsHeld(t *testing.T) {
	t.Parallel()
	s, _, clock := samFixture(t)
	reads := make(chan struct{}, 64)
	s.readJS = func(string) ([]byte, error) {
		select {
		case reads <- struct{}{}:
		default:
		}
		b := make([]byte, jsEventSize)
		b[4], b[5] = 0x20, 0x00 // value 32, inside the deadzone
		b[6], b[7] = jsAxis|jsInit, 0
		return b, nil
	}
	c := newCounter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Watch(ctx, true, c.fire) }()
	t.Cleanup(cancel)

	for range 5 {
		clock.Advance(time.Second)
		select {
		case <-reads:
		case <-time.After(waitFor):
			t.Fatal("joystick never polled")
		}
	}
	assert.Zero(t, c.n.Load(), "a held, unchanged stick is not activity")
	stopWatch(t, cancel, done)
}

func TestSAMSampleInterval(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 100*time.Millisecond, samSampleInterval(false))
	assert.Equal(t, ResponsivePollInterval, samSampleInterval(true))
	assert.Less(t, samSampleInterval(false), 120*time.Millisecond, "must catch taps of about 120ms")
}
