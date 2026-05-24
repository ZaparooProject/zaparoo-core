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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/mackerelio/go-osstat/uptime"
	"github.com/rs/zerolog/log"
)

type uptimeProvider func() (time.Duration, error)

// monitorClockAndHealTimestamps monitors the system clock and heals timestamps when NTP syncs.
// This is critical for MiSTer devices that boot without RTC and initially show 1970 epoch time.
// Once NTP syncs, we can mathematically reconstruct correct timestamps using monotonic uptime.
func monitorClockAndHealTimestamps(ctx context.Context, db *database.Database, bootUUID string) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	healed := false
	wasReliable := helpers.IsClockReliable(time.Now())

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			wasReliableBeforeHeal := wasReliable
			healed = healTimestampsIfClockReliable(db, bootUUID, now, wasReliable, healed, uptime.Get)
			isReliable := helpers.IsClockReliable(now)
			// Keep the previous unreliable state when healing fails so the next tick
			// can retry the same NTP transition. A successful zero-row heal marks done.
			if !isReliable || healed || wasReliableBeforeHeal {
				wasReliable = isReliable
			}

		case <-ctx.Done():
			return
		}
	}
}

func healTimestampsIfClockReliable(
	db *database.Database,
	bootUUID string,
	now time.Time,
	wasReliable bool,
	healed bool,
	getUptime uptimeProvider,
) bool {
	isReliable := helpers.IsClockReliable(now)
	if !isReliable || wasReliable || healed {
		return healed
	}

	log.Info().Msg("clock became reliable (NTP sync detected), healing timestamps")

	// Calculate true boot time: Current Time - System Uptime
	systemUptime, err := getUptime()
	if err != nil {
		log.Error().Err(err).Msg("failed to get system uptime for timestamp healing")
		return healed
	}

	trueBootTime := now.Add(-systemUptime)
	log.Info().
		Time("true_boot_time", trueBootTime).
		Dur("uptime", systemUptime).
		Msg("calculated true boot time")

	// Heal all timestamps for this boot session
	rowsHealed, healErr := db.UserDB.HealTimestamps(bootUUID, trueBootTime)
	if healErr != nil {
		log.Error().Err(healErr).Msg("failed to heal timestamps")
		return healed
	}
	if rowsHealed > 0 {
		log.Info().Int64("rows", rowsHealed).Msg("successfully healed timestamps")
	}
	return true
}
