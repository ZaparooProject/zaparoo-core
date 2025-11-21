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

	if status == nil {
		// No limits manager or no active session
		return models.PlaytimeStatusResponse{
			SessionActive: false,
			ClockReliable: true,
			LimitsEnabled: env.Config.PlaytimeLimitsEnabled(),
		}, nil
	}

	// Build response
	resp := models.PlaytimeStatusResponse{
		SessionActive: status.SessionActive,
		ClockReliable: status.ClockReliable,
		LimitsEnabled: env.Config.PlaytimeLimitsEnabled(),
		WarningsGiven: make([]string, 0, len(status.WarningsGiven)),
	}

	// Add optional fields if session is active
	if status.SessionActive {
		// Session started timestamp (ISO8601)
		startedStr := status.SessionStarted.Format("2006-01-02T15:04:05Z07:00")
		resp.SessionStarted = &startedStr

		// Session duration
		durationStr := status.SessionDuration.String()
		resp.SessionDuration = &durationStr

		// Session remaining time
		if status.SessionRemaining > 0 {
			remainingStr := status.SessionRemaining.String()
			resp.SessionRemaining = &remainingStr
		}

		// Daily usage
		if status.DailyUsageToday > 0 {
			usageStr := status.DailyUsageToday.String()
			resp.DailyUsageToday = &usageStr
		}

		// Daily remaining time (only if clock is reliable)
		if status.DailyRemaining > 0 && status.ClockReliable {
			remainingStr := status.DailyRemaining.String()
			resp.DailyRemaining = &remainingStr
		}

		// Warnings given
		for _, warning := range status.WarningsGiven {
			resp.WarningsGiven = append(resp.WarningsGiven, warning.String())
		}
	}

	return resp, nil
}
