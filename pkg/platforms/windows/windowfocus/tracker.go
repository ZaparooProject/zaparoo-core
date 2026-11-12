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

package windowfocus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// processPollInterval is how often a wait that expects a process to appear
// looks for it.
const processPollInterval = 250 * time.Millisecond

// Tracker remembers the focus attempt of the process Core is tracking, so a
// readiness probe can wait for that process's window.
type Tracker struct {
	pending *Pending
	mu      syncutil.Mutex
}

// Track starts focusing pid's window and remembers the attempt as the current
// one, replacing any earlier attempt.
func (t *Tracker) Track(ctx context.Context, f Focuser, pid uint32) *Pending {
	pending := Start(ctx, f, pid)
	t.mu.Lock()
	t.pending = pending
	t.mu.Unlock()
	return pending
}

// Forget drops the current attempt, as when its process is gone.
func (t *Tracker) Forget() {
	t.mu.Lock()
	t.pending = nil
	t.mu.Unlock()
}

func (t *Tracker) current() *Pending {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pending
}

// Wait returns once the tracked process's window has taken focus. A program
// that opens no window within the focus timeout is treated as ready: it is not
// waiting on anything Core can see.
//
// With expectProcess the wait also covers a process that is not tracked yet,
// as with a launcher whose media is published before its game process exists;
// without it, no tracked process means there is nothing to wait for.
func (t *Tracker) Wait(ctx context.Context, clock clockwork.Clock, expectProcess bool) error {
	err := platforms.PollReady(ctx, clock, processPollInterval, func(ctx context.Context) (bool, error) {
		pending := t.current()
		if pending == nil {
			return !expectProcess, nil
		}
		focusErr := pending.Err(ctx)
		switch {
		case focusErr == nil:
			return true, nil
		case ctx.Err() != nil:
			return false, fmt.Errorf("wait for the program window: %w", ctx.Err())
		case errors.Is(focusErr, context.Canceled):
			// The attempt was replaced or stopped; look again for the next one.
			return false, nil
		default:
			log.Debug().Err(focusErr).Msg("launched program opened no window to wait for; treating it as ready")
			return true, nil
		}
	})
	if err != nil {
		return fmt.Errorf("wait for the tracked window: %w", err)
	}
	return nil
}
