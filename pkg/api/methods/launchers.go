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
	"path/filepath"
	"sort"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

func HandleLaunchers(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use
	log.Debug().Msg("received launchers request")

	if env.LauncherCache == nil {
		return models.LaunchersResponse{Launchers: []models.Launcher{}}, nil
	}

	all := env.LauncherCache.GetAllLaunchers()
	resp := make([]models.Launcher, 0, len(all))
	for i := range all {
		l := all[i]
		var groups []string
		if len(l.Groups) > 0 {
			groups = make([]string, len(l.Groups))
			copy(groups, l.Groups)
		}
		entry := models.Launcher{
			ID:       l.ID,
			SystemID: l.SystemID,
			Groups:   groups,
		}
		if l.SystemID != "" {
			if sm, mErr := assets.GetSystemMetadata(l.SystemID); mErr == nil && sm.Name != "" {
				entry.SystemName = sm.Name
			}
		}
		resp = append(resp, entry)
	}

	sort.SliceStable(resp, func(i, j int) bool {
		if resp[i].SystemID != resp[j].SystemID {
			return resp[i].SystemID < resp[j].SystemID
		}
		return resp[i].ID < resp[j].ID
	})

	return models.LaunchersResponse{Launchers: resp}, nil
}

func HandleLaunchersRefresh(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use
	log.Info().Msg("received launchers refresh request")

	err := env.Config.Load()
	if err != nil {
		log.Error().Err(err).Msg("error reloading config")
		return nil, errors.New("error reloading config")
	}

	launchersDir := filepath.Join(helpers.DataDir(env.Platform), config.LaunchersDir)
	err = env.Config.LoadCustomLaunchers(launchersDir)
	if err != nil {
		log.Error().Err(err).Msg("error loading custom launchers")
		return nil, errors.New("error loading custom launchers")
	}

	env.LauncherCache.Refresh(env.Platform, env.Config)

	log.Info().Msg("launcher cache refreshed")
	return NoContent{}, nil
}
