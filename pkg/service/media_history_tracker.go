/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/mackerelio/go-osstat/uptime"
	"github.com/rs/zerolog/log"
)

// mediaHistoryTracker encapsulates the state and logic for tracking media history.
// It coordinates between the notification listener and the periodic PlayTime updater.
type mediaHistoryTracker struct {
	clock                     clockwork.Clock
	currentMediaStartTime     time.Time
	currentMediaStartTimeMono time.Time
	st                        *state.State
	db                        *database.Database
	currentHistoryDBID        int64
	mu                        syncutil.RWMutex
}

// listen processes media start/stop notifications and records them in the database.
func (t *mediaHistoryTracker) listen(notificationChan <-chan models.Notification) {
	for notif := range notificationChan {
		switch notif.Method {
		case models.NotificationStarted:
			// Media started - create new history entry
			activeMedia := t.st.ActiveMedia()
			if activeMedia != nil {
				now := t.clock.Now()
				nowMono := time.Now() // Monotonic clock for duration calculation

				// Calculate system uptime for timestamp healing on MiSTer
				systemUptime, uptimeErr := uptime.Get()
				if uptimeErr != nil {
					log.Warn().Err(uptimeErr).Msg("failed to get system uptime, using 0")
					systemUptime = 0
				}
				monotonicStart := int64(systemUptime.Seconds())

				// Determine clock reliability and source
				clockReliable := helpers.IsClockReliable(now)
				var clockSource string
				if clockReliable {
					clockSource = helpers.ClockSourceSystem
				} else {
					clockSource = helpers.ClockSourceEpoch
				}

				entry := &database.MediaHistoryEntry{
					ID:             uuid.New().String(),
					StartTime:      activeMedia.Started,
					SystemID:       activeMedia.SystemID,
					SystemName:     activeMedia.SystemName,
					MediaPath:      activeMedia.Path,
					MediaName:      activeMedia.Name,
					LauncherID:     activeMedia.LauncherID,
					PlayTime:       0,
					BootUUID:       t.st.BootUUID(),
					MonotonicStart: monotonicStart,
					DurationSec:    0,
					WallDuration:   0,
					TimeSkewFlag:   false,
					ClockReliable:  clockReliable,
					ClockSource:    clockSource,
					CreatedAt:      now,
					UpdatedAt:      now,
				}
				dbid, addErr := t.db.UserDB.AddMediaHistory(entry)
				if addErr != nil {
					log.Error().Err(addErr).Msg("failed to add media history entry")
				} else {
					t.mu.Lock()
					t.currentHistoryDBID = dbid
					t.currentMediaStartTime = activeMedia.Started
					t.currentMediaStartTimeMono = nowMono
					t.mu.Unlock()
					log.Debug().Int64("dbid", dbid).Msg("created media history entry")
				}
			}

		case models.NotificationStopped:
			// Media stopped - close history entry
			t.mu.Lock()
			dbid := t.currentHistoryDBID
			startTime := t.currentMediaStartTime
			startTimeMono := t.currentMediaStartTimeMono
			t.currentHistoryDBID = 0
			t.currentMediaStartTime = time.Time{}
			t.currentMediaStartTimeMono = time.Time{}
			t.mu.Unlock()

			if dbid != 0 {
				endTime := t.clock.Now()

				// Calculate duration - prefer monotonic if available, fall back to wall-clock
				var playTime int
				if !startTimeMono.IsZero() {
					// Use monotonic clock (more accurate, handles sleep)
					endTimeMono := time.Now()
					playTime = int(endTimeMono.Sub(startTimeMono).Seconds())
				} else {
					// Fall back to wall-clock (for tests or if mono not initialized)
					playTime = int(endTime.Sub(startTime).Seconds())
				}

				closeErr := t.db.UserDB.CloseMediaHistory(dbid, endTime, playTime)
				if closeErr != nil {
					log.Error().Err(closeErr).Int64("dbid", dbid).Msg("failed to close media history entry")
				} else {
					log.Debug().Int64("dbid", dbid).Int("playTime", playTime).Msg("closed media history entry")
				}
			}
		}
	}
}

// updatePlayTime periodically updates the PlayTime for the currently active media
// history entry every minute.
func (t *mediaHistoryTracker) updatePlayTime(ctx context.Context) {
	ticker := t.clock.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.Chan():
			t.mu.RLock()
			dbid := t.currentHistoryDBID
			startTime := t.currentMediaStartTime
			startTimeMono := t.currentMediaStartTimeMono
			t.mu.RUnlock()

			if dbid != 0 {
				// Calculate duration - prefer monotonic if available, fall back to wall-clock
				var playTime int
				switch {
				case !startTimeMono.IsZero():
					// Use monotonic clock (more accurate, handles sleep/hibernate)
					nowMono := time.Now()
					playTime = int(nowMono.Sub(startTimeMono).Seconds())
				case !startTime.IsZero():
					// Fall back to wall-clock (for tests or if mono not initialized)
					playTime = int(t.clock.Since(startTime).Seconds())
				default:
					// No valid start time - skip update
					continue
				}

				updateErr := t.db.UserDB.UpdateMediaHistoryTime(dbid, playTime)
				if updateErr != nil {
					log.Warn().Err(updateErr).Msg("failed to update media history play time")
				} else {
					log.Debug().Int64("dbid", dbid).Int("playTime", playTime).Msg("updated media history play time")
				}
			}
		case <-ctx.Done():
			return
		}
	}
}
