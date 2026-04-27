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

package middleware

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/rs/zerolog/log"
)

// DefaultLastSeenFlushInterval is how often the LastSeenTracker flushes
// pending updates to the database when StartFlushLoop is used without an
// explicit interval. 30s trades a small amount of database activity for a
// "last seen" estimate that is fresh enough to be useful in the paired
// clients list.
const DefaultLastSeenFlushInterval = 30 * time.Second

// LastSeenTracker batches in-memory LastSeenAt updates and flushes to DB
// periodically. High-frequency Touches collapse to one UPDATE per interval.
type LastSeenTracker struct {
	db    database.UserDBI
	dirty map[string]int64
	mu    syncutil.Mutex
}

// NewLastSeenTracker constructs a tracker bound to the given UserDBI.
func NewLastSeenTracker(db database.UserDBI) *LastSeenTracker {
	return &LastSeenTracker{
		db:    db,
		dirty: make(map[string]int64),
	}
}

// Touch records the latest seen timestamp (newest wins, goroutine-safe).
func (t *LastSeenTracker) Touch(authToken string, ts int64) {
	if authToken == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if existing, ok := t.dirty[authToken]; ok && existing >= ts {
		return
	}
	t.dirty[authToken] = ts
}

// Flush writes pending updates (errors logged, not fatal). Snapshots under
// lock so concurrent Touches are not blocked.
func (t *LastSeenTracker) Flush(ctx context.Context) {
	t.mu.Lock()
	if len(t.dirty) == 0 {
		t.mu.Unlock()
		return
	}
	snapshot := t.dirty
	t.dirty = make(map[string]int64)
	t.mu.Unlock()

	for token, ts := range snapshot {
		if err := ctx.Err(); err != nil {
			// Re-queue remaining tokens to avoid silent loss on shutdown.
			t.mu.Lock()
			for rt, rts := range snapshot {
				if existing, ok := t.dirty[rt]; !ok || existing < rts {
					t.dirty[rt] = rts
				}
			}
			t.mu.Unlock()
			return
		}
		if err := t.db.UpdateClientLastSeen(token, ts); err != nil {
			log.Warn().
				Err(err).
				Str("auth_token", redactToken(token)).
				Msg("encryption: failed to persist client last seen")
		}
		delete(snapshot, token)
	}
}

// StartFlushLoop runs Flush periodically with a final flush on shutdown.
// Pass zero interval for DefaultLastSeenFlushInterval. The returned channel is
// closed after the final flush has completed.
func (t *LastSeenTracker) StartFlushLoop(ctx context.Context, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = DefaultLastSeenFlushInterval
	}
	done := make(chan struct{})
	// G118: this goroutine intentionally falls back to a fresh background
	// context for the shutdown flush. The flush is reacting to ctx being
	// canceled, so reusing ctx would abort every UpdateClientLastSeen call
	// before it touched the DB and silently drop the pending updates.
	//nolint:gosec // G118: see comment above
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				t.Flush(ctx)
			case <-ctx.Done():
				t.Flush(context.Background())
				return
			}
		}
	}()
	return done
}
