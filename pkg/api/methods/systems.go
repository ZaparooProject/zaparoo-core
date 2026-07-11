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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/rs/zerolog/log"
)

func HandleSystems(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received systems request")

	var params models.SystemsParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	indexed, err := env.Database.MediaDB.IndexedSystems()
	if err != nil {
		log.Error().Err(err).Msg("error getting indexed systems")
		indexed = []string{}
	}

	if len(indexed) == 0 {
		log.Debug().Msg("no indexed systems found")
	}

	systemIDs := make([]string, 0, len(indexed))
	seen := make(map[string]struct{}, len(indexed))
	addSystemID := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		systemIDs = append(systemIDs, id)
	}
	for _, id := range indexed {
		addSystemID(id)
	}

	if env.LauncherCache != nil {
		launchers := env.LauncherCache.GetAllLaunchers()
		for i := range launchers {
			if !params.All && !launchers[i].Available {
				continue
			}
			addSystemID(launchers[i].SystemID)
		}
	}

	respSystems := make([]models.System, 0, len(systemIDs))
	for _, id := range systemIDs {
		system, systemErr := systemdefs.GetSystem(id)
		if systemErr != nil {
			log.Error().Err(systemErr).Msgf("error getting system: %s", id)
			continue
		}

		sr := models.System{ID: system.ID}
		sm, metadataErr := assets.GetSystemMetadata(id)
		if metadataErr != nil {
			log.Error().Err(metadataErr).Msgf("error getting system metadata: %s", id)
		} else {
			sr.Name = sm.Name
			sr.Category = sm.Category
			if sm.ReleaseDate != "" {
				sr.ReleaseDate = &sm.ReleaseDate
			}
			if sm.Manufacturer != "" {
				sr.Manufacturer = &sm.Manufacturer
			}
		}
		respSystems = append(respSystems, sr)
	}

	if env.LauncherCache != nil {
		for _, system := range env.LauncherCache.GetLaunchableSystems() {
			id := launchables.EncodeID(system.ID)
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			respSystems = append(respSystems, models.System{
				ID:        id,
				Name:      system.Name,
				Category:  system.Category,
				ZapScript: system.ZapScript(),
			})
		}
	}

	return models.SystemsResponse{Systems: respSystems}, nil
}
