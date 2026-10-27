//go:build linux && !android

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

package gamehandoff

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// Options say what counts as the game being handed over.
type Options struct {
	OpenWindowStart <-chan struct{}
	Settle          time.Duration
	OpenWindow      time.Duration
	IdleHandover    time.Duration
	UntilClose      bool
}

// Wait returns once events show the game handed over and Settle has passed
// with no further activity on it. It returns nil without a handover when the
// game is never opened within OpenWindow, and ctx's error if the wait is
// cancelled first.
func Wait(ctx context.Context, clock clockwork.Clock, events <-chan Kind, opts Options) error {
	var open int
	var quiet clockwork.Timer
	stopQuiet := func() {
		if quiet != nil {
			quiet.Stop()
			quiet = nil
		}
	}
	defer stopQuiet()
	var noOpen clockwork.Timer
	var noOpenC <-chan time.Time
	stopNoOpen := func() {
		if noOpen != nil {
			noOpen.Stop()
		}
		noOpenC = nil
	}
	defer stopNoOpen()
	opened := false
	windowStart := opts.OpenWindowStart
	if windowStart == nil {
		noOpen = clock.NewTimer(opts.OpenWindow)
		noOpenC = noOpen.Chan()
	}
	var idle clockwork.Timer
	stopIdle := func() {
		if idle != nil {
			idle.Stop()
			idle = nil
		}
	}
	defer stopIdle()

	for {
		var quietC, idleC <-chan time.Time
		if quiet != nil {
			quietC = quiet.Chan()
		}
		if idle != nil {
			idleC = idle.Chan()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for game handover: %w", ctx.Err())
		case <-windowStart:
			windowStart = nil
			if !opened {
				noOpen = clock.NewTimer(opts.OpenWindow)
				noOpenC = noOpen.Chan()
			}
		case <-noOpenC:
			log.Debug().Msg("game was never opened; treating it as handed over")
			return nil
		case kind, ok := <-events:
			if !ok {
				return fmt.Errorf("%w: watch on the game ended", platforms.ErrReadinessUnavailable)
			}
			stopIdle()
			switch kind {
			case Open:
				open++
				opened = true
				windowStart = nil
				stopNoOpen()
				stopQuiet()
				if !opts.UntilClose {
					quiet = clock.NewTimer(opts.Settle)
				}
			case Close:
				open = max(open-1, 0)
				if opts.UntilClose && open == 0 && opened {
					stopQuiet()
					quiet = clock.NewTimer(opts.Settle)
				}
			case Access:
			}
			if opts.UntilClose && opts.IdleHandover > 0 && open > 0 {
				idle = clock.NewTimer(opts.IdleHandover)
			}
		case <-idleC:
			idle = nil
			if quiet == nil {
				quiet = clock.NewTimer(opts.Settle)
			}
		case <-quietC:
			return nil
		}
	}
}

// LaunchLog remembers the last game Core launched. A probe waits only for a
// game launched by Core: one already running when a tracker noticed it was
// opened before anything could watch, so waiting on it would stall until the
// open window ends.
type LaunchLog struct {
	clock clockwork.Clock
	at    time.Time
	path  string
	mu    syncutil.Mutex
}

// NewLaunchLog returns an empty log.
func NewLaunchLog(clock clockwork.Clock) *LaunchLog {
	return &LaunchLog{clock: clock}
}

// Note records that Core is launching path.
func (l *LaunchLog) Note(path string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.path, l.at = path, l.clock.Now()
}

// Recent reports whether path is the game Core launched within the last
// within.
func (l *LaunchLog) Recent(path string, within time.Duration) bool {
	_, ok := l.Since(path, within)
	return ok
}

// Since returns when path was launched, if it is the game Core launched within
// the last within.
func (l *LaunchLog) Since(path string, within time.Duration) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.path == "" || !samePath(l.path, path) || l.clock.Since(l.at) >= within {
		return time.Time{}, false
	}
	return l.at, true
}

// samePath reports whether two spellings name the launched game. Only one
// launch is remembered and only for a short while, so a matching file name is
// enough when the directories were spelled differently.
func samePath(launched, published string) bool {
	launched, published = filepath.Clean(launched), filepath.Clean(published)
	return launched == published || filepath.Base(launched) == filepath.Base(published)
}
