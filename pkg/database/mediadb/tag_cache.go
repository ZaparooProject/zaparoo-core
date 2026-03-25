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

package mediadb

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/rs/zerolog/log"
)

// tagCache holds pre-computed tag lists in memory for instant lookups.
// Built from the SystemTagsCache SQL table after media indexing completes.
type tagCache struct {
	bySystem map[string][]database.TagInfo
	allTags  []database.TagInfo
}

// tagsForSystems collects and deduplicates tags across the requested systems.
func (c *tagCache) tagsForSystems(systems []systemdefs.System) []database.TagInfo {
	if len(systems) == 1 {
		return slices.Clone(c.bySystem[systems[0].ID])
	}

	seen := make(map[database.TagInfo]struct{})
	var result []database.TagInfo
	for _, sys := range systems {
		for _, tag := range c.bySystem[sys.ID] {
			if _, exists := seen[tag]; !exists {
				seen[tag] = struct{}{}
				result = append(result, tag)
			}
		}
	}
	return result
}

// buildTagCache reads the SystemTagsCache table and builds both the allTags
// and bySystem views in a single pass.
func buildTagCache(ctx context.Context, db *sql.DB) (*tagCache, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.SystemID, stc.TagType, stc.Tag
		FROM SystemTagsCache stc
		JOIN Systems s ON stc.SystemDBID = s.DBID
		ORDER BY stc.TagType, stc.Tag`)
	if err != nil {
		return nil, fmt.Errorf("failed to query system tags cache: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cache := &tagCache{
		bySystem: make(map[string][]database.TagInfo),
	}
	seen := make(map[database.TagInfo]struct{})

	for rows.Next() {
		var systemID, tagType, tag string
		if err := rows.Scan(&systemID, &tagType, &tag); err != nil {
			return nil, fmt.Errorf("failed to scan tag cache row: %w", err)
		}

		ti := database.TagInfo{Type: tagType, Tag: tag}
		cache.bySystem[systemID] = append(cache.bySystem[systemID], ti)

		if _, exists := seen[ti]; !exists {
			seen[ti] = struct{}{}
			cache.allTags = append(cache.allTags, ti)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tag cache rows iteration error: %w", err)
	}

	return cache, nil
}

// RebuildTagCache builds or rebuilds the in-memory tag cache from the
// SystemTagsCache SQL table. Should be called after media indexing completes.
func (db *MediaDB) RebuildTagCache() error {
	cache, err := buildTagCache(db.ctx, db.sql)
	if err != nil {
		return fmt.Errorf("failed to build tag cache: %w", err)
	}
	db.inMemoryTagCache.Store(cache)
	log.Info().
		Int("tags", len(cache.allTags)).
		Int("systems", len(cache.bySystem)).
		Msg("tag cache built")
	return nil
}
