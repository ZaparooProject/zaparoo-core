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
	dbtags "github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/rs/zerolog/log"
)

// tagKey uniquely identifies a tag by its type and value. Used as a map key
// to deduplicate and accumulate counts across multiple systems.
type tagKey struct{ typ, tag string }

// tagCache holds pre-computed tag lists in memory for instant lookups.
// Built from the SystemTagsCache SQL table after media indexing completes.
type tagCache struct {
	bySystem map[string][]database.TagInfo
	allTags  []database.TagInfo
}

// tagsForSystems collects and deduplicates tags across the requested systems,
// summing counts when the same (type, tag) appears in multiple systems.
func (c *tagCache) tagsForSystems(systems []systemdefs.System) []database.TagInfo {
	if len(systems) == 1 {
		first := systems[0] //nolint:gosec // G602 false positive: len==1 guarantees valid index
		tagList := c.bySystem[first.ID]
		if tagList == nil {
			return []database.TagInfo{}
		}
		return slices.Clone(tagList)
	}

	counts := make(map[tagKey]int64)
	order := make([]tagKey, 0)
	for _, sys := range systems {
		for _, tag := range c.bySystem[sys.ID] {
			k := tagKey{tag.Type, tag.Tag}
			if _, exists := counts[k]; !exists {
				order = append(order, k)
			}
			counts[k] += tag.Count
		}
	}

	result := make([]database.TagInfo, 0, len(order))
	for _, k := range order {
		result = append(result, database.TagInfo{Type: k.typ, Tag: k.tag, Count: counts[k]})
	}
	return result
}

// buildTagCache reads the SystemTagsCache table and builds both the allTags
// and bySystem views in a single pass. allTags accumulates counts across all
// systems so the global list reflects aggregate popularity.
func buildTagCache(ctx context.Context, db *sql.DB) (*tagCache, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.SystemID, stc.TagType, stc.Tag, stc.Count
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

	allCounts := make(map[tagKey]int64)
	allOrder := make([]tagKey, 0)

	for rows.Next() {
		var systemID, tagType, tag string
		var count int64
		if err := rows.Scan(&systemID, &tagType, &tag, &count); err != nil {
			return nil, fmt.Errorf("failed to scan tag cache row: %w", err)
		}

		unpadded := dbtags.UnpadTagValue(tag)
		ti := database.TagInfo{Type: tagType, Tag: unpadded, Count: count}
		cache.bySystem[systemID] = append(cache.bySystem[systemID], ti)

		k := tagKey{tagType, unpadded}
		if _, exists := allCounts[k]; !exists {
			allOrder = append(allOrder, k)
		}
		allCounts[k] += count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tag cache rows iteration error: %w", err)
	}

	cache.allTags = make([]database.TagInfo, 0, len(allOrder))
	for _, k := range allOrder {
		cache.allTags = append(cache.allTags, database.TagInfo{
			Type: k.typ, Tag: k.tag, Count: allCounts[k],
		})
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
	if len(cache.allTags) == 0 {
		log.Debug().Msg("tag cache is empty after rebuild, falling back to SQL")
		db.inMemoryTagCache.Store(nil)
		return nil
	}
	db.inMemoryTagCache.Store(cache)
	log.Info().
		Int("tags", len(cache.allTags)).
		Int("systems", len(cache.bySystem)).
		Msg("tag cache built")
	return nil
}
