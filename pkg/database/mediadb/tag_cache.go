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
	labels := make(map[tagKey]string)
	order := make([]tagKey, 0)
	for _, sys := range systems {
		for _, tag := range c.bySystem[sys.ID] {
			k := tagKey{tag.Type, tag.Tag}
			if _, exists := counts[k]; !exists {
				order = append(order, k)
			}
			if labels[k] == "" {
				labels[k] = tag.Label
			}
			counts[k] += tag.Count
		}
	}

	result := make([]database.TagInfo, 0, len(order))
	for _, k := range order {
		result = append(result, database.TagInfo{
			Type: k.typ, Tag: k.tag, Label: labels[k], Count: counts[k],
		})
	}
	return result
}

// buildTagCache reads the SystemTagsCache table and builds both the allTags
// and bySystem views in a single pass. allTags accumulates counts across all
// systems so the global list reflects aggregate popularity.
func buildTagCache(ctx context.Context, db *sql.DB) (*tagCache, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT s.SystemID, stc.TagType, stc.Tag, t.DisplayName, stc.Count
		FROM SystemTagsCache stc
		JOIN Systems s ON stc.SystemDBID = s.DBID
		JOIN Tags t ON stc.TagDBID = t.DBID
		ORDER BY stc.TagType, stc.Tag`)
	if err != nil {
		return nil, fmt.Errorf("failed to query system tags cache: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cache := &tagCache{
		bySystem: make(map[string][]database.TagInfo),
	}

	allCounts := make(map[tagKey]int64)
	allLabels := make(map[tagKey]string)
	allOrder := make([]tagKey, 0)

	for rows.Next() {
		var systemID, tagType, tag, label string
		var count int64
		if err := rows.Scan(&systemID, &tagType, &tag, &label, &count); err != nil {
			return nil, fmt.Errorf("failed to scan tag cache row: %w", err)
		}

		unpadded := dbtags.UnpadTagValue(tag)
		ti := database.TagInfo{Type: tagType, Tag: unpadded, Label: label, Count: count}
		cache.bySystem[systemID] = append(cache.bySystem[systemID], ti)

		k := tagKey{tagType, unpadded}
		if _, exists := allCounts[k]; !exists {
			// Rows are ordered by type and tag, so global deduplication uses
			// the first non-empty label seen for each tag as its display label.
			allOrder = append(allOrder, k)
		}
		if allLabels[k] == "" {
			allLabels[k] = label
		}
		allCounts[k] += count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tag cache rows iteration error: %w", err)
	}

	cache.allTags = make([]database.TagInfo, 0, len(allOrder))
	for _, k := range allOrder {
		cache.allTags = append(cache.allTags, database.TagInfo{
			Type: k.typ, Tag: k.tag, Label: allLabels[k], Count: allCounts[k],
		})
	}

	return cache, nil
}

// RebuildTagCache builds or rebuilds the in-memory tag cache from the
// SystemTagsCache SQL table. Should be called after media indexing completes.
func (db *MediaDB) RebuildTagCache() error {
	cache, err := buildTagCache(db.ctx, db.sql.Load())
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

// RefreshScrapeTagCache brings the tag caches up to date with committed scrape
// writes. Scrapes change tags outside indexing, which is otherwise the only
// thing that refreshes SystemTagsCache, so without this media.tags keeps
// serving the pre-scrape tag set until the next reindex. It rebuilds the
// SystemTagsCache rows of the systems scrape writes touched, then the in-memory
// cache and its persisted snapshot. On failure the systems stay recorded so a
// later call retries them.
func (db *MediaDB) RefreshScrapeTagCache(ctx context.Context) error {
	systemIDs, all := db.consumeScrapeTagChanges()
	if len(systemIDs) == 0 && !all {
		return nil
	}

	var err error
	if all {
		err = db.PopulateSystemTagsCache(ctx)
	} else {
		systems := make([]systemdefs.System, 0, len(systemIDs))
		for _, systemID := range systemIDs {
			system, lookupErr := systemdefs.GetSystem(systemID)
			if lookupErr != nil {
				log.Debug().Err(lookupErr).Str("system", systemID).
					Msg("skipping unknown system in scrape tag cache refresh")
				continue
			}
			systems = append(systems, *system)
		}
		err = db.PopulateSystemTagsCacheForSystems(ctx, systems)
	}
	if err != nil {
		db.addScrapeTagSystems(systemIDs)
		if all {
			db.scrapeTagChangesMu.Lock()
			db.scrapeTagChangesAll = true
			db.scrapeTagChangesMu.Unlock()
		}
		return fmt.Errorf("failed to refresh system tags cache after scrape: %w", err)
	}

	if err := db.RebuildTagCache(); err != nil {
		// The in-memory cache would otherwise keep serving the pre-scrape
		// set; without it, reads fall through to the refreshed table.
		db.inMemoryTagCache.Store(nil)
		if persistErr := db.PersistTagCache(); persistErr != nil {
			log.Warn().Err(persistErr).Msg("failed to remove stale persisted tag cache after scrape")
		}
		return err
	}
	return db.PersistTagCache()
}
