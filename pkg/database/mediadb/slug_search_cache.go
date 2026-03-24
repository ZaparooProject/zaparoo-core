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
	"bytes"
	"context"
	"database/sql"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

const ctxCheckInterval = 10000

func formatBytes(b int) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// SlugSearchCache holds all slug data in memory for fast substring matching.
// It replaces SQL LIKE '%variant%' queries with in-memory bytes.Contains scans.
type SlugSearchCache struct {
	systemDBIDToID map[int64]string
	systemIDToDBID map[string]int64
	slugData       []byte
	slugOffsets    []uint32
	secSlugData    []byte
	secSlugOffsets []uint32
	titleDBIDs     []int64
	systemDBIDs    []int64
	entryCount     int
}

// Size returns the approximate memory footprint of the cache in bytes.
func (c *SlugSearchCache) Size() int {
	if c == nil {
		return 0
	}
	return len(c.slugData) + len(c.secSlugData) +
		len(c.slugOffsets)*4 + len(c.secSlugOffsets)*4 +
		len(c.titleDBIDs)*8 + len(c.systemDBIDs)*8
}

// buildSlugSearchCache reads all slug data from the database into an in-memory cache.
func buildSlugSearchCache(ctx context.Context, db *sql.DB) (*SlugSearchCache, error) {
	// Build system lookup maps
	systemRows, err := db.QueryContext(ctx, "SELECT DBID, SystemID FROM Systems")
	if err != nil {
		return nil, fmt.Errorf("failed to query systems: %w", err)
	}
	defer func() { _ = systemRows.Close() }()

	systemDBIDToID := make(map[int64]string)
	systemIDToDBID := make(map[string]int64)
	for systemRows.Next() {
		var dbid int64
		var systemID string
		if scanErr := systemRows.Scan(&dbid, &systemID); scanErr != nil {
			return nil, fmt.Errorf("failed to scan system row: %w", scanErr)
		}
		systemDBIDToID[dbid] = systemID
		systemIDToDBID[systemID] = dbid
	}
	if err = systemRows.Err(); err != nil {
		return nil, fmt.Errorf("system rows iteration error: %w", err)
	}

	// Read all MediaTitles
	titleRows, err := db.QueryContext(ctx,
		"SELECT DBID, SystemDBID, Slug, SecondarySlug FROM MediaTitles ORDER BY DBID")
	if err != nil {
		return nil, fmt.Errorf("failed to query media titles: %w", err)
	}
	defer func() { _ = titleRows.Close() }()

	cache := &SlugSearchCache{
		slugData:       make([]byte, 0, 1<<20),   // 1MB initial
		slugOffsets:    make([]uint32, 0, 1<<16), // 64K entries
		secSlugData:    make([]byte, 0, 1<<18),   // 256KB initial
		secSlugOffsets: make([]uint32, 0, 1<<16), // 64K entries
		titleDBIDs:     make([]int64, 0, 1<<16),  // 64K entries
		systemDBIDs:    make([]int64, 0, 1<<16),  // 64K entries
		systemDBIDToID: systemDBIDToID,
		systemIDToDBID: systemIDToDBID,
	}

	count := 0
	for titleRows.Next() {
		if count%ctxCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		var titleDBID, systemDBID int64
		var slug string
		var secSlug sql.NullString
		if scanErr := titleRows.Scan(&titleDBID, &systemDBID, &slug, &secSlug); scanErr != nil {
			return nil, fmt.Errorf("failed to scan title row: %w", scanErr)
		}

		//nolint:gosec // Safe: slug data won't exceed 4GB
		cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
		cache.slugData = append(cache.slugData, slug...)
		cache.slugData = append(cache.slugData, 0) // null separator

		//nolint:gosec // Safe: slug data won't exceed 4GB
		cache.secSlugOffsets = append(cache.secSlugOffsets, uint32(len(cache.secSlugData)))
		if secSlug.Valid && secSlug.String != "" {
			cache.secSlugData = append(cache.secSlugData, secSlug.String...)
			cache.secSlugData = append(cache.secSlugData, 0)
		}

		cache.titleDBIDs = append(cache.titleDBIDs, titleDBID)
		cache.systemDBIDs = append(cache.systemDBIDs, systemDBID)
		count++
	}
	if err = titleRows.Err(); err != nil {
		return nil, fmt.Errorf("title rows iteration error: %w", err)
	}

	// Sentinel offsets for bounds calculation
	//nolint:gosec // Safe: slug data won't exceed 4GB
	cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
	//nolint:gosec // Safe: slug data won't exceed 4GB
	cache.secSlugOffsets = append(cache.secSlugOffsets, uint32(len(cache.secSlugData)))
	cache.entryCount = count

	return cache, nil
}

// Search finds all title DBIDs matching the given system filter and variant groups.
// systemDBIDs is the system filter (empty = no filter). variantGroups is AND-of-ORs:
// each group is a set of byte variants for one query word (OR'd together),
// and all groups must match (AND'd together).
func (c *SlugSearchCache) Search(systemDBIDs []int64, variantGroups [][][]byte) []int64 {
	if c == nil || c.entryCount == 0 {
		return nil
	}

	systemSet := buildSystemSet(systemDBIDs)
	candidates := make([]int64, 0, min(c.entryCount, 1024))

	for i := range c.entryCount {
		if systemSet != nil {
			if _, ok := systemSet[c.systemDBIDs[i]]; !ok {
				continue
			}
		}

		slug := c.slugForEntry(i)
		secSlug := c.secSlugForEntry(i)

		// AND-of-ORs: every group must have at least one variant match
		allGroupsMatch := true
		for _, group := range variantGroups {
			groupMatched := false
			for _, variant := range group {
				if bytes.Contains(slug, variant) {
					groupMatched = true
					break
				}
				if len(secSlug) > 0 && bytes.Contains(secSlug, variant) {
					groupMatched = true
					break
				}
			}
			if !groupMatched {
				allGroupsMatch = false
				break
			}
		}

		if allGroupsMatch {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	}

	return candidates
}

// slugForEntry returns the slug bytes for entry i (excluding null separator).
func (c *SlugSearchCache) slugForEntry(i int) []byte {
	start := c.slugOffsets[i]
	end := c.slugOffsets[i+1]
	if end > start && c.slugData[end-1] == 0 {
		end--
	}
	return c.slugData[start:end]
}

// secSlugForEntry returns the secondary slug bytes for entry i (excluding null separator).
// Returns nil if the entry has no secondary slug.
func (c *SlugSearchCache) secSlugForEntry(i int) []byte {
	start := c.secSlugOffsets[i]
	end := c.secSlugOffsets[i+1]
	if end <= start {
		return nil
	}
	if c.secSlugData[end-1] == 0 {
		end--
	}
	return c.secSlugData[start:end]
}

// buildSystemSet converts a slice of system DBIDs into a fast lookup set.
// Returns nil when systemDBIDs is empty (meaning no filter).
func buildSystemSet(systemDBIDs []int64) map[int64]struct{} {
	if len(systemDBIDs) == 0 {
		return nil
	}
	set := make(map[int64]struct{}, len(systemDBIDs))
	for _, id := range systemDBIDs {
		set[id] = struct{}{}
	}
	return set
}

// ExactSlugMatch returns title DBIDs where the slug exactly matches the given bytes.
func (c *SlugSearchCache) ExactSlugMatch(systemDBIDs []int64, slug []byte) []int64 {
	if c == nil || c.entryCount == 0 {
		return nil
	}
	systemSet := buildSystemSet(systemDBIDs)
	var candidates []int64
	for i := range c.entryCount {
		if systemSet != nil {
			if _, ok := systemSet[c.systemDBIDs[i]]; !ok {
				continue
			}
		}
		if bytes.Equal(c.slugForEntry(i), slug) {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	}
	return candidates
}

// ExactSecondarySlugMatch returns title DBIDs where the secondary slug exactly matches the given bytes.
func (c *SlugSearchCache) ExactSecondarySlugMatch(systemDBIDs []int64, secSlug []byte) []int64 {
	if c == nil || c.entryCount == 0 {
		return nil
	}
	systemSet := buildSystemSet(systemDBIDs)
	var candidates []int64
	for i := range c.entryCount {
		if systemSet != nil {
			if _, ok := systemSet[c.systemDBIDs[i]]; !ok {
				continue
			}
		}
		entrySecSlug := c.secSlugForEntry(i)
		if len(entrySecSlug) > 0 && bytes.Equal(entrySecSlug, secSlug) {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	}
	return candidates
}

// PrefixSlugMatch returns title DBIDs where the slug starts with the given prefix.
func (c *SlugSearchCache) PrefixSlugMatch(systemDBIDs []int64, prefix []byte) []int64 {
	if c == nil || c.entryCount == 0 {
		return nil
	}
	systemSet := buildSystemSet(systemDBIDs)
	var candidates []int64
	for i := range c.entryCount {
		if systemSet != nil {
			if _, ok := systemSet[c.systemDBIDs[i]]; !ok {
				continue
			}
		}
		if bytes.HasPrefix(c.slugForEntry(i), prefix) {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	}
	return candidates
}

// ExactSlugMatchAny returns title DBIDs where the slug exactly matches any of the given slugs.
func (c *SlugSearchCache) ExactSlugMatchAny(systemDBIDs []int64, slugList [][]byte) []int64 {
	if c == nil || c.entryCount == 0 || len(slugList) == 0 {
		return nil
	}
	systemSet := buildSystemSet(systemDBIDs)
	// Build a set for O(1) lookups when the list is large enough
	slugSet := make(map[string]struct{}, len(slugList))
	for _, s := range slugList {
		slugSet[string(s)] = struct{}{}
	}
	var candidates []int64
	for i := range c.entryCount {
		if systemSet != nil {
			if _, ok := systemSet[c.systemDBIDs[i]]; !ok {
				continue
			}
		}
		if _, ok := slugSet[string(c.slugForEntry(i))]; ok {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	}
	return candidates
}

// RandomEntry picks a random title DBID from entries matching the system filter.
// Uses two passes to avoid allocating a candidates slice.
func (c *SlugSearchCache) RandomEntry(systemDBIDs []int64) (int64, bool) {
	if c == nil || c.entryCount == 0 {
		return 0, false
	}
	systemSet := buildSystemSet(systemDBIDs)

	// Pass 1: count matching entries
	var count int
	if systemSet == nil {
		count = c.entryCount
	} else {
		for i := range c.entryCount {
			if _, ok := systemSet[c.systemDBIDs[i]]; ok {
				count++
			}
		}
	}
	if count == 0 {
		return 0, false
	}

	// Pick a random index in [0, count)
	target, err := helpers.RandomInt(count)
	if err != nil {
		return 0, false
	}

	// Pass 2: walk to the target-th match
	seen := 0
	for i := range c.entryCount {
		if systemSet != nil {
			if _, ok := systemSet[c.systemDBIDs[i]]; !ok {
				continue
			}
		}
		if seen == target {
			return c.titleDBIDs[i], true
		}
		seen++
	}
	return 0, false
}

// ResolveSystemDBIDs converts system ID strings to database IDs using the cache's lookup map.
func (c *SlugSearchCache) ResolveSystemDBIDs(systemIDs []string) []int64 {
	if c == nil {
		return nil
	}
	result := make([]int64, 0, len(systemIDs))
	for _, id := range systemIDs {
		if dbid, ok := c.systemIDToDBID[id]; ok {
			result = append(result, dbid)
		}
	}
	return result
}

// RebuildSlugSearchCache builds or rebuilds the in-memory slug search cache.
func (db *MediaDB) RebuildSlugSearchCache() error {
	cache, err := buildSlugSearchCache(db.ctx, db.sql)
	if err != nil {
		return fmt.Errorf("failed to build slug search cache: %w", err)
	}
	db.slugSearchCache.Store(cache)
	log.Info().
		Int("entries", cache.entryCount).
		Str("size", formatBytes(cache.Size())).
		Msg("slug search cache built")
	return nil
}
