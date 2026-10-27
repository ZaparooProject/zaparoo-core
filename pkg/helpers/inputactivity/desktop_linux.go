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
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/godbus/dbus/v5"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	dbusCallTimeout = 2 * time.Second
	probeTTL        = 30 * time.Second

	screenSaverName  = "org.freedesktop.ScreenSaver"
	mutterIdleName   = "org.gnome.Mutter.IdleMonitor"
	mutterIdlePath   = "/org/gnome/Mutter/IdleMonitor/Core"
	logindName       = "org.freedesktop.login1"
	logindSessionObj = "/org/freedesktop/login1/session/auto"
	logindSessionIf  = "org.freedesktop.login1.Session"
)

var (
	errNoIdleSource = errors.New("no session idle source on the bus")
	errBadReply     = errors.New("unexpected D-Bus reply")
)

// bus is the small slice of D-Bus the idle probes need.
type bus interface {
	hasOwner(ctx context.Context, name string) bool
	call(ctx context.Context, name string, path dbus.ObjectPath, method string) (any, error)
	property(ctx context.Context, name string, path dbus.ObjectPath, prop string) (any, error)
}

type dbusConn struct {
	conn *dbus.Conn
}

func (b dbusConn) hasOwner(ctx context.Context, name string) bool {
	var has bool
	err := b.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, name).Store(&has)
	return err == nil && has
}

func (b dbusConn) call(ctx context.Context, name string, path dbus.ObjectPath, method string) (any, error) {
	var out any
	// NoAutoStart: asking must never launch a service on Core's behalf.
	err := b.conn.Object(name, path).CallWithContext(ctx, method, dbus.FlagNoAutoStart).Store(&out)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}
	return out, nil
}

func (b dbusConn) property(ctx context.Context, name string, path dbus.ObjectPath, prop string) (any, error) {
	var v dbus.Variant
	err := b.conn.Object(name, path).CallWithContext(
		ctx, "org.freedesktop.DBus.Properties.Get", dbus.FlagNoAutoStart,
		logindSessionIf, prop,
	).Store(&v)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", prop, err)
	}
	return v.Value(), nil
}

// idleProbe reads how long the session has been idle. Reads that cannot know
// report zero, which counts as activity, so a source that stops working keeps
// rotation from firing instead of letting it fire under someone.
type idleProbe struct {
	read   func(ctx context.Context) (time.Duration, error)
	source string
}

func asMillis(v any) (time.Duration, error) {
	switch n := v.(type) {
	case uint32:
		return time.Duration(n) * time.Millisecond, nil
	case uint64:
		return time.Duration(n) * time.Millisecond, nil //nolint:gosec // idle time fits easily
	case int64:
		return time.Duration(n) * time.Millisecond, nil
	}
	return 0, errBadReply
}

func screenSaverProbe(ctx context.Context, b bus) (idleProbe, bool) {
	if !b.hasOwner(ctx, screenSaverName) {
		return idleProbe{}, false
	}
	for _, path := range []dbus.ObjectPath{"/org/freedesktop/ScreenSaver", "/ScreenSaver"} {
		read := func(ctx context.Context) (time.Duration, error) {
			v, err := b.call(ctx, screenSaverName, path, screenSaverName+".GetSessionIdleTime")
			if err != nil {
				return 0, err
			}
			return asMillis(v)
		}
		if _, err := read(ctx); err == nil {
			return idleProbe{source: "screensaver", read: read}, true
		}
	}
	return idleProbe{}, false
}

func mutterProbe(ctx context.Context, b bus) (idleProbe, bool) {
	if !b.hasOwner(ctx, mutterIdleName) {
		return idleProbe{}, false
	}
	read := func(ctx context.Context) (time.Duration, error) {
		v, err := b.call(ctx, mutterIdleName, mutterIdlePath, mutterIdleName+".GetIdletime")
		if err != nil {
			return 0, err
		}
		return asMillis(v)
	}
	if _, err := read(ctx); err != nil {
		return idleProbe{}, false
	}
	return idleProbe{source: "mutter", read: read}, true
}

// logindProbe uses the session's IdleHint. It is coarse, since the desktop
// only raises the hint after its own idle delay, but it errs safe: while the
// hint is clear the session reads as active.
//
// It only counts as a source once the desktop has set the hint at some point.
// KDE and SteamOS's game mode never do, and there a hint that is always clear
// read as input on every poll: it cancelled every playlist countdown and kept
// idle rotation from ever firing, while claiming keyboard and mouse coverage.
func logindProbe(ctx context.Context, b bus, clock clockwork.Clock) (idleProbe, bool) {
	if !b.hasOwner(ctx, logindName) {
		return idleProbe{}, false
	}
	since, err := b.property(ctx, logindName, logindSessionObj, "IdleSinceHint")
	if err != nil {
		return idleProbe{}, false
	}
	if usec, ok := since.(uint64); !ok || usec == 0 {
		log.Debug().Msg("input activity: the desktop never sets logind's idle hint; not using it")
		return idleProbe{}, false
	}
	read := func(ctx context.Context) (time.Duration, error) {
		hint, err := b.property(ctx, logindName, logindSessionObj, "IdleHint")
		if err != nil {
			return 0, err
		}
		idle, ok := hint.(bool)
		if !ok {
			return 0, errBadReply
		}
		if !idle {
			return 0, nil
		}
		since, err := b.property(ctx, logindName, logindSessionObj, "IdleSinceHint")
		if err != nil {
			return 0, err
		}
		usec, ok := since.(uint64)
		if !ok || usec == 0 {
			return 0, errBadReply
		}
		return clock.Since(time.UnixMicro(int64(usec))), nil //nolint:gosec // microsecond timestamp
	}
	if _, err := read(ctx); err != nil {
		return idleProbe{}, false
	}
	return idleProbe{source: "logind", read: read}, true
}

// probeIdle finds the first working idle source: the desktop's own idle
// timer on the session bus, then logind on the system bus.
func probeIdle(
	ctx context.Context,
	session, system func() (bus, error),
	clock clockwork.Clock,
) (idleProbe, error) {
	if b, err := session(); err == nil {
		if p, ok := screenSaverProbe(ctx, b); ok {
			return p, nil
		}
		if p, ok := mutterProbe(ctx, b); ok {
			return p, nil
		}
	}
	if b, err := system(); err == nil {
		if p, ok := logindProbe(ctx, b, clock); ok {
			return p, nil
		}
	}
	return idleProbe{}, errNoIdleSource
}

// Desktop watches a desktop session: controllers through event nodes and
// keyboard plus mouse through the session's idle time on D-Bus.
type Desktop struct {
	clock    clockwork.Clock
	session  func() (bus, error)
	system   func() (bus, error)
	cachedAt time.Time
	cached   *idleProbe
	evdev    *Evdev
	// watchControllers watches the controller event nodes; tests replace it.
	watchControllers func(ctx context.Context, responsive bool, onActivity func()) error
	mu               syncutil.Mutex
}

// NewDesktop returns a source for a non-root desktop session.
func NewDesktop() *Desktop {
	return &Desktop{
		clock: clockwork.NewRealClock(),
		session: func() (bus, error) {
			conn, err := dbus.SessionBus()
			if err != nil {
				return nil, fmt.Errorf("session bus: %w", err)
			}
			return dbusConn{conn: conn}, nil
		},
		system: func() (bus, error) {
			conn, err := dbus.SystemBus()
			if err != nil {
				return nil, fmt.Errorf("system bus: %w", err)
			}
			return dbusConn{conn: conn}, nil
		},
		evdev: NewEvdev(Controller),
	}
}

// idle returns the working idle probe, probing again only after probeTTL.
func (d *Desktop) idle(ctx context.Context) (idleProbe, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.cachedAt.IsZero() && d.clock.Since(d.cachedAt) < probeTTL {
		if d.cached == nil {
			return idleProbe{}, false
		}
		return *d.cached, true
	}
	probeCtx, cancel := context.WithTimeout(ctx, dbusCallTimeout*3)
	defer cancel()
	probe, err := probeIdle(probeCtx, d.session, d.system, d.clock)
	d.cachedAt = d.clock.Now()
	if err != nil {
		d.cached = nil
		return idleProbe{}, false
	}
	d.cached = &probe
	return probe, true
}

// Coverage reports keyboard and mouse only when a session idle source works,
// and controllers only when event nodes can be read.
func (d *Desktop) Coverage() Class {
	out := d.evdev.Coverage()
	if _, ok := d.idle(context.Background()); ok {
		out |= Keyboard | Mouse
	}
	return out
}

// Watch blocks until ctx is cancelled. It fails at once when neither source
// works, and returns the error when the controller watch stops on its own:
// coverage still claims controllers, so carrying on without them would leave
// a person using one looking idle.
func (d *Desktop) Watch(ctx context.Context, responsive bool, onActivity func()) error {
	probe, haveIdle := d.idle(ctx)
	haveControllers := d.evdev.Coverage() != 0
	if !haveIdle && !haveControllers {
		return errNoIdleSource
	}

	throttle := NewThrottle(d.clock, NotifyInterval, onActivity)
	var wg sync.WaitGroup
	defer wg.Wait()
	idleCtx, stopIdle := context.WithCancel(ctx)
	defer stopIdle()
	if haveIdle {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.pollIdle(idleCtx, probe, pollEvery(responsive), throttle.Trigger)
		}()
	}
	if haveControllers {
		watch := d.watchControllers
		if watch == nil {
			watch = d.evdev.Watch
		}
		if err := watch(ctx, responsive, throttle.Trigger); err != nil && ctx.Err() == nil {
			stopIdle()
			return fmt.Errorf("controller watch stopped: %w", err)
		}
	}
	<-ctx.Done()
	return nil
}

func (d *Desktop) pollIdle(ctx context.Context, probe idleProbe, every time.Duration, notify func()) {
	tracker := NewIdleTracker(d.clock)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		callCtx, cancel := context.WithTimeout(ctx, dbusCallTimeout)
		idle, err := probe.read(callCtx)
		cancel()
		if err != nil {
			log.Debug().Err(err).Str("source", probe.source).Msg("input activity: idle read failed")
			// Not knowing counts as activity so rotation never fires blind.
			notify()
			continue
		}
		if tracker.Observe(idle) || idle < every {
			notify()
		}
	}
}
