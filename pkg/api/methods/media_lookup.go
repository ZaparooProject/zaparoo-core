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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/titles"
	"github.com/rs/zerolog/log"
)

func HandleMediaLookup(env requests.RequestEnv) (any, error) { //nolint:gocritic // single-use parameter in API handler
	var params models.MediaLookupParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	fuzzy := params.FuzzySystem != nil && *params.FuzzySystem
	system, err := resolveSystem(params.System, fuzzy)
	if err != nil {
		return nil, err
	}

	var launchers []platforms.Launcher
	if env.LauncherCache != nil {
		launchers = env.LauncherCache.GetLaunchersBySystem(system.ID)
	}

	ctx := env.State.GetContext()
	result, err := titles.ResolveTitle(ctx, &titles.ResolveParams{
		SystemID:  system.ID,
		GameName:  params.Name,
		MediaDB:   env.Database.MediaDB,
		Cfg:       env.Config,
		Launchers: launchers,
		MediaType: system.GetMediaType(),
	})
	if err != nil {
		if errors.Is(err, titles.ErrNoMatch) || errors.Is(err, titles.ErrLowConfidence) {
			return models.MediaLookupResponse{Match: nil}, nil
		}
		return nil, fmt.Errorf("title resolution failed: %w", err)
	}

	resultSystem := models.System{
		ID: system.ID,
	}
	metadata, metaErr := assets.GetSystemMetadata(system.ID)
	if metaErr != nil {
		resultSystem.Name = system.ID
		log.Err(metaErr).Msg("error getting system metadata")
	} else {
		resultSystem.Name = metadata.Name
		resultSystem.Category = metadata.Category
		if metadata.ReleaseDate != "" {
			resultSystem.ReleaseDate = &metadata.ReleaseDate
		}
		if metadata.Manufacturer != "" {
			resultSystem.Manufacturer = &metadata.Manufacturer
		}
	}

	zapScript := result.Result.ZapScript()

	return models.MediaLookupResponse{
		Match: &models.MediaLookupMatch{
			System:     resultSystem,
			Name:       result.Result.Name,
			Path:       result.Result.Path,
			ZapScript:  zapScript,
			Tags:       result.Result.Tags,
			Confidence: result.Confidence,
		},
	}, nil
}
