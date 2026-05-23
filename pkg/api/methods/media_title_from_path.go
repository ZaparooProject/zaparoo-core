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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/slugs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
)

// HandleMediaTitleFromPath computes a MediaTitle from a system ID and path
// without touching the filesystem or database. Used to preview the title
// parsing and slug generation that the media scanner would produce.
func HandleMediaTitleFromPath(
	env requests.RequestEnv, //nolint:gocritic // single-use parameter in API handler
) (any, error) {
	var params models.MediaTitleFromPathParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	mediaType := slugs.MediaTypeGame
	if params.SystemID != "" {
		if system, err := systemdefs.GetSystem(params.SystemID); err == nil {
			mediaType = system.GetMediaType()
		}
	}

	pf := mediascanner.GetPathFragments(&mediascanner.PathFragmentParams{
		Config:    env.Config,
		Path:      params.Path,
		SystemID:  params.SystemID,
		MediaType: mediaType,
	})

	metadata := mediadb.GenerateSlugMetadataFromTokens(mediaType, pf.Title, pf.Slug, pf.SlugTokens)

	var secondarySlug *string
	if metadata.SecondarySlug != "" {
		s := metadata.SecondarySlug
		secondarySlug = &s
	}

	return models.MediaTitleFromPathResponse{
		Slug:          pf.Slug,
		Name:          pf.Title,
		SecondarySlug: secondarySlug,
		SlugLength:    metadata.SlugLength,
		SlugWordCount: metadata.SlugWordCount,
	}, nil
}
