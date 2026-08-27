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
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

// normalizedSystemFilter canonicalizes requested system IDs for filtering.
// Unlike resolveSystems (media.go), an ID that doesn't match a known system
// is kept as-is rather than erroring: launcher system IDs can be launchable
// or virtual systems outside systemdefs, and a filter that matches nothing
// should return an empty launcher list, not a client error.
func normalizedSystemFilter(ids []string, fuzzy bool) []string {
	normalized := make([]string, len(ids))
	for i, id := range ids {
		normalized[i] = id
		if !fuzzy {
			continue
		}
		if sys, err := systemdefs.LookupSystem(id); err == nil {
			normalized[i] = sys.ID
		}
	}
	return normalized
}

func matchesSystemFilter(systemID string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	return slices.ContainsFunc(filter, func(id string) bool {
		return strings.EqualFold(id, systemID)
	})
}

// isDefaultLauncher reports whether l is the configured default launcher for
// its system, matched by launcher ID or by any of the launcher's groups —
// the same matching settings.systemDefaults.launcher documents.
func isDefaultLauncher(cfg *config.Instance, l *platforms.Launcher) bool {
	if cfg == nil || l.SystemID == "" {
		return false
	}
	defaults, ok := cfg.LookupSystemDefaults(l.SystemID)
	if !ok || defaults.Launcher == "" {
		return false
	}
	if strings.EqualFold(defaults.Launcher, l.ID) {
		return true
	}
	return slices.ContainsFunc(l.Groups, func(group string) bool {
		return strings.EqualFold(defaults.Launcher, group)
	})
}

func HandleLaunchers(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use
	log.Debug().Msg("received launchers request")

	if env.LauncherCache == nil {
		return models.LaunchersResponse{Launchers: []models.Launcher{}}, nil
	}

	var params models.LaunchersParams
	if len(env.Params) > 0 {
		if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	var systemFilter []string
	if params.Systems != nil {
		fuzzy := params.FuzzySystem != nil && *params.FuzzySystem
		systemFilter = normalizedSystemFilter(*params.Systems, fuzzy)
	}

	runtimeProvider, hasRuntimeProvider := env.Platform.(platforms.LauncherRuntimeProvider)

	all := env.LauncherCache.GetAllLaunchers()
	resp := make([]models.Launcher, 0, len(all))
	for i := range all {
		l := all[i]
		if !matchesSystemFilter(l.SystemID, systemFilter) {
			continue
		}
		var groups []string
		if len(l.Groups) > 0 {
			groups = make([]string, len(l.Groups))
			copy(groups, l.Groups)
		}
		entry := models.Launcher{
			ID:                 l.ID,
			SystemID:           l.SystemID,
			Groups:             groups,
			Available:          l.Available,
			AvailabilityReason: l.AvailabilityReason,
			Default:            isDefaultLauncher(env.Config, &l),
		}
		if l.SystemID != "" {
			if sm, mErr := assets.GetSystemMetadata(l.SystemID); mErr == nil && sm.Name != "" {
				entry.SystemName = sm.Name
			}
		}
		if hasRuntimeProvider {
			entry.LauncherRuntime = runtimeProvider.LauncherRuntime(env.Config, &l)
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

func refreshLauncherDependencies(pl platforms.Platform) error {
	refresher, ok := pl.(platforms.LauncherRefreshProvider)
	if !ok {
		return nil
	}
	if err := refresher.RefreshLauncherDependencies(); err != nil {
		return fmt.Errorf("refresh platform launcher dependencies: %w", err)
	}
	return nil
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

	if err = refreshLauncherDependencies(env.Platform); err != nil {
		log.Error().Err(err).Msg("error refreshing launcher dependencies")
		return nil, errors.New("error refreshing launcher dependencies")
	}
	env.LauncherCache.Refresh(env.Platform, env.Config)

	log.Info().Msg("launcher cache refreshed")
	return NoContent{}, nil
}
