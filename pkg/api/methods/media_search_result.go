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
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/rs/zerolog/log"
)

// searchResultBuilder turns media rows into the result shape media.search
// returns, for every method that lists media. The durations record where a
// build spent its time, for the caller's step log.
type searchResultBuilder struct {
	env               *requests.RequestEnv
	launchableSystems map[string]launchables.VirtualSystem
	rootDirs          []string
	categoryResolver  config.CategoryResolver
	cover             time.Duration
	rows              time.Duration
	system            time.Duration
	zapScript         time.Duration
	relativePath      time.Duration
}

func newSearchResultBuilder(env *requests.RequestEnv) *searchResultBuilder {
	b := &searchResultBuilder{env: env, categoryResolver: systemCategoryResolver(env.Config)}
	if env.LauncherCache != nil && env.Platform != nil {
		b.rootDirs = env.Platform.RootDirs(env.Config)
	}
	if env.LauncherCache != nil {
		cachedSystems := env.LauncherCache.GetLaunchableSystems()
		b.launchableSystems = make(map[string]launchables.VirtualSystem, len(cachedSystems))
		for i := range cachedSystems {
			b.launchableSystems[launchables.EncodeID(cachedSystems[i].ID)] = cachedSystems[i]
		}
	}
	return b
}

// build returns one result per row, in order, with cover status and colour
// attached where the database answers in time.
func (b *searchResultBuilder) build(
	ctx context.Context, rows []database.SearchResultWithCursor,
) []models.SearchResultMedia {
	coverStarted := time.Now()
	coverStatuses := make(map[int64]bool)
	// Unknown must remain true in the response so clients do not suppress a
	// valid image request merely because optional enrichment timed out.
	coverStatusesKnown := false
	if len(rows) > 0 && b.env.Database != nil {
		coverRefs := make([]database.MediaRef, len(rows))
		for i := range rows {
			coverRefs[i] = database.MediaRef{
				MediaDBID:      rows[i].MediaID,
				MediaTitleDBID: rows[i].MediaTitleID,
			}
		}
		coverCtx, cancelCoverLookup := optionalDBEnrichmentContext(ctx)
		resolvedCoverStatuses, coverErr := b.env.Database.MediaDB.GetMediaCoverStatus(coverCtx, coverRefs)
		cancelCoverLookup()
		if coverErr != nil {
			log.Debug().Err(coverErr).Msg("could not enrich media search cover status")
		} else {
			coverStatuses = resolvedCoverStatuses
			coverStatusesKnown = true
		}
	}
	b.cover += time.Since(coverStarted)

	rowsStarted := time.Now()
	results := make([]models.SearchResultMedia, 0, len(rows))
	for i := range rows {
		result := &rows[i]
		stageStarted := time.Now()
		resultSystem := searchResultSystem(result.SystemID, b.launchableSystems, b.categoryResolver)
		b.system += time.Since(stageStarted)

		stageStarted = time.Now()
		zapScript := result.ZapScript()
		b.zapScript += time.Since(stageStarted)

		stageStarted = time.Now()
		var relPath *string
		if b.env.LauncherCache != nil {
			rel := b.env.LauncherCache.ToRelativePath(b.rootDirs, result.SystemID, result.Path)
			if rel != result.Path {
				relPath = &rel
			}
		}
		b.relativePath += time.Since(stageStarted)

		hasCover := true
		if coverStatusesKnown {
			hasCover = coverStatuses[result.MediaID]
		}
		results = append(results, models.SearchResultMedia{
			MediaID:            result.MediaID,
			TitleID:            result.MediaTitleID,
			RelPath:            relPath,
			HasCover:           hasCover,
			System:             resultSystem,
			Name:               result.Name,
			Path:               result.Path,
			ZapScript:          zapScript,
			Tags:               result.Tags,
			DisambiguatingTags: result.ZapScriptTags,
		})
	}

	if b.env.Database != nil && len(results) > 0 {
		ids := make([]int64, len(results))
		for i := range results {
			ids[i] = results[i].MediaID
		}
		if colors := mediaCoverColors(ctx, b.env.Database.MediaDB, ids); len(colors) > 0 {
			for i := range results {
				results[i].CoverColor = colors[results[i].MediaID]
			}
		}
	}
	b.rows += time.Since(rowsStarted)
	return results
}
