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
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/rs/zerolog/log"
)

const (
	ctxCheckInterval = 10000

	// Trigram index constants. Slugs use a 37-char alphabet: 0-9, a-z, and '-'.
	trigramAlphabetSize = 37
	trigramCount        = trigramAlphabetSize * trigramAlphabetSize * trigramAlphabetSize // 50,653

	// trigramMaxIntersect is the maximum number of trigrams to intersect per
	// query variant. Using the rarest trigrams first keeps intersection cheap.
	trigramMaxIntersect = 4

	// trigramMaxFreqPct is the maximum frequency (as percentage of entry count)
	// for a trigram to be included in the index. Trigrams appearing in more
	// entries than this threshold are "capped" — not indexed because they
	// provide diminishing selectivity at high memory cost.
	trigramMaxFreqPct = 50
)

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
// Entries are sorted by systemDBID for contiguous system-filtered scans.
// A trigram inverted index accelerates unfiltered substring searches.
type SlugSearchCache struct {
	systemDBIDToID  map[int64]string
	systemIDToDBID  map[string]int64
	systemRanges    map[int64][2]int
	coveredSystems  map[string]struct{}
	secSlugOffsets  []uint32
	slugOffsets     []uint32
	secSlugData     []byte
	slugData        []byte
	titleDBIDs      []int64
	systemDBIDs     []int64
	trigramOffsets  []uint32
	trigramPostings []uint32
	trigramCapped   []bool
	entryCount      int
	complete        bool
}

// Size returns the approximate memory footprint of the cache in bytes.
func (c *SlugSearchCache) Size() int {
	if c == nil {
		return 0
	}
	return len(c.slugData) + len(c.secSlugData) +
		len(c.slugOffsets)*4 + len(c.secSlugOffsets)*4 +
		len(c.titleDBIDs)*8 + len(c.systemDBIDs)*8 +
		len(c.trigramOffsets)*4 + len(c.trigramPostings)*4 +
		len(c.trigramCapped)
}

// ---------- Build ----------

// buildSlugSearchCache reads all slug data from the database into an in-memory cache.
func buildSlugSearchCache(ctx context.Context, db *sql.DB) (*SlugSearchCache, error) {
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

	titleRows, err := db.QueryContext(ctx,
		"SELECT DBID, SystemDBID, Slug, SecondarySlug FROM MediaTitles ORDER BY DBID")
	if err != nil {
		return nil, fmt.Errorf("failed to query media titles: %w", err)
	}
	defer func() { _ = titleRows.Close() }()

	cache := &SlugSearchCache{
		slugData:       make([]byte, 0, 1<<20),
		slugOffsets:    make([]uint32, 0, 1<<16),
		secSlugData:    make([]byte, 0, 1<<18),
		secSlugOffsets: make([]uint32, 0, 1<<16),
		titleDBIDs:     make([]int64, 0, 1<<16),
		systemDBIDs:    make([]int64, 0, 1<<16),
		systemDBIDToID: systemDBIDToID,
		systemIDToDBID: systemIDToDBID,
		complete:       true,
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
		cache.slugData = append(cache.slugData, 0)

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

	//nolint:gosec // Safe: slug data won't exceed 4GB
	cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
	//nolint:gosec // Safe: slug data won't exceed 4GB
	cache.secSlugOffsets = append(cache.secSlugOffsets, uint32(len(cache.secSlugData)))
	cache.entryCount = count

	finalizeCache(cache)
	return cache, nil
}

func buildSlugSearchCacheForSystems(ctx context.Context, db *sql.DB, systemIDs []string) (*SlugSearchCache, error) {
	coverage := normalizeCacheSystemIDs(systemIDs)
	cache := &SlugSearchCache{
		slugData:       make([]byte, 0, 1<<18),
		slugOffsets:    make([]uint32, 0, 1<<14),
		secSlugData:    make([]byte, 0, 1<<16),
		secSlugOffsets: make([]uint32, 0, 1<<14),
		titleDBIDs:     make([]int64, 0, 1<<14),
		systemDBIDs:    make([]int64, 0, 1<<14),
		systemDBIDToID: make(map[int64]string),
		systemIDToDBID: make(map[string]int64),
		coveredSystems: coverage,
	}

	if len(coverage) == 0 {
		finalizeCache(cache)
		return cache, nil
	}

	ids := sortedCoveredSystems(coverage)
	placeholders := prepareVariadic("?", ",", len(ids))
	args := make([]any, len(ids))
	for i, systemID := range ids {
		args[i] = systemID
	}

	//nolint:gosec // Placeholder list is generated internally to match bound args.
	querySystems := fmt.Sprintf(
		"SELECT DBID, SystemID FROM Systems WHERE SystemID IN (%s)",
		placeholders,
	)
	systemRows, err := db.QueryContext(ctx, querySystems, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query systems for selective slug cache: %w", err)
	}
	defer func() { _ = systemRows.Close() }()

	for systemRows.Next() {
		var dbid int64
		var systemID string
		if scanErr := systemRows.Scan(&dbid, &systemID); scanErr != nil {
			return nil, fmt.Errorf("failed to scan selective system row: %w", scanErr)
		}
		cache.systemDBIDToID[dbid] = systemID
		cache.systemIDToDBID[systemID] = dbid
	}
	if err = systemRows.Err(); err != nil {
		return nil, fmt.Errorf("selective system rows iteration error: %w", err)
	}

	//nolint:gosec // Placeholder list is generated internally to match bound args.
	queryTitles := fmt.Sprintf(`
		SELECT mt.DBID, mt.SystemDBID, mt.Slug, mt.SecondarySlug
		FROM MediaTitles mt
		JOIN Systems s ON mt.SystemDBID = s.DBID
		WHERE s.SystemID IN (%s)
		ORDER BY mt.DBID`, placeholders)
	titleRows, err := db.QueryContext(ctx, queryTitles, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query selective media titles: %w", err)
	}
	defer func() { _ = titleRows.Close() }()

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
			return nil, fmt.Errorf("failed to scan selective title row: %w", scanErr)
		}

		//nolint:gosec // Safe: slug data won't exceed 4GB
		cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
		cache.slugData = append(cache.slugData, slug...)
		cache.slugData = append(cache.slugData, 0)

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
		return nil, fmt.Errorf("selective title rows iteration error: %w", err)
	}

	cache.entryCount = count
	finalizeCache(cache)
	return cache, nil
}

// finalizeCache sorts entries by system, builds system ranges, and builds the
// trigram index. Called after raw data population by both production and test paths.
func finalizeCache(cache *SlugSearchCache) {
	if cache.coveredSystems == nil && !cache.complete {
		cache.coveredSystems = make(map[string]struct{})
	}

	cache.trigramOffsets = nil
	cache.trigramPostings = nil
	cache.trigramCapped = nil

	if cache.entryCount == 0 {
		cache.systemRanges = make(map[int64][2]int)
		if len(cache.slugOffsets) == 0 {
			cache.slugOffsets = []uint32{0}
		}
		if len(cache.secSlugOffsets) == 0 {
			cache.secSlugOffsets = []uint32{0}
		}
		return
	}

	if len(cache.slugOffsets) == cache.entryCount {
		//nolint:gosec // Safe: slug data won't exceed 4GB
		cache.slugOffsets = append(cache.slugOffsets, uint32(len(cache.slugData)))
	}
	if len(cache.secSlugOffsets) == cache.entryCount {
		//nolint:gosec // Safe: slug data won't exceed 4GB
		cache.secSlugOffsets = append(cache.secSlugOffsets, uint32(len(cache.secSlugData)))
	}

	sortCacheBySystem(cache)
	cache.systemRanges = buildSystemRanges(cache.systemDBIDs, cache.entryCount)
	buildTrigramIndex(cache)

	cache.slugData = slices.Clip(cache.slugData)
	cache.slugOffsets = slices.Clip(cache.slugOffsets)
	cache.secSlugData = slices.Clip(cache.secSlugData)
	cache.secSlugOffsets = slices.Clip(cache.secSlugOffsets)
	cache.titleDBIDs = slices.Clip(cache.titleDBIDs)
	cache.systemDBIDs = slices.Clip(cache.systemDBIDs)
	cache.trigramPostings = slices.Clip(cache.trigramPostings)
	cache.trigramCapped = slices.Clip(cache.trigramCapped)
}

func normalizeCacheSystemIDs(systemIDs []string) map[string]struct{} {
	if len(systemIDs) == 0 {
		return make(map[string]struct{})
	}
	covered := make(map[string]struct{}, len(systemIDs))
	for _, systemID := range systemIDs {
		if systemID == "" {
			continue
		}
		covered[systemID] = struct{}{}
	}
	return covered
}

func sortedCoveredSystems(covered map[string]struct{}) []string {
	ids := make([]string, 0, len(covered))
	for systemID := range covered {
		ids = append(ids, systemID)
	}
	slices.Sort(ids)
	return ids
}

func (c *SlugSearchCache) CanServeSystems(systemIDs []string) bool {
	if c == nil {
		return false
	}
	if c.complete {
		return true
	}
	if len(systemIDs) == 0 {
		return false
	}
	for _, systemID := range systemIDs {
		if _, ok := c.coveredSystems[systemID]; !ok {
			return false
		}
	}
	return true
}

func (c *SlugSearchCache) withoutSystems(systemIDs []string) *SlugSearchCache {
	if c == nil {
		return nil
	}

	remove := normalizeCacheSystemIDs(systemIDs)
	if len(remove) == 0 {
		return c
	}

	trimmed := &SlugSearchCache{
		slugData:       make([]byte, 0, len(c.slugData)),
		slugOffsets:    make([]uint32, 0, len(c.slugOffsets)),
		secSlugData:    make([]byte, 0, len(c.secSlugData)),
		secSlugOffsets: make([]uint32, 0, len(c.secSlugOffsets)),
		titleDBIDs:     make([]int64, 0, len(c.titleDBIDs)),
		systemDBIDs:    make([]int64, 0, len(c.systemDBIDs)),
		systemDBIDToID: make(map[int64]string),
		systemIDToDBID: make(map[string]int64),
		coveredSystems: make(map[string]struct{}),
	}

	if c.complete {
		for systemID := range c.systemIDToDBID {
			if _, drop := remove[systemID]; !drop {
				trimmed.coveredSystems[systemID] = struct{}{}
			}
		}
	} else {
		for systemID := range c.coveredSystems {
			if _, drop := remove[systemID]; !drop {
				trimmed.coveredSystems[systemID] = struct{}{}
			}
		}
	}

	for dbid, systemID := range c.systemDBIDToID {
		if _, drop := remove[systemID]; drop {
			continue
		}
		trimmed.systemDBIDToID[dbid] = systemID
		trimmed.systemIDToDBID[systemID] = dbid
	}

	for i := range c.entryCount {
		systemID := c.systemDBIDToID[c.systemDBIDs[i]]
		if _, drop := remove[systemID]; drop {
			continue
		}
		appendSlugCacheEntry(trimmed, c, i)
	}

	trimmed.entryCount = len(trimmed.titleDBIDs)
	finalizeCache(trimmed)
	return trimmed
}

func mergeSlugSearchCaches(base, replacement *SlugSearchCache) *SlugSearchCache {
	if replacement == nil {
		return base
	}
	if replacement.complete || base == nil {
		return replacement
	}

	merged := &SlugSearchCache{
		slugData:       make([]byte, 0, len(base.slugData)+len(replacement.slugData)),
		slugOffsets:    make([]uint32, 0, len(base.slugOffsets)+len(replacement.slugOffsets)),
		secSlugData:    make([]byte, 0, len(base.secSlugData)+len(replacement.secSlugData)),
		secSlugOffsets: make([]uint32, 0, len(base.secSlugOffsets)+len(replacement.secSlugOffsets)),
		titleDBIDs:     make([]int64, 0, len(base.titleDBIDs)+len(replacement.titleDBIDs)),
		systemDBIDs:    make([]int64, 0, len(base.systemDBIDs)+len(replacement.systemDBIDs)),
		systemDBIDToID: make(map[int64]string),
		systemIDToDBID: make(map[string]int64),
		complete:       base.complete,
	}

	if merged.complete {
		merged.coveredSystems = nil
	} else {
		merged.coveredSystems = make(map[string]struct{})
		for systemID := range base.coveredSystems {
			merged.coveredSystems[systemID] = struct{}{}
		}
		for systemID := range replacement.coveredSystems {
			merged.coveredSystems[systemID] = struct{}{}
		}
	}

	replace := replacement.coveredSystems
	for dbid, systemID := range base.systemDBIDToID {
		if _, drop := replace[systemID]; drop {
			continue
		}
		merged.systemDBIDToID[dbid] = systemID
		merged.systemIDToDBID[systemID] = dbid
	}
	for dbid, systemID := range replacement.systemDBIDToID {
		merged.systemDBIDToID[dbid] = systemID
		merged.systemIDToDBID[systemID] = dbid
	}

	for i := range base.entryCount {
		systemID := base.systemDBIDToID[base.systemDBIDs[i]]
		if _, drop := replace[systemID]; drop {
			continue
		}
		appendSlugCacheEntry(merged, base, i)
	}
	for i := range replacement.entryCount {
		appendSlugCacheEntry(merged, replacement, i)
	}

	merged.entryCount = len(merged.titleDBIDs)
	finalizeCache(merged)
	return merged
}

func appendSlugCacheEntry(dst, src *SlugSearchCache, i int) {
	//nolint:gosec // Safe: slug data won't exceed 4GB
	dst.slugOffsets = append(dst.slugOffsets, uint32(len(dst.slugData)))
	dst.slugData = append(dst.slugData, src.slugForEntry(i)...)
	dst.slugData = append(dst.slugData, 0)

	//nolint:gosec // Safe: slug data won't exceed 4GB
	dst.secSlugOffsets = append(dst.secSlugOffsets, uint32(len(dst.secSlugData)))
	if sec := src.secSlugForEntry(i); len(sec) > 0 {
		dst.secSlugData = append(dst.secSlugData, sec...)
		dst.secSlugData = append(dst.secSlugData, 0)
	}

	dst.titleDBIDs = append(dst.titleDBIDs, src.titleDBIDs[i])
	dst.systemDBIDs = append(dst.systemDBIDs, src.systemDBIDs[i])
}

// sortCacheBySystem reorders all parallel arrays so entries are grouped by
// systemDBID. Uses a stable sort to preserve within-system DBID ordering.
func sortCacheBySystem(cache *SlugSearchCache) {
	n := cache.entryCount
	if n <= 1 {
		return
	}

	// Skip if already sorted.
	sorted := true
	for i := 1; i < n; i++ {
		if cache.systemDBIDs[i] < cache.systemDBIDs[i-1] {
			sorted = false
			break
		}
	}
	if sorted {
		return
	}

	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	slices.SortStableFunc(indices, func(a, b int) int {
		return cmp.Compare(cache.systemDBIDs[a], cache.systemDBIDs[b])
	})

	// Rebuild slug data in sorted order for contiguous memory access per system.
	newSlugData := make([]byte, 0, len(cache.slugData))
	newSlugOffsets := make([]uint32, 0, n+1)
	newSecSlugData := make([]byte, 0, len(cache.secSlugData))
	newSecSlugOffsets := make([]uint32, 0, n+1)
	newTitleDBIDs := make([]int64, n)
	newSystemDBIDs := make([]int64, n)

	for newIdx, oldIdx := range indices {
		//nolint:gosec // Safe: slug data won't exceed 4GB
		newSlugOffsets = append(newSlugOffsets, uint32(len(newSlugData)))
		newSlugData = append(newSlugData, cache.slugData[cache.slugOffsets[oldIdx]:cache.slugOffsets[oldIdx+1]]...)

		//nolint:gosec // Safe: slug data won't exceed 4GB
		newSecSlugOffsets = append(newSecSlugOffsets, uint32(len(newSecSlugData)))
		secStart := cache.secSlugOffsets[oldIdx]
		secEnd := cache.secSlugOffsets[oldIdx+1]
		if secEnd > secStart {
			newSecSlugData = append(newSecSlugData, cache.secSlugData[secStart:secEnd]...)
		}

		newTitleDBIDs[newIdx] = cache.titleDBIDs[oldIdx]
		newSystemDBIDs[newIdx] = cache.systemDBIDs[oldIdx]
	}

	//nolint:gosec // Safe: slug data won't exceed 4GB
	newSlugOffsets = append(newSlugOffsets, uint32(len(newSlugData)))
	//nolint:gosec // Safe: slug data won't exceed 4GB
	newSecSlugOffsets = append(newSecSlugOffsets, uint32(len(newSecSlugData)))

	cache.slugData = newSlugData
	cache.slugOffsets = newSlugOffsets
	cache.secSlugData = newSecSlugData
	cache.secSlugOffsets = newSecSlugOffsets
	cache.titleDBIDs = newTitleDBIDs
	cache.systemDBIDs = newSystemDBIDs
}

// buildSystemRanges scans sorted systemDBIDs and returns a map from each
// systemDBID to its [start, end) index range.
func buildSystemRanges(systemDBIDs []int64, n int) map[int64][2]int {
	ranges := make(map[int64][2]int)
	if n == 0 {
		return ranges
	}
	start := 0
	cur := systemDBIDs[0]
	for i := 1; i < n; i++ {
		if systemDBIDs[i] != cur {
			ranges[cur] = [2]int{start, i}
			cur = systemDBIDs[i]
			start = i
		}
	}
	ranges[cur] = [2]int{start, n}
	return ranges
}

// ---------- Trigram index ----------

// trigramCharIndex maps a slug byte to its index in the trigram alphabet.
// Returns -1 for bytes outside the alphabet.
func trigramCharIndex(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'z':
		return int(b-'a') + 10
	case b == '-':
		return 36
	default:
		return -1
	}
}

// encodeTrigramID converts three consecutive slug bytes into a flat index
// in [0, trigramCount). Returns (0, false) if any byte is outside the alphabet.
func encodeTrigramID(b0, b1, b2 byte) (uint32, bool) {
	i0 := trigramCharIndex(b0)
	if i0 < 0 {
		return 0, false
	}
	i1 := trigramCharIndex(b1)
	if i1 < 0 {
		return 0, false
	}
	i2 := trigramCharIndex(b2)
	if i2 < 0 {
		return 0, false
	}
	//nolint:gosec // Safe: max value is 36*37*37+36*37+36 = 50,652
	return uint32(i0*trigramAlphabetSize*trigramAlphabetSize + i1*trigramAlphabetSize + i2), true
}

// buildTrigramIndex constructs the trigram inverted index using two passes
// over the slug data. The first pass counts per-trigram entries; the second
// fills a single contiguous posting array.
// Fixed memory: ~250KB for trigramOffsets + trigramCapped (independent of entry count).
// Variable memory: trigramPostings grows with unique trigram-entry pairs.
func buildTrigramIndex(cache *SlugSearchCache) {
	n := cache.entryCount
	if n == 0 {
		return
	}

	// Count pass: for each trigram, count how many entries contain it.
	// lastSeen tracks the last entry index that incremented a trigram count
	// to deduplicate within an entry without per-entry allocation.
	lastSeen := make([]int32, trigramCount)
	for i := range lastSeen {
		lastSeen[i] = -1
	}
	trigramCounts := make([]uint32, trigramCount)

	for i := range n {
		//nolint:gosec // Safe: entryCount < 2^31
		idx := int32(i)
		countEntryTrigrams(cache.slugForEntry(i), idx, lastSeen, trigramCounts)
		if sec := cache.secSlugForEntry(i); len(sec) > 0 {
			countEntryTrigrams(sec, idx, lastSeen, trigramCounts)
		}
	}

	// Frequency cap: skip trigrams that appear in too many entries.
	// These consume memory without providing useful selectivity.
	//nolint:gosec // Safe: entryCount < 2^31
	threshold := uint32(cache.entryCount * trigramMaxFreqPct / 100)
	if threshold < 1 {
		threshold = 1
	}
	cache.trigramCapped = make([]bool, trigramCount)
	for t := range trigramCount {
		if trigramCounts[t] > threshold {
			cache.trigramCapped[t] = true
			trigramCounts[t] = 0
		}
	}

	// Prefix sum: convert counts to offsets.
	cache.trigramOffsets = make([]uint32, trigramCount+1)
	var total uint32
	for t := range trigramCount {
		cache.trigramOffsets[t] = total
		total += trigramCounts[t]
	}
	cache.trigramOffsets[trigramCount] = total

	if total == 0 {
		return
	}

	// Fill pass: write entry indices into posting lists.
	cache.trigramPostings = make([]uint32, total)
	writeCursors := make([]uint32, trigramCount)
	copy(writeCursors, cache.trigramOffsets[:trigramCount])

	for i := range lastSeen {
		lastSeen[i] = -1
	}

	for i := range n {
		//nolint:gosec // Safe: entryCount < 2^31 and < 2^32
		idx32 := int32(i)
		//nolint:gosec // Safe: entryCount < 2^32
		uidx := uint32(i)
		capped := cache.trigramCapped
		fillEntryTrigrams(
			cache.slugForEntry(i), idx32, uidx,
			lastSeen, capped, writeCursors, cache.trigramPostings,
		)
		if sec := cache.secSlugForEntry(i); len(sec) > 0 {
			fillEntryTrigrams(
				sec, idx32, uidx,
				lastSeen, capped, writeCursors, cache.trigramPostings,
			)
		}
	}
}

func countEntryTrigrams(data []byte, entryIdx int32, lastSeen []int32, counts []uint32) {
	for i := 0; i <= len(data)-3; i++ {
		id, ok := encodeTrigramID(data[i], data[i+1], data[i+2])
		if !ok {
			continue
		}
		if lastSeen[id] != entryIdx {
			lastSeen[id] = entryIdx
			counts[id]++
		}
	}
}

func fillEntryTrigrams(
	data []byte, entryIdx int32, entryUIdx uint32,
	lastSeen []int32, capped []bool, cursors []uint32, postings []uint32,
) {
	for i := 0; i <= len(data)-3; i++ {
		id, ok := encodeTrigramID(data[i], data[i+1], data[i+2])
		if !ok {
			continue
		}
		if capped[id] {
			continue
		}
		if lastSeen[id] != entryIdx {
			lastSeen[id] = entryIdx
			postings[cursors[id]] = entryUIdx
			cursors[id]++
		}
	}
}

// postingList returns the sorted entry indices for the given trigram ID.
func (c *SlugSearchCache) postingList(trigramID uint32) []uint32 {
	start := c.trigramOffsets[trigramID]
	end := c.trigramOffsets[trigramID+1]
	return c.trigramPostings[start:end]
}

// extractQueryTrigrams returns the unique trigram IDs in data.
func extractQueryTrigrams(data []byte) []uint32 {
	if len(data) < 3 {
		return nil
	}
	seen := make(map[uint32]struct{}, len(data))
	trigrams := make([]uint32, 0, len(data)-2)
	for i := 0; i <= len(data)-3; i++ {
		id, ok := encodeTrigramID(data[i], data[i+1], data[i+2])
		if !ok {
			continue
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			trigrams = append(trigrams, id)
		}
	}
	return trigrams
}

// ---------- Set operations on sorted uint32 slices ----------

// sortedIntersection returns elements present in both sorted slices.
// Reuses a's backing array when possible.
func sortedIntersection(a, b []uint32) []uint32 {
	result := a[:0]
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			result = append(result, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return result
}

// sortedUnion returns all unique elements from both sorted slices.
func sortedUnion(a, b []uint32) []uint32 {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	result := make([]uint32, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			result = append(result, a[i])
			i++
			j++
		case a[i] < b[j]:
			result = append(result, a[i])
			i++
		default:
			result = append(result, b[j])
			j++
		}
	}
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}

// ---------- Search ----------

// Search finds all title DBIDs matching the given system filter and variant groups.
// systemDBIDs is the system filter (empty = no filter). variantGroups is AND-of-ORs:
// each group is a set of byte variants for one query word (OR'd together),
// and all groups must match (AND'd together).
func (c *SlugSearchCache) Search(systemDBIDs []int64, variantGroups [][][]byte) []int64 {
	if c == nil || c.entryCount == 0 {
		return nil
	}

	// Empty variant groups matches all entries.
	if len(variantGroups) == 0 {
		return c.collectEntries(systemDBIDs)
	}

	// System-filtered path: scan only the contiguous ranges for each system.
	if len(systemDBIDs) > 0 && c.systemRanges != nil {
		candidates := make([]int64, 0, min(c.entryCount, 1024))
		for _, sysID := range systemDBIDs {
			r, ok := c.systemRanges[sysID]
			if !ok {
				continue
			}
			for i := r[0]; i < r[1]; i++ {
				if c.matchesVariantGroups(i, variantGroups) {
					candidates = append(candidates, c.titleDBIDs[i])
				}
			}
		}
		return candidates
	}

	// Unfiltered path: use trigram index if available.
	if len(c.trigramPostings) > 0 {
		return c.trigramSearch(variantGroups)
	}

	// Fallback: linear scan.
	return c.linearSearch(variantGroups)
}

// trigramSearch uses the trigram inverted index to narrow candidates before
// verifying with bytes.Contains. Groups whose variants are all >= 3 bytes
// produce trigram candidate sets; groups with short variants are verified in
// the final pass.
func (c *SlugSearchCache) trigramSearch(variantGroups [][][]byte) []int64 {
	var candidates []uint32
	hasTrigramGroup := false

	for _, group := range variantGroups {
		groupCandidates := c.trigramCandidatesForGroup(group)
		if groupCandidates == nil {
			// Group contains a short variant — can't filter, verify later.
			continue
		}
		if !hasTrigramGroup {
			candidates = groupCandidates
			hasTrigramGroup = true
		} else {
			candidates = sortedIntersection(candidates, groupCandidates)
		}
		if hasTrigramGroup && len(candidates) == 0 {
			return nil
		}
	}

	if !hasTrigramGroup {
		return c.linearSearch(variantGroups)
	}

	result := make([]int64, 0, len(candidates))
	for _, idx := range candidates {
		if c.matchesVariantGroups(int(idx), variantGroups) {
			result = append(result, c.titleDBIDs[idx])
		}
	}
	return result
}

// trigramCandidatesForGroup returns the union of trigram candidates across all
// variants in the group, or nil if any variant is too short for trigrams.
func (c *SlugSearchCache) trigramCandidatesForGroup(group [][]byte) []uint32 {
	var groupCandidates []uint32
	first := true

	for _, variant := range group {
		varCandidates := c.trigramCandidatesForVariant(variant)
		if varCandidates == nil {
			// Short variant in an OR group means we can't narrow candidates.
			return nil
		}
		if first {
			groupCandidates = varCandidates
			first = false
		} else {
			groupCandidates = sortedUnion(groupCandidates, varCandidates)
		}
	}

	if first {
		return nil
	}
	return groupCandidates
}

// trigramCandidatesForVariant intersects posting lists for the rarest trigrams
// in the variant to produce a sorted candidate set.
func (c *SlugSearchCache) trigramCandidatesForVariant(variant []byte) []uint32 {
	trigrams := extractQueryTrigrams(variant)
	if len(trigrams) == 0 {
		return nil
	}

	// Filter out frequency-capped trigrams (too common to be useful).
	filtered := trigrams[:0]
	for _, t := range trigrams {
		if !c.trigramCapped[t] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		return nil // all trigrams too common — falls back to linear scan via nil propagation
	}

	// Sort by posting list size (rarest first) for efficient intersection.
	slices.SortFunc(filtered, func(a, b uint32) int {
		sizeA := c.trigramOffsets[a+1] - c.trigramOffsets[a]
		sizeB := c.trigramOffsets[b+1] - c.trigramOffsets[b]
		return cmp.Compare(sizeA, sizeB)
	})

	limit := min(len(filtered), trigramMaxIntersect)
	result := slices.Clone(c.postingList(filtered[0]))
	if len(result) == 0 {
		return result // empty non-nil = no candidates (distinct from nil = can't filter)
	}
	for _, t := range filtered[1:limit] {
		result = sortedIntersection(result, c.postingList(t))
		if len(result) == 0 {
			return result
		}
	}
	return result
}

// linearSearch does a full scan of all entries, matching variant groups.
func (c *SlugSearchCache) linearSearch(variantGroups [][][]byte) []int64 {
	candidates := make([]int64, 0, min(c.entryCount, 1024))
	for i := range c.entryCount {
		if c.matchesVariantGroups(i, variantGroups) {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	}
	return candidates
}

// matchesVariantGroups checks if entry i matches the AND-of-ORs pattern.
func (c *SlugSearchCache) matchesVariantGroups(i int, variantGroups [][][]byte) bool {
	slug := c.slugForEntry(i)
	secSlug := c.secSlugForEntry(i)
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
			return false
		}
	}
	return true
}

// collectEntries returns all titleDBIDs matching the system filter (or all if
// no filter). Used when variantGroups is empty.
func (c *SlugSearchCache) collectEntries(systemDBIDs []int64) []int64 {
	if len(systemDBIDs) > 0 && c.systemRanges != nil {
		var result []int64
		for _, sysID := range systemDBIDs {
			r, ok := c.systemRanges[sysID]
			if !ok {
				continue
			}
			result = append(result, c.titleDBIDs[r[0]:r[1]]...)
		}
		return result
	}
	result := make([]int64, c.entryCount)
	copy(result, c.titleDBIDs)
	return result
}

// ---------- Exact / Prefix / Any / Random ----------

// ExactSlugMatch returns title DBIDs where the slug exactly matches the given bytes.
func (c *SlugSearchCache) ExactSlugMatch(systemDBIDs []int64, slug []byte) []int64 {
	if c == nil || c.entryCount == 0 {
		return nil
	}
	var candidates []int64
	c.iterateEntries(systemDBIDs, func(i int) {
		if bytes.Equal(c.slugForEntry(i), slug) {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	})
	return candidates
}

// ExactSecondarySlugMatch returns title DBIDs where the secondary slug exactly matches the given bytes.
func (c *SlugSearchCache) ExactSecondarySlugMatch(systemDBIDs []int64, secSlug []byte) []int64 {
	if c == nil || c.entryCount == 0 {
		return nil
	}
	var candidates []int64
	c.iterateEntries(systemDBIDs, func(i int) {
		entrySecSlug := c.secSlugForEntry(i)
		if len(entrySecSlug) > 0 && bytes.Equal(entrySecSlug, secSlug) {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	})
	return candidates
}

// PrefixSlugMatch returns title DBIDs where the slug starts with the given prefix.
func (c *SlugSearchCache) PrefixSlugMatch(systemDBIDs []int64, prefix []byte) []int64 {
	if c == nil || c.entryCount == 0 {
		return nil
	}
	var candidates []int64
	c.iterateEntries(systemDBIDs, func(i int) {
		if bytes.HasPrefix(c.slugForEntry(i), prefix) {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	})
	return candidates
}

// ExactSlugMatchAny returns title DBIDs where the slug exactly matches any of the given slugs.
func (c *SlugSearchCache) ExactSlugMatchAny(systemDBIDs []int64, slugList [][]byte) []int64 {
	if c == nil || c.entryCount == 0 || len(slugList) == 0 {
		return nil
	}
	slugSet := make(map[string]struct{}, len(slugList))
	for _, s := range slugList {
		slugSet[string(s)] = struct{}{}
	}
	var candidates []int64
	c.iterateEntries(systemDBIDs, func(i int) {
		if _, ok := slugSet[string(c.slugForEntry(i))]; ok {
			candidates = append(candidates, c.titleDBIDs[i])
		}
	})
	return candidates
}

// RandomEntry picks a random title DBID from entries matching the system filter.
func (c *SlugSearchCache) RandomEntry(systemDBIDs []int64) (int64, bool) {
	if c == nil || c.entryCount == 0 {
		return 0, false
	}

	if len(systemDBIDs) > 0 && c.systemRanges != nil {
		type indexRange struct{ start, size int }
		ranges := make([]indexRange, 0, len(systemDBIDs))
		var count int
		for _, sysID := range systemDBIDs {
			r, ok := c.systemRanges[sysID]
			if !ok {
				continue
			}
			sz := r[1] - r[0]
			ranges = append(ranges, indexRange{r[0], sz})
			count += sz
		}
		if count == 0 {
			return 0, false
		}
		target, err := helpers.RandomInt(count)
		if err != nil {
			return 0, false
		}
		for _, rng := range ranges {
			if target < rng.size {
				return c.titleDBIDs[rng.start+target], true
			}
			target -= rng.size
		}
		return 0, false
	}

	// No filter: direct index.
	target, err := helpers.RandomInt(c.entryCount)
	if err != nil {
		return 0, false
	}
	return c.titleDBIDs[target], true
}

// ---------- Iteration helpers ----------

// iterateEntries calls fn for each entry matching the system filter.
// Uses system ranges when available for O(subset) instead of O(n).
func (c *SlugSearchCache) iterateEntries(systemDBIDs []int64, fn func(i int)) {
	if len(systemDBIDs) > 0 && c.systemRanges != nil {
		for _, sysID := range systemDBIDs {
			r, ok := c.systemRanges[sysID]
			if !ok {
				continue
			}
			for i := r[0]; i < r[1]; i++ {
				fn(i)
			}
		}
		return
	}
	for i := range c.entryCount {
		fn(i)
	}
}

// ---------- Entry accessors ----------

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

// ---------- Utility ----------

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

// TrigramIndexSize returns the memory footprint of the trigram index in bytes.
func (c *SlugSearchCache) TrigramIndexSize() int {
	if c == nil {
		return 0
	}
	return len(c.trigramOffsets)*4 + len(c.trigramPostings)*4
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
		Int("systems", len(cache.systemRanges)).
		Str("size", formatBytes(cache.Size())).
		Str("trigramIndex", formatBytes(cache.TrigramIndexSize())).
		Msg("slug search cache built")
	return nil
}

func (db *MediaDB) RefreshSlugSearchCacheForSystems(ctx context.Context, systemIDs []string) error {
	fragment, err := buildSlugSearchCacheForSystems(ctx, db.sql, systemIDs)
	if err != nil {
		return fmt.Errorf("failed to build selective slug search cache: %w", err)
	}

	current := db.slugSearchCache.Load()
	refreshed := mergeSlugSearchCaches(current, fragment)
	db.slugSearchCache.Store(refreshed)

	log.Info().
		Int("entries", refreshed.entryCount).
		Int("systems", len(fragment.coveredSystems)).
		Bool("complete", refreshed.complete).
		Msg("slug search cache refreshed for systems")
	return nil
}
