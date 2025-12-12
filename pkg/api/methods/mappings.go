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
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

func HandleMappings(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received mappings request")

	resp := models.AllMappingsResponse{
		Mappings: make([]models.MappingResponse, 0),
	}

	mappings, err := env.Database.UserDB.GetAllMappings()
	if err != nil {
		log.Error().Err(err).Msg("error getting mappings")
		return nil, errors.New("error getting mappings")
	}

	mrs := make([]models.MappingResponse, 0)

	for _, m := range mappings {
		t := time.Unix(0, m.Added*int64(time.Millisecond))

		// keep compatibility for v0.1 api
		switch m.Type {
		case userdb.MappingTypeID:
			m.Type = userdb.LegacyMappingTypeUID
		case userdb.MappingTypeValue:
			m.Type = userdb.LegacyMappingTypeText
		}

		mr := models.MappingResponse{
			ID:       strconv.FormatInt(m.DBID, 10),
			Added:    t.Format(time.RFC3339),
			Label:    m.Label,
			Enabled:  m.Enabled,
			Type:     m.Type,
			Match:    m.Match,
			Pattern:  m.Pattern,
			Override: m.Override,
		}

		mrs = append(mrs, mr)
	}

	resp.Mappings = mrs

	return resp, nil
}

func HandleAddMapping(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received add mapping request")

	var params models.AddMappingParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Error().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// convert old type names
	switch params.Type {
	case userdb.LegacyMappingTypeUID:
		params.Type = userdb.MappingTypeID
	case userdb.LegacyMappingTypeText:
		params.Type = userdb.MappingTypeValue
	}

	// validate regex pattern compiles if match type is regex
	if params.Match == userdb.MatchTypeRegex {
		if err := validation.ValidateRegexPattern(params.Pattern); err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
	}

	m := database.Mapping{
		Label:    params.Label,
		Enabled:  params.Enabled,
		Type:     params.Type,
		Match:    params.Match,
		Pattern:  params.Pattern,
		Override: params.Override,
	}

	err := env.Database.UserDB.AddMapping(&m)
	if err != nil {
		return nil, fmt.Errorf("failed to add mapping: %w", err)
	}

	return NoContent{}, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleDeleteMapping(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received delete mapping request")

	var params models.DeleteMappingParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Error().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	err := env.Database.UserDB.DeleteMapping(int64(params.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to delete mapping: %w", err)
	}

	return NoContent{}, nil
}

func validateUpdateMappingHasFields(params *models.UpdateMappingParams) error {
	if params.Label == nil && params.Enabled == nil && params.Type == nil &&
		params.Match == nil && params.Pattern == nil && params.Override == nil {
		return errors.New("at least one field must be provided")
	}
	return nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleUpdateMapping(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received update mapping request")

	var params models.UpdateMappingParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Error().Err(err).Msg("invalid params")
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// check at least one field is provided
	if err := validateUpdateMappingHasFields(&params); err != nil {
		return nil, err
	}

	// convert old type names
	if params.Type != nil {
		switch *params.Type {
		case userdb.LegacyMappingTypeUID:
			*params.Type = userdb.MappingTypeID
		case userdb.LegacyMappingTypeText:
			*params.Type = userdb.MappingTypeValue
		}
	}

	// validate regex pattern compiles if match type is regex
	if params.Match != nil && *params.Match == userdb.MatchTypeRegex {
		if params.Pattern == nil {
			return nil, errors.New("pattern is required for regex match")
		}
		if err := validation.ValidateRegexPattern(*params.Pattern); err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
	}

	oldMapping, err := env.Database.UserDB.GetMapping(int64(params.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping: %w", err)
	}

	newMapping := oldMapping

	if params.Label != nil {
		newMapping.Label = *params.Label
	}

	if params.Enabled != nil {
		newMapping.Enabled = *params.Enabled
	}

	if params.Type != nil {
		newMapping.Type = *params.Type
	}

	if params.Match != nil {
		newMapping.Match = *params.Match
	}

	if params.Pattern != nil {
		newMapping.Pattern = *params.Pattern
	}

	if params.Override != nil {
		newMapping.Override = *params.Override
	}

	err = env.Database.UserDB.UpdateMapping(int64(params.ID), &newMapping)
	if err != nil {
		return nil, fmt.Errorf("failed to update mapping: %w", err)
	}

	return NoContent{}, nil
}

//nolint:gocritic // single-use parameter in API handler
func HandleReloadMappings(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received reload mappings request")

	mapDir := filepath.Join(helpers.DataDir(env.Platform), config.MappingsDir)
	err := env.Config.LoadMappings(mapDir)
	if err != nil {
		log.Error().Err(err).Msg("error loading mappings")
		return nil, errors.New("error loading mappings")
	}

	return NoContent{}, nil
}
