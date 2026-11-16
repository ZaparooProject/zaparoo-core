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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/filters"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/rs/zerolog/log"
)

const defaultMediaArtworkRandomLimit = 10

func parseMediaArtworkRandomParams(raw json.RawMessage) (*models.MediaArtworkRandomParams, error) {
	var params models.MediaArtworkRandomParams
	if len(raw) == 0 {
		return nil, models.ClientErrf("invalid params: imageTypes is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if err := validation.DefaultValidator.Validate(&params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	for _, imageType := range params.ImageTypes {
		if _, ok := resolveImageTypeTag(imageType); !ok {
			return nil, models.ClientErrf("invalid params: unknown image type %q", imageType)
		}
	}
	return &params, nil
}

// HandleMediaArtworkRandom returns random media that have indexed artwork of
// the requested types, for slideshows and screensavers. It reads only: nothing
// is launched, fetched, or recorded in history.
//
//nolint:gocritic // single-use parameter in API handler
func HandleMediaArtworkRandom(env requests.RequestEnv) (any, error) {
	params, err := parseMediaArtworkRandomParams(env.Params)
	if err != nil {
		return nil, err
	}

	var systemIDs []string
	if params.Systems != nil && len(*params.Systems) > 0 {
		fuzzy := params.FuzzySystem != nil && *params.FuzzySystem
		systems, resolveErr := resolveSystems(*params.Systems, fuzzy)
		if resolveErr != nil {
			return nil, resolveErr
		}
		systemIDs = make([]string, len(systems))
		for i := range systems {
			systemIDs[i] = systems[i].ID
		}
	}

	var tagFilters []zapscript.TagFilter
	if len(params.Tags) > 0 {
		tagFilters, err = filters.ParseTagFilters(params.Tags)
		if err != nil {
			return nil, models.ClientErrf("failed to parse tag filters: %w", err)
		}
	}

	var pathPrefix string
	if params.PathPrefix != nil {
		pathPrefix = *params.PathPrefix
	}
	limit := defaultMediaArtworkRandomLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	typeTags := make([]string, len(params.ImageTypes))
	shortNames := make(map[string]string, len(params.ImageTypes))
	for i, imageType := range params.ImageTypes {
		typeTag, _ := resolveImageTypeTag(imageType)
		typeTags[i] = typeTag
		shortNames[typeTag] = imageType
	}

	if env.Database == nil || env.Database.MediaDB == nil {
		return nil, errors.New("media database is not available")
	}

	endSlot := apidiag.Begin(env.Context, apidiag.ConcurrencySlot)
	select {
	case searchSem <- struct{}{}:
		defer func() { <-searchSem }()
	case <-env.Context.Done():
		endSlot()
		return nil, env.Context.Err()
	}
	endSlot()

	samples, err := env.Database.MediaDB.SampleArtworkMedia(env.Context, &database.ArtworkSampleQuery{
		MediaQuery: database.MediaQuery{
			Systems:    systemIDs,
			PathPrefix: pathPrefix,
			Tags:       tagFilters,
		},
		ImageTypes:    typeTags,
		ExcludeDBIDs:  params.ExcludeIDs,
		Limit:         limit,
		ExcludeHidden: !filters.IncludesHidden(tagFilters, params.IncludeHidden),
	})
	if err != nil {
		if !apidiag.IsContextFailure(env.Context, err) {
			log.Error().Err(err).Msg("error sampling artwork media")
		}
		return nil, fmt.Errorf("error sampling artwork media: %w", err)
	}

	var launchableSystems map[string]launchables.VirtualSystem
	if env.LauncherCache != nil {
		cachedSystems := env.LauncherCache.GetLaunchableSystems()
		launchableSystems = make(map[string]launchables.VirtualSystem, len(cachedSystems))
		for i := range cachedSystems {
			launchableSystems[launchables.EncodeID(cachedSystems[i].ID)] = cachedSystems[i]
		}
	}
	categoryResolver := systemCategoryResolver(env.Config)

	items := make([]models.MediaArtworkRandomItem, 0, len(samples))
	for i := range samples {
		imageTypes := make([]string, 0, len(samples[i].ImageTypes))
		for _, typeTag := range samples[i].ImageTypes {
			if name, ok := shortNames[typeTag]; ok {
				imageTypes = append(imageTypes, name)
			}
		}
		items = append(items, models.MediaArtworkRandomItem{
			MediaID:    samples[i].MediaID,
			System:     searchResultSystem(samples[i].SystemID, launchableSystems, categoryResolver),
			Name:       samples[i].Name,
			Path:       samples[i].Path,
			ImageTypes: imageTypes,
		})
	}
	return models.MediaArtworkRandomResponse{Items: items}, nil
}
