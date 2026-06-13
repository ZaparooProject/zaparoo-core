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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/rs/zerolog/log"
)

// errProfilesUnavailable is returned when the profiles service was not
// wired into the request environment (should not happen in production).
var errProfilesUnavailable = errors.New("profiles service not available")

func profileResponse(p *database.Profile) models.ProfileResponse {
	return models.ProfileResponse{
		ProfileID:     p.ProfileID,
		Name:          p.Name,
		SwitchID:      p.SwitchID,
		HasPIN:        p.PINHash != "",
		LimitsEnabled: p.LimitsEnabled,
		DailyLimit:    p.DailyLimit,
		SessionLimit:  p.SessionLimit,
		CreatedAt:     p.CreatedAt,
		LastUpdatedAt: p.UpdatedAt,
	}
}

// profileError maps service errors to client errors where the client is at
// fault (bad PIN, unknown profile), passing other errors through.
func profileError(err error) error {
	switch {
	case errors.Is(err, profiles.ErrPINRequired),
		errors.Is(err, profiles.ErrPINIncorrect),
		errors.Is(err, profiles.ErrPINRateLimited),
		errors.Is(err, profiles.ErrInvalidPINFormat),
		errors.Is(err, profiles.ErrNotFound):
		return models.ClientErrf("%w", err)
	default:
		return err
	}
}

// HandleProfiles lists all profiles.
//
//nolint:gocritic // single-use parameter in API handler
func HandleProfiles(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received profiles list request")
	if env.Profiles == nil {
		return nil, errProfilesUnavailable
	}

	list, err := env.Profiles.List()
	if err != nil {
		log.Error().Err(err).Msg("error listing profiles")
		return nil, errors.New("error listing profiles")
	}

	resp := models.ProfilesResponse{
		Profiles: make([]models.ProfileResponse, len(list)),
	}
	for i := range list {
		resp.Profiles[i] = profileResponse(&list[i])
	}
	return resp, nil
}

// HandleProfilesNew creates a new profile.
//
//nolint:gocritic // single-use parameter in API handler
func HandleProfilesNew(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received profiles new request")
	if env.Profiles == nil {
		return nil, errProfilesUnavailable
	}

	var params models.NewProfileParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	p, err := env.Profiles.Create(&params)
	if err != nil {
		return nil, profileError(err)
	}
	return profileResponse(p), nil
}

// HandleProfilesUpdate updates an existing profile.
//
//nolint:gocritic // single-use parameter in API handler
func HandleProfilesUpdate(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received profiles update request")
	if env.Profiles == nil {
		return nil, errProfilesUnavailable
	}

	var params models.UpdateProfileParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	p, err := env.Profiles.Update(&params)
	if err != nil {
		return nil, profileError(err)
	}
	return profileResponse(p), nil
}

// HandleProfilesDelete removes a profile, deactivating it first if it is
// the active profile.
//
//nolint:gocritic // single-use parameter in API handler
func HandleProfilesDelete(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received profiles delete request")
	if env.Profiles == nil {
		return nil, errProfilesUnavailable
	}

	var params models.DeleteProfileParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	if err := env.Profiles.Delete(params.ProfileID); err != nil {
		return nil, profileError(err)
	}
	return NoContent{}, nil
}

// HandleProfilesActive returns the active profile, or null when none.
//
//nolint:gocritic // single-use parameter in API handler
func HandleProfilesActive(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received profiles active request")
	if env.Profiles == nil {
		return nil, errProfilesUnavailable
	}
	return env.Profiles.Active(), nil
}

// HandleProfilesSwitch switches the active profile. Switching to a
// PIN-protected profile requires its PIN, by profile ID or switch ID
// equally — only physical card scans bypass the PIN. Passing neither
// profileId nor switchId deactivates, which is always free (PINs gate
// entry only).
//
//nolint:gocritic // single-use parameter in API handler
func HandleProfilesSwitch(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received profiles switch request")
	if env.Profiles == nil {
		return nil, errProfilesUnavailable
	}

	var params models.SwitchProfileParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			log.Warn().Err(err).Msg("invalid params")
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	pin := ""
	if params.PIN != nil {
		pin = *params.PIN
	}

	switch {
	case params.ProfileID != nil && *params.ProfileID != "":
		active, err := env.Profiles.ActivateByID(*params.ProfileID, pin)
		if err != nil {
			return nil, profileError(err)
		}
		return active, nil
	case params.SwitchID != nil && *params.SwitchID != "":
		active, err := env.Profiles.ActivateBySwitchIDChecked(*params.SwitchID, pin)
		if err != nil {
			return nil, profileError(err)
		}
		return active, nil
	default:
		if err := env.Profiles.Deactivate(); err != nil {
			return nil, profileError(err)
		}
		return (*models.ActiveProfile)(nil), nil
	}
}
