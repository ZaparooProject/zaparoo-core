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

package methods

import (
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
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

	// Report what is actually being enforced, which the active profile's
	// override can differ from the global config setting.
	resp.LimitsEnabled = env.LimitsManager.EffectiveLimitsEnabled()

	// Update with actual status
	resp.State = status.State
	resp.SessionActive = status.SessionActive

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

		// Session remaining (only if session limit is configured). A day
		// waiver zeroes the session limit, so this is omitted while one is
		// active.
		if status.SessionRemaining > 0 {
			remainingStr := status.SessionRemaining.String()
			resp.SessionRemaining = &remainingStr
		}

		if status.SessionExtension > 0 {
			extensionStr := status.SessionExtension.String()
			resp.SessionExtension = &extensionStr
		}
	}

	// A session-limit waiver is scoped to a profile and a day rather than a
	// session, so it is reported in every state.
	if !status.SessionExtendedUntil.IsZero() {
		untilStr := status.SessionExtendedUntil.Format(time.RFC3339)
		resp.SessionExtendedUntil = &untilStr
	}

	// nil = not calculated (limits disabled or clock unreliable)
	if status.DailyUsageToday != nil {
		usageStr := status.DailyUsageToday.String()
		resp.DailyUsageToday = &usageStr
	}

	// nil = not calculated (limits disabled or clock unreliable)
	if status.DailyRemaining != nil {
		remainingStr := status.DailyRemaining.String()
		resp.DailyRemaining = &remainingStr
	}

	return resp, nil
}

// grantClientError maps a rejected grant onto a client-fault error. Storage
// failures are deliberately absent: those are the server's problem and must
// surface as a server error so the caller retries rather than giving up.
func grantClientError(err error) error {
	switch {
	case errors.Is(err, playtime.ErrGrantModeInvalid),
		errors.Is(err, playtime.ErrGrantDurationRange),
		errors.Is(err, playtime.ErrGrantCapExceeded),
		errors.Is(err, playtime.ErrGrantNoSession),
		errors.Is(err, playtime.ErrGrantLimitsDisabled),
		errors.Is(err, playtime.ErrGrantClockUnreliable):
		return models.ClientErrf("%w", err)
	default:
		return fmt.Errorf("failed to extend playtime: %w", err)
	}
}

// HandlePlaytimeExtend grants extra time to the session currently being
// limited, without stopping what is playing and without changing any
// configured limit. The daily limit is left alone in every mode: it is the
// hard ceiling, and raising it is a settings change, not a grant.
//
//nolint:gocritic // single-use parameter in API handler
func HandlePlaytimeExtend(env requests.RequestEnv) (any, error) {
	var params models.ExtendPlaytimeParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	if err := requireCapability(&env, permissions.CapPlaytimeExtend); err != nil {
		return nil, err
	}

	log.Info().Str("mode", params.Mode).Msg("received playtime extend request")

	if env.LimitsManager == nil {
		return nil, errors.New("playtime limits are not available")
	}

	req := &playtime.GrantRequest{
		Source:             "api",
		AuthorizerClientID: env.ClientID,
		// An API request ID stays valid for as long as the grant it
		// produced, so a retry after a dropped connection still matches.
		IdempotencyKey: params.RequestID,
	}

	switch params.Mode {
	case models.PlaytimeExtendModeDuration:
		if params.Duration == nil || *params.Duration == "" {
			return nil, models.ClientErrf("duration is required for mode %q", params.Mode)
		}
		parsed, err := time.ParseDuration(*params.Duration)
		if err != nil {
			return nil, models.ClientErrf("invalid duration: %w", err)
		}
		req.Mode = playtime.GrantModeDuration
		req.Duration = parsed
	case models.PlaytimeExtendModeToday:
		req.Mode = playtime.GrantModeToday
	default:
		return nil, models.ClientErrf("%w: %q", playtime.ErrGrantModeInvalid, params.Mode)
	}

	result, err := env.LimitsManager.Grant(req)
	if err != nil {
		log.Warn().Err(err).Str("mode", params.Mode).Msg("playtime: extension refused")
		return nil, grantClientError(err)
	}

	resp := models.ExtendPlaytimeResponse{
		Mode:      string(result.Mode),
		ProfileID: result.RecipientProfileID,
		Replayed:  result.Replayed,
	}
	if result.Duration > 0 {
		resp.Duration = result.Duration.String()
	}
	if result.SessionExtension > 0 {
		resp.SessionExtension = result.SessionExtension.String()
	}
	if !result.ExpiresAt.IsZero() {
		resp.Expires = result.ExpiresAt.Format(time.RFC3339)
	}

	// A replay granted nothing new, so it must not look like a fresh grant
	// to notification subscribers.
	if !result.Replayed && env.State != nil {
		notifications.PlaytimeExtended(env.State.Notifications, &models.PlaytimeExtendedParams{
			Mode:             resp.Mode,
			Duration:         resp.Duration,
			Expires:          resp.Expires,
			SessionExtension: resp.SessionExtension,
			ProfileID:        resp.ProfileID,
			GrantedBy:        result.AuthorizerProfileID,
		})
	}

	return resp, nil
}
