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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

func HandleSystems(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	log.Info().Msg("received systems request")

	indexed, err := env.Database.MediaDB.IndexedSystems()
	if err != nil {
		log.Error().Err(err).Msgf("error getting indexed systems")
		indexed = []string{}
	}

	if len(indexed) == 0 {
		log.Warn().Msg("no indexed systems found")
	}

	respSystems := make([]models.System, 0)

	for _, id := range indexed {
		system, err := systemdefs.GetSystem(id)
		if err != nil {
			log.Error().Err(err).Msgf("error getting system: %s", id)
			continue
		}

		sr := models.System{
			ID: system.ID,
		}

		sm, err := assets.GetSystemMetadata(id)
		if err != nil {
			log.Error().Err(err).Msgf("error getting system metadata: %s", id)
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

	return models.SystemsResponse{
		Systems: respSystems,
	}, nil
}
