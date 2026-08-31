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

package updater

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// stallGuard cancels a transfer that has stopped making progress, without
// putting a ceiling on one that is merely slow. A download to an SD card over a
// congested link can legitimately take minutes, so a wall-clock timeout would
// make those devices unable to update at all; silence is the thing worth
// bounding. A blocked Read cannot time itself out, so a watcher goroutine
// cancels the request context instead.
type stallGuard struct {
	cancel context.CancelFunc
	done   chan struct{}
	// start anchors every measurement the guard makes. It carries a monotonic
	// reading, so progress is stored and compared as an elapsed duration since
	// this instant rather than as a wall-clock instant. That matters on the
	// devices this exists for: MiSTer and MiSTeX have no RTC and get stepped by
	// NTP shortly after the network comes up, which is the same window an update
	// check and download run in. Measured against the wall clock, a forward step
	// would abandon a perfectly healthy transfer and a backward step would
	// disable stall detection until the step was worked off.
	start    time.Time
	progress atomic.Int64
	fired    atomic.Bool
	timeout  time.Duration
}

// newStallGuard returns a context to make the request with and the guard
// watching it. The caller must stop the guard once the body is fully read,
// which also releases the context.
func newStallGuard(parent context.Context, timeout time.Duration) (context.Context, *stallGuard) {
	ctx, cancel := context.WithCancel(parent)
	g := &stallGuard{
		cancel:  cancel,
		done:    make(chan struct{}),
		start:   time.Now(),
		timeout: timeout,
	}
	g.touch()
	go g.watch(ctx)
	return ctx, g
}

// reader wraps a reader so arriving bytes count as progress.
func (g *stallGuard) reader(r io.Reader) io.Reader {
	return &progressReader{guard: g, source: r}
}

// tripped reports whether the guard is what cancelled the context, which is how
// a stall is told apart from the caller giving up.
func (g *stallGuard) tripped() bool {
	return g.fired.Load()
}

// stop ends the watcher and releases the context. Safe to call more than once.
func (g *stallGuard) stop() {
	g.cancel()
	<-g.done
}

// touch records that progress happened now, as nanoseconds since the guard
// started rather than as a clock reading.
func (g *stallGuard) touch() {
	g.progress.Store(int64(g.since()))
}

// since is how long the guard has been running, from its monotonic anchor.
func (g *stallGuard) since() time.Duration {
	return time.Since(g.start)
}

func (g *stallGuard) watch(ctx context.Context) {
	defer close(g.done)

	// Checking several times per timeout keeps the worst-case detection delay to
	// a fraction of it rather than nearly double. The floor is there because
	// time.NewTicker panics on a non-positive interval, and nothing in an update
	// is worth taking the service down over.
	interval := g.timeout / stallChecks
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if idle := g.since() - time.Duration(g.progress.Load()); idle >= g.timeout {
				g.fired.Store(true)
				g.cancel()
				return
			}
		}
	}
}

// progressReader reports every read that produced bytes to its guard.
type progressReader struct {
	guard  *stallGuard
	source io.Reader
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if n > 0 {
		r.guard.touch()
	}
	//nolint:wrapcheck // a reader wrapper has to pass io.EOF and the source's errors through unchanged
	return n, err
}
