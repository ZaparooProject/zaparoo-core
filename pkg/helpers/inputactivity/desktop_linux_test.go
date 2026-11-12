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
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/jonboulle/clockwork"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFakeBus = errors.New("fake bus failure")

type fakeBus struct {
	owners map[string]bool
	calls  map[string]any
	props  map[string]any
	nCalls atomic.Int64
}

func (b *fakeBus) hasOwner(_ context.Context, name string) bool { return b.owners[name] }

func (b *fakeBus) call(_ context.Context, name string, path dbus.ObjectPath, method string) (any, error) {
	b.nCalls.Add(1)
	v, ok := b.calls[name+string(path)+"|"+method]
	if !ok {
		return nil, errFakeBus
	}
	if err, isErr := v.(error); isErr {
		return nil, err
	}
	return v, nil
}

func (b *fakeBus) property(_ context.Context, name string, _ dbus.ObjectPath, prop string) (any, error) {
	v, ok := b.props[name+"|"+prop]
	if !ok {
		return nil, errFakeBus
	}
	return v, nil
}

func busFn(b *fakeBus) func() (bus, error) {
	return func() (bus, error) { return b, nil }
}

func noBus() (bus, error) { return nil, errFakeBus }

func TestProbeIdle(t *testing.T) {
	t.Parallel()
	ssPath := screenSaverName + "/org/freedesktop/ScreenSaver|" + screenSaverName + ".GetSessionIdleTime"
	ssAlt := screenSaverName + "/ScreenSaver|" + screenSaverName + ".GetSessionIdleTime"
	mutter := mutterIdleName + mutterIdlePath + "|" + mutterIdleName + ".GetIdletime"

	tests := []struct {
		session *fakeBus
		system  *fakeBus
		name    string
		source  string
		wantErr bool
	}{
		{
			name: "screensaver",
			session: &fakeBus{
				owners: map[string]bool{screenSaverName: true},
				calls:  map[string]any{ssPath: uint32(1500)},
			},
			source: "screensaver",
		},
		{
			name: "screensaver on the short path",
			session: &fakeBus{
				owners: map[string]bool{screenSaverName: true},
				calls:  map[string]any{ssAlt: uint32(1500)},
			},
			source: "screensaver",
		},
		{
			name: "screensaver present but unusable falls through to mutter",
			session: &fakeBus{
				owners: map[string]bool{screenSaverName: true, mutterIdleName: true},
				calls:  map[string]any{mutter: uint64(900)},
			},
			source: "mutter",
		},
		{
			name:    "logind only",
			session: &fakeBus{},
			system: &fakeBus{
				owners: map[string]bool{logindName: true},
				props: map[string]any{
					logindName + "|IdleHint":      false,
					logindName + "|IdleSinceHint": uint64(1_700_000_000_000_000),
				},
			},
			source: "logind",
		},
		{
			// KDE Plasma and SteamOS game mode, 2026-10-26: IdleSinceHint was
			// still 0 after three days, and the clear hint read as input on
			// every poll.
			name:    "logind whose idle hint the desktop never sets",
			session: &fakeBus{},
			system: &fakeBus{
				owners: map[string]bool{logindName: true},
				props: map[string]any{
					logindName + "|IdleHint":      false,
					logindName + "|IdleSinceHint": uint64(0),
				},
			},
			wantErr: true,
		},
		{
			name:    "logind name without the property",
			session: &fakeBus{},
			system:  &fakeBus{owners: map[string]bool{logindName: true}},
			wantErr: true,
		},
		{name: "nothing on either bus", session: &fakeBus{}, system: &fakeBus{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			system := noBus
			if tt.system != nil {
				system = busFn(tt.system)
			}
			p, err := probeIdle(context.Background(), busFn(tt.session), system, clockwork.NewFakeClock())
			if tt.wantErr {
				require.ErrorIs(t, err, errNoIdleSource)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.source, p.source)
		})
	}
}

func TestProbeIdleWithNoBuses(t *testing.T) {
	t.Parallel()
	_, err := probeIdle(context.Background(), noBus, noBus, clockwork.NewFakeClock())
	require.ErrorIs(t, err, errNoIdleSource)
}

func TestLogindReadErrsSafe(t *testing.T) {
	t.Parallel()
	clock := clockwork.NewFakeClock()
	b := &fakeBus{
		owners: map[string]bool{logindName: true},
		props: map[string]any{
			logindName + "|IdleHint":      false,
			logindName + "|IdleSinceHint": uint64(clock.Now().Add(-time.Hour).UnixMicro()), //nolint:gosec // test time
		},
	}
	p, ok := logindProbe(context.Background(), b, clock)
	require.True(t, ok)

	idle, err := p.read(context.Background())
	require.NoError(t, err)
	assert.Zero(t, idle, "an active session reads as no idle time")

	b.props[logindName+"|IdleHint"] = true
	b.props[logindName+"|IdleSinceHint"] = uint64(clock.Now().Add(-time.Minute).UnixMicro()) //nolint:gosec // test time
	idle, err = p.read(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, time.Minute.Seconds(), idle.Seconds(), 0.01)

	b.props[logindName+"|IdleSinceHint"] = "garbage"
	_, err = p.read(context.Background())
	assert.ErrorIs(t, err, errBadReply)
}

func desktopFixture(t *testing.T, session, system *fakeBus) *Desktop {
	t.Helper()
	d := NewDesktop()
	d.session = busFn(session)
	d.system = busFn(system)
	d.evdev = NewEvdev(Controller)
	d.evdev.fs = afero.NewMemMapFs()
	d.evdev.readable = func(string) bool { return false }
	return d
}

func TestDesktopCoverage(t *testing.T) {
	t.Parallel()
	ssPath := screenSaverName + "/org/freedesktop/ScreenSaver|" + screenSaverName + ".GetSessionIdleTime"
	ss := &fakeBus{
		owners: map[string]bool{screenSaverName: true},
		calls:  map[string]any{ssPath: uint32(0)},
	}

	t.Run("idle source and readable controllers", func(t *testing.T) {
		t.Parallel()
		d := desktopFixture(t, ss, &fakeBus{})
		d.evdev.readable = func(string) bool { return true }
		require.NoError(t, afero.WriteFile(d.evdev.fs, "/dev/input/event0", nil, 0o644))
		assert.Equal(t, Keyboard|Mouse|Controller, d.Coverage())
	})

	t.Run("idle source only", func(t *testing.T) {
		t.Parallel()
		d := desktopFixture(t, ss, &fakeBus{})
		assert.Equal(t, Keyboard|Mouse, d.Coverage())
	})

	t.Run("no idle source", func(t *testing.T) {
		t.Parallel()
		d := desktopFixture(t, &fakeBus{}, &fakeBus{})
		assert.Equal(t, Class(0), d.Coverage())
		err := d.Watch(context.Background(), false, func() {})
		require.ErrorIs(t, err, errNoIdleSource)
	})

	t.Run("probe result is cached", func(t *testing.T) {
		t.Parallel()
		own := &fakeBus{owners: ss.owners, calls: ss.calls}
		d := desktopFixture(t, own, &fakeBus{})
		clock := clockwork.NewFakeClock()
		d.clock = clock
		d.Coverage()
		before := own.nCalls.Load()
		d.Coverage()
		d.Coverage()
		assert.Equal(t, before, own.nCalls.Load(), "no new D-Bus calls inside the probe window")
		clock.Advance(probeTTL + time.Second)
		d.Coverage()
		assert.Greater(t, own.nCalls.Load(), before, "probe repeats after it expires")
	})
}

func TestDesktopWatchReportsInput(t *testing.T) {
	t.Parallel()
	var lastInput atomic.Int64
	start := time.Now()
	lastInput.Store(start.Add(-time.Hour).UnixNano())

	path := screenSaverName + "/org/freedesktop/ScreenSaver|" + screenSaverName + ".GetSessionIdleTime"
	ss := &fakeBus{owners: map[string]bool{screenSaverName: true}, calls: map[string]any{}}
	d := desktopFixture(t, ss, &fakeBus{})
	// Serve idle time computed from the last "input" instant, like a real timer.
	d.session = func() (bus, error) { return idleBus{fakeBus: ss, path: path, last: &lastInput}, nil }

	c := newCounter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Watch(ctx, true, c.fire) }()
	t.Cleanup(cancel)

	// Let a few idle readings pass: no activity.
	time.Sleep(150 * time.Millisecond)
	assert.Zero(t, c.n.Load())

	lastInput.Store(time.Now().UnixNano())
	c.wait(t)
	stopWatch(t, cancel, done)
}

// idleBus answers the idle call with time since a settable instant.
type idleBus struct {
	*fakeBus
	last *atomic.Int64
	path string
}

func (b idleBus) call(_ context.Context, _ string, _ dbus.ObjectPath, _ string) (any, error) {
	idle := time.Since(time.Unix(0, b.last.Load()))
	return uint32(idle.Milliseconds()), nil //nolint:gosec // test value fits
}

func TestDesktopWatchTreatsReadFailureAsActivity(t *testing.T) {
	t.Parallel()
	ss := &fakeBus{owners: map[string]bool{screenSaverName: true}}
	d := desktopFixture(t, ss, &fakeBus{})
	failing := &flakyBus{fakeBus: ss}
	d.session = func() (bus, error) { return failing, nil }
	require.Equal(t, Keyboard|Mouse, d.Coverage(), "probe succeeds and is cached before reads start failing")

	c := newCounter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Watch(ctx, true, c.fire) }()
	t.Cleanup(cancel)

	failing.fail.Store(true)
	c.wait(t)
	stopWatch(t, cancel, done)
}

type flakyBus struct {
	*fakeBus
	fail atomic.Bool
}

func (b *flakyBus) call(context.Context, string, dbus.ObjectPath, string) (any, error) {
	if b.fail.Load() {
		return nil, errFakeBus
	}
	return uint32(60000), nil
}

// Coverage claims controllers, so a controller watch that stops on its own
// must end the whole watch rather than leave a controller player unseen.
func TestDesktopWatchReturnsWhenTheControllerWatchFails(t *testing.T) {
	t.Parallel()
	ssPath := screenSaverName + "/org/freedesktop/ScreenSaver|" + screenSaverName + ".GetSessionIdleTime"
	ss := &fakeBus{
		owners: map[string]bool{screenSaverName: true},
		calls:  map[string]any{ssPath: uint32(60000)},
	}
	d := desktopFixture(t, ss, &fakeBus{})
	d.evdev.readable = func(string) bool { return true }
	require.NoError(t, afero.WriteFile(d.evdev.fs, "/dev/input/event0", nil, 0o644))
	failure := errors.New("poll set unavailable")
	d.watchControllers = func(context.Context, bool, func()) error { return failure }

	done := make(chan error, 1)
	go func() { done <- d.Watch(t.Context(), false, func() {}) }()

	select {
	case err := <-done:
		require.ErrorIs(t, err, failure)
	case <-time.After(5 * time.Second):
		t.Fatal("Watch kept running after its controller watch failed")
	}
}
