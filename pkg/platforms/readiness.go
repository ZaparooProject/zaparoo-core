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

package platforms

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
)

// ErrReadinessUnavailable reports that the integration a readiness probe asks
// cannot be reached or does not report readiness, as opposed to the media
// simply not being ready yet. PollReady treats it as retryable, since an
// emulator's API is briefly down while it restarts, and returns it only if it
// is still the latest outcome when the wait ends.
var ErrReadinessUnavailable = errors.New("readiness integration unavailable")

// ReadyCheck reports whether launched media is ready. A false result with a
// nil error means not ready yet. An error wrapping ErrReadinessUnavailable is
// retried until the wait ends; any other error ends the wait immediately.
type ReadyCheck func(ctx context.Context) (bool, error)

// PollReady calls check every interval until the media is ready, check fails
// permanently, or ctx ends. If ctx ends while the latest check reported the
// integration unavailable, the returned error wraps ErrReadinessUnavailable so
// callers can tell a missing integration from a slow one.
func PollReady(ctx context.Context, clock clockwork.Clock, interval time.Duration, check ReadyCheck) error {
	var lastErr error
	for {
		ready, err := check(ctx)
		switch {
		case err == nil:
			lastErr = nil
			if ready {
				return nil
			}
		case errors.Is(err, ErrReadinessUnavailable):
			lastErr = err
		case ctx.Err() != nil:
			// A check cut short by the wait ending is the wait ending.
		default:
			return fmt.Errorf("readiness check: %w", err)
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("media not ready: %w", errors.Join(ctx.Err(), lastErr))
			}
			return fmt.Errorf("media not ready: %w", ctx.Err())
		case <-clock.After(interval):
		}
	}
}
