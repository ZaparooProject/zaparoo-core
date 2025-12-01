// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package methods

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/rs/zerolog/log"
)

//nolint:gocritic // single-use parameter in API handler
func HandlePlaytime(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received playtime status request")

	// Get status from LimitsManager
	var status *playtime.StatusInfo
	if env.LimitsManager != nil {
		status = env.LimitsManager.GetStatus()
	}

	// Build base response
	resp := models.PlaytimeStatusResponse{
		State:         "reset",
		SessionActive: false,
		LimitsEnabled: env.Config.PlaytimeLimitsEnabled(),
	}

	if status == nil {
		// No limits manager - return base response
		return resp, nil
	}

	// Update with actual status
	resp.State = status.State
	resp.SessionActive = status.SessionActive

	// Daily usage and remaining are available in all states (if calculable)
	if status.DailyUsageToday > 0 {
		usageStr := status.DailyUsageToday.String()
		resp.DailyUsageToday = &usageStr
	}
	if status.DailyRemaining > 0 {
		remainingStr := status.DailyRemaining.String()
		resp.DailyRemaining = &remainingStr
	}

	// Cooldown remaining (only during cooldown state)
	if status.CooldownRemaining > 0 {
		remainingStr := status.CooldownRemaining.String()
		resp.CooldownRemaining = &remainingStr
	}

	// Session info (only during active and cooldown states)
	if status.State != "reset" {
		// Session started timestamp (only during active - cooldown has no current game)
		if !status.SessionStarted.IsZero() {
			startedStr := status.SessionStarted.Format("2006-01-02T15:04:05Z07:00")
			resp.SessionStarted = &startedStr
		}

		// Session duration and cumulative time
		durationStr := status.SessionDuration.String()
		resp.SessionDuration = &durationStr

		cumulativeStr := status.SessionCumulativeTime.String()
		resp.SessionCumulativeTime = &cumulativeStr

		// Session remaining (only if session limit is configured)
		if status.SessionRemaining > 0 {
			remainingStr := status.SessionRemaining.String()
			resp.SessionRemaining = &remainingStr
		}
	}

	return resp, nil
}
